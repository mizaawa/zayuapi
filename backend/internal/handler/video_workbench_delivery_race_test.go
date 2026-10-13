//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The owner can download a completed video and delete its task immediately.
// This store deletes the record as soon as it becomes completed.
type deleteOnCompletionVideoStore struct{ *videoHandlerStore }

func (s *deleteOnCompletionVideoStore) Finish(ctx context.Context, task *service.VideoWorkbenchRecord, ttl time.Duration, expiredBefore int64) (bool, error) {
	ok, err := s.videoHandlerStore.Finish(ctx, task, ttl, expiredBefore)
	if ok && err == nil && task.Status == "completed" {
		_ = s.Delete(ctx, task, time.Now())
	}
	return ok, err
}

func TestVideoWorkbenchBillsDeliveredVideoDeletedBeforeFinalization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openAI, ledger, key := newWorkbenchBillingHandler(t, service.PlatformCustom, &workbenchBillingUpstream{statuses: []int{200}})
	store := &deleteOnCompletionVideoStore{&videoHandlerStore{tasks: make(map[string]service.VideoWorkbenchRecord)}}
	files := &videoHandlerStorage{files: make(map[string][]byte)}
	tasks := service.NewVideoWorkbenchServiceWithSettings(store, files, func(context.Context) service.VideoWorkbenchRuntime {
		return service.VideoWorkbenchRuntime{Enabled: true, MaxConcurrent: 3, RetentionMinutes: 15}
	})
	h := NewVideoWorkbenchHandler(tasks, openAI)
	h.SetAuthRefresh(func(*gin.Context) error { return nil })
	task, err := tasks.Create(context.Background(), key.UserID, service.VideoWorkbenchMetadata{APIKeyID: key.ID, Model: "grok-imagine-video", Duration: 5})
	require.NoError(t, err)
	seconds, mode := 5, string(service.BillingModeVideo)
	reservation := &service.MediaBillingReservation{ID: "media:delete-race", WorkbenchKind: "video", WorkbenchDelivery: true, WorkbenchTaskID: task.ID, Dispatched: true, Quote: service.MediaBillingQuote{
		Command: service.UsageBillingCommand{RequestID: "media:delete-race", UserID: key.UserID, APIKeyID: key.ID, BalanceCost: .5, APIKeyQuotaCost: .5},
		Usage:   service.UsageLog{ActualCost: .5, TotalCost: .5, BillingMode: &mode, VideoDurationSeconds: &seconds},
	}}
	require.NoError(t, ledger.ReserveMedia(context.Background(), reservation))
	c, _ := workbenchBillingContext(key, service.PlatformCustom, "")
	c.Set(mediaPrechargeContextKey, reservation)
	h.execute = func(c *gin.Context, operation string) {
		switch operation {
		case "create":
			c.JSON(http.StatusOK, gin.H{"id": "upstream-task", "status": "completed", "duration": 5})
		case "content":
			c.Data(http.StatusOK, "video/mp4", []byte("fixture-video"))
		default:
			t.Fatalf("unexpected operation %s", operation)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = c.Request.WithContext(ctx)
	h.run(task, c, []byte(`{"model":"grok-imagine-video"}`), cancel)

	_, err = store.Get(context.Background(), task.ID)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound, "fixture must delete the delivered task")
	require.Len(t, ledger.commands, 1, "a delivered video stays billed after its owner deletes it")
	require.InDelta(t, .5, ledger.commands[0].BalanceCost, 1e-8)
	require.Zero(t, ledger.refunded)
}
