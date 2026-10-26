package admin

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const upstreamBalanceExtraKey = "upstream_balance_usd"
const upstreamBalanceUpdatedAtExtraKey = "upstream_balance_updated_at"

type upstreamBalanceSnapshot struct {
	AccountID  int64    `json:"account_id"`
	BalanceUSD *float64 `json:"balance_usd"`
	UpdatedAt  string   `json:"updated_at,omitempty"`
}

type upstreamBalanceSummary struct {
	TotalUSD     float64 `json:"total_usd"`
	KnownCount   int     `json:"known_count"`
	UnknownCount int     `json:"unknown_count"`
}

// accountUpstreamBalance returns nil for an unknown balance. Zero is a known,
// valid balance and must not be confused with an account that was never checked.
func accountUpstreamBalance(account *service.Account) upstreamBalanceSnapshot {
	snapshot := upstreamBalanceSnapshot{AccountID: account.ID}
	if account.Extra == nil {
		return snapshot
	}
	var balance float64
	switch value := account.Extra[upstreamBalanceExtraKey].(type) {
	case float64:
		balance = value
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return snapshot
		}
		balance = parsed
	case int:
		balance = float64(value)
	default:
		return snapshot
	}
	if math.IsNaN(balance) || math.IsInf(balance, 0) || balance < 0 || balance > 1e12 {
		return snapshot
	}
	snapshot.BalanceUSD = &balance
	snapshot.UpdatedAt, _ = account.Extra[upstreamBalanceUpdatedAtExtraKey].(string)
	return snapshot
}

// GetUpstreamBalanceSummary aggregates known USD balances across all real
// upstream accounts, rather than only the visible page of ten.
// GET /api/v1/admin/accounts/upstream-balances
func (h *AccountHandler) GetUpstreamBalanceSummary(c *gin.Context) {
	accounts, err := h.adminService.ListAccountsForSchedulerScoreFilter(c.Request.Context(), "", "", "", "", 0, "")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := upstreamBalanceSummary{}
	for index := range accounts {
		account := &accounts[index]
		if account.ParentAccountID != nil {
			continue // A shadow account shares its parent's upstream balance.
		}
		balance := accountUpstreamBalance(account)
		if balance.BalanceUSD == nil {
			result.UnknownCount++
			continue
		}
		result.KnownCount++
		result.TotalUSD += *balance.BalanceUSD
	}
	response.Success(c, result)
}

// SetUpstreamBalance records an explicitly supplied balance for providers that
// do not expose a standard balance API. The UI must label this as a manual
// snapshot, never as a live probe.
// PUT /api/v1/admin/accounts/:id/upstream-balance
func (h *AccountHandler) SetUpstreamBalance(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var input struct {
		BalanceUSD json.RawMessage `json:"balance_usd"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || len(input.BalanceUSD) == 0 {
		response.BadRequest(c, "balance_usd is required")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	if account.ParentAccountID != nil {
		response.BadRequest(c, "Shadow accounts share the parent account balance")
		return
	}
	var balance *float64
	if strings.TrimSpace(string(input.BalanceUSD)) != "null" {
		var value float64
		if err := json.Unmarshal(input.BalanceUSD, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1e12 {
			response.BadRequest(c, "balance_usd must be a finite amount between 0 and 1000000000000, or null")
			return
		}
		balance = &value
	}
	updates := map[string]any{upstreamBalanceExtraKey: balance, upstreamBalanceUpdatedAtExtraKey: nil}
	if balance != nil {
		updates[upstreamBalanceUpdatedAtExtraKey] = time.Now().UTC().Format(time.RFC3339)
	}
	if err := h.adminService.UpdateAccountExtra(c.Request.Context(), accountID, updates); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	updated := *account
	updated.Extra = make(map[string]any, len(account.Extra)+2)
	for key, value := range account.Extra {
		updated.Extra[key] = value
	}
	for key, value := range updates {
		updated.Extra[key] = value
	}
	response.Success(c, accountUpstreamBalance(&updated))
}
