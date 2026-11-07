package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type upstreamRateNotifySenderStub struct {
	to       []string
	subjects []string
	bodies   []string
	failNext bool
}

func (s *upstreamRateNotifySenderStub) SendEmail(_ context.Context, to, subject, body string) error {
	s.to = append(s.to, to)
	s.subjects = append(s.subjects, subject)
	s.bodies = append(s.bodies, body)
	if s.failNext {
		s.failNext = false
		return errors.New("smtp temporarily unavailable")
	}
	return nil
}

func rateNotifyTestSnapshot(rate float64, at time.Time) *UpstreamBillingProbeSnapshot {
	return &UpstreamBillingProbeSnapshot{
		Status:        UpstreamBillingProbeStatusOK,
		Protocol:      UpstreamBillingProtocolSub2API,
		Data:          map[string]any{"resolved_rate_multiplier": rate},
		ReceivedAt:    upstreamRateNotifyTimePtr(at),
		LastAttemptAt: at,
		NextProbeAt:   at.Add(30 * time.Minute),
	}
}

func newRateNotifyTestService(t *testing.T, enabled bool) (*UpstreamBillingProbeService, *upstreamBillingProbeAccountRepo, *upstreamRateNotifySenderStub, *time.Time) {
	t.Helper()
	baseline := 1.0
	account := &Account{
		ID: 17, Name: "Key A", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials:    map[string]any{"base_url": "https://key.example/v1", "api_key": "secret"},
		Extra:          map[string]any{UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingRateSyncEnabledExtraKey: enabled},
		RateMultiplier: &baseline,
		Groups:         []*Group{{Name: "主分组", RateMultiplier: 1.25}},
	}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	settingsRepo := &upstreamBillingProbeSettingRepo{values: map[string]string{
		SettingKeyUpstreamBillingProbeSettings: `{"enabled":true,"interval_seconds":1800,"newapi_multiplier_consensus_count":3,"rate_change_notify_enabled":true,"rate_change_notify_email":"notice@example.com","rate_change_notify_epoch":"epoch-1"}`,
	}}
	settingService := NewSettingService(settingsRepo, &config.Config{})
	sender := &upstreamRateNotifySenderStub{}
	now := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	wrapped := &rateNotifyAccountRepository{AccountRepository: repo, settingService: settingService, sender: sender, now: func() time.Time { return now }}
	svc := NewUpstreamBillingProbeService(wrapped, nil, settingService)
	svc.now = func() time.Time { return now }
	return svc, repo, sender, &now
}

func observeRateNotify(t *testing.T, svc *UpstreamBillingProbeService, repo *upstreamBillingProbeAccountRepo, at time.Time, rate float64) *UpstreamBillingProbeSnapshot {
	t.Helper()
	account, err := repo.GetByID(context.Background(), 17)
	require.NoError(t, err)
	snapshot := rateNotifyTestSnapshot(rate, at)
	require.NoError(t, svc.updateSnapshot(context.Background(), account, snapshot, &rate))
	latest, err := repo.GetByID(context.Background(), 17)
	require.NoError(t, err)
	return decodeUpstreamBillingProbeSnapshot(latest.Extra)
}

func TestUpstreamRateNotificationIgnoresAccountsWithoutSync(t *testing.T) {
	svc, repo, sender, now := newRateNotifyTestService(t, false)
	snapshot := observeRateNotify(t, svc, repo, *now, 2)
	require.Nil(t, snapshot.RateNotification)
	*now = now.Add(6 * time.Minute)
	observeRateNotify(t, svc, repo, *now, 2)
	require.Empty(t, sender.to)
}

func TestUpstreamRateNotificationWaitsRecoversAndDeduplicates(t *testing.T) {
	svc, repo, sender, now := newRateNotifyTestService(t, true)
	first := observeRateNotify(t, svc, repo, *now, 2)
	require.Equal(t, 1.0, first.RateNotification.Baseline)
	require.Equal(t, now.Add(5*time.Minute), first.NextProbeAt)
	require.Empty(t, sender.to)
	*now = now.Add(4 * time.Minute)
	observeRateNotify(t, svc, repo, *now, 2)
	require.Empty(t, sender.to)
	*now = now.Add(time.Minute)
	sent := observeRateNotify(t, svc, repo, *now, 2)
	require.Len(t, sender.to, 1)
	require.NotNil(t, sent.RateNotification.SentAt)
	*now = now.Add(time.Minute)
	observeRateNotify(t, svc, repo, *now, 2)
	require.Len(t, sender.to, 1)
	*now = now.Add(time.Minute)
	recovered := observeRateNotify(t, svc, repo, *now, 1)
	require.Nil(t, recovered.RateNotification.Candidate)
	*now = now.Add(time.Minute)
	observeRateNotify(t, svc, repo, *now, 2)
	*now = now.Add(5 * time.Minute)
	observeRateNotify(t, svc, repo, *now, 2)
	require.Len(t, sender.to, 2)
}

