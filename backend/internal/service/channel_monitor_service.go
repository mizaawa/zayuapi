package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// ChannelMonitorRepository 渠道监控数据访问接口。
// 入参/返回的指针类型均使用 service 包的 ChannelMonitor 模型，
// repository 实现负责与 ent 模型互转，并保持 api_key_encrypted 字段为密文。
type ChannelMonitorRepository interface {
	// CRUD
	Create(ctx context.Context, m *ChannelMonitor) error
	GetByID(ctx context.Context, id int64) (*ChannelMonitor, error)
	Update(ctx context.Context, m *ChannelMonitor) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitor, int64, error)
	UpdateSortOrders(ctx context.Context, updates []ChannelMonitorSortOrderUpdate) error
	FindByDuplicateOperationID(ctx context.Context, operationID string) (*ChannelMonitor, error)

	// 调度器辅助
	ListEnabled(ctx context.Context) ([]*ChannelMonitor, error)
	MarkChecked(ctx context.Context, id int64, checkedAt time.Time) error
	InsertHistoryBatch(ctx context.Context, rows []*ChannelMonitorHistoryRow) error
	DeleteHistoryBefore(ctx context.Context, before time.Time) (int64, error)

	// 历史记录
	ListHistory(ctx context.Context, monitorID int64, model string, limit int) ([]*ChannelMonitorHistoryEntry, error)

	// 用户视图聚合
	ListLatestPerModel(ctx context.Context, monitorID int64) ([]*ChannelMonitorLatest, error)
	ComputeAvailability(ctx context.Context, monitorID int64, windowDays int) ([]*ChannelMonitorAvailability, error)

	// 批量聚合（admin/user list 用，避免 N+1）
	ListLatestForMonitorIDs(ctx context.Context, ids []int64) (map[int64][]*ChannelMonitorLatest, error)
	ComputeAvailabilityForMonitors(ctx context.Context, ids []int64, windowDays int) (map[int64][]*ChannelMonitorAvailability, error)
	// ListRecentHistoryForMonitors 批量取多个 monitor 各自主模型（primaryModels[monitorID]）最近 perMonitorLimit 条历史。
	// 返回的 entry 已按 checked_at DESC 排序（最新在前），不含 message 字段。
	ListRecentHistoryForMonitors(ctx context.Context, ids []int64, primaryModels map[int64]string, perMonitorLimit int) (map[int64][]*ChannelMonitorHistoryEntry, error)

	// ---------- 聚合维护（OpsCleanupService 调用） ----------

	// UpsertDailyRollupsFor 把 targetDate 当天的明细按 (monitor_id, model, bucket_date)
	// 聚合到 channel_monitor_daily_rollups。targetDate 会被截断到日期；
	// 用 ON CONFLICT DO UPDATE 实现幂等回填，返回 upsert 影响的行数。
	UpsertDailyRollupsFor(ctx context.Context, targetDate time.Time) (int64, error)
	// DeleteRollupsBefore 软删 bucket_date < beforeDate 的聚合行，返回删除行数。
	DeleteRollupsBefore(ctx context.Context, beforeDate time.Time) (int64, error)
	// LoadAggregationWatermark 读 watermark（id=1）。
	// 返回 nil 表示从未聚合过；watermark 表本身预期已存在单行（migration 110 写入）。
	LoadAggregationWatermark(ctx context.Context) (*time.Time, error)
	// UpdateAggregationWatermark 写 watermark（UPSERT 到 id=1）。
	UpdateAggregationWatermark(ctx context.Context, date time.Time) error
}

// ChannelMonitorGroupReader is the minimal group dependency needed to validate
// monitor configuration without coupling tests to the full GroupRepository.
type ChannelMonitorGroupReader interface {
	GetByIDLite(ctx context.Context, id int64) (*Group, error)
}

// ChannelMonitorAPIKeyManager owns the internal keys used by group-based
// monitors. APIKeyService is the production implementation.
type ChannelMonitorAPIKeyManager interface {
	CreateChannelMonitorKey(ctx context.Context, userID, groupID int64, monitorName string) (*APIKey, error)
	DeleteChannelMonitorKey(ctx context.Context, rawKey string, userID int64) error
}

// ChannelMonitorService 渠道监控管理服务。
type ChannelMonitorService struct {
	repo                   ChannelMonitorRepository
	encryptor              SecretEncryptor
	groupReader            ChannelMonitorGroupReader
	apiKeyManager          ChannelMonitorAPIKeyManager
	managedGatewayEndpoint string
	managedGatewayAttestor *ChannelMonitorAttestor
	// scheduler 由 wire 通过 SetScheduler 注入；CRUD 后调用对应钩子即时同步任务。
	// 测试或未注入场景下保持 nil，所有钩子调用变为 no-op。
	scheduler MonitorScheduler
}

const maxChannelMonitorNameRunes = 100

// ChannelMonitorDuplicateOperationIDMetadataKey is stored in the existing
// extra_headers JSON column to avoid a schema migration. The colon makes it an
// invalid HTTP header name, and repository adapters remove it before exposing
// ExtraHeaders to the service layer.
const ChannelMonitorDuplicateOperationIDMetadataKey = "sub2api:duplicate_operation_id"

// NewChannelMonitorService 创建渠道监控服务实例。
func NewChannelMonitorService(repo ChannelMonitorRepository, encryptor SecretEncryptor) *ChannelMonitorService {
	return &ChannelMonitorService{repo: repo, encryptor: encryptor}
}

