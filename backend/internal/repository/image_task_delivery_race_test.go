package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Finish can lose to a different terminal writer after the service reads the
// task. Keep that race deterministic while exercising the real Redis scripts.
type imageDeliveryRaceStore struct {
	*imageTaskStore
	reads            int
	failBeforeFinish bool
}

func (s *imageDeliveryRaceStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	task, err := s.imageTaskStore.Get(ctx, id)
	s.reads++
	if err == nil && s.failBeforeFinish && s.reads == 2 {
		failed := *task
		failed.Status = service.ImageTaskStatusFailed
		failed.Error = json.RawMessage(`{"message":"interrupted"}`)
		if err := s.imageTaskStore.Finish(ctx, &failed, time.Hour); err != nil {
			return nil, err
		}
	}
	return task, err
}

type imageDeliveryRaceStorage struct {
	finish func() error
}

func (s imageDeliveryRaceStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	if s.finish != nil {
		if err := s.finish(); err != nil {
			return "", err
		}
	}
	return "https://images.example/" + key, nil
}

func TestImageTaskDeliveryRejectsConcurrentFailure(t *testing.T) {
	for _, workbench := range []bool{false, true} {
		for _, atomicRace := range []bool{false, true} {
			name := map[bool]string{false: "api", true: "workbench"}[workbench] + "/" + map[bool]string{false: "during_upload", true: "before_atomic_finish"}[atomicRace]
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				mr := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				base := NewImageTaskStore(rdb).(*imageTaskStore)
				store := &imageDeliveryRaceStore{imageTaskStore: base, failBeforeFinish: atomicRace}
				task := &service.ImageTaskRecord{ID: "imgtask_delivery_race", UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com", Status: service.ImageTaskStatusProcessing, ExpiresAt: time.Now().Add(time.Hour).Unix(), BillingID: "media:delivery-race"}
				if workbench {
					task.Workbench = &service.ImageWorkbenchMetadata{APIKeyID: task.APIKeyID}
					require.NoError(t, base.CreateWorkbench(ctx, task, time.Hour, 10))
				} else {
					require.NoError(t, base.Save(ctx, task, time.Hour))
				}
				storage := imageDeliveryRaceStorage{}
				if !atomicRace {
					storage.finish = func() error {
						failed := *task
						failed.Status = service.ImageTaskStatusFailed
						failed.Error = json.RawMessage(`{"message":"interrupted"}`)
						return base.Finish(ctx, &failed, time.Hour)
					}
				}
				uploader := service.NewImageResultUploader(storage, "images/", 0, nil)
				svc := service.NewImageTaskServiceWithUploader(store, uploader, time.Hour, time.Minute)
				delivered, err := svc.Deliver(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`))
				require.NoError(t, err)
				require.False(t, delivered, "a failed terminal task exposes no image and must be refunded")
				stored, err := base.Get(ctx, task.ID)
				require.NoError(t, err)
				require.Equal(t, service.ImageTaskStatusFailed, stored.Status)
				require.Empty(t, stored.Result)
				receipt, err := base.GetMediaDelivery(ctx, task.ID)
				require.NoError(t, err)
				require.Nil(t, receipt, "failed delivery cannot create evidence of a paid image")
			})
		}
	}
}

type imageDeliveryDeleteStore struct {
	*imageTaskStore
	lostReply    bool
	receiptError bool
	otherOwner   bool
}

func (s imageDeliveryDeleteStore) FinishWithResult(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) (bool, error) {
	stored, err := s.imageTaskStore.FinishWithResult(ctx, task, ttl)
	if err == nil && stored && task.Status == service.ImageTaskStatusCompleted {
		// The owner can retrieve the completed task and delete it before the
		// worker receives the successful write acknowledgement.
		err = s.rdb.Del(ctx, imageTaskKey(task.ID)).Err()
		if err == nil && s.lostReply {
			err = errors.New("completed write reply lost")
		}
	}
	return stored, err
}

func (s imageDeliveryDeleteStore) GetMediaDelivery(ctx context.Context, id string) (*service.MediaWorkbenchDeliveryReceipt, error) {
	if s.receiptError {
		return nil, errors.New("delivery receipt unavailable")
	}
	receipt, err := s.imageTaskStore.GetMediaDelivery(ctx, id)
	if receipt != nil && s.otherOwner {
		receipt.UserID++
	}
	return receipt, err
}

func TestImageTaskDeliveryReceiptSurvivesImmediateDeletion(t *testing.T) {
	for _, workbench := range []bool{false, true} {
		for _, tc := range []struct {
			name         string
			lostReply    bool
			receiptError bool
			otherOwner   bool
		}{
			{name: "acknowledged"},
			{name: "write_reply_lost", lostReply: true},
			{name: "receipt_unavailable", lostReply: true, receiptError: true},
			{name: "receipt_wrong_owner", lostReply: true, otherOwner: true},
		} {
			t.Run(map[bool]string{false: "api", true: "workbench"}[workbench]+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				mr := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				base := NewImageTaskStore(rdb).(*imageTaskStore)
				store := imageDeliveryDeleteStore{imageTaskStore: base, lostReply: tc.lostReply, receiptError: tc.receiptError, otherOwner: tc.otherOwner}
				uploader := service.NewImageResultUploader(imageDeliveryRaceStorage{}, "images/", 0, nil)
				svc := service.NewImageTaskServiceWithUploader(store, uploader, time.Hour, time.Minute)
				owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com", GroupID: 3, BillingID: "media:delivered"}
				var task *service.ImageTask
				var err error
				if workbench {
					task, err = svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
				} else {
					task, err = svc.Create(ctx, owner)
				}
				require.NoError(t, err)
				pending, err := base.Get(ctx, task.ID)
				require.NoError(t, err)
				require.Equal(t, owner.BillingID, pending.BillingID)
				require.Equal(t, owner.GroupID, pending.GroupID)
				delivered, err := svc.Deliver(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`))
				if tc.receiptError || tc.otherOwner {
					require.Error(t, err, "uncertain or mismatched evidence must await reconciliation")
					require.False(t, delivered)
					if tc.otherOwner {
						require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
					}
				} else {
					require.NoError(t, err)
					require.True(t, delivered, "successful delivery remains billable after its owner deletes it")
				}
				_, err = base.Get(ctx, task.ID)
				require.ErrorIs(t, err, service.ErrImageTaskNotFound)
				receipt, err := base.GetMediaDelivery(ctx, task.ID)
				require.NoError(t, err)
				require.Equal(t, &service.MediaWorkbenchDeliveryReceipt{TaskID: task.ID, UserID: owner.UserID, APIKeyID: owner.APIKeyID, BillingID: owner.BillingID}, receipt)
				require.Equal(t, time.Duration(-1), rdb.TTL(ctx, imageTaskDeliveryKey(task.ID)).Val(), "unsettled payment evidence has no TTL")
				mr.FastForward(48 * time.Hour)
				require.NoError(t, base.DeleteMediaDelivery(ctx, task.ID, "media:unrelated"))
				receipt, err = base.GetMediaDelivery(ctx, task.ID)
				require.NoError(t, err)
				require.NotNil(t, receipt, "another reservation cannot delete delivery evidence")
				require.NoError(t, base.DeleteMediaDelivery(ctx, task.ID, owner.BillingID))
				receipt, err = base.GetMediaDelivery(ctx, task.ID)
				require.NoError(t, err)
				require.Nil(t, receipt)
			})
		}
	}
}

