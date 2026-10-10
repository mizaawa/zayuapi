//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type redeemCodeCreationSettingRepo struct {
	SettingRepository
	value string
	err   error
}

func (r *redeemCodeCreationSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyDisableRedeemCodeCreationLimit {
		return "", ErrSettingNotFound
	}
	return r.value, r.err
}

type redeemCodeCreationRepo struct {
	RedeemCodeRepository
	codes []RedeemCode
}

func (r *redeemCodeCreationRepo) Create(_ context.Context, code *RedeemCode) error {
	code.ID = int64(len(r.codes) + 1)
	r.codes = append(r.codes, *code)
	return nil
}

func TestAdminService_GenerateRedeemCodes_CreationLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		readErr error
		count   int
		errCode string
	}{
		{name: "default boundary", count: 100},
		{name: "default exceeded", count: 101, errCode: "REDEEM_CODE_CREATION_LIMIT_EXCEEDED"},
		{name: "disabled", value: "false", count: 301, errCode: "REDEEM_CODE_CREATION_LIMIT_EXCEEDED"},
		{name: "enabled above previous limit", value: "true", count: 301},
		{name: "enabled above legacy service limit", value: "true", count: 1001},
		{name: "missing setting", readErr: ErrSettingNotFound, count: 101, errCode: "REDEEM_CODE_CREATION_LIMIT_EXCEEDED"},
		{name: "read failure", value: "true", readErr: errors.New("read failed"), count: 101, errCode: "REDEEM_CODE_CREATION_LIMIT_EXCEEDED"},
		{name: "invalid setting", value: "invalid", count: 101, errCode: "REDEEM_CODE_CREATION_LIMIT_EXCEEDED"},
		{name: "zero", value: "true", count: 0, errCode: "REDEEM_CODE_COUNT_INVALID"},
		{name: "negative", value: "true", count: -1, errCode: "REDEEM_CODE_COUNT_INVALID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &redeemCodeCreationRepo{}
			svc := &adminServiceImpl{
				redeemCodeRepo: repo,
				settingService: &SettingService{settingRepo: &redeemCodeCreationSettingRepo{
					value: test.value,
					err:   test.readErr,
				}},
			}
			codes, err := svc.GenerateRedeemCodes(context.Background(), &GenerateRedeemCodesInput{
				Count: test.count,
				Type:  RedeemTypeBalance,
				Value: 10,
			})
			if test.errCode != "" {
				require.Error(t, err)
				require.Equal(t, test.errCode, infraerrors.Reason(err))
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				require.Empty(t, repo.codes)
				require.Nil(t, codes)
				return
			}
			require.NoError(t, err)
			require.Len(t, codes, test.count)
			require.Len(t, repo.codes, test.count)
		})
	}
}

func TestAdminService_GenerateRedeemCodes_CreationLimitUpdatesImmediately(t *testing.T) {
	settings := &redeemCodeCreationSettingRepo{value: "false"}
	repo := &redeemCodeCreationRepo{}
	svc := &adminServiceImpl{redeemCodeRepo: repo, settingService: &SettingService{settingRepo: settings}}
	input := &GenerateRedeemCodesInput{Count: 301, Type: RedeemTypeInvitation}

	_, err := svc.GenerateRedeemCodes(context.Background(), input)
	require.Error(t, err)
	settings.value = "true"
	_, err = svc.GenerateRedeemCodes(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, repo.codes, 301)
	settings.value = "false"
	_, err = svc.GenerateRedeemCodes(context.Background(), input)
	require.Error(t, err)
	require.Len(t, repo.codes, 301)

	svc.settingService = nil
	_, err = svc.GenerateRedeemCodes(context.Background(), input)
	require.Error(t, err)
	require.Len(t, repo.codes, 301)
}

func TestSettingService_RedeemCodeCreationLimitSettings(t *testing.T) {
	for _, value := range []string{"", "true", "false", "invalid"} {
		t.Run("stored="+value, func(t *testing.T) {
			svc := NewSettingService(&settingGetAllRepoStub{values: map[string]string{
				SettingKeyDisableRedeemCodeCreationLimit: value,
			}}, &config.Config{})
			settings, err := svc.GetAllSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, value == "true", settings.DisableRedeemCodeCreationLimit)
		})
	}
	for _, enabled := range []bool{true, false} {
		repo := &settingUpdateRepoStub{}
		svc := NewSettingService(repo, &config.Config{})
		require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{DisableRedeemCodeCreationLimit: enabled}))
		require.Contains(t, repo.updates, SettingKeyDisableRedeemCodeCreationLimit)
		require.Equal(t, enabled, repo.updates[SettingKeyDisableRedeemCodeCreationLimit] == "true")
	}
}

func TestSettingService_RedeemCodeCreationLimitSettingIsAdminOnly(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{
		SettingKeyDisableRedeemCodeCreationLimit: "true",
	}}, &config.Config{})
	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	injected, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	for _, value := range []any{settings, injected} {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(body), SettingKeyDisableRedeemCodeCreationLimit)
	}
}
