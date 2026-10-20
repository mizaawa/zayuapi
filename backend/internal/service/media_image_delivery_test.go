//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imagePaymentRecoveryStore struct {
	task    *ImageTaskRecord
	receipt *MediaWorkbenchDeliveryReceipt
	err     error
	deleted bool
}

func (s *imagePaymentRecoveryStore) Get(context.Context, string) (*ImageTaskRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copyTask := *s.task
	return &copyTask, nil
}
func (s *imagePaymentRecoveryStore) Save(_ context.Context, task *ImageTaskRecord, _ time.Duration) error {
	copyTask := *task
	s.task = &copyTask
	return nil
}
func (s *imagePaymentRecoveryStore) GetMediaDelivery(context.Context, string) (*MediaWorkbenchDeliveryReceipt, error) {
	return s.receipt, s.err
}
func (s *imagePaymentRecoveryStore) DeleteMediaDelivery(context.Context, string, string) error {
	s.deleted = true
	return nil
}

func TestImagePrepaymentRecoversDeliveryAndInterruptedWorkers(t *testing.T) {
	for _, scenario := range []string{"completed", "failed", "interrupted", "active", "deleted-after-delivery", "missing", "redis-unavailable", "wrong-owner", "wrong-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &mediaOutcomeRepository{}
			svc := &OpenAIGatewayService{usageBillingRepo: repo}
			id := "imgtask_0123456789abcdef0123456789abcdef"
			quote := MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:recover", UserID: 7, APIKeyID: 9, BalanceCost: .2}}
			r := &MediaBillingReservation{ID: "media:recover", WorkbenchKind: "image", ImageTaskID: id, ImageOutcome: &quote, Quote: MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:recover", UserID: 7, APIKeyID: 9, BalanceCost: .5}}, QueueLeaseUntil: time.Now().Add(-time.Minute), Dispatched: true}
			store := &imagePaymentRecoveryStore{task: &ImageTaskRecord{ID: id, UserID: 7, APIKeyID: 9, BillingID: r.ID, Status: ImageTaskStatusProcessing}}
			settle, refund, wantErr := false, false, false
			switch scenario {
			case "completed":
				store.task.Status = ImageTaskStatusCompleted
				settle = true
			case "failed":
				store.task.Status = ImageTaskStatusFailed
				refund = true
			case "interrupted":
				refund = true
			case "active":
				r.QueueLeaseUntil = time.Now().Add(time.Minute)
			case "deleted-after-delivery":
				store.task = nil
				store.receipt = &MediaWorkbenchDeliveryReceipt{TaskID: id, UserID: 7, APIKeyID: 9, BillingID: r.ID}
				settle = true
			case "missing":
				store.task = nil
				refund = true
			case "redis-unavailable":
				store.err = errors.New("redis unavailable")
				wantErr = true
			case "wrong-owner":
				store.task.UserID++
				wantErr = true
			case "wrong-receipt":
				store.task = nil
				store.receipt = &MediaWorkbenchDeliveryReceipt{TaskID: id, UserID: 8, APIKeyID: 9, BillingID: r.ID}
				wantErr = true
			}
			svc.SetMediaBillingImageTasks(NewImageTaskService(store))
			err := svc.reconcilePendingMedia(context.Background(), r)
			if wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if settle {
				require.NotNil(t, repo.settled)
				require.InDelta(t, .2, repo.settled.BalanceCost, 1e-8)
				require.True(t, store.deleted)
			} else {
				require.Nil(t, repo.settled)
			}
			if refund {
				require.Equal(t, 1, repo.refunds)
			} else {
				require.Zero(t, repo.refunds)
			}
		})
	}
}

type deletedVideoReceiptStore struct {
	VideoWorkbenchStore
	receipt *MediaWorkbenchDeliveryReceipt
	deleted bool
}

type currentImagePaymentRepository struct {
	mediaOutcomeRepository
	current *MediaBillingReservation
}

func (r *currentImagePaymentRepository) GetMediaImageReservation(context.Context, string) (*MediaBillingReservation, error) {
	return r.current, nil
}

func TestImagePrepaymentRecoveryRefreshesExpiredBatchSnapshot(t *testing.T) {
	quote := MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:renewed", UserID: 7, APIKeyID: 9, BalanceCost: .5}}
	current := &MediaBillingReservation{ID: quote.Command.RequestID, WorkbenchKind: "image", ImageTaskID: "imgtask_0123456789abcdef0123456789abcdef", Quote: quote, QueueLeaseUntil: time.Now().Add(time.Minute)}
	stale := *current
	stale.QueueLeaseUntil = time.Now().Add(-time.Minute)
	repo := &currentImagePaymentRepository{current: current}
	store := &imagePaymentRecoveryStore{task: &ImageTaskRecord{ID: current.ImageTaskID, UserID: 7, APIKeyID: 9, BillingID: current.ID, Status: ImageTaskStatusProcessing}}
	svc := &OpenAIGatewayService{usageBillingRepo: repo}
	svc.SetMediaBillingImageTasks(NewImageTaskService(store))
	require.NoError(t, svc.reconcilePendingMedia(context.Background(), &stale))
	require.Equal(t, ImageTaskStatusProcessing, store.task.Status, "an active worker renewed after the batch was selected")
	require.Zero(t, repo.refunds)
	require.Nil(t, repo.settled)
}

func (s *deletedVideoReceiptStore) Get(context.Context, string) (*VideoWorkbenchRecord, error) {
	return nil, ErrVideoTaskNotFound
}
func (s *deletedVideoReceiptStore) GetMediaDelivery(context.Context, string) (*MediaWorkbenchDeliveryReceipt, error) {
	return s.receipt, nil
}
func (s *deletedVideoReceiptStore) DeleteMediaDelivery(context.Context, string, string) error {
	s.deleted = true
	return nil
}

func TestVideoPrepaymentDeletedDeliveredTaskNeverRefunds(t *testing.T) {
	repo := &mediaOutcomeRepository{}
	svc := &OpenAIGatewayService{usageBillingRepo: repo}
	quote := MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:video-deleted", UserID: 7, APIKeyID: 9, BalanceCost: .3}}
	r := &MediaBillingReservation{ID: quote.Command.RequestID, WorkbenchKind: "video", WorkbenchDelivery: true, WorkbenchTaskID: "local-video", Quote: quote, VideoOutcome: &quote, QueueLeaseUntil: time.Now().Add(-time.Minute)}
	store := &deletedVideoReceiptStore{receipt: &MediaWorkbenchDeliveryReceipt{TaskID: r.WorkbenchTaskID, UserID: 7, APIKeyID: 9, BillingID: r.ID}}
	svc.SetMediaBillingVideoWorkbench(NewVideoWorkbenchServiceWithSettings(store, nil, nil))
	require.NoError(t, svc.reconcilePendingMedia(context.Background(), r))
	require.Zero(t, repo.refunds)
	require.NotNil(t, repo.settled)
	require.InDelta(t, .3, repo.settled.BalanceCost, 1e-8)
	require.True(t, store.deleted)
}