// SetGroupDependencies enables the group-based monitor flow. It remains a
// setter so existing focused unit tests can construct the legacy service with
// only a repository and encryptor.
func (s *ChannelMonitorService) SetGroupDependencies(groupReader ChannelMonitorGroupReader, apiKeyManager ChannelMonitorAPIKeyManager) {
	s.groupReader = groupReader
	s.apiKeyManager = apiKeyManager
}

// SetManagedGatewayEndpoint pins group-based checks to this deployment's own
// gateway. It is injected from trusted server configuration, never request
// input, so generated credentials cannot be exfiltrated to another host.
func (s *ChannelMonitorService) SetManagedGatewayEndpoint(endpoint string) {
	s.managedGatewayEndpoint = normalizeEndpoint(endpoint)
}

// SetManagedGatewayAttestor injects the signer used for internal gateway
// probes. Managed checks fail closed when no signer is available.
func (s *ChannelMonitorService) SetManagedGatewayAttestor(attestor *ChannelMonitorAttestor) {
	s.managedGatewayAttestor = attestor
}

func (s *ChannelMonitorService) requireManagedGateway() error {
	if s == nil || strings.TrimSpace(s.managedGatewayEndpoint) == "" || s.managedGatewayAttestor == nil {
		return ErrChannelMonitorManagedGatewayUnavailable
	}
	return nil
}

// ---------- CRUD ----------

// List 列表查询（支持 provider/enabled/search 过滤 + 分页）。
// 管理读取永不返回密钥（明文或密文）；这里只验证密文是否仍可解密，
// 供 UI 展示修复提示。只有 getForExecution 会把明文交给检测器。
func (s *ChannelMonitorService) List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitor, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		params.PageSize = 20
	}
	items, total, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list channel monitors: %w", err)
	}
	for _, it := range items {
		s.redactAPIKeyForRead(it)
	}
	return items, total, nil
}

// UpdateSortOrders 批量更新渠道监控排序。
func (s *ChannelMonitorService) UpdateSortOrders(ctx context.Context, updates []ChannelMonitorSortOrderUpdate) error {
	if err := s.repo.UpdateSortOrders(ctx, updates); err != nil {
		return fmt.Errorf("update channel monitor sort orders: %w", err)
	}
	return nil
}

// Get 查询单个监控。返回对象不携带密钥（明文或密文）。
func (s *ChannelMonitorService) Get(ctx context.Context, id int64) (*ChannelMonitor, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.redactAPIKeyForRead(m)
	return m, nil
}

// Create creates a monitor. Group-based monitors receive a dedicated internal
// API key automatically; legacy callers that still provide an API key remain
// supported during the migration window.
func (s *ChannelMonitorService) Create(ctx context.Context, p ChannelMonitorCreateParams) (*ChannelMonitor, error) {
	managedGateway := p.GroupID > 0
	if managedGateway {
		if err := s.requireManagedGateway(); err != nil {
			return nil, err
		}
		p.Endpoint = s.managedGatewayEndpoint
	}
	if err := validateCreateParams(p, managedGateway); err != nil {
		return nil, err
	}
	if err := validateBodyModeForProtocol(p.Provider, p.APIMode, p.BodyOverrideMode, p.BodyOverride); err != nil {
		return nil, err
	}
	if err := validateExtraHeaders(p.ExtraHeaders); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(p.Name)
	var group *Group
	plainAPIKey := strings.TrimSpace(p.APIKey)
	managedKeyCreated := false
	if p.GroupID > 0 {
		var err error
		group, err = s.resolveMonitorGroup(ctx, p.GroupID, p.Provider)
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = group.Name
		}
		if s.apiKeyManager == nil {
			return nil, fmt.Errorf("channel monitor api key manager is not configured")
		}
		managedKey, err := s.apiKeyManager.CreateChannelMonitorKey(ctx, p.CreatedBy, p.GroupID, name)
		if err != nil {
			return nil, fmt.Errorf("create managed channel monitor key: %w", err)
		}
		plainAPIKey = managedKey.Key
		managedKeyCreated = true
	}
	if s.encryptor == nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, plainAPIKey, p.CreatedBy)
		}
		return nil, fmt.Errorf("channel monitor secret encryptor is not configured")
	}
	encrypted, err := s.encryptor.Encrypt(plainAPIKey)
	if err != nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, plainAPIKey, p.CreatedBy)
		}
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}
	m := &ChannelMonitor{
		Name:             name,
		Provider:         p.Provider,
		APIMode:          defaultAPIMode(p.APIMode),
		Endpoint:         normalizeEndpoint(p.Endpoint),
		APIKey:           encrypted, // 注意：传入 repository 时该字段为密文
		PrimaryModel:     normalizeMonitorPrimaryModel(p.Provider, p.PrimaryModel),
		ExtraModels:      normalizeModels(p.ExtraModels),
		GroupName:        strings.TrimSpace(p.GroupName),
		Enabled:          p.Enabled,
		SimulateRequests: p.SimulateRequests,
		IntervalSeconds:  p.IntervalSeconds,
		JitterSeconds:    p.JitterSeconds,
		CreatedBy:        p.CreatedBy,
		TemplateID:       p.TemplateID,
		ExtraHeaders:     emptyHeadersIfNil(p.ExtraHeaders),
		BodyOverrideMode: defaultBodyMode(p.BodyOverrideMode),
		BodyOverride:     p.BodyOverride,
	}
	if group != nil {
		groupID := group.ID
		m.GroupID = &groupID
		m.GroupName = group.Name
		m.GroupRateMultiplier = group.RateMultiplier
		m.GroupPlatform = group.Platform
	}
	if err := s.repo.Create(ctx, m); err != nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, plainAPIKey, p.CreatedBy)
		}
		return nil, fmt.Errorf("create channel monitor: %w", err)
	}
	// API key is runtime-only secret material. CRUD responses and scheduler
	// metadata must never carry either its plaintext or ciphertext.
	m.APIKey = ""
	if s.scheduler != nil {
		s.scheduler.Schedule(m)
	}
	return m, nil
}

