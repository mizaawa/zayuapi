package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

type MediaImageBillingRepository interface {
	BindMediaImageTask(context.Context, string, string, int64, int64) error
	StageMediaImageOutcome(context.Context, string, *MediaBillingQuote) error
}

type MediaImageRecoveryRepository interface {
	GetMediaImageReservation(context.Context, string) (*MediaBillingReservation, error)
}

type mediaImageQuoteCaptureKey struct{}

func (s *OpenAIGatewayService) SetMediaBillingImageTasks(tasks *ImageTaskService) {
	s.mediaBillingImageTasks.Store(tasks)
}

func (s *OpenAIGatewayService) BindMediaImageTask(ctx context.Context, r *MediaBillingReservation, taskID string) error {
	if r == nil || r.WorkbenchKind != "image" {
		return nil
	}
	if !strings.HasPrefix(taskID, "imgtask_") || len(taskID) != 40 {
		return ErrImageTaskNotFound
	}
	repo, ok := s.usageBillingRepo.(MediaImageBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	if err := repo.BindMediaImageTask(ctx, r.ID, taskID, r.Quote.Command.UserID, r.Quote.Command.APIKeyID); err != nil {
		return err
	}
	r.ImageTaskID = taskID
	return nil
}

// Calculate and persist the exact charge before uploading the result. A
// candidate outcome remains refundable until the task store confirms delivery.
func (s *OpenAIGatewayService) PrepareMediaImageDelivery(ctx context.Context, r *MediaBillingReservation, record UsageRecordTask) error {
	if r == nil || r.ImageTaskID == "" {
		return nil
	}
	if record == nil {
		return ErrBillingServiceUnavailable
	}
	var quote *MediaBillingQuote
	record(context.WithValue(ctx, mediaImageQuoteCaptureKey{}, &quote))
	if quote == nil || quote.Command.RequestID != r.ID || quote.Command.UserID != r.Quote.Command.UserID || quote.Command.APIKeyID != r.Quote.Command.APIKeyID {
		return ErrBillingServiceUnavailable
	}
	repo, ok := s.usageBillingRepo.(MediaImageBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	if err := repo.StageMediaImageOutcome(ctx, r.ID, quote); err != nil {
		return err
	}
	r.ImageOutcome = quote
	return nil
}

func (s *OpenAIGatewayService) FinalizeMediaImageDelivery(ctx context.Context, r *MediaBillingReservation) error {
	if r == nil || r.ImageOutcome == nil {
		return ErrBillingServiceUnavailable
	}
	return s.settleMediaQuote(ctx, r, r.ImageOutcome)
}

func (s *OpenAIGatewayService) reconcileMediaImage(ctx context.Context, r *MediaBillingReservation) error {
	if repo, ok := s.usageBillingRepo.(MediaImageRecoveryRepository); ok {
		current, err := repo.GetMediaImageReservation(ctx, r.ID)
		if err != nil {
			return err
		}
		if current == nil {
			s.clearMediaDeliveryReceipt(ctx, r)
			return nil
		}
		r = current
		if r.RefundPending || r.FinalQuote != nil {
			return s.reconcilePendingMedia(ctx, r)
		}
	}
	tasks := s.mediaBillingImageTasks.Load()
	if tasks == nil || tasks.store == nil {
		return ErrBillingServiceUnavailable
	}
	task, err := tasks.store.Get(ctx, r.ImageTaskID)
	if errors.Is(err, ErrImageTaskNotFound) {
		if receipts, ok := tasks.store.(MediaWorkbenchDeliveryStore); ok {
			receipt, receiptErr := receipts.GetMediaDelivery(ctx, r.ImageTaskID)
			if receiptErr != nil {
				return receiptErr
			}
			if receipt != nil {
				if !mediaDeliveryMatches(receipt, r, r.ImageTaskID) {
					return ErrUsageBillingRequestConflict
				}
				return s.FinalizeMediaImageDelivery(ctx, r)
			}
		}
		if r.QueueLeaseUntil.After(time.Now()) {
			return nil
		}
		return s.RefundMedia(ctx, r.ID)
	}
	if err != nil {
		return err
	}
	if task.UserID != r.Quote.Command.UserID || task.APIKeyID != r.Quote.Command.APIKeyID || task.BillingID != r.ID {
		return ErrUsageBillingRequestConflict
	}
	if (task.Status == ImageTaskStatusQueued || task.Status == ImageTaskStatusProcessing) && !r.QueueLeaseUntil.After(time.Now()) {
		if err := tasks.Fail(ctx, task.ID, http.StatusGatewayTimeout, imageTaskErrorJSON("timeout_error", "image generation was interrupted")); err != nil {
			return err
		}
		// Completion may have won the atomic terminal transition.
		task, err = tasks.store.Get(ctx, task.ID)
		if err != nil {
			return err
		}
	}
	switch task.Status {
	case ImageTaskStatusCompleted:
		return s.FinalizeMediaImageDelivery(ctx, r)
	case ImageTaskStatusFailed:
		return s.RefundMedia(ctx, r.ID)
	default:
		return nil
	}
}

func (s *OpenAIGatewayService) clearMediaDeliveryReceipt(ctx context.Context, r *MediaBillingReservation) {
	var store MediaWorkbenchDeliveryStore
	var taskID string
	if r.ImageTaskID != "" {
		if tasks := s.mediaBillingImageTasks.Load(); tasks != nil {
			store, _ = tasks.store.(MediaWorkbenchDeliveryStore)
			taskID = r.ImageTaskID
		}
	} else if r.WorkbenchDelivery {
		if tasks := s.mediaBillingVideoWorkbench.Load(); tasks != nil {
			store, _ = tasks.store.(MediaWorkbenchDeliveryStore)
			taskID = r.WorkbenchTaskID
		}
	}
	if store != nil && taskID != "" {
		// A cache cleanup failure must not undo a committed payment.
		_ = store.DeleteMediaDelivery(ctx, taskID, r.ID)
	}
}
