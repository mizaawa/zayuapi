package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	UsageCleanupStatusPending   = "pending"
	UsageCleanupStatusRunning   = "running"
	UsageCleanupStatusSucceeded = "succeeded"
	UsageCleanupStatusFailed    = "failed"
	UsageCleanupStatusCanceled  = "canceled"
)

// UsageCleanupFilters 定义清理任务过滤条件
// 时间范围为必填，其他字段可选
// JSON 序列化用于存储任务参数
//
// start_time/end_time 使用 RFC3339 时间格式
// 以 UTC 或用户时区解析后的时间为准
//
// 说明：
// - nil 表示未设置该过滤条件
// - 过滤条件均为精确匹配
type UsageCleanupFilters struct {
	StartTime       time.Time  `json:"start_time"`
	EndTime         time.Time  `json:"end_time"`
	RetentionCutoff *time.Time `json:"retention_cutoff,omitempty"`
	DeleteThroughID *int64     `json:"delete_through_id,omitempty"`
	UserID          *int64     `json:"user_id,omitempty"`
	APIKeyID        *int64     `json:"api_key_id,omitempty"`
	AccountID       *int64     `json:"account_id,omitempty"`
	GroupID         *int64     `json:"group_id,omitempty"`
	Model           *string    `json:"model,omitempty"`
	RequestType     *int16     `json:"request_type,omitempty"`
	Stream          *bool      `json:"stream,omitempty"`
	BillingType     *int8      `json:"billing_type,omitempty"`
}

type UsageCleanupScheduleSettings struct {
	Enabled       bool       `json:"enabled"`
	IntervalDays  int        `json:"interval_days"`
	RetentionDays int        `json:"retention_days"`
	DeleteAll     bool       `json:"delete_all"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	UpdatedBy     int64      `json:"updated_by,omitempty"`
}

type DatabaseTableStorageStats struct {
	TableName  string `json:"table_name"`
	TableBytes int64  `json:"table_bytes"`
	IndexBytes int64  `json:"index_bytes"`
	TotalBytes int64  `json:"total_bytes"`
}

type DatabaseStorageStats struct {
	DatabaseName  string                      `json:"database_name"`
	DatabaseBytes int64                       `json:"database_bytes"`
	Tables        []DatabaseTableStorageStats `json:"tables"`
	TableBytes    int64                       `json:"table_bytes"`
	IndexBytes    int64                       `json:"index_bytes"`
	TotalBytes    int64                       `json:"total_bytes"`
	MeasuredAt    time.Time                   `json:"measured_at"`
}

// UsageCleanupMaintenanceRepository provides storage-aware retention cleanup.
// It is optional so task-only test repositories and existing integrations remain valid.
type UsageCleanupMaintenanceRepository interface {
	GetDatabaseStorageStats(ctx context.Context) (*DatabaseStorageStats, error)
	GetUsageLogsMaxID(ctx context.Context) (int64, error)
	DeleteUsageLogsBeforeBatch(ctx context.Context, cutoff time.Time, limit int) (int64, error)
	DeleteUsageLogsThroughIDBatch(ctx context.Context, maxID int64, limit int) (int64, error)
}

// UsageCleanupTask 表示使用记录清理任务
// 状态包含 pending/running/succeeded/failed/canceled
type UsageCleanupTask struct {
	ID          int64
	Status      string
	Filters     UsageCleanupFilters
	CreatedBy   int64
	DeletedRows int64
	ErrorMsg    *string
	CanceledBy  *int64
	CanceledAt  *time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UsageCleanupRepository 定义清理任务持久层接口
type UsageCleanupRepository interface {
	CreateTask(ctx context.Context, task *UsageCleanupTask) error
	ListTasks(ctx context.Context, params pagination.PaginationParams) ([]UsageCleanupTask, *pagination.PaginationResult, error)
	// ClaimNextPendingTask 抢占下一条可执行任务：
	// - 优先 pending
	// - 若 running 超过 staleRunningAfterSeconds（可能由于进程退出/崩溃/超时），允许重新抢占继续执行
	ClaimNextPendingTask(ctx context.Context, staleRunningAfterSeconds int64) (*UsageCleanupTask, error)
	// GetTaskStatus 查询任务状态；若不存在返回 sql.ErrNoRows
	GetTaskStatus(ctx context.Context, taskID int64) (string, error)
	// UpdateTaskProgress 更新任务进度（deleted_rows）用于断点续跑/展示
	UpdateTaskProgress(ctx context.Context, taskID int64, deletedRows int64) error
	// CancelTask 将任务标记为 canceled（仅允许 pending/running）
	CancelTask(ctx context.Context, taskID int64, canceledBy int64) (bool, error)
	MarkTaskSucceeded(ctx context.Context, taskID int64, deletedRows int64) error
	MarkTaskFailed(ctx context.Context, taskID int64, deletedRows int64, errorMsg string) error
	DeleteUsageLogsBatch(ctx context.Context, filters UsageCleanupFilters, limit int) (int64, error)
}