// Duplicate creates an independent, disabled copy of an existing monitor.
// The API key stays server-side: it is decrypted only long enough to encrypt a
// fresh ciphertext for the new row. Runtime state and history are not copied.
func (s *ChannelMonitorService) Duplicate(
	ctx context.Context,
	id, createdBy int64,
	actorScope, operationKey string,
) (*ChannelMonitor, error) {
	operationID := duplicateChannelMonitorOperationID(id, actorScope, operationKey)
	existing, err := s.RecoverDuplicate(ctx, id, actorScope, operationKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	source, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	duplicateName := duplicateChannelMonitorName(source.Name)
	managedKeyCreated := false
	var plainAPIKey string
	var group *Group
	if source.GroupID != nil {
		if err := s.requireManagedGateway(); err != nil {
			return nil, err
		}
		group, err = s.resolveMonitorGroup(ctx, *source.GroupID, source.Provider)
		if err != nil {
			return nil, err
		}
		if s.apiKeyManager == nil {
			return nil, fmt.Errorf("channel monitor api key manager is not configured")
		}
		managedKey, err := s.apiKeyManager.CreateChannelMonitorKey(ctx, createdBy, *source.GroupID, duplicateName)
		if err != nil {
			return nil, fmt.Errorf("create duplicate channel monitor key: %w", err)
		}
		plainAPIKey = managedKey.Key
		managedKeyCreated = true
	} else {
		plainAPIKey, err = s.decryptAPIKeyForDuplicate(source)
		if err != nil {
			return nil, err
		}
	}
	if s.encryptor == nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, plainAPIKey, createdBy)
		}
		return nil, fmt.Errorf("channel monitor secret encryptor is not configured")
	}
	encryptedAPIKey, err := s.encryptor.Encrypt(plainAPIKey)
	if err != nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, plainAPIKey, createdBy)
		}
		return nil, fmt.Errorf("encrypt duplicate channel monitor api key: %w", err)
	}
	bodyOverride, err := cloneChannelMonitorJSONMap(source.BodyOverride)
	if err != nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, plainAPIKey, createdBy)
		}
		return nil, fmt.Errorf("clone duplicate channel monitor body override: %w", err)
	}

	duplicate := &ChannelMonitor{
		Name:                 duplicateName,
		Provider:             source.Provider,
		APIMode:              source.APIMode,
		Endpoint:             source.Endpoint,
		APIKey:               encryptedAPIKey,
		PrimaryModel:         source.PrimaryModel,
		ExtraModels:          append([]string{}, source.ExtraModels...),
		GroupName:            source.GroupName,
		SortOrder:            source.SortOrder,
		Enabled:              false,
		SimulateRequests:     source.SimulateRequests,
		IntervalSeconds:      source.IntervalSeconds,
		JitterSeconds:        source.JitterSeconds,
		CreatedBy:            createdBy,
		TemplateID:           cloneInt64Pointer(source.TemplateID),
		ExtraHeaders:         cloneChannelMonitorHeaders(source.ExtraHeaders),
		BodyOverrideMode:     source.BodyOverrideMode,
		BodyOverride:         bodyOverride,
		DuplicateOperationID: operationID,
	}
	if source.GroupID != nil {
		duplicate.Endpoint = s.managedGatewayEndpoint
	}
	if group != nil {
		groupID := group.ID
		duplicate.GroupID = &groupID
		duplicate.GroupName = group.Name
		duplicate.GroupRateMultiplier = group.RateMultiplier
		duplicate.GroupPlatform = group.Platform
	}
	if err := s.repo.Create(ctx, duplicate); err != nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, plainAPIKey, createdBy)
		}
		return nil, fmt.Errorf("duplicate channel monitor: %w", err)
	}

	// Keep the generated key server-side. The repository already received the
	// ciphertext and the scheduler only needs non-secret monitor metadata.
	duplicate.APIKey = ""
	return duplicate, nil
}

// RecoverDuplicate performs a read-only lookup for a duplicate that was
// already committed for the same actor, source monitor, and idempotency key.
// It deliberately never repeats the create side effect.
func (s *ChannelMonitorService) RecoverDuplicate(
	ctx context.Context,
	id int64,
	actorScope, operationKey string,
) (*ChannelMonitor, error) {
	operationID := duplicateChannelMonitorOperationID(id, actorScope, operationKey)
	if operationID == "" {
		return nil, nil
	}
	monitor, err := s.repo.FindByDuplicateOperationID(ctx, operationID)
	if err != nil {
		return nil, fmt.Errorf("find duplicate channel monitor operation: %w", err)
	}
	if monitor == nil {
		return nil, nil
	}
	s.redactAPIKeyForRead(monitor)
	return monitor, nil
}

func duplicateChannelMonitorOperationID(sourceID int64, actorScope, operationKey string) string {
	operationKey = strings.TrimSpace(operationKey)
	if operationKey == "" {
		return ""
	}
	actorScope = strings.TrimSpace(actorScope)
	if actorScope == "" {
		actorScope = "admin:0"
	}
	payload := "admin.channel_monitors.duplicate\x00" + actorScope + "\x00" + strconv.FormatInt(sourceID, 10) + "\x00" + operationKey
	digest := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", digest)
}