func TestUpstreamRateNotificationSecondChangeRestartsTimer(t *testing.T) {
	svc, repo, sender, now := newRateNotifyTestService(t, true)
	observeRateNotify(t, svc, repo, *now, 2)
	*now = now.Add(3 * time.Minute)
	changed := observeRateNotify(t, svc, repo, *now, 3)
	require.Equal(t, *now, *changed.RateNotification.ChangedAt)
	*now = now.Add(2 * time.Minute)
	observeRateNotify(t, svc, repo, *now, 3)
	require.Empty(t, sender.to)
	*now = now.Add(3 * time.Minute)
	observeRateNotify(t, svc, repo, *now, 3)
	require.Len(t, sender.to, 1)
	require.Contains(t, sender.subjects[0], "1 → 3")
}

func TestUpstreamRateNotificationRetriesFailedSend(t *testing.T) {
	svc, repo, sender, now := newRateNotifyTestService(t, true)
	sender.failNext = true
	observeRateNotify(t, svc, repo, *now, 2)
	*now = now.Add(5 * time.Minute)
	failed := observeRateNotify(t, svc, repo, *now, 2)
	require.Len(t, sender.to, 1)
	require.Nil(t, failed.RateNotification.SentAt)
	require.Equal(t, "smtp temporarily unavailable", failed.RateNotification.LastError)
	require.Equal(t, now.Add(time.Minute), *failed.RateNotification.RetryAt)
	*now = now.Add(30 * time.Second)
	observeRateNotify(t, svc, repo, *now, 2)
	require.Len(t, sender.to, 1)
	*now = now.Add(30 * time.Second)
	retried := observeRateNotify(t, svc, repo, *now, 2)
	require.Len(t, sender.to, 2)
	require.NotNil(t, retried.RateNotification.SentAt)
	require.Empty(t, retried.RateNotification.LastError)
}

func TestUpstreamRateNotificationDisableAndRecipientChangeResetBaseline(t *testing.T) {
	svc, repo, sender, now := newRateNotifyTestService(t, true)
	observeRateNotify(t, svc, repo, *now, 2)
	settingsRepo, ok := svc.settingService.settingRepo.(*upstreamBillingProbeSettingRepo)
	if !ok {
		t.Fatal("unexpected settings repository type")
	}
	settingsRepo.values[SettingKeyUpstreamBillingProbeSettings] = `{"enabled":true,"interval_seconds":1800,"rate_change_notify_enabled":false,"rate_change_notify_email":"notice@example.com","rate_change_notify_epoch":"epoch-2"}`
	*now = now.Add(5 * time.Minute)
	disabled := observeRateNotify(t, svc, repo, *now, 2)
	require.Nil(t, disabled.RateNotification)
	require.Empty(t, sender.to)
	settingsRepo.values[SettingKeyUpstreamBillingProbeSettings] = `{"enabled":true,"interval_seconds":1800,"rate_change_notify_enabled":true,"rate_change_notify_email":"other@example.com","rate_change_notify_epoch":"epoch-3"}`
	*now = now.Add(time.Minute)
	reenabled := observeRateNotify(t, svc, repo, *now, 2)
	require.Equal(t, 2.0, reenabled.RateNotification.Baseline)
	require.Nil(t, reenabled.RateNotification.Candidate)
	*now = now.Add(time.Minute)
	observeRateNotify(t, svc, repo, *now, 3)
	*now = now.Add(5 * time.Minute)
	observeRateNotify(t, svc, repo, *now, 3)
	require.Equal(t, []string{"other@example.com"}, sender.to)
}

func TestUpstreamRateNotificationSurvivesServiceRestart(t *testing.T) {
	svc, repo, _, now := newRateNotifyTestService(t, true)
	first := observeRateNotify(t, svc, repo, *now, 2)
	require.NotNil(t, first.RateNotification.ChangedAt)
	*now = now.Add(5 * time.Minute)
	sender := &upstreamRateNotifySenderStub{}
	newService := NewUpstreamBillingProbeService(&rateNotifyAccountRepository{
		AccountRepository: repo, settingService: svc.settingService, sender: sender,
		now: func() time.Time { return *now },
	}, nil, svc.settingService)
	newService.now = func() time.Time { return *now }
	result := observeRateNotify(t, newService, repo, *now, 2)
	require.Len(t, sender.to, 1)
	require.Equal(t, 1.0, result.RateNotification.Baseline)
	require.Equal(t, first.RateNotification.ChangedAt, result.RateNotification.ChangedAt)
}

