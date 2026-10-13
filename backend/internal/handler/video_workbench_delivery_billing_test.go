//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failedVideoDeliveryStorage struct{ *videoHandlerStorage }

func (s *failedVideoDeliveryStorage) Write(context.Context, string, io.Reader, time.Duration) (int64, string, error) {
	return 0, "", errors.New("disk write failed")
}

func TestVideoWorkbenchDeliveryControlsBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"success", "content-rejected", "interrupted-content", "storage-failed", "panic", "expired-queue"} {
		t.Run(scenario, func(t *testing.T) {
			openAI, ledger, key := newWorkbenchBillingHandler(t, service.PlatformCustom, &workbenchBillingUpstream{statuses: []int{200}})
			store := &videoHandlerStore{tasks: make(map[string]service.VideoWorkbenchRecord)}
			files := &videoHandlerStorage{files: make(map[string][]byte)}
			var storage service.VideoWorkbenchLocalStorage = files
			if scenario == "storage-failed" {
				storage = &failedVideoDeliveryStorage{files}
			}
			tasks := service.NewVideoWorkbenchServiceWithSettings(store, storage, func(context.Context) service.VideoWorkbenchRuntime {
				return service.VideoWorkbenchRuntime{Enabled: true, MaxConcurrent: 3, RetentionMinutes: 15}
			})
			h := NewVideoWorkbenchHandler(tasks, openAI)
			h.SetAuthRefresh(func(*gin.Context) error { return nil })
			task, err := tasks.Create(context.Background(), key.UserID, service.VideoWorkbenchMetadata{APIKeyID: key.ID, Model: "grok-imagine-video", Duration: 5})
			require.NoError(t, err)
			if scenario == "expired-queue" {
				task.Deadline = time.Now().Add(-time.Second).Unix()
			}
			seconds, mode := 5, string(service.BillingModeVideo)
			reservation := &service.MediaBillingReservation{ID: "media:delivery", WorkbenchKind: "video", WorkbenchDelivery: true, WorkbenchTaskID: task.ID, Dispatched: true, Quote: service.MediaBillingQuote{
				Command: service.UsageBillingCommand{RequestID: "media:delivery", UserID: key.UserID, APIKeyID: key.ID, BalanceCost: .5, APIKeyQuotaCost: .5},
				Usage:   service.UsageLog{ActualCost: .5, TotalCost: .5, BillingMode: &mode, VideoDurationSeconds: &seconds},
			}}
			require.NoError(t, ledger.ReserveMedia(context.Background(), reservation))
			c, _ := workbenchBillingContext(key, service.PlatformCustom, "")
			c.Set(mediaPrechargeContextKey, reservation)
			h.execute = func(c *gin.Context, operation string) {
				require.Empty(t, ledger.commands, "billing must wait until content is saved")
				switch operation {
				case "create":
					if scenario == "panic" {
						panic("fixture worker interruption")
					}
					c.JSON(http.StatusOK, gin.H{"id": "upstream-task", "status": "completed", "duration": 3})
				case "content":
					if scenario == "content-rejected" {
						c.JSON(http.StatusForbidden, gin.H{"error": "unsupported protocol"})
						return
					}
					c.Data(http.StatusOK, "video/mp4", []byte("fixture-video"))
					if scenario == "interrupted-content" {
						_ = c.Error(io.ErrUnexpectedEOF)
					}
				default:
					t.Fatalf("unexpected operation %s", operation)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			c.Request = c.Request.WithContext(ctx)
			h.run(task, c, []byte(`{"model":"grok-imagine-video"}`), cancel)
			listed, err := tasks.List(context.Background(), key.UserID)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			if scenario == "success" {
				require.Equal(t, "completed", listed[0].Status)
				require.Len(t, ledger.commands, 1)
				require.InDelta(t, .3, ledger.commands[0].BalanceCost, 1e-8)
				require.Zero(t, ledger.refunded)
			} else {
				require.Equal(t, "failed", listed[0].Status)
				if scenario == "panic" || scenario == "expired-queue" {
					require.Equal(t, "interrupted", listed[0].ErrorCode)
				} else {
					require.Equal(t, "storage_failed", listed[0].ErrorCode)
				}
				require.Empty(t, ledger.commands)
				require.Equal(t, 1, ledger.refunded)
			}
			require.Empty(t, ledger.reservations)
		})
	}
}