func (s *ChannelMonitorService) decryptAPIKeyForDuplicate(source *ChannelMonitor) (string, error) {
	if source == nil || strings.TrimSpace(source.APIKey) == "" {
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	if s.encryptor == nil {
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	plain, err := s.encryptor.Decrypt(source.APIKey)
	if err != nil || strings.TrimSpace(plain) == "" {
		slog.Warn("channel_monitor: decrypt api key for duplicate failed",
			"monitor_id", source.ID, "error", err)
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	return plain, nil
}

func duplicateChannelMonitorName(sourceName string) string {
	const suffix = " (Copy)"
	nameRunes := []rune(strings.TrimSpace(sourceName))
	maxBaseRunes := maxChannelMonitorNameRunes - len([]rune(suffix))
	if len(nameRunes) > maxBaseRunes {
		nameRunes = nameRunes[:maxBaseRunes]
	}
	return string(nameRunes) + suffix
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelMonitorHeaders(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneChannelMonitorJSONMap(source map[string]any) (map[string]any, error) {
	if source == nil {
		return nil, nil
	}
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	cloned := make(map[string]any, len(source))
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

// validateCreateParams 把 Create 入参的所有校验聚拢为一个函数，避免 Create 主体超过 30 行。
func validateCreateParams(p ChannelMonitorCreateParams, managedGateway bool) error {
	if len([]rune(strings.TrimSpace(p.Name))) > maxChannelMonitorNameRunes {
		return ErrChannelMonitorInvalidName
	}
	if err := validateProvider(p.Provider); err != nil {
		return err
	}
	if err := validateAPIMode(p.Provider, p.APIMode); err != nil {
		return err
	}
	if err := validateInterval(p.IntervalSeconds); err != nil {
		return err
	}
	if err := validateJitter(p.JitterSeconds, p.IntervalSeconds); err != nil {
		return err
	}
	if managedGateway {
		if strings.TrimSpace(p.Endpoint) == "" {
			return ErrChannelMonitorInvalidEndpoint
		}
	} else {
		if err := validateEndpoint(p.Endpoint); err != nil {
			return err
		}
	}
	if p.GroupID < 0 || (p.GroupID == 0 && strings.TrimSpace(p.APIKey) == "") {
		return ErrChannelMonitorMissingGroup
	}
	if normalizeMonitorPrimaryModel(p.Provider, p.PrimaryModel) == "" {
		return ErrChannelMonitorMissingPrimaryModel
	}
	return nil
}

func (s *ChannelMonitorService) resolveMonitorGroup(ctx context.Context, groupID int64, provider string) (*Group, error) {
	if groupID <= 0 {
		return nil, ErrChannelMonitorMissingGroup
	}
	if s.groupReader == nil {
		return nil, fmt.Errorf("channel monitor group reader is not configured")
	}
	group, err := s.groupReader.GetByIDLite(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get channel monitor group: %w", err)
	}
	if group == nil {
		return nil, ErrGroupNotFound
	}
	if group.Status != StatusActive {
		return nil, ErrChannelMonitorGroupInactive
	}
	if (provider == MonitorProviderCustom && group.Platform != PlatformComposite) ||
		(provider != MonitorProviderCustom && group.Platform != provider) {
		return nil, ErrChannelMonitorGroupPlatformMismatch
	}
	return group, nil
}

// Update updates a monitor. Moving a group-based monitor to another group
// rotates its managed API key so the credential always stays bound to the
// selected group.
func (s *ChannelMonitorService) Update(ctx context.Context, id int64, p ChannelMonitorUpdateParams) (*ChannelMonitor, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	originalName := strings.TrimSpace(existing.Name)
	originalGroupName := strings.TrimSpace(existing.GroupName)
	nameFollowedGroup := originalName == "" || originalName == originalGroupName
	submittedNameUnchanged := p.Name == nil || strings.TrimSpace(*p.Name) == originalName
	originalGroupID := cloneInt64Pointer(existing.GroupID)
	originalEncryptedKey := existing.APIKey
	managedGateway := existing.GroupID != nil || (p.GroupID != nil && *p.GroupID > 0)
	if managedGateway {
		if err := s.requireManagedGateway(); err != nil {
			return nil, err
		}
		// Ignore externally supplied endpoints for group-based monitors. The
		// generated credential is scoped to this deployment's gateway.
		p.Endpoint = nil
	}
	if err := applyMonitorUpdate(existing, p); err != nil {
		return nil, err
	}
	if managedGateway {
		existing.Endpoint = s.managedGatewayEndpoint
	}
	if len([]rune(existing.Name)) > maxChannelMonitorNameRunes {
		return nil, ErrChannelMonitorInvalidName
	}
	if p.GroupID != nil {
		if *p.GroupID <= 0 {
			return nil, ErrChannelMonitorMissingGroup
		}
		groupID := *p.GroupID
		existing.GroupID = &groupID
	}

	groupChanged := !sameOptionalInt64(originalGroupID, existing.GroupID)
	managedKeyCreated := false
	newPlainAPIKey := ""
	apiKeyUpdated := false
	if existing.GroupID != nil {
		group, groupErr := s.resolveMonitorGroup(ctx, *existing.GroupID, existing.Provider)
		if groupErr != nil {
			return nil, groupErr
		}
		existing.GroupName = group.Name
		existing.GroupRateMultiplier = group.RateMultiplier
		existing.GroupPlatform = group.Platform
		if strings.TrimSpace(existing.Name) == "" || (groupChanged && nameFollowedGroup && submittedNameUnchanged) {
			existing.Name = group.Name
		}
		if groupChanged {
			if s.apiKeyManager == nil || s.encryptor == nil {
				return nil, fmt.Errorf("channel monitor managed key dependencies are not configured")
			}
			managedKey, createErr := s.apiKeyManager.CreateChannelMonitorKey(ctx, existing.CreatedBy, *existing.GroupID, existing.Name)
			if createErr != nil {
				return nil, fmt.Errorf("rotate managed channel monitor key: %w", createErr)
			}
			newPlainAPIKey = managedKey.Key
			managedKeyCreated = true
			encryptedKey, encryptErr := s.encryptor.Encrypt(newPlainAPIKey)
			if encryptErr != nil {
				_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, newPlainAPIKey, existing.CreatedBy)
				return nil, fmt.Errorf("encrypt rotated channel monitor key: %w", encryptErr)
			}
			existing.APIKey = encryptedKey
			existing.APIKeyDecryptFailed = false
			apiKeyUpdated = true
		}
	} else {
		newPlainAPIKey, apiKeyUpdated, err = s.applyAPIKeyUpdate(existing, p.APIKey)
		if err != nil {
			return nil, err
		}
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		if managedKeyCreated {
			_ = s.apiKeyManager.DeleteChannelMonitorKey(ctx, newPlainAPIKey, existing.CreatedBy)
		}
		return nil, fmt.Errorf("update channel monitor: %w", err)
	}
	if managedKeyCreated {
		s.deleteManagedKeyFromCiphertext(ctx, originalEncryptedKey, existing.CreatedBy, existing.ID)
	}

	// Never return runtime credentials from the management path. When the key
	// was not rotated, validate the stored ciphertext so the UI can still show
	// the decrypt-failed state without receiving the plaintext.
	if apiKeyUpdated {
		existing.APIKey = ""
	} else {
		s.redactAPIKeyForRead(existing)
	}
	if s.scheduler != nil {
		// Schedule 内部根据 Enabled 自动选择 Unschedule 或重建任务，
		// IntervalSeconds 变化也会被自然吸收（旧 task 取消 + 新 task 用新 interval）。
		s.scheduler.Schedule(existing)
	}
	return existing, nil
}

func sameOptionalInt64(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// applyAPIKeyUpdate 处理 Update 中的 APIKey 字段：
//   - 入参 raw 为 nil 或空白：不修改 existing.APIKey（仍为密文），返回 updated=false
//   - 非空：加密后写入 existing.APIKey；同时返回明文，仅供写库失败时清理
//     新建的托管 key。成功响应会在 Update 返回前清空 existing.APIKey。
func (s *ChannelMonitorService) applyAPIKeyUpdate(existing *ChannelMonitor, raw *string) (plain string, updated bool, err error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", false, nil
	}
	if s.encryptor == nil {
		return "", false, fmt.Errorf("channel monitor secret encryptor is not configured")
	}
	plain = strings.TrimSpace(*raw)
	encrypted, encErr := s.encryptor.Encrypt(plain)
	if encErr != nil {
		return "", false, fmt.Errorf("encrypt api key: %w", encErr)
	}
	existing.APIKey = encrypted
	return plain, true, nil
}

// Delete removes the monitor and then retires its managed API key. Credential
// cleanup is best-effort after the monitor row has been deleted; a cleanup
// failure must not resurrect a monitor whose history was already removed.
func (s *ChannelMonitorService) Delete(ctx context.Context, id int64) error {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete channel monitor: %w", err)
	}
	s.deleteManagedKeyFromCiphertext(ctx, existing.APIKey, existing.CreatedBy, existing.ID)
	if s.scheduler != nil {
		s.scheduler.Unschedule(id)
	}
	return nil
}

func (s *ChannelMonitorService) deleteManagedKeyFromCiphertext(ctx context.Context, ciphertext string, ownerID, monitorID int64) {
	if s.encryptor == nil || s.apiKeyManager == nil || strings.TrimSpace(ciphertext) == "" {
		return
	}
	plain, err := s.encryptor.Decrypt(ciphertext)
	if err != nil || strings.TrimSpace(plain) == "" {
		slog.Warn("channel_monitor: unable to decrypt managed api key for cleanup",
			"monitor_id", monitorID, "error", err)
		return
	}
	if err := s.apiKeyManager.DeleteChannelMonitorKey(ctx, plain, ownerID); err != nil {
		// Legacy monitors can still contain an ordinary user key. The key
		// manager's durable purpose check is authoritative, so a refusal here
		// means there is nothing owned by the monitor service to reclaim.
		if errors.Is(err, ErrInsufficientPerms) {
			return
		}
		slog.Warn("channel_monitor: managed api key cleanup failed",
			"monitor_id", monitorID, "error", err)
	}
}

// ListHistory 列出某个监控最近的检测历史。
// model 为空表示返回所有模型；limit <= 0 时使用默认值，超过上限会被截断。
func (s *ChannelMonitorService) ListHistory(ctx context.Context, id int64, model string, limit int) ([]*ChannelMonitorHistoryEntry, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = MonitorHistoryDefaultLimit
	}
	if limit > MonitorHistoryMaxLimit {
		limit = MonitorHistoryMaxLimit
	}
	entries, err := s.repo.ListHistory(ctx, id, strings.TrimSpace(model), limit)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	return entries, nil
}

// ---------- 业务 ----------

// RunCheck 同步触发对一个监控的检测：并发跑 primary + extra 模型，
// 写历史记录并更新 last_checked_at。返回每个模型的检测结果。
func (s *ChannelMonitorService) RunCheck(ctx context.Context, id int64) ([]*CheckResult, error) {
	m, err := s.getForExecution(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.SimulateRequests {
		results := simulatedMonitorResults(m)
		s.persistCheckResults(ctx, m, results)
		return results, nil
	}
	if m.GroupID != nil {
		if _, groupErr := s.resolveMonitorGroup(ctx, *m.GroupID, m.Provider); groupErr != nil {
			results := monitorConfigurationErrorResults(m, groupErr)
			s.persistCheckResults(ctx, m, results)
			return results, nil
		}
		if gatewayErr := s.requireManagedGateway(); gatewayErr != nil {
			results := monitorConfigurationErrorResults(m, gatewayErr)
			s.persistCheckResults(ctx, m, results)
			return results, nil
		}
	}
	if m.APIKeyDecryptFailed {
		return nil, ErrChannelMonitorAPIKeyDecryptFailed
	}
	results := s.runChecksConcurrent(ctx, m)
	s.persistCheckResults(ctx, m, results)
	return results, nil
}

func simulatedMonitorResults(m *ChannelMonitor) []*CheckResult {
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	checkedAt := time.Now()
	results := make([]*CheckResult, 0, len(models))
	for _, model := range models {
		latencyMs, pingLatencyMs := 0, 0
		results = append(results, &CheckResult{
			Model:         model,
			Status:        MonitorStatusOperational,
			LatencyMs:     &latencyMs,
			PingLatencyMs: &pingLatencyMs,
			Message:       "Simulated monitor check succeeded; no upstream request sent",
			CheckedAt:     checkedAt,
		})
	}
	return results
}

func monitorConfigurationErrorResults(m *ChannelMonitor, cause error) []*CheckResult {
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	checkedAt := time.Now()
	message := truncateMessage(sanitizeErrorMessage(cause.Error()))
	results := make([]*CheckResult, 0, len(models))
	for _, model := range models {
		results = append(results, &CheckResult{
			Model:     model,
			Status:    MonitorStatusError,
			Message:   message,
			CheckedAt: checkedAt,
		})
	}
	return results
}

// persistCheckResults 写入本次检测的历史记录并更新 last_checked_at。
// 任一写库失败都只记日志，不影响调用方拿到 results（与 MVP 期望一致：宁可漏记历史也要先返回结果）。
func (s *ChannelMonitorService) persistCheckResults(ctx context.Context, m *ChannelMonitor, results []*CheckResult) {
	rows := make([]*ChannelMonitorHistoryRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, &ChannelMonitorHistoryRow{
			MonitorID:     m.ID,
			Model:         r.Model,
			Status:        r.Status,
			LatencyMs:     r.LatencyMs,
			PingLatencyMs: r.PingLatencyMs,
			Message:       r.Message,
			CheckedAt:     r.CheckedAt,
		})
	}
	if err := s.repo.InsertHistoryBatch(ctx, rows); err != nil {
		slog.Error("channel_monitor: insert history failed",
			"monitor_id", m.ID, "name", m.Name, "error", err)
	}
	if err := s.repo.MarkChecked(ctx, m.ID, time.Now()); err != nil {
		slog.Error("channel_monitor: mark checked failed",
			"monitor_id", m.ID, "error", err)
	}
}

// runChecksConcurrent 对 primary + extra 模型并发执行检测。
// errgroup 仅用于等待，不传播错误（每个 model 失败都已打包进 CheckResult）。
func (s *ChannelMonitorService) runChecksConcurrent(ctx context.Context, m *ChannelMonitor) []*CheckResult {
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	results := make([]*CheckResult, len(models))

	// ping 共享一次，所有模型记录同一个 ping 延迟。
	managedGateway := m.GroupID != nil
	endpoint := m.Endpoint
	if managedGateway {
		endpoint = s.managedGatewayEndpoint
	}
	pingMs := pingEndpointOrigin(ctx, endpoint, managedGateway)

	// 所有模型共用同一份 CheckOptions（来自监控的快照字段）。
	opts := &CheckOptions{
		APIMode:          m.APIMode,
		ExtraHeaders:     m.ExtraHeaders,
		BodyOverrideMode: m.BodyOverrideMode,
		BodyOverride:     m.BodyOverride,
		ManagedGateway:   managedGateway,
		ManagedAttestor:  s.managedGatewayAttestor,
	}

	var eg errgroup.Group
	var mu sync.Mutex
	for i, model := range models {
		i, model := i, model
		eg.Go(func() error {
			r := runCheckForModel(ctx, m.Provider, endpoint, m.APIKey, model, opts)
			r.PingLatencyMs = pingMs
			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	return results
}

// ---------- 调度器协作 ----------

// SetScheduler 由 wire 在 runner 构造后注入，用于在 CRUD 时即时同步任务表。
// 通过 setter 注入避免 service ↔ runner 的依赖环。
func (s *ChannelMonitorService) SetScheduler(sched MonitorScheduler) {
	s.scheduler = sched
}

// ListEnabledMonitors 返回所有 enabled=true 的监控元数据，供 runner 启动时建立任务表。
// runner 每次执行会通过 RunCheck 单独读取密钥，因此这里也不暴露密钥。
func (s *ChannelMonitorService) ListEnabledMonitors(ctx context.Context) ([]*ChannelMonitor, error) {
	all, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		s.redactAPIKeyForRead(m)
	}
	return all, nil
}

// cleanupOldHistory 删除 monitorHistoryRetentionDays 天之前的明细历史记录。
// 由 RunDailyMaintenance 调用；SoftDeleteMixin 自动把 DELETE 改为 UPDATE deleted_at。
func (s *ChannelMonitorService) cleanupOldHistory(ctx context.Context) error {
	before := time.Now().UTC().AddDate(0, 0, -monitorHistoryRetentionDays)
	deleted, err := s.repo.DeleteHistoryBefore(ctx, before)
	if err != nil {
		return fmt.Errorf("delete history before %s: %w", before.Format(time.RFC3339), err)
	}
	if deleted > 0 {
		slog.Info("channel_monitor: history cleanup",
			"deleted_rows", deleted, "before", before.Format(time.RFC3339))
	}
	return nil
}

// RunDailyMaintenance 每日维护任务：聚合昨天之前未聚合的明细，软删过期明细和聚合。
// 由 OpsCleanupService 的 cron 调度触发（共享 schedule 和 leader lock）。
//
// 幂等性：
//   - watermark 保证已聚合的日期不会重复处理；
//   - UpsertDailyRollupsFor 内部使用 ON CONFLICT DO UPDATE，同一日重复跑结果一致。
//
// 每一步失败都只记 slog.Warn，整体函数始终返回 nil 让后续步骤能继续跑
// （与 OpsCleanupService.runCleanupOnce 风格一致）。
func (s *ChannelMonitorService) RunDailyMaintenance(ctx context.Context) error {
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)

	if err := s.runDailyAggregation(ctx, today); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "aggregate", "error", err)
	}
	if err := s.cleanupOldHistory(ctx); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "prune_history", "error", err)
	}
	if err := s.cleanupOldRollups(ctx, today); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "prune_rollups", "error", err)
	}
	return nil
}

