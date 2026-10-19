package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var ErrUsageBillingRequestIDRequired = errors.New("usage billing request_id is required")
var ErrUsageBillingRequestConflict = errors.New("usage billing request fingerprint conflict")

// UsageBillingCommand describes one billable request that must be applied at most once.
type UsageBillingCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string

	UserID              int64
	AccountID           int64
	SubscriptionID      *int64
	AccountType         string
	Model               string
	ServiceTier         string
	ReasoningEffort     string
	BillingType         int8
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	ImageCount          int
	MediaType           string

	BalanceCost         float64
	SubscriptionCost    float64
	APIKeyQuotaCost     float64
	APIKeyRateLimitCost float64
	AccountQuotaCost    float64
	// UserPlatformQuotaCost is persisted by the unified billing transaction.
	// Keeping it on the receipt lets asynchronous media failures reverse every
	// ledger touched by the original charge, not only the user's balance.
	UserPlatformQuotaCost     float64
	Platform                  string
	ChargedAt                 time.Time
	PlatformDailyWindowStart  time.Time
	PlatformWeeklyWindowStart time.Time
}

// NormalizeMonetaryFields rounds every billable component to the database
// accounting scale. The normalized values are reused by balance, quota,
// cache, notifications, and usage-log persistence so all ledgers agree.
func (c *CostBreakdown) NormalizeMonetaryFields() {
	if c == nil {
		return
	}
	c.InputCost = QuantizeUsageBillingAmount(c.InputCost)
	c.ImageInputCost = QuantizeUsageBillingAmount(c.ImageInputCost)
	c.OutputCost = QuantizeUsageBillingAmount(c.OutputCost)
	c.ImageOutputCost = QuantizeUsageBillingAmount(c.ImageOutputCost)
	c.CacheCreationCost = QuantizeUsageBillingAmount(c.CacheCreationCost)
	c.CacheReadCost = QuantizeUsageBillingAmount(c.CacheReadCost)
	c.TotalCost = QuantizeUsageBillingAmount(c.TotalCost)
	c.ActualCost = QuantizeUsageBillingAmount(c.ActualCost)
}

func (c *UsageBillingCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildUsageBillingFingerprint(c)
	}
	// 量化必须在指纹计算之后：指纹是请求幂等键，保持由原始金额派生可以避免
	// 升级前后同一 request_id 的重试算出不同指纹而被判为 fingerprint conflict。
	c.quantizeMonetaryFields()
}

// UsageBillingMonetaryScale 是所有计费金额的规范小数位数，
// 对齐 users.balance / api_keys.quota_used 的 NUMERIC(20,8)。
const UsageBillingMonetaryScale = 8

// quantizeMonetaryFields 把命令中的金额统一量化到 NUMERIC(20,8)。
//
// 不量化时，同一笔 ActualCost 会在两条方向相反的 SQL 上被 PostgreSQL 分别舍入：
//
//	balance    = balance - $1      // 存剩余额度，舍入的是「减法结果」
//	quota_used = quota_used + $1   // 存累计用量，舍入的是「加法结果」
//
// PostgreSQL 对 NUMERIC 采用 half-away-from-zero。当金额在第 9 位出现 half 边界
// （例：10 输入 token × 0.00000125 + 5 输出 token × 0.00001000，再乘分组倍率
// 1.25 = 0.000078125）时：
//
//	balance:    10000 - 0.000078125 = 9999.999921875 → 9999.99992188（delta 0.00007812）
//	quota_used:     0 + 0.000078125 =     0.000078125 →     0.00007813（delta 0.00007813）
//
// 两个 delta 相差 1e-8，且方向相反——余额少扣、Key 配额多记，随请求量线性累积，
// 使余额、API Key 配额与用量记录无法精确对账（需要 epsilon 比较才能勉强吻合）。
//
// 在参数进入 SQL 之前量化一次，两条语句就都拿到已经落在 8 位刻度上的同一个金额，
// 存储阶段不再发生任何舍入，delta 精确相等。
func (c *UsageBillingCommand) quantizeMonetaryFields() {
	c.BalanceCost = QuantizeUsageBillingAmount(c.BalanceCost)
	c.SubscriptionCost = QuantizeUsageBillingAmount(c.SubscriptionCost)
	c.APIKeyQuotaCost = QuantizeUsageBillingAmount(c.APIKeyQuotaCost)
	c.APIKeyRateLimitCost = QuantizeUsageBillingAmount(c.APIKeyRateLimitCost)
	c.AccountQuotaCost = QuantizeUsageBillingAmount(c.AccountQuotaCost)
	c.UserPlatformQuotaCost = QuantizeUsageBillingAmount(c.UserPlatformQuotaCost)
}

// QuantizeUsageBillingAmount 把金额舍入到 UsageBillingMonetaryScale 位小数，
// 采用与 PostgreSQL NUMERIC 一致的 half-away-from-zero 规则。
//
// 走 decimal 而不是 math.Round(v*1e8)/1e8：后者在乘除过程中会引入额外的二进制
// 误差，边界值可能被推到错误的一侧。decimal.NewFromFloat 取 float64 的最短十进制
// 表示，正是 PostgreSQL 把 float8 参数转成 numeric 时所用的表示。
func QuantizeUsageBillingAmount(v float64) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	quantized, _ := decimal.NewFromFloat(v).Round(UsageBillingMonetaryScale).Float64()
	return quantized
}

