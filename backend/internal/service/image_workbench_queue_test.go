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

func TestWorkbenchPayloadReservationShrinksToRetainedBody(t *testing.T) {
	for name, reserve := range map[string]func(int) (*WorkbenchPayloadReservation, error){
		"image": NewImageTaskService(nil).ReserveWorkbenchUpload,
		"video": NewVideoWorkbenchServiceWithSettings(nil, nil, nil).ReserveUpload,
	} {
		t.Run(name, func(t *testing.T) {
			// A body without Content-Length is admitted at the full request limit.
			unknownLength, err := reserve(workbenchPayloadBytes)
			require.NoError(t, err)
			_, err = reserve(1)
			require.Error(t, err)
			unknownLength.Shrink(1024)
			unknownLength.Shrink(workbenchPayloadBytes) // never grows back
			other, err := reserve(workbenchPayloadBytes - 1024)
			require.NoError(t, err, "a small queued body must not hold the whole budget")
			_, err = reserve(1)
			require.Error(t, err)
			unknownLength.Release()
			unknownLength.Release()
			unknownLength.Shrink(0)
			small, err := reserve(1024)
			require.NoError(t, err)
			small.Release()
			other.Release()
			full, err := reserve(workbenchPayloadBytes)
			require.NoError(t, err, "released and shrunk reservations return exactly their bytes")
			full.Release()
		})
	}
}
