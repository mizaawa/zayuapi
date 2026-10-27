package admin

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type providerAlertMailer interface {
	SendEmail(context.Context, string, string, string) error
}

const providerRefreshConcurrency = 8

type providerProbeResult struct {
	original providerRecord
	updated  providerRecord
	err      error
}

// updateCachedProviderWarning reflects a manual probe immediately in the
// visible badge. Scheduled refreshes remain responsible for sending email.
func (h *AccountHandler) updateCachedProviderWarning(ctx context.Context, record *providerRecord) {
	settings, err := h.providerSettings.GetAllSettings(ctx)
	if err != nil {
		slog.Warn("load provider warning settings", "error", err)
		return
	}
	updateProviderLowBalanceAlert(ctx, record, settings, nil)
}

// RunProviderRefreshLoop owns provider polling independently of browser tabs.
// The 30-second heartbeat notices changes to the configured interval without
// requiring a restart; each provider retains its most recently observed data.
func (h *AccountHandler) RunProviderRefreshLoop(ctx context.Context, mailer *service.EmailService) {
	if h == nil || h.providerSettings == nil || h.providerEncryptor == nil {
		return
	}
	lastRefresh := time.Time{}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		settings, err := h.providerSettings.GetAllSettings(ctx)
		if err != nil {
			slog.Warn("load provider refresh settings", "error", err)
		} else if lastRefresh.IsZero() || time.Since(lastRefresh) >= time.Duration(settings.ProviderRefreshIntervalSeconds)*time.Second {
			h.refreshProviderBalances(ctx, settings, mailer, h.probeRecordContext)
			lastRefresh = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *AccountHandler) refreshProviderBalances(ctx context.Context, settings *service.SystemSettings, mailer providerAlertMailer, probe func(context.Context, *providerRecord, string) error) {
	var snapshot []providerRecord
	if err := h.providerSettings.LoadProviderRecords(ctx, &snapshot); err != nil {
		slog.Warn("load providers for automatic refresh", "error", err)
		return
	}
	// Probe remote sites concurrently, but serialize changes to the single
	// settings record. A slow provider must not hold up every other balance.
	results := make(chan providerProbeResult, len(snapshot))
	permits := make(chan struct{}, providerRefreshConcurrency)
	var probes sync.WaitGroup
	for _, original := range snapshot {
		probes.Add(1)
		go func(original providerRecord) {
			defer probes.Done()
			select {
			case permits <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-permits }()
			updated := original
			probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			probeErr := probe(probeCtx, &updated, "")
			cancel()
			if probeErr != nil {
				updated.Status = "disconnected"
				updated.LastError = providerPublicError(probeErr)
				// A remote failure does not invalidate the last known balance or
				// start a new alert episode.
			}
			results <- providerProbeResult{original: original, updated: updated, err: probeErr}
		}(original)
	}
	go func() {
		probes.Wait()
		close(results)
	}()
	for result := range results {
		if ctx.Err() != nil {
			return
		}
		var pendingAlert *providerRecord
		providerWriteMu.Lock()
		var current []providerRecord
		if err := h.providerSettings.LoadProviderRecords(ctx, &current); err != nil {
			providerWriteMu.Unlock()
			slog.Warn("reload provider before automatic refresh", "error", err)
			continue
		}
		for i := range current {
			if current[i].ID != result.original.ID {
				continue
			}
			if !sameProviderIdentity(current[i], result.original) {
				break // An admin edited this identity while the remote request ran.
			}
			if result.err == nil {
				current[i].Metrics = result.updated.Metrics
				current[i].SecretEncrypted = result.updated.SecretEncrypted
				current[i].UpdatedAt = result.updated.UpdatedAt
			}
			current[i].Status = result.updated.Status
			current[i].LastError = result.updated.LastError
			if result.err == nil {
				// Persist the observed balance and badge before attempting email.
				// SMTP can be slow; it must not hold the provider write lock.
				updateProviderLowBalanceAlert(ctx, &current[i], settings, nil)
				if current[i].LowBalanceAlert && !current[i].AlertSent {
					alert := current[i]
					pendingAlert = &alert
				}
			}
			if err := h.providerSettings.SaveProviderRecords(ctx, current); err != nil {
				slog.Warn("save automatically refreshed provider", "error", err)
				pendingAlert = nil
			}
			break
		}
		providerWriteMu.Unlock()
		if pendingAlert != nil {
			h.sendProviderLowBalanceAlert(ctx, pendingAlert, mailer)
		}
	}
}

// sameProviderIdentity reports whether a record still points at the same
// upstream account and credential as the snapshot that was probed.
func sameProviderIdentity(a, b providerRecord) bool {
	return a.SecretEncrypted == b.SecretEncrypted && a.BaseURL == b.BaseURL && a.Account == b.Account &&
		a.SiteType == b.SiteType && a.AuthMode == b.AuthMode && a.UserID == b.UserID
}

func (h *AccountHandler) sendProviderLowBalanceAlert(ctx context.Context, alert *providerRecord, mailer providerAlertMailer) {
	if mailer == nil || ctx.Err() != nil {
		return
	}
	// Settings and suppression can change while the remote balance is probed.
	settings, err := h.providerSettings.GetAllSettings(ctx)
	if err != nil || !settings.ProviderLowBalanceEnabled || settings.ProviderLowBalanceEmail == "" {
		return
	}
	providerWriteMu.Lock()
	var records []providerRecord
	err = h.providerSettings.LoadProviderRecords(ctx, &records)
	allowed := false
	if err == nil {
		for _, record := range records {
			if record.ID == alert.ID && record.LowBalanceAlert && !record.AlertSent && !record.SuppressLowBalanceAlert && record.Metrics.Balance != nil && *record.Metrics.Balance < settings.ProviderLowBalanceThreshold {
				allowed = true
				break
			}
		}
	}
	providerWriteMu.Unlock()
	if err != nil || !allowed {
		return
	}
	updateProviderLowBalanceAlert(ctx, alert, settings, mailer)
	if !alert.AlertSent {
		return
	}
	providerWriteMu.Lock()
	defer providerWriteMu.Unlock()
	if err := h.providerSettings.LoadProviderRecords(ctx, &records); err != nil {
		slog.Warn("reload provider after low-balance email", "error", err)
		return
	}
	for i := range records {
		if records[i].ID == alert.ID && records[i].LowBalanceAlert && !records[i].SuppressLowBalanceAlert && !records[i].AlertSent {
			records[i].AlertSent = true
			if err := h.providerSettings.SaveProviderRecords(ctx, records); err != nil {
				slog.Warn("save provider low-balance email state", "error", err)
			}
			return
		}
	}
}

// AlertSent is persisted with each provider, so reloads and process restarts
// cannot resend a successfully delivered warning until balance recovers.
func updateProviderLowBalanceAlert(ctx context.Context, record *providerRecord, settings *service.SystemSettings, mailer providerAlertMailer) {
	if record.SuppressLowBalanceAlert || record.Metrics.Balance == nil {
		record.LowBalanceAlert = false
		if record.SuppressLowBalanceAlert {
			record.AlertSent = false
		}
		return
	}
	// Provider probes normalize balances to USD. The configured warning
	// threshold is also in USD; the exchange rate is only for the ¥ display.
	amount := *record.Metrics.Balance
	if amount >= settings.ProviderLowBalanceThreshold {
		record.LowBalanceAlert = false
		record.AlertSent = false
		return
	}
	record.LowBalanceAlert = true
	if !settings.ProviderLowBalanceEnabled || record.AlertSent || mailer == nil || settings.ProviderLowBalanceEmail == "" {
		return
	}
	subject := "上游余额即将告罄"
	body := fmt.Sprintf("<p>服务提供商 <strong>%s</strong> 的余额为 $%.2f，低于预设金额 $%.2f。</p>", html.EscapeString(record.Name), amount, settings.ProviderLowBalanceThreshold)
	if err := mailer.SendEmail(ctx, settings.ProviderLowBalanceEmail, subject, body); err != nil {
		slog.Warn("send provider low-balance email", "provider_id", record.ID, "error", err)
		return
	}
	record.AlertSent = true
}