// runDailyAggregation 从 watermark+1 聚合到昨天（UTC）。
// 首次跑（watermark nil）：从 today-monitorRollupRetentionDays 开始回填。
// 每次最多聚合 monitorMaintenanceMaxDaysPerRun 天，避免长事务。
func (s *ChannelMonitorService) runDailyAggregation(ctx context.Context, today time.Time) error {
	watermark, err := s.repo.LoadAggregationWatermark(ctx)
	if err != nil {
		return fmt.Errorf("load watermark: %w", err)
	}

	start := s.resolveAggregationStart(watermark, today)
	if !start.Before(today) {
		return nil // 没有需要聚合的日期
	}

	iterations := 0
	for d := start; d.Before(today); d = d.Add(24 * time.Hour) {
		if iterations >= monitorMaintenanceMaxDaysPerRun {
			slog.Info("channel_monitor: maintenance aggregation capped",
				"max_days", monitorMaintenanceMaxDaysPerRun,
				"next_resume", d.Format("2006-01-02"))
			break
		}
		affected, upErr := s.repo.UpsertDailyRollupsFor(ctx, d)
		if upErr != nil {
			return fmt.Errorf("upsert rollups for %s: %w", d.Format("2006-01-02"), upErr)
		}
		if err := s.repo.UpdateAggregationWatermark(ctx, d); err != nil {
			return fmt.Errorf("update watermark to %s: %w", d.Format("2006-01-02"), err)
		}
		slog.Info("channel_monitor: rollups upserted",
			"date", d.Format("2006-01-02"), "affected_rows", affected)
		iterations++
	}
	return nil
}

