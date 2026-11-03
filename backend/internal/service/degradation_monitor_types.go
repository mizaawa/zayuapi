package service

import (
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Degradation monitor（降智检测）asks a group's model to draw the same SVG/HTML
// animation on a schedule so users can compare how the model performs over time.
// Probes run through this deployment's own gateway with a managed internal key:
// they never charge the creating administrator and their upstream spend is
// reported as monitor consumption on the operations dashboard.

const (
	DegradationProtocolOpenAIChat      = "openai_chat"
	DegradationProtocolOpenAIResponses = "openai_responses"
	DegradationProtocolAnthropic       = "anthropic"

	DegradationRunStatusRunning = "running"
	DegradationRunStatusSuccess = "success"
	DegradationRunStatusFailed  = "failed"

	DegradationTriggerSchedule = "schedule"
	DegradationTriggerManual   = "manual"

	// DegradationRunRetention is how many results each monitor keeps.
	DegradationRunRetention = 100

	DegradationMinIntervalMinutes     = 5
	DegradationMaxIntervalMinutes     = 10080
	DegradationDefaultIntervalMinutes = 60
	DegradationMinTimeoutMinutes      = 1
	DegradationMaxTimeoutMinutes      = 60
	DegradationDefaultTimeoutMinutes  = 10
	DegradationMaxRetries             = 5
	DegradationDefaultRetries         = 1
	DegradationMinDisplayPerGroup     = 1
	DegradationMaxDisplayPerGroup     = DegradationRunRetention
	DegradationDefaultDisplayPerGroup = 25
	DegradationDefaultReasoningEffort = "high"

	// DegradationMaxOutputBytes caps a generated document. Longer output stops
	// the request and is reported as a failure instead of growing the row.
	DegradationMaxOutputBytes = 1 << 20
	DegradationPromptMaxBytes = 16000
	DegradationNameMaxRunes   = 100
	DegradationModelMaxLength = 200

	degradationErrorMaxBytes = 1000
	degradationRetryDelay    = 2 * time.Second

	// DegradationPelicanPrompt is the built-in fallback used when no preset exists.
	DegradationPelicanPrompt     = "创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的 2D 动画。你不需要任何测试。"
	DegradationPelicanPromptName = "鹈鹕骑自行车"

	// degradationSystemInstructions keeps the answer renderable in the sandboxed preview.
	degradationSystemInstructions = "Return a complete, self-contained HTML document for the user's request. Include all SVG, CSS and JavaScript inline. Do not use external resources. Return only HTML, without Markdown fences or explanations."
)

// DegradationReasoningEfforts lists the effort labels accepted from administrators.
var DegradationReasoningEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

var (
	ErrDegradationMonitorNotFound = infraerrors.NotFound(
		"DEGRADATION_MONITOR_NOT_FOUND", "degradation monitor not found",
	)
	ErrDegradationPromptNotFound = infraerrors.NotFound(
		"DEGRADATION_PROMPT_NOT_FOUND", "degradation prompt preset not found",
	)
	ErrDegradationRunNotFound = infraerrors.NotFound(
		"DEGRADATION_RUN_NOT_FOUND", "degradation result not found",
	)
	ErrDegradationInvalidName = infraerrors.BadRequest(
		"DEGRADATION_INVALID_NAME", "name must not exceed 100 characters",
	)
	ErrDegradationInvalidModel = infraerrors.BadRequest(
		"DEGRADATION_INVALID_MODEL", "model is required and must not exceed 200 characters",
	)
	ErrDegradationInvalidProtocol = infraerrors.BadRequest(
		"DEGRADATION_INVALID_PROTOCOL", "protocol must be openai_chat, openai_responses or anthropic",
	)
	ErrDegradationInvalidEffort = infraerrors.BadRequest(
		"DEGRADATION_INVALID_REASONING_EFFORT", "reasoning_effort must be none, minimal, low, medium, high, xhigh or max",
	)
	ErrDegradationInvalidInterval = infraerrors.BadRequest(
		"DEGRADATION_INVALID_INTERVAL", "interval_minutes must be empty or between 5 and 10080",
	)
	ErrDegradationMissingGroup = infraerrors.BadRequest(
		"DEGRADATION_MISSING_GROUP", "group_id is required",
	)
	ErrDegradationGroupInactive = infraerrors.BadRequest(
		"DEGRADATION_GROUP_INACTIVE", "the selected group is not active",
	)
	ErrDegradationGroupPlatform = infraerrors.BadRequest(
		"DEGRADATION_GROUP_PLATFORM", "the selected group platform is not supported",
	)
	ErrDegradationInvalidPrompt = infraerrors.BadRequest(
		"DEGRADATION_INVALID_PROMPT", "prompt is required and must not exceed 16000 bytes",
	)
	ErrDegradationInvalidPromptName = infraerrors.BadRequest(
		"DEGRADATION_INVALID_PROMPT_NAME", "prompt name must not exceed 100 characters",
	)
	ErrDegradationInvalidConfig = infraerrors.BadRequest(
		"DEGRADATION_INVALID_CONFIG", "degradation monitor settings are out of range",
	)
	ErrDegradationRunInProgress = infraerrors.Conflict(
		"DEGRADATION_RUN_IN_PROGRESS", "a check for this group is already running",
	)
	ErrDegradationDisabled = infraerrors.BadRequest(
		"DEGRADATION_DISABLED", "degradation monitoring is disabled in feature settings",
	)
	ErrDegradationGatewayUnavailable = infraerrors.InternalServer(
		"DEGRADATION_GATEWAY_UNAVAILABLE", "managed gateway is unavailable",
	)
	ErrDegradationRunnerBusy = infraerrors.ServiceUnavailable(
		"DEGRADATION_RUNNER_BUSY", "the degradation runner is busy, try again shortly",
	)
)

// DegradationConfig is the global configuration edited on the admin page.
type DegradationConfig struct {
	DefaultIntervalMinutes int    `json:"default_interval_minutes"`
	TimeoutMinutes         int    `json:"timeout_minutes"`
	MaxRetries             int    `json:"max_retries"`
	MaxDisplayPerGroup     int    `json:"max_display_per_group"`
	DefaultReasoningEffort string `json:"default_reasoning_effort"`
	DefaultPromptID        int64  `json:"default_prompt_id"`
}

// DefaultDegradationConfig returns the configuration used before an administrator saves one.
func DefaultDegradationConfig() DegradationConfig {
	return DegradationConfig{
		DefaultIntervalMinutes: DegradationDefaultIntervalMinutes,
		TimeoutMinutes:         DegradationDefaultTimeoutMinutes,
		MaxRetries:             DegradationDefaultRetries,
		MaxDisplayPerGroup:     DegradationDefaultDisplayPerGroup,
		DefaultReasoningEffort: DegradationDefaultReasoningEffort,
	}
}

// Validate reports whether every value is inside its documented range.
func (c DegradationConfig) Validate() error {
	if c.DefaultIntervalMinutes < DegradationMinIntervalMinutes || c.DefaultIntervalMinutes > DegradationMaxIntervalMinutes ||
		c.TimeoutMinutes < DegradationMinTimeoutMinutes || c.TimeoutMinutes > DegradationMaxTimeoutMinutes ||
		c.MaxRetries < 0 || c.MaxRetries > DegradationMaxRetries ||
		c.MaxDisplayPerGroup < DegradationMinDisplayPerGroup || c.MaxDisplayPerGroup > DegradationMaxDisplayPerGroup ||
		c.DefaultPromptID < 0 {
		return ErrDegradationInvalidConfig
	}
	if !IsDegradationReasoningEffort(c.DefaultReasoningEffort) {
		return ErrDegradationInvalidEffort
	}
	return nil
}

// normalized repairs a stored configuration so a damaged row never stops the runner.
func (c DegradationConfig) normalized() DegradationConfig {
	defaults := DefaultDegradationConfig()
	if c.DefaultIntervalMinutes < DegradationMinIntervalMinutes || c.DefaultIntervalMinutes > DegradationMaxIntervalMinutes {
		c.DefaultIntervalMinutes = defaults.DefaultIntervalMinutes
	}
	if c.TimeoutMinutes < DegradationMinTimeoutMinutes || c.TimeoutMinutes > DegradationMaxTimeoutMinutes {
		c.TimeoutMinutes = defaults.TimeoutMinutes
	}
	if c.MaxRetries < 0 || c.MaxRetries > DegradationMaxRetries {
		c.MaxRetries = defaults.MaxRetries
	}
	if c.MaxDisplayPerGroup < DegradationMinDisplayPerGroup || c.MaxDisplayPerGroup > DegradationMaxDisplayPerGroup {
		c.MaxDisplayPerGroup = defaults.MaxDisplayPerGroup
	}
	c.DefaultReasoningEffort = strings.ToLower(strings.TrimSpace(c.DefaultReasoningEffort))
	if !IsDegradationReasoningEffort(c.DefaultReasoningEffort) {
		c.DefaultReasoningEffort = defaults.DefaultReasoningEffort
	}
	if c.DefaultPromptID < 0 {
		c.DefaultPromptID = 0
	}
	return c
}

// IsDegradationReasoningEffort reports whether effort is an accepted label.
func IsDegradationReasoningEffort(effort string) bool {
	for _, candidate := range DegradationReasoningEfforts {
		if effort == candidate {
			return true
		}
	}
	return false
}

// IsDegradationProtocol reports whether protocol names a supported request format.
func IsDegradationProtocol(protocol string) bool {
	switch protocol {
	case DegradationProtocolOpenAIChat, DegradationProtocolOpenAIResponses, DegradationProtocolAnthropic:
		return true
	default:
		return false
	}
}

// DefaultDegradationProtocol is Anthropic Messages for Anthropic groups and the
// OpenAI Responses API for every other platform.
func DefaultDegradationProtocol(platform string) string {
	if platform == PlatformAnthropic {
		return DegradationProtocolAnthropic
	}
	return DegradationProtocolOpenAIResponses
}

// IsDegradationGroupPlatform reports whether a site group can host a monitor.
func IsDegradationGroupPlatform(platform string) bool {
	switch platform {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformGrok, PlatformComposite:
		return true
	default:
		return false
	}
}

// DegradationMonitor is one scheduled check: a site group, a model and a protocol.
type DegradationMonitor struct {
	ID              int64
	Name            string
	GroupID         int64
	GroupName       string
	GroupPlatform   string
	GroupStatus     string
	GroupDeleted    bool
	Model           string
	ReasoningEffort string // empty follows the global default
	Protocol        string
	IntervalMinutes int // 0 follows the global default
	PromptID        *int64
	PromptName      string
	RandomPrompt    bool
	Enabled         bool
	SortOrder       int
	APIKeyID        int64
	CreatedBy       int64
	LastRunAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// EffectiveIntervalMinutes resolves the monitor interval against the global default.
func (m *DegradationMonitor) EffectiveIntervalMinutes(cfg DegradationConfig) int {
	if m != nil && m.IntervalMinutes > 0 {
		return m.IntervalMinutes
	}
	return cfg.DefaultIntervalMinutes
}

// EffectiveReasoningEffort resolves the monitor effort against the global default.
func (m *DegradationMonitor) EffectiveReasoningEffort(cfg DegradationConfig) string {
	if m != nil && m.ReasoningEffort != "" {
		return m.ReasoningEffort
	}
	return cfg.DefaultReasoningEffort
}

// DegradationMonitorInput carries create and update fields from administrators.
type DegradationMonitorInput struct {
	Name            string
	GroupID         int64
	Model           string
	ReasoningEffort string
	Protocol        string
	IntervalMinutes int
	PromptID        *int64
	RandomPrompt    bool
	Enabled         bool
}

// DegradationSortOrderUpdate is one entry of a drag-and-drop reorder.
type DegradationSortOrderUpdate struct {
	ID        int64
	SortOrder int
}

// DegradationPrompt is an editable prompt preset.
type DegradationPrompt struct {
	ID         int64
	Name       string
	Prompt     string
	BuiltinKey string
	SortOrder  int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DegradationRun is one stored check result. Output is only loaded by detail reads.
type DegradationRun struct {
	ID                  int64
	MonitorID           int64
	Status              string
	TriggerSource       string
	Model               string
	ResponseModel       string
	ReasoningEffort     string
	Protocol            string
	PromptID            *int64
	PromptName          string
	Prompt              string
	Output              string
	OutputBytes         int64
	ErrorMessage        string
	Attempts            int
	FirstTokenMs        *int
	DurationMs          *int
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	Cost                *float64
	UsageSynced         bool
	StartedAt           time.Time
	FinishedAt          *time.Time
}

// TotalTokens is every billed token of the run, cache reads included.
func (r *DegradationRun) TotalTokens() int64 {
	if r == nil {
		return 0
	}
	return r.InputTokens + r.OutputTokens + r.CacheReadTokens + r.CacheCreationTokens
}

// CacheRate is the share of prompt tokens served from cache, in [0, 1].
func (r *DegradationRun) CacheRate() *float64 {
	if r == nil {
		return nil
	}
	prompt := r.InputTokens + r.CacheReadTokens + r.CacheCreationTokens
	if prompt <= 0 {
		return nil
	}
	rate := float64(r.CacheReadTokens) / float64(prompt)
	return &rate
}

// DegradationMonitorStats summarizes stored results for the admin list.
type DegradationMonitorStats struct {
	StoredRuns    int64
	StoredBytes   int64
	LastRunStatus string
	LastRunAt     *time.Time
	LastRunError  string
}

// DegradationStorageStats is the stored-animation footprint of one monitor.
type DegradationStorageStats struct {
	Count int64
	Bytes int64
}

// DegradationUsageSummary aggregates gateway usage logs written for one run.
type DegradationUsageSummary struct {
	Requests            int64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	Cost                float64
}

// DegradationMonitorView is the admin list row.
type DegradationMonitorView struct {
	Monitor                  *DegradationMonitor
	Stats                    DegradationMonitorStats
	Running                  bool
	EffectiveIntervalMinutes int
	EffectiveReasoningEffort string
}

// DegradationUserMonitor is a monitor as shown on the user page.
type DegradationUserMonitor struct {
	ID                       int64
	Name                     string
	GroupName                string
	Platform                 string
	Model                    string
	Protocol                 string
	EffectiveReasoningEffort string
	EffectiveIntervalMinutes int
	LastRunAt                *time.Time
	Runs                     []*DegradationRun
}

// DegradationOverview groups user-visible monitors by platform.
type DegradationOverview struct {
	Enabled      bool
	DisplayLimit int
	Monitors     []*DegradationUserMonitor
}
