//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestVideoWorkbenchSettingsDefaultsAndRoundtrip(t *testing.T) {
	repo := &settingPublicRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	p := svc.GetVideoWorkbenchRuntime(context.Background())
	require.False(t, p.Enabled)
	require.Equal(t, 3, p.MaxConcurrent)
	require.Equal(t, 6, p.MaxReferenceImages)
	require.Equal(t, 6, p.MaxReferenceVideos)
	require.Equal(t, 6, p.MaxReferenceAudios)
	require.Equal(t, 30*time.Minute, p.Retention())
	require.Empty(t, p.TutorialURL)
	require.Empty(t, p.DocumentationURL)
	repo.values = map[string]string{SettingKeyVideoWorkbenchEnabled: "true", SettingKeyVideoWorkbenchMaxConcurrent: "5", SettingKeyVideoWorkbenchMaxReferenceVideos: "2", SettingKeyVideoWorkbenchRetentionMinutes: "60", SettingKeyVideoWorkbenchTutorialURL: "https://docs.example/video", SettingKeyVideoWorkbenchDocumentationURL: "javascript:alert(1)"}
	p = svc.GetVideoWorkbenchRuntime(context.Background())
	require.True(t, p.Enabled)
	require.Equal(t, 5, p.MaxConcurrent)
	require.Equal(t, 2, p.MaxReferenceVideos)
	require.Equal(t, time.Hour, p.Retention())
	require.Empty(t, p.DocumentationURL)
	public, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.True(t, public.VideoWorkbenchEnabled)
	require.Equal(t, 2, public.VideoWorkbenchMaxReferenceVideos)
	injection, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	require.True(t, injection.(*PublicSettingsInjectionPayload).VideoWorkbenchEnabled)
	repo.err = errors.New("unavailable")
	require.False(t, svc.GetVideoWorkbenchRuntime(context.Background()).Enabled)
	for _, invalid := range []*SystemSettings{{VideoWorkbenchMaxConcurrent: 101}, {VideoWorkbenchRetentionMinutes: -1}, {VideoWorkbenchMaxReferenceAudios: -1}, {VideoWorkbenchTutorialURL: "javascript:alert(1)"}, {VideoWorkbenchDocumentationURL: "https://user:secret@example.com"}} {
		require.Error(t, ValidateVideoWorkbenchSettings(invalid))
	}
}

func TestVideoWorkbenchCustomBillingPreservesLongDurations(t *testing.T) {
	price := 0.1
	billing := &BillingService{}
	for _, seconds := range []int{5, 10, 30, 60, 123} {
		info := normalizeCustomVideoRequestInfo(ParseGrokMediaRequest("application/json", []byte(`{"model":"grok-imagine-video","duration":`+strconv.Itoa(seconds)+`}`)))
		require.Equal(t, seconds, info.DurationSeconds)
		usage := grokMediaUsageFromResponse(GrokMediaEndpointVideosGenerations, info, []byte(`{"id":"task-1"}`))
		require.Equal(t, seconds, usage.VideoDurationSeconds)
		cost := billing.CalculateCustomVideoCost("custom-video", "480p", 1, seconds, &VideoPriceConfig{Price480P: &price}, 2)
		require.InDelta(t, float64(seconds)*price*2, cost.ActualCost, 1e-12)
	}
	legacy := billing.CalculateVideoCost("grok-imagine-video", "480p", 1, 60, &VideoPriceConfig{Price480P: &price}, 1)
	require.InDelta(t, 1.5, legacy.ActualCost, 1e-12, "Grok retains its existing duration policy")
}
