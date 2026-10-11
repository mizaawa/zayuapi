package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkbenchPayloadBudgetBoundsCountAndBytes(t *testing.T) {
	svc := NewImageTaskService(nil)
	release, err := svc.ReserveWorkbenchPayload(workbenchPayloadBytes)
	require.NoError(t, err)
	_, err = svc.ReserveWorkbenchPayload(1)
	require.ErrorIs(t, err, ErrImageWorkbenchQueueFull)
	release()
	release()
	var releases []func()
	for range workbenchPayloadLimit {
		release, err := svc.ReserveWorkbenchPayload(1)
		require.NoError(t, err)
		releases = append(releases, release)
	}
	_, err = svc.ReserveWorkbenchPayload(1)
	require.ErrorIs(t, err, ErrImageWorkbenchQueueFull)
	for _, release := range releases {
		release()
	}
	release, err = svc.ReserveWorkbenchPayload(workbenchPayloadBytes)
	require.NoError(t, err)
	release()
}