func TestImageTaskDeliveryWithoutPrepaymentHasNoReceipt(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb).(*imageTaskStore)
	ctx := context.Background()
	task := &service.ImageTaskRecord{ID: "imgtask_unpaid", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusProcessing}
	require.NoError(t, store.Save(ctx, task, time.Hour))
	task.Status = service.ImageTaskStatusCompleted
	stored, err := store.FinishWithResult(ctx, task, time.Hour)
	require.NoError(t, err)
	require.True(t, stored)
	receipt, err := store.GetMediaDelivery(ctx, task.ID)
	require.NoError(t, err)
	require.Nil(t, receipt)
}

func TestImageTaskDuplicateCompletionDoesNotRecreateSettledReceipt(t *testing.T) {
	for _, workbench := range []bool{false, true} {
		t.Run(map[bool]string{false: "api", true: "workbench"}[workbench], func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			store := NewImageTaskStore(rdb).(*imageTaskStore)
			ctx := context.Background()
			task := &service.ImageTaskRecord{ID: "imgtask_repeat", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusProcessing, BillingID: "media:repeat"}
			if workbench {
				task.Workbench = &service.ImageWorkbenchMetadata{}
				require.NoError(t, store.CreateWorkbench(ctx, task, time.Hour, 10))
			} else {
				require.NoError(t, store.Save(ctx, task, time.Hour))
			}
			task.Status = service.ImageTaskStatusCompleted
			stored, err := store.FinishWithResult(ctx, task, time.Hour)
			require.NoError(t, err)
			require.True(t, stored)
			receipt, err := store.GetMediaDelivery(ctx, task.ID)
			require.NoError(t, err)
			require.NotNil(t, receipt)
			require.NoError(t, store.DeleteMediaDelivery(ctx, task.ID, task.BillingID))
			stored, err = store.FinishWithResult(ctx, task, time.Hour)
			require.NoError(t, err)
			require.True(t, stored)
			receipt, err = store.GetMediaDelivery(ctx, task.ID)
			require.NoError(t, err)
			require.Nil(t, receipt, "terminal retries cannot leak a receipt after payment cleanup")
		})
	}
}
