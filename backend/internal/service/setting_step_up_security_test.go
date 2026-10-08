package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type stepUpSettingRepoStub struct {
	SettingRepository
	value string
	err   error
}

func (s stepUpSettingRepoStub) GetValue(context.Context, string) (string, error) {
	return s.value, s.err
}

func TestStepUpSettingFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		err   error
		want  bool
	}{
		{name: "enabled", value: "true", want: true},
		{name: "explicitly disabled", value: "false"},
		{name: "missing keeps default", err: ErrSettingNotFound},
		{name: "database outage", err: errors.New("database unavailable"), want: true},
		{name: "invalid value", value: "invalid", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &SettingService{settingRepo: stepUpSettingRepoStub{value: tc.value, err: tc.err}}
			require.Equal(t, tc.want, svc.IsStepUpEnabled(context.Background()))
		})
	}
}