func buildUsageBillingFingerprint(c *UsageBillingCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%s|%d|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f",
		c.UserID,
		c.AccountID,
		c.APIKeyID,
		strings.TrimSpace(c.AccountType),
		strings.TrimSpace(c.Model),
		strings.TrimSpace(c.ServiceTier),
		strings.TrimSpace(c.ReasoningEffort),
		c.BillingType,
		c.InputTokens,
		c.OutputTokens,
		c.CacheCreationTokens,
		c.CacheReadTokens,
		c.ImageCount,
		strings.TrimSpace(c.MediaType),
		valueOrZero(c.SubscriptionID),
		c.BalanceCost,
		c.SubscriptionCost,
		c.APIKeyQuotaCost,
		c.APIKeyRateLimitCost,
		c.AccountQuotaCost,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	// Keep the legacy fingerprint byte-for-byte stable for ordinary requests.
	// Platform quota metadata is appended only when that ledger is actually
	// updated, avoiding rolling-upgrade conflicts for existing idempotency keys.
	if c.UserPlatformQuotaCost > 0 {
		raw += fmt.Sprintf("|platform:%s|platform_cost:%0.10f", strings.TrimSpace(c.Platform), c.UserPlatformQuotaCost)
		// Async video receipts are later presented from the task ledger to
		// select the exact quota windows being reversed. Bind those mutable
		// selectors into the persisted charge fingerprint. Ordinary request
		// fingerprints remain byte-for-byte compatible across rolling deploys.
		if strings.HasPrefix(strings.TrimSpace(c.RequestID), "video:") {
			raw += "|charged_at:" + c.ChargedAt.UTC().Format(time.RFC3339Nano)
			raw += "|platform_day:" + c.PlatformDailyWindowStart.UTC().Format(time.RFC3339Nano)
			raw += "|platform_week:" + c.PlatformWeeklyWindowStart.UTC().Format(time.RFC3339Nano)
		}
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (c *UsageBillingCommand) HasValidFingerprint() bool {
	if c == nil || strings.TrimSpace(c.RequestFingerprint) == "" {
		return false
	}
	want := strings.TrimSpace(c.RequestFingerprint)
	copyCommand := *c
	copyCommand.RequestFingerprint = ""
	copyCommand.Normalize()
	return copyCommand.RequestFingerprint == want
}

// UsageBillingReversalCommand describes an idempotent compensation for a
// previously applied charge. Original carries the exact normalized effects
// returned by the billing path, so settings changed after task creation cannot
// alter the refund amount.
type UsageBillingReversalCommand struct {
	ReversalRequestID string
	Original          UsageBillingCommand
}

func (c *UsageBillingReversalCommand) Normalize() {
	if c == nil {
		return
	}
	c.ReversalRequestID = strings.TrimSpace(c.ReversalRequestID)
	c.Original.Normalize()
}

type UsageBillingReversalResult struct {
	Applied    bool
	NewBalance *float64
}

func UsageBillingReversalFingerprint(c *UsageBillingReversalCommand) string {
	if c == nil {
		return ""
	}
	original := c.Original
	original.Normalize()
	raw := "reverse|" + original.RequestID + "|" + original.RequestFingerprint
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// UsageBillingReversalRepository is optional so existing test doubles and
// alternate repositories remain source compatible. Production's unified
// repository implements it.
type UsageBillingReversalRepository interface {
	Reverse(ctx context.Context, cmd *UsageBillingReversalCommand) (*UsageBillingReversalResult, error)
}

func HashUsageRequestPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// AccountQuotaState holds the post-increment quota state returned by the DB transaction.
// All values are post-update (i.e., already include the increment).
type AccountQuotaState struct {
	TotalUsed   float64
	TotalLimit  float64
	DailyUsed   float64
	DailyLimit  float64
	WeeklyUsed  float64
	WeeklyLimit float64
}

type UsageBillingApplyResult struct {
	Applied                  bool
	APIKeyQuotaExhausted     bool
	UserPlatformQuotaApplied bool
	NewBalance               *float64           // post-deduction balance (nil = no balance deduction)
	BalanceOverdrafted       bool               // true when the sufficient-balance guard missed and debt was still recorded
	BalanceReserved          bool               // token reservation already moved the available balance; invalidate cache instead of deducting twice
	OutstandingBalanceCost   float64            // auditable over-maximum fee, paid before new token admission
	QuotaState               *AccountQuotaState // post-increment quota state (nil = no quota increment)
}

// BatchImageBalanceHoldCommand describes an idempotent balance hold operation.
type BatchImageBalanceHoldCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string
	UserID             int64
	BatchID            string
	HoldAmount         float64
	ActualAmount       float64
}

func (c *BatchImageBalanceHoldCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	c.BatchID = strings.TrimSpace(c.BatchID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildBatchImageBalanceHoldFingerprint(c)
	}
	c.HoldAmount = QuantizeUsageBillingAmount(c.HoldAmount)
	c.ActualAmount = QuantizeUsageBillingAmount(c.ActualAmount)
}

func buildBatchImageBalanceHoldFingerprint(c *BatchImageBalanceHoldCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%s|%0.10f|%0.10f",
		c.UserID,
		c.APIKeyID,
		strings.TrimSpace(c.BatchID),
		c.HoldAmount,
		c.ActualAmount,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type BatchImageBalanceHoldResult struct {
	Applied       bool
	NewBalance    *float64
	FrozenBalance *float64
}

type UsageBillingRepository interface {
	Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error)
	ReserveBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	CaptureBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	ReleaseBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
}
