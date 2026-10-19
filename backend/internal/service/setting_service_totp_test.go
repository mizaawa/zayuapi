//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestIsTotpEnabledDistinguishesMissingSettingFromReadFailure(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		readErr error
		want    bool
		wantErr bool
	}{
		{name: "enabled", value: "true", want: true},
		{name: "disabled", value: "false"},
		{name: "missing defaults disabled", readErr: ErrSettingNotFound},
		{name: "database failure is unavailable", readErr: errors.New("database down"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &bmRepoStub{getValueFn: func(context.Context, string) (string, error) { return tt.value, tt.readErr }}
			enabled, err := NewSettingService(repo, &config.Config{}).IsTotpEnabled(context.Background())
			require.Equal(t, tt.want, enabled)
			if tt.wantErr {
				require.Equal(t, 503, infraerrors.Code(err))
				require.Equal(t, "TOTP_SETTINGS_UNAVAILABLE", infraerrors.Reason(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTurnstileVerificationFailsClosedOnSettingsReadFailure(t *testing.T) {
	repo := &bmRepoStub{getValueFn: func(context.Context, string) (string, error) {
		return "", errors.New("database down")
	}}
	service := NewTurnstileService(NewSettingService(repo, &config.Config{}), nil)
	err := service.VerifyToken(context.Background(), "", "203.0.113.10")
	require.Equal(t, 503, infraerrors.Code(err))
	require.Equal(t, "CAPTCHA_SETTINGS_UNAVAILABLE", infraerrors.Reason(err))
}

func TestTurnstileVerificationTreatsMissingSettingAsDisabled(t *testing.T) {
	repo := &bmRepoStub{getValueFn: func(context.Context, string) (string, error) {
		return "", ErrSettingNotFound
	}}
	service := NewTurnstileService(NewSettingService(repo, &config.Config{}), nil)
	require.NoError(t, service.VerifyToken(context.Background(), "", "203.0.113.10"))
}