// resolveAggregationStart 计算本次聚合起点：
//   - watermark == nil：today - monitorRollupRetentionDays（首次回填最多 30 天）
//   - watermark != nil：*watermark + 1 day
func (s *ChannelMonitorService) resolveAggregationStart(watermark *time.Time, today time.Time) time.Time {
	if watermark == nil {
		return today.AddDate(0, 0, -monitorRollupRetentionDays)
	}
	return watermark.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
}

// cleanupOldRollups 软删 bucket_date < today - monitorRollupRetentionDays 的日聚合行。
func (s *ChannelMonitorService) cleanupOldRollups(ctx context.Context, today time.Time) error {
	cutoff := today.AddDate(0, 0, -monitorRollupRetentionDays)
	deleted, err := s.repo.DeleteRollupsBefore(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("delete rollups before %s: %w", cutoff.Format("2006-01-02"), err)
	}
	if deleted > 0 {
		slog.Info("channel_monitor: rollups cleanup",
			"deleted_rows", deleted, "before", cutoff.Format("2006-01-02"))
	}
	return nil
}

// ---------- helpers ----------

// getForExecution is the only read path that returns a monitor carrying a
// plaintext API key. Keep it private to prevent handlers from using it.
func (s *ChannelMonitorService) getForExecution(ctx context.Context, id int64) (*ChannelMonitor, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// Simulation never needs credentials or network access.
	if m.SimulateRequests {
		m.APIKey = ""
		return m, nil
	}
	s.decryptInPlace(m)
	return m, nil
}

