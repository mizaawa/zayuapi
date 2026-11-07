package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"math"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	upstreamRateNotifyDelay      = 5 * time.Minute
	upstreamRateNotifyRetryDelay = time.Minute
	upstreamRateNotifyLease      = 2 * time.Minute
)

// UpstreamRateNotification is stored inside the account's probe snapshot. A
// snapshot and its notification state therefore pass the same repository CAS.
// Baseline survives restarts and remains fixed until the upstream returns to it.
type UpstreamRateNotification struct {
	Epoch          string     `json:"epoch"`
	Recipient      string     `json:"recipient"`
	SourceHash     string     `json:"source_hash"`
	Baseline       float64    `json:"baseline"`
	Candidate      *float64   `json:"candidate,omitempty"`
	ChangedAt      *time.Time `json:"changed_at,omitempty"`
	LastObservedAt *time.Time `json:"last_observed_at,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	RetryAt        *time.Time `json:"retry_at,omitempty"`
	LeaseToken     string     `json:"lease_token,omitempty"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	Attempts       int        `json:"attempts,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

type upstreamRateEmailSender interface {
	SendEmail(context.Context, string, string, string) error
}

// rateNotifyAccountRepository decorates only the probe's repository. It keeps
// the probe's normal CAS write authoritative and leaves all other account
// services and the shared probe implementation unchanged.
type rateNotifyAccountRepository struct {
	AccountRepository
	settingService *SettingService
	sender         upstreamRateEmailSender
	now            func() time.Time
	async          bool
}

func (r *rateNotifyAccountRepository) currentTime() time.Time {
	if r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

func (r *rateNotifyAccountRepository) getSettings(ctx context.Context) (*UpstreamBillingProbeSettings, error) {
	if r.settingService == nil {
		return defaultUpstreamBillingProbeSettings(), nil
	}
	return r.settingService.GetUpstreamBillingProbeSettings(ctx)
}

func (r *rateNotifyAccountRepository) UpdateUpstreamBillingProbeSnapshot(ctx context.Context, account *Account, snapshot *UpstreamBillingProbeSnapshot, rate *float64) error {
	r.prepareRateNotification(ctx, account, snapshot)
	writer, ok := r.AccountRepository.(upstreamBillingProbeSnapshotWriter)
	if !ok {
		return ErrUpstreamBillingProbeUnavailable
	}
	if err := writer.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, rate); err != nil {
		return err
	}
	if r.sender != nil && upstreamRateNotificationReady(snapshot.RateNotification, r.currentTime()) {
		if r.async {
			go func() {
				notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
				defer cancel()
				r.deliverRateNotification(notifyCtx, account.ID)
			}()
		} else {
			r.deliverRateNotification(ctx, account.ID)
		}
	}
	return nil
}

func (r *rateNotifyAccountRepository) ListDueUpstreamBillingProbeAccounts(ctx context.Context, now time.Time, limit int) ([]Account, error) {
	if lister, ok := r.AccountRepository.(upstreamBillingProbeDueAccountLister); ok {
		return lister.ListDueUpstreamBillingProbeAccounts(ctx, now, limit)
	}
	return r.AccountRepository.FindByExtraField(ctx, UpstreamBillingProbeEnabledExtraKey, true)
}

// ProvideRateNotifyUpstreamBillingProbeService replaces the plain provider in
// Wire, while callers of NewUpstreamBillingProbeService retain its old behavior.
func ProvideRateNotifyUpstreamBillingProbeService(accountRepo AccountRepository, accountTestService *AccountTestService,
	settingService *SettingService, lockCache LeaderLockCache, db *sql.DB, emailService *EmailService) *UpstreamBillingProbeService {
	wrapped := &rateNotifyAccountRepository{AccountRepository: accountRepo, settingService: settingService, sender: emailService, now: time.Now, async: true}
	svc := NewUpstreamBillingProbeService(wrapped, accountTestService, settingService)
	svc.SetLeaderLock(lockCache, db)
	svc.Start()
	return svc
}

func validateUpstreamRateNotifyEmail(email string) error {
	if email == "" {
		return errors.New("notification email is required when rate change notification is enabled")
	}
	if len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return errors.New("invalid notification email")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" {
		return errors.New("invalid notification email")
	}
	return nil
}

// advanceUpstreamRateNotification observes a fresh declared multiplier. A
// second change restarts the five-minute window; returning to baseline clears
// the candidate. A sent candidate is never mailed twice during one excursion.
func advanceUpstreamRateNotification(previous *UpstreamRateNotification, epoch, recipient, sourceHash string, baseline, observed float64, at time.Time) *UpstreamRateNotification {
	if previous == nil || previous.Epoch != epoch || previous.Recipient != recipient || previous.SourceHash != sourceHash {
		previous = &UpstreamRateNotification{Epoch: epoch, Recipient: recipient, SourceHash: sourceHash, Baseline: baseline}
	}
	state := *previous
	state.LastObservedAt = upstreamRateNotifyTimePtr(at)
	if equalBillingMultiplier(observed, state.Baseline) {
		state.Candidate = nil
		state.ChangedAt = nil
		state.SentAt = nil
		state.RetryAt = nil
		state.LeaseToken = ""
		state.LeaseUntil = nil
		state.Attempts = 0
		state.LastError = ""
		return &state
	}
	if state.Candidate == nil || !equalBillingMultiplier(*state.Candidate, observed) {
		state.Candidate = upstreamRateNotifyFloatPtr(observed)
		state.ChangedAt = upstreamRateNotifyTimePtr(at)
		state.SentAt = nil
		state.RetryAt = nil
		state.LeaseToken = ""
		state.LeaseUntil = nil
		state.Attempts = 0
		state.LastError = ""
	}
	return &state
}

func upstreamRateNotifySourceHash(account *Account) string {
	if account == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(account.GetCredential("base_url") + "\x00" + account.GetCredential("api_key")))
	return hex.EncodeToString(sum[:])
}

func upstreamRateNotifyTimePtr(value time.Time) *time.Time { return &value }
func upstreamRateNotifyFloatPtr(value float64) *float64    { return &value }

func upstreamRateNotificationReady(state *UpstreamRateNotification, now time.Time) bool {
	return state != nil && state.Candidate != nil && state.ChangedAt != nil &&
		state.LastObservedAt != nil && state.SentAt == nil &&
		!now.Before(state.ChangedAt.Add(upstreamRateNotifyDelay)) &&
		!state.LastObservedAt.Before(state.ChangedAt.Add(upstreamRateNotifyDelay)) &&
		(state.RetryAt == nil || !now.Before(*state.RetryAt)) &&
		(state.LeaseUntil == nil || !now.Before(*state.LeaseUntil))
}

func upstreamRateNotifyObserved(snapshot *UpstreamBillingProbeSnapshot) (float64, bool) {
	if snapshot == nil || snapshot.Data == nil || snapshot.ReceivedAt == nil {
		return 0, false
	}
	if snapshot.Status != UpstreamBillingProbeStatusOK {
		if snapshot.Status != UpstreamBillingProbeStatusWaitingConsensus ||
			snapshot.MultiplierMatchCount < upstreamRateNotifyConsensusRequired(snapshot.Data) {
			return 0, false
		}
	}
	value, ok := resolveAccountExtraNumber(snapshot.Data, "resolved_rate_multiplier")
	return value, ok && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func upstreamRateNotifyConsensusRequired(data map[string]any) int {
	value, ok := resolveAccountExtraNumber(data, "consensus_required")
	if !ok || value < 1 || value > 100 {
		return upstreamBillingProbeDefaultConsensusCount
	}
	return int(value)
}

// prepareRateNotification runs before the snapshot CAS. Notification errors
// never prevent the upstream multiplier from being written back.
func (r *rateNotifyAccountRepository) prepareRateNotification(ctx context.Context, account *Account, snapshot *UpstreamBillingProbeSnapshot) {
	if account == nil || snapshot == nil {
		return
	}
	previous := decodeUpstreamBillingProbeSnapshot(account.Extra)
	if previous != nil {
		snapshot.RateNotification = previous.RateNotification
	}
	settings, err := r.getSettings(ctx)
	if err != nil {
		slog.Error("upstream_rate_notification_settings_failed", "account_id", account.ID, "error", err)
		return
	}
	if !settings.RateChangeNotifyEnabled || settings.RateChangeNotifyEmail == "" || !upstreamBillingRateSyncEnabled(account) {
		snapshot.RateNotification = nil
		return
	}
	observed, ok := upstreamRateNotifyObserved(snapshot)
	if !ok {
		return
	}
	state := advanceUpstreamRateNotification(snapshot.RateNotification, settings.RateChangeNotifyEpoch,
		settings.RateChangeNotifyEmail, upstreamRateNotifySourceHash(account), account.BillingRateMultiplier(), observed, snapshot.LastAttemptAt)
	snapshot.RateNotification = state
	if state.Candidate != nil && state.SentAt == nil && state.ChangedAt != nil {
		due := state.ChangedAt.Add(upstreamRateNotifyDelay)
		if state.RetryAt != nil && state.RetryAt.After(due) {
			due = *state.RetryAt
		}
		if !due.After(snapshot.LastAttemptAt) {
			due = snapshot.LastAttemptAt.Add(upstreamRateNotifyRetryDelay)
		}
		if snapshot.NextProbeAt.IsZero() || due.Before(snapshot.NextProbeAt) {
			snapshot.NextProbeAt = due
		}
	}
}

// deliverRateNotification uses a persisted lease to suppress ordinary repeats
// across instances. A crash between SMTP acceptance and recording success can
// still yield an at-least-once retry, as SMTP has no transaction with the DB.
func (r *rateNotifyAccountRepository) deliverRateNotification(ctx context.Context, accountID int64) {
	if r.sender == nil || r.AccountRepository == nil {
		return
	}
	account, err := r.AccountRepository.GetByID(ctx, accountID)
	if err != nil || !upstreamBillingRateSyncEnabled(account) {
		return
	}
	settings, err := r.getSettings(ctx)
	if err != nil || !settings.RateChangeNotifyEnabled {
		return
	}
	snapshot := decodeUpstreamBillingProbeSnapshot(account.Extra)
	if snapshot == nil || snapshot.RateNotification == nil ||
		snapshot.RateNotification.Epoch != settings.RateChangeNotifyEpoch ||
		snapshot.RateNotification.Recipient != settings.RateChangeNotifyEmail {
		return
	}
	now := r.currentTime()
	state := snapshot.RateNotification
	if !upstreamRateNotificationReady(state, now) {
		return
	}
	// Only a confirmed fresh probe may trigger delivery. Failed probes preserve
	// the pending state but cannot assert that the changed rate still holds.
	observed, ok := upstreamRateNotifyObserved(snapshot)
	if !ok || !equalBillingMultiplier(observed, *state.Candidate) ||
		snapshot.LastAttemptAt.Before(state.ChangedAt.Add(upstreamRateNotifyDelay)) {
		return
	}
	token := uuid.NewString()
	state.LeaseToken = token
	state.LeaseUntil = upstreamRateNotifyTimePtr(now.Add(upstreamRateNotifyLease))
	state.Attempts++
	writer, ok := r.AccountRepository.(upstreamBillingProbeSnapshotWriter)
	if !ok {
		return
	}
	if err := writer.UpdateUpstreamBillingProbeSnapshot(ctx, account, snapshot, nil); err != nil {
		slog.Warn("upstream_rate_notification_claim_failed", "account_id", accountID, "error", err)
		return
	}
	freshSettings, err := r.getSettings(ctx)
	if err != nil {
		slog.Error("upstream_rate_notification_settings_recheck_failed", "account_id", accountID, "error", err)
		return
	}
	if !freshSettings.RateChangeNotifyEnabled || freshSettings.RateChangeNotifyEpoch != state.Epoch || freshSettings.RateChangeNotifyEmail != state.Recipient {
		return
	}
	subject, body := buildUpstreamRateNotificationEmail(account, state, now)
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	sendErr := r.sender.SendEmail(sendCtx, state.Recipient, subject, body)
	cancel()
	if sendErr != nil {
		slog.Error("upstream_rate_notification_send_failed", "account_id", accountID,
			"recipient", state.Recipient, "attempt", state.Attempts, "error", sendErr)
	}
	latest, err := r.AccountRepository.GetByID(ctx, accountID)
	if err != nil {
		slog.Error("upstream_rate_notification_state_reload_failed", "account_id", accountID, "error", err)
		return
	}
	current := decodeUpstreamBillingProbeSnapshot(latest.Extra)
	if current == nil || current.RateNotification == nil || current.RateNotification.LeaseToken != token {
		return
	}
	currentState := current.RateNotification
	currentState.LeaseToken = ""
	currentState.LeaseUntil = nil
	if sendErr == nil {
		currentState.SentAt = upstreamRateNotifyTimePtr(r.currentTime())
		currentState.RetryAt = nil
		currentState.LastError = ""
		slog.Info("upstream_rate_notification_sent", "account_id", accountID, "recipient", currentState.Recipient,
			"baseline", currentState.Baseline, "current", *currentState.Candidate)
	} else {
		currentState.RetryAt = upstreamRateNotifyTimePtr(r.currentTime().Add(upstreamRateNotifyRetryDelay))
		currentState.LastError = truncateUpstreamRateNotifyError(sendErr.Error())
		if current.NextProbeAt.After(*currentState.RetryAt) {
			current.NextProbeAt = *currentState.RetryAt
		}
	}
	if err := writer.UpdateUpstreamBillingProbeSnapshot(ctx, latest, current, nil); err != nil {
		slog.Error("upstream_rate_notification_state_save_failed", "account_id", accountID, "error", err)
	}
}

func truncateUpstreamRateNotifyError(value string) string {
	if len(value) > 300 {
		return value[:300]
	}
	return value
}

func buildUpstreamRateNotificationEmail(account *Account, state *UpstreamRateNotification, detectedAt time.Time) (string, string) {
	groupLines := make([]string, 0, len(account.Groups))
	for _, group := range account.Groups {
		if group != nil {
			groupLines = append(groupLines, fmt.Sprintf("%s (当前倍率 %s)", group.Name, formatUpstreamRate(group.RateMultiplier)))
		}
	}
	sort.Strings(groupLines)
	if len(groupLines) == 0 {
		groupLines = append(groupLines, "未关联分组")
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	source := "API Key 导入：" + baseURL
	if binding, ok := account.Extra["provider_binding"].(map[string]any); ok {
		if name, ok := binding["provider_name"].(string); ok && strings.TrimSpace(name) != "" {
			source = "服务商导入：" + strings.TrimSpace(name) + " (baseURL: " + baseURL + ")"
		}
	}
	safeSubjectName := strings.NewReplacer("\r", " ", "\n", " ").Replace(account.Name)
	subject := fmt.Sprintf("上游倍率持续变化：%s %s → %s", safeSubjectName, formatUpstreamRate(state.Baseline), formatUpstreamRate(*state.Candidate))
	lines := []string{
		"对应分组：" + strings.Join(groupLines, "、"),
		fmt.Sprintf("对应账号：%s (ID %d)", account.Name, account.ID),
		fmt.Sprintf("上游声明倍率：%s → %s", formatUpstreamRate(state.Baseline), formatUpstreamRate(*state.Candidate)),
		"站点来源：" + source,
		"检测时间：" + detectedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		"开始变化：" + state.ChangedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		"持续时长：" + detectedAt.Sub(*state.ChangedAt).Round(time.Second).String(),
	}
	var body strings.Builder
	body.WriteString("<p>上游声明倍率持续变化，请核对以下账号：</p><ul>")
	for _, line := range lines {
		body.WriteString("<li>")
		body.WriteString(html.EscapeString(line))
		body.WriteString("</li>")
	}
	body.WriteString("</ul>")
	return subject, body.String()
}

func formatUpstreamRate(rate float64) string { return strconv.FormatFloat(rate, 'f', -1, 64) }
