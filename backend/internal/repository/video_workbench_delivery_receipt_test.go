package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoWorkbenchDeliveryReceiptSurvivesTaskRemoval(t *testing.T) {
	for _, removal := range []string{"delete", "expire"} {
		t.Run(removal, func(t *testing.T) {
			store, ok := videoStoreFixture(t).(*videoWorkbenchStore)
			require.True(t, ok)
			ctx, now := context.Background(), time.Now().UTC()
			task := videoTaskFixture(1, 7, now)
			task.BillingID = "media:video-delivery"
			require.NoError(t, store.Create(ctx, task, time.Hour))
			started, err := store.TryStart(ctx, task, now, time.Minute, 3)
			require.NoError(t, err)
			require.True(t, started)
			task.Status = "completed"
			finished, err := store.Finish(ctx, task, time.Minute, 0)
			require.NoError(t, err)
			require.True(t, finished)

			if removal == "delete" {
				require.NoError(t, store.Delete(ctx, task, now))
			} else {
				require.NoError(t, store.rdb.Expire(ctx, videoTaskPrefix+task.ID, 0).Err())
			}
			require.NoError(t, store.AcknowledgeCleanup(ctx, task.ID))
			_, err = store.Get(ctx, task.ID)
			require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
			receipt, err := store.GetMediaDelivery(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, &service.MediaWorkbenchDeliveryReceipt{
				TaskID: task.ID, UserID: task.UserID, APIKeyID: task.Workbench.APIKeyID, BillingID: task.BillingID,
			}, receipt, "completed delivery must remain provable after public task removal")
			ttl, err := store.rdb.TTL(ctx, videoDeliveryPrefix+task.ID).Result()
			require.NoError(t, err)
			require.Equal(t, time.Duration(-1), ttl, "a ledger outage must not expire delivery evidence")

			require.NoError(t, store.DeleteMediaDelivery(ctx, task.ID, "media:another-task"))
			receipt, err = store.GetMediaDelivery(ctx, task.ID)
			require.NoError(t, err)
			require.NotNil(t, receipt, "another bill cannot acknowledge this delivery")
			require.NoError(t, store.DeleteMediaDelivery(ctx, task.ID, task.BillingID))
			require.NoError(t, store.DeleteMediaDelivery(ctx, task.ID, task.BillingID))
			receipt, err = store.GetMediaDelivery(ctx, task.ID)
			require.NoError(t, err)
			require.Nil(t, receipt)
		})
	}
}

func TestVideoWorkbenchDeliveryReceiptFollowsCompletionCAS(t *testing.T) {
	for _, scenario := range []string{"failure-wins", "billing-mismatch", "unbilled"} {
		t.Run(scenario, func(t *testing.T) {
			store, ok := videoStoreFixture(t).(*videoWorkbenchStore)
			require.True(t, ok)
			ctx, now := context.Background(), time.Now().UTC()
			task := videoTaskFixture(1, 7, now)
			if scenario != "unbilled" {
				task.BillingID = "media:video-delivery"
			}
			require.NoError(t, store.Create(ctx, task, time.Hour))
			started, err := store.TryStart(ctx, task, now, time.Minute, 3)
			require.NoError(t, err)
			require.True(t, started)
			if scenario == "failure-wins" {
				failed := *task
				failed.Status = "failed"
				finished, err := store.Finish(ctx, &failed, time.Minute, 0)
				require.NoError(t, err)
				require.True(t, finished)
			}
			task.Status = "completed"
			if scenario == "billing-mismatch" {
				task.BillingID = "media:forged"
			}
			finished, err := store.Finish(ctx, task, time.Minute, 0)
			if scenario == "billing-mismatch" {
				require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, scenario == "unbilled", finished)
			receipt, err := store.GetMediaDelivery(ctx, task.ID)
			require.NoError(t, err)
			require.Nil(t, receipt, "only a winning billed completion may publish delivery evidence")
		})
	}
}
