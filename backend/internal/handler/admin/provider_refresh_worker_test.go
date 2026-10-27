package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type providerRefreshTestRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *providerRefreshTestRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}
func (r *providerRefreshTestRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}
func (r *providerRefreshTestRepo) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

type providerRefreshTestMailer struct {
	to   []string
	fail bool
}

type providerRefreshBlockingMailer struct {
	started chan struct{}
	release chan struct{}
}

func (m *providerRefreshBlockingMailer) SendEmail(context.Context, string, string, string) error {
	close(m.started)
	<-m.release
	return nil
}

func (m *providerRefreshTestMailer) SendEmail(_ context.Context, to, subject, body string) error {
	if m.fail {
		return errors.New("SMTP unavailable")
	}
	m.to = append(m.to, to)
	if !strings.Contains(subject, "余额") || !strings.Contains(body, "300") {
		return fmt.Errorf("unexpected notification content")
	}
	return nil
}

func TestProviderBackgroundRefreshAlertsOncePerLowEpisodeAndRespectsSuppression(t *testing.T) {
	repo := &providerRefreshTestRepo{values: map[string]string{
		service.SettingKeyProviderLowBalanceEnabled: "true", service.SettingKeyProviderLowBalanceThreshold: "300", service.SettingKeyProviderLowBalanceEmail: "ops@example.com",
	}}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, &config.Config{}), providerTestEncryptor{})
	seed := []providerRecord{{ID: "a", Name: "Vendor A", BaseURL: "https://example.com", ExchangeRate: 1, SecretEncrypted: "cipher"}}
	require.NoError(t, h.providerSettings.SaveProviderRecords(context.Background(), seed))
	settings := &service.SystemSettings{ProviderLowBalanceEnabled: true, ProviderLowBalanceThreshold: 300, ProviderLowBalanceEmail: "ops@example.com"}
	mailer := &providerRefreshTestMailer{}
	balance := 200.0
	probe := func(_ context.Context, record *providerRecord, _ string) error {
		record.Status = "connected"
		record.Metrics.Balance = &balance
		now := time.Now()
		record.UpdatedAt = &now
		return nil
	}
	read := func() providerRecord {
		var records []providerRecord
		require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
		require.Len(t, records, 1)
		return records[0]
	}

	h.refreshProviderBalances(context.Background(), settings, mailer, probe)
	require.True(t, read().LowBalanceAlert)
	require.True(t, read().AlertSent)
	require.Equal(t, []string{"ops@example.com"}, mailer.to)
	h.refreshProviderBalances(context.Background(), settings, mailer, probe)
	require.Len(t, mailer.to, 1, "a low balance must not send on every refresh")
	balance = 350
	h.refreshProviderBalances(context.Background(), settings, mailer, probe)
	require.False(t, read().LowBalanceAlert)
	require.False(t, read().AlertSent)
	balance = 250
	h.refreshProviderBalances(context.Background(), settings, mailer, probe)
	require.Len(t, mailer.to, 2, "a fresh low episode sends again after recovery")

	record := read()
	record.SuppressLowBalanceAlert = true
	require.NoError(t, h.providerSettings.SaveProviderRecords(context.Background(), []providerRecord{record}))
	h.refreshProviderBalances(context.Background(), settings, mailer, probe)
	require.False(t, read().LowBalanceAlert)
	require.False(t, read().AlertSent)
	require.Len(t, mailer.to, 2)
	public, err := json.Marshal(read().view())
	require.NoError(t, err)
	require.NotContains(t, string(public), "alert_sent")
}

func TestProviderBackgroundRefreshKeepsLastBalanceOnFailureAndRetriesFailedEmail(t *testing.T) {
	repo := &providerRefreshTestRepo{values: map[string]string{
		service.SettingKeyProviderLowBalanceEnabled: "true", service.SettingKeyProviderLowBalanceThreshold: "300", service.SettingKeyProviderLowBalanceEmail: "ops@example.com",
	}}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, &config.Config{}), providerTestEncryptor{})
	old := 200.0
	seed := []providerRecord{{ID: "a", Name: "Vendor A", BaseURL: "https://example.com", ExchangeRate: 1, SecretEncrypted: "cipher", Metrics: providerMetrics{Balance: &old}}}
	require.NoError(t, h.providerSettings.SaveProviderRecords(context.Background(), seed))
	settings := &service.SystemSettings{ProviderLowBalanceEnabled: true, ProviderLowBalanceThreshold: 300, ProviderLowBalanceEmail: "ops@example.com"}
	mailer := &providerRefreshTestMailer{fail: true}
	h.refreshProviderBalances(context.Background(), settings, mailer, func(_ context.Context, _ *providerRecord, _ string) error { return errors.New("remote down") })
	var records []providerRecord
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	require.Equal(t, old, *records[0].Metrics.Balance)
	require.Equal(t, "disconnected", records[0].Status)
	require.Empty(t, mailer.to)
	h.refreshProviderBalances(context.Background(), settings, mailer, func(_ context.Context, record *providerRecord, _ string) error {
		record.Metrics.Balance = &old
		record.Status = "connected"
		return nil
	})
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	require.True(t, records[0].LowBalanceAlert)
	require.False(t, records[0].AlertSent)
	mailer.fail = false
	h.refreshProviderBalances(context.Background(), settings, mailer, func(_ context.Context, record *providerRecord, _ string) error {
		record.Metrics.Balance = &old
		record.Status = "connected"
		return nil
	})
	require.Equal(t, []string{"ops@example.com"}, mailer.to)
}