// redactAPIKeyForRead validates an encrypted key for the admin health hint and
// then clears it. Callers can inspect APIKeyDecryptFailed but can never receive
// plaintext or ciphertext through a management/read response object.
func (s *ChannelMonitorService) redactAPIKeyForRead(m *ChannelMonitor) {
	if m == nil {
		return
	}
	ciphertext := m.APIKey
	m.APIKey = ""
	m.APIKeyDecryptFailed = false
	if strings.TrimSpace(ciphertext) == "" {
		return
	}
	if s.encryptor == nil {
		m.APIKeyDecryptFailed = true
		slog.Warn("channel_monitor: secret encryptor unavailable while validating api key",
			"monitor_id", m.ID)
		return
	}
	if _, err := s.encryptor.Decrypt(ciphertext); err != nil {
		m.APIKeyDecryptFailed = true
		slog.Warn("channel_monitor: decrypt api key failed",
			"monitor_id", m.ID, "error", err)
	}
}

// decryptInPlace 把 ChannelMonitor.APIKey 从密文解密为明文。
// 解密失败时把字段清空 + 设置 APIKeyDecryptFailed=true（不返回错误，避免阻断列表渲染）。
// runner / RunCheck 必须读取该标志位并拒绝执行检测。
func (s *ChannelMonitorService) decryptInPlace(m *ChannelMonitor) {
	if m == nil || m.APIKey == "" {
		return
	}
	if s.encryptor == nil {
		m.APIKey = ""
		m.APIKeyDecryptFailed = true
		slog.Warn("channel_monitor: secret encryptor unavailable while decrypting api key",
			"monitor_id", m.ID)
		return
	}
	plain, err := s.encryptor.Decrypt(m.APIKey)
	if err != nil {
		slog.Warn("channel_monitor: decrypt api key failed",
			"monitor_id", m.ID, "error", err)
		m.APIKey = ""
		m.APIKeyDecryptFailed = true
		return
	}
	m.APIKey = plain
}

