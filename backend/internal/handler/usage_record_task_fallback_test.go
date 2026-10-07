package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Billing tasks must survive pool shutdown and every queue overflow policy.

func newStoppedUsageRecordPoolForTest() *service.UsageRecordWorkerPool {
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:    1,
		QueueSize:      1,
		TaskTimeout:    time.Second,
		OverflowPolicy: config.UsageRecordOverflowPolicySync,
	})
	pool.Stop()
	return pool
}

func TestGatewayHandlerSubmitUsageRecordTask_StoppedPoolFallsBackToSync(t *testing.T) {
	h := &GatewayHandler{usageRecordWorkerPool: newStoppedUsageRecordPoolForTest()}

	executed := false
	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		executed = true
	})
	require.True(t, executed, "池已停止时计费任务必须内联同步执行")
}

func TestOpenAIGatewayHandlerSubmitUsageRecordTask_StoppedPoolFallsBackToSync(t *testing.T) {
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: newStoppedUsageRecordPoolForTest()}

	executed := false
	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		executed = true
	})
	require.True(t, executed, "池已停止时计费任务必须内联同步执行")
}

func TestGatewayHandlersSubmitUsageRecordTask_OverflowStillBills(t *testing.T) {
	for _, policy := range []string{config.UsageRecordOverflowPolicyDrop, config.UsageRecordOverflowPolicySample, config.UsageRecordOverflowPolicySync} {
		for _, kind := range []string{"gateway", "openai"} {
			t.Run(kind+"/"+policy, func(t *testing.T) {
				pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
					WorkerCount:           1,
					QueueSize:             1,
					TaskTimeout:           time.Minute,
					OverflowPolicy:        policy,
					OverflowSamplePercent: 1,
				})
				t.Cleanup(pool.Stop)
				submit := (&GatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
				if kind == "openai" {
					submit = (&OpenAIGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
				}

				started := make(chan struct{})
				block := make(chan struct{})
				t.Cleanup(func() { close(block) })
				// 占满 worker 槽位后再填满队列，保证第三个任务触发溢出。
				require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
					close(started)
					<-block
				}))
				<-started
				require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
					<-block
				}))

				parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxkey.ClientRequestID, "billing-request"))
				cancel()
				var executed atomic.Int32
				for range 3 {
					submit(parent, func(ctx context.Context) {
						require.NoError(t, ctx.Err())
						require.Equal(t, "billing-request", ctx.Value(ctxkey.ClientRequestID))
						_, hasDeadline := ctx.Deadline()
						require.True(t, hasDeadline)
						executed.Add(1)
					})
				}
				require.EqualValues(t, 3, executed.Load(), "every rejected billing task must execute once")
			})
		}
	}
}