func TestProviderBackgroundRefreshBoundsConcurrentProbes(t *testing.T) {
	repo := &providerRefreshTestRepo{values: map[string]string{}}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, &config.Config{}), providerTestEncryptor{})
	seed := make([]providerRecord, 12)
	for i := range seed {
		seed[i] = providerRecord{ID: fmt.Sprint(i), Name: fmt.Sprint(i), BaseURL: "https://example.com", ExchangeRate: 1, SecretEncrypted: "cipher"}
	}
	require.NoError(t, h.providerSettings.SaveProviderRecords(context.Background(), seed))
	var active, peak atomic.Int32
	h.refreshProviderBalances(context.Background(), &service.SystemSettings{}, nil, func(_ context.Context, record *providerRecord, _ string) error {
		current := active.Add(1)
		for {
			old := peak.Load()
			if old >= current || peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		balance := 10.0
		record.Metrics.Balance = &balance
		record.Status = "connected"
		return nil
	})
	require.Greater(t, peak.Load(), int32(1))
	require.LessOrEqual(t, peak.Load(), int32(providerRefreshConcurrency))
	var records []providerRecord
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	require.Len(t, records, len(seed))
	for _, record := range records {
		require.Equal(t, 10.0, *record.Metrics.Balance)
	}
}

func TestProviderLowBalanceBadgeDoesNotRequireEmailSwitch(t *testing.T) {
	amount := 200.0
	record := providerRecord{ID: "a", Name: "Vendor", ExchangeRate: 7, Metrics: providerMetrics{Balance: &amount}}
	settings := &service.SystemSettings{ProviderLowBalanceEnabled: false, ProviderLowBalanceThreshold: 300, ProviderLowBalanceEmail: "ops@example.com"}
	mailer := &providerRefreshTestMailer{}
	updateProviderLowBalanceAlert(context.Background(), &record, settings, mailer)
	require.True(t, record.LowBalanceAlert)
	require.False(t, record.AlertSent)
	require.Empty(t, mailer.to)
	settings.ProviderLowBalanceEnabled = true
	updateProviderLowBalanceAlert(context.Background(), &record, settings, mailer)
	require.True(t, record.LowBalanceAlert)
	require.True(t, record.AlertSent)
	require.Equal(t, []string{"ops@example.com"}, mailer.to)
}

func TestManualProviderProbeUpdatesWarningWithoutSendingEmail(t *testing.T) {
	repo := &providerRefreshTestRepo{values: map[string]string{service.SettingKeyProviderLowBalanceThreshold: "300", service.SettingKeyProviderLowBalanceEnabled: "true", service.SettingKeyProviderLowBalanceEmail: "ops@example.com"}}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, &config.Config{}), providerTestEncryptor{})
	amount := 200.0
	record := providerRecord{ExchangeRate: 7, Metrics: providerMetrics{Balance: &amount}}
	h.updateCachedProviderWarning(context.Background(), &record)
	require.True(t, record.LowBalanceAlert)
	require.False(t, record.AlertSent)
	amount = 350
	h.updateCachedProviderWarning(context.Background(), &record)
	require.False(t, record.LowBalanceAlert)
}

func TestProviderBackgroundRefreshDoesNotHoldWriteLockDuringSMTP(t *testing.T) {
	repo := &providerRefreshTestRepo{values: map[string]string{}}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, &config.Config{}), providerTestEncryptor{})
	seed := []providerRecord{{ID: "a", Name: "Vendor", BaseURL: "https://example.com", ExchangeRate: 1, SecretEncrypted: "cipher"}}
	require.NoError(t, h.providerSettings.SaveProviderRecords(context.Background(), seed))
	// The worker reloads the current settings just before sending.
	repo.values[service.SettingKeyProviderLowBalanceEnabled] = "true"
	repo.values[service.SettingKeyProviderLowBalanceThreshold] = "300"
	repo.values[service.SettingKeyProviderLowBalanceEmail] = "ops@example.com"
	settings := &service.SystemSettings{ProviderLowBalanceEnabled: true, ProviderLowBalanceThreshold: 300, ProviderLowBalanceEmail: "ops@example.com"}
	mailer := &providerRefreshBlockingMailer{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.refreshProviderBalances(context.Background(), settings, mailer, func(_ context.Context, record *providerRecord, _ string) error {
			balance := 200.0
			record.Metrics.Balance = &balance
			record.Status = "connected"
			return nil
		})
	}()
	select {
	case <-mailer.started:
	case <-time.After(3 * time.Second):
		close(mailer.release)
		t.Fatal("low-balance email was not attempted")
	}
	lockAvailable := providerWriteMu.TryLock()
	if lockAvailable {
		providerWriteMu.Unlock()
	}
	close(mailer.release)
	<-done
	require.True(t, lockAvailable, "SMTP must not block provider writes")
}