// applyMonitorUpdate 把 update params 中非 nil 的字段应用到 existing 上。
// APIKey 字段在调用方单独处理（涉及加密）。
//
// 行数稍超过 30：这是逐字段平铺的 dispatcher，每个 if 都是 1-3 行的"非 nil 则覆盖"模式，
// 拆分反而会增加跳转噪音、影响可读性，故保留为单函数。
func applyMonitorUpdate(existing *ChannelMonitor, p ChannelMonitorUpdateParams) error {
	providerChanged := false
	if p.Name != nil {
		existing.Name = strings.TrimSpace(*p.Name)
	}
	if p.Provider != nil {
		if err := validateProvider(*p.Provider); err != nil {
			return err
		}
		providerChanged = existing.Provider != *p.Provider
		existing.Provider = *p.Provider
	}
	if p.Endpoint != nil {
		if err := validateEndpoint(*p.Endpoint); err != nil {
			return err
		}
		existing.Endpoint = normalizeEndpoint(*p.Endpoint)
	}
	if p.PrimaryModel != nil {
		primaryModel := normalizeMonitorPrimaryModel(existing.Provider, *p.PrimaryModel)
		if primaryModel == "" {
			return ErrChannelMonitorMissingPrimaryModel
		}
		existing.PrimaryModel = primaryModel
	} else if providerChanged && existing.Provider == MonitorProviderGrok {
		existing.PrimaryModel = MonitorDefaultGrokModel
	}
	if p.ExtraModels != nil {
		existing.ExtraModels = normalizeModels(*p.ExtraModels)
	}
	if p.GroupName != nil {
		existing.GroupName = strings.TrimSpace(*p.GroupName)
	}
	if p.Enabled != nil {
		existing.Enabled = *p.Enabled
	}
	if p.SimulateRequests != nil {
		existing.SimulateRequests = *p.SimulateRequests
	}
	if p.IntervalSeconds != nil {
		if err := validateInterval(*p.IntervalSeconds); err != nil {
			return err
		}
		existing.IntervalSeconds = *p.IntervalSeconds
	}
	if p.JitterSeconds != nil {
		existing.JitterSeconds = *p.JitterSeconds
	}
	if p.IntervalSeconds != nil || p.JitterSeconds != nil {
		// interval 与 jitter 任一变化都需要重新校验组合约束（interval - jitter >= 下限）。
		if err := validateJitter(existing.JitterSeconds, existing.IntervalSeconds); err != nil {
			return err
		}
	}
	return applyMonitorAdvancedUpdate(existing, p, providerChanged)
}

// applyMonitorAdvancedUpdate 处理自定义请求快照相关字段，从 applyMonitorUpdate 拆出避免过长。
func applyMonitorAdvancedUpdate(existing *ChannelMonitor, p ChannelMonitorUpdateParams, providerChanged bool) error {
	if p.ClearTemplate {
		existing.TemplateID = nil
	} else if p.TemplateID != nil {
		id := *p.TemplateID
		existing.TemplateID = &id
	}
	if p.ExtraHeaders != nil {
		if err := validateExtraHeaders(*p.ExtraHeaders); err != nil {
			return err
		}
		existing.ExtraHeaders = emptyHeadersIfNil(*p.ExtraHeaders)
	}
	newAPIMode := defaultAPIMode(existing.APIMode)
	if p.APIMode != nil {
		newAPIMode = defaultAPIMode(*p.APIMode)
	} else if !isMonitorResponsesProvider(existing.Provider) {
		newAPIMode = MonitorAPIModeChatCompletions
	}
	if err := validateAPIMode(existing.Provider, newAPIMode); err != nil {
		return err
	}
	// BodyOverrideMode / BodyOverride 联合校验，和模板一致。
	newMode := existing.BodyOverrideMode
	newBody := existing.BodyOverride
	if p.BodyOverrideMode != nil {
		newMode = *p.BodyOverrideMode
	}
	if p.BodyOverride != nil {
		newBody = *p.BodyOverride
	}
	if providerChanged || p.APIMode != nil || p.BodyOverrideMode != nil || p.BodyOverride != nil {
		if err := validateBodyModeForProtocol(existing.Provider, newAPIMode, newMode, newBody); err != nil {
			return err
		}
		existing.BodyOverrideMode = defaultBodyMode(newMode)
		existing.BodyOverride = newBody
	}
	existing.APIMode = newAPIMode
	return nil
}