func TestUpstreamRateNotificationEmailFields(t *testing.T) {
	start := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	rate := 0.8
	state := &UpstreamRateNotification{Baseline: 1.2, Candidate: &rate, ChangedAt: &start}
	account := &Account{ID: 41, Name: "A <B>", Credentials: map[string]any{"base_url": "https://provider.example/v1"},
		Extra:  map[string]any{"provider_binding": map[string]any{"provider_name": "服务商 <甲>"}},
		Groups: []*Group{{Name: "分组一", RateMultiplier: 1.5}}}
	subject, body := buildUpstreamRateNotificationEmail(account, state, start.Add(5*time.Minute))
	for _, field := range []string{"分组一", "1.5", "A &lt;B&gt;", "1.2 → 0.8", "服务商 &lt;甲&gt;", "https://provider.example/v1", "2026-11-07 01:05:00 UTC", "5m0s"} {
		require.Contains(t, body, field)
	}
	require.Contains(t, subject, "1.2 → 0.8")
	require.NotContains(t, body, "<甲>")
	account.Extra = nil
	_, body = buildUpstreamRateNotificationEmail(account, state, start.Add(5*time.Minute))
	require.Contains(t, body, "API Key 导入：https://provider.example/v1")
	require.False(t, strings.Contains(body, "secret"))
}

func TestUpstreamRateNotificationAcceptsConfirmedNewAPIZeroDeclaration(t *testing.T) {
	at := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	snapshot := &UpstreamBillingProbeSnapshot{
		Status: UpstreamBillingProbeStatusWaitingConsensus, Protocol: UpstreamBillingProtocolNewAPI,
		ReceivedAt: &at, MultiplierMatchCount: 3,
		Data: map[string]any{"resolved_rate_multiplier": float64(0), "consensus_required": 3},
	}
	rate, ok := upstreamRateNotifyObserved(snapshot)
	require.True(t, ok)
	require.Zero(t, rate)
	snapshot.MultiplierMatchCount = 2
	_, ok = upstreamRateNotifyObserved(snapshot)
	require.False(t, ok)
}

func TestUpstreamRateNotificationRunsAfterRealSub2APIProbe(t *testing.T) {
	initial := 1.0
	account := &Account{
		ID: 99, Name: "real probe", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Concurrency: 1, Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://upstream.example/v1"},
		Extra:          map[string]any{UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingRateSyncEnabledExtraKey: true},
		RateMultiplier: &initial,
	}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	settingsRepo := &upstreamBillingProbeSettingRepo{values: map[string]string{
		SettingKeyUpstreamBillingProbeSettings: `{"enabled":true,"interval_seconds":1800,"rate_change_notify_enabled":true,"rate_change_notify_email":"notice@example.com","rate_change_notify_epoch":"epoch-1"}`,
	}}
	settings := NewSettingService(settingsRepo, &config.Config{})
	sender := &upstreamRateNotifySenderStub{}
	now := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	wrapped := &rateNotifyAccountRepository{AccountRepository: repo, settingService: settings, sender: sender, now: func() time.Time { return now }}
	svc := newUpstreamBillingProbeTestService(wrapped, &upstreamBillingProbeHTTPStub{}, settingsRepo)
	svc.now = func() time.Time { return now }
	first, err := svc.ProbeAccount(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, first.Status)
	require.Equal(t, 0.8, *account.RateMultiplier)
	require.Empty(t, sender.to)
	now = now.Add(5 * time.Minute)
	_, err = svc.ProbeAccount(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"notice@example.com"}, sender.to)
}

func TestUpstreamRateNotificationSettingsValidateEmailAndRotateEpoch(t *testing.T) {
	repo := &upstreamBillingProbeSettingRepo{}
	svc := NewSettingService(repo, &config.Config{})
	ctx := context.Background()
	settings := &UpstreamBillingProbeSettings{
		Enabled: true, IntervalSeconds: 60, NewAPIMultiplierConsensusCount: 3,
		RateChangeNotifyEnabled: true, RateChangeNotifyEmail: "bad\n@example.com",
	}
	require.Error(t, svc.SetUpstreamBillingProbeSettings(ctx, settings))
	settings.RateChangeNotifyEmail = "first@example.com"
	require.NoError(t, svc.SetUpstreamBillingProbeSettings(ctx, settings))
	first, err := svc.GetUpstreamBillingProbeSettings(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, first.RateChangeNotifyEpoch)
	require.Equal(t, "first@example.com", first.RateChangeNotifyEmail)
	require.NoError(t, svc.SetUpstreamBillingProbeSettings(ctx, first))
	unchanged, err := svc.GetUpstreamBillingProbeSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, first.RateChangeNotifyEpoch, unchanged.RateChangeNotifyEpoch)
	unchanged.RateChangeNotifyEmail = "second@example.com"
	require.NoError(t, svc.SetUpstreamBillingProbeSettings(ctx, unchanged))
	changed, err := svc.GetUpstreamBillingProbeSettings(ctx)
	require.NoError(t, err)
	require.NotEqual(t, first.RateChangeNotifyEpoch, changed.RateChangeNotifyEpoch)
	beforeDisable := changed.RateChangeNotifyEpoch
	changed.RateChangeNotifyEnabled = false
	require.NoError(t, svc.SetUpstreamBillingProbeSettings(ctx, changed))
	disabled, err := svc.GetUpstreamBillingProbeSettings(ctx)
	require.NoError(t, err)
	require.NotEqual(t, beforeDisable, disabled.RateChangeNotifyEpoch)
}
