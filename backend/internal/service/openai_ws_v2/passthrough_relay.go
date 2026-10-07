package openai_ws_v2

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

const (
	maxReportedUsageTokens  = 100_000_000
	maxCompletedResponseIDs = 4096
)

type FrameConn interface {
	ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error)
	WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error
	Close() error
}

type Usage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	ImageOutputTokens        int
}

type RelayResult struct {
	ServiceTier             string
	RequestModel            string
	ResponseModel           string
	ResponseModelConflict   bool
	Usage                   Usage
	RequestID               string
	TerminalEventType       string
	FirstTokenMs            *int
	Duration                time.Duration
	ClientToUpstreamFrames  int64
	UpstreamToClientFrames  int64
	DroppedDownstreamFrames int64
}

type RelayTurnResult struct {
	ServiceTier           string
	RequestModel          string
	ResponseModel         string
	ResponseModelConflict bool
	Usage                 Usage
	RequestID             string
	TerminalEventType     string
	Duration              time.Duration
	FirstTokenMs          *int
}

type RelayExit struct {
	Stage           string
	Err             error
	Graceful        bool
	WroteDownstream bool
}

type RelayOptions struct {
	WriteTimeout                    time.Duration
	IdleTimeout                     time.Duration
	UpstreamDrainTimeout            time.Duration
	FirstMessageType                coderws.MessageType
	FirstMessageSent                bool
	StartClientAfterFirstDownstream bool
	OnUsageParseFailure             func(eventType string, usageRaw string)
	OnTurnComplete                  func(turn RelayTurnResult)
	BeforeWriteClient               func(msgType coderws.MessageType, payload []byte, wroteDownstream bool) error
	BeforeClientWrite               func(msgType coderws.MessageType, payload []byte)
	AfterClientWrite                func(msgType coderws.MessageType, payload []byte, writeErr error)
	BeforeRelayCancel               func(exit RelayExit)
	ReadClientFrame                 func(ctx context.Context, clientConn FrameConn) (coderws.MessageType, []byte, error)
	OnTrace                         func(event RelayTraceEvent)
	Now                             func() time.Time
}

type RelayTraceEvent struct {
	Stage           string
	Direction       string
	MessageType     string
	PayloadBytes    int
	Graceful        bool
	WroteDownstream bool
	Error           string
}

type relayState struct {
	// mu serializes state transitions between the client reader, upstream
	// reader, and relay shutdown/finalization. The atomic terminal markers below
	// make individual reads safe, but the associated map/snapshot updates must
	// remain one transaction.
	mu sync.Mutex
	// usage is the aggregate across completed Responses turns.  turnUsage is
	// deliberately kept separate: upstream usage frames are snapshots, not
	// deltas, so adding every frame would multiply the bill for one turn.
	usage                  Usage
	turnUsage              Usage
	completedResponseIDs   map[string]struct{}
	completedResponseOrder []string
	// ID-less Responses WS turns have one billable terminal snapshot. For an
	// ID-bearing turn, this marker is kept until the client explicitly starts a
	// new response.create turn.  Upstream data frames must not clear it: doing so
	// would let a forged terminal(A) -> data -> terminal(B) sequence reopen the
	// billing window with a rewritten response ID.
	terminalSeen             atomic.Bool
	nonTerminalAfterTerminal atomic.Bool
	// Some upstreams omit response.id on terminal events. Keep a per-turn
	// terminal marker so repeated anonymous terminal snapshots cannot be
	// accumulated as additional billable usage. It is reset when the client
	// starts the next response.create turn.
	anonymousTerminalSeen atomic.Bool
	// lifecycleMu protects the upstream response.created identity.  A
	// response.created event is the only upstream signal we use to associate a
	// response ID with the current client turn; arbitrary data events must not
	// reopen the billing window.
	lifecycleMu         sync.RWMutex
	lifecycleStarted    bool
	lifecycleResponseID string
	requestModelMu      sync.RWMutex
	requestModel        string
	lastResponseID      string
	lastResponseModel   string
	lastServiceTier     string
	responseConflict    bool
	terminalEventType   string
	firstTokenMs        *int
	turnTimingByID      map[string]*relayTurnTiming
	activeTurn          *relayTurnTiming
	pendingBareError    *observedUpstreamEvent
}

type relayExitSignal struct {
	stage           string
	err             error
	graceful        bool
	wroteDownstream bool
}

type observedUpstreamEvent struct {
	serviceTier       string
	terminal          bool
	duplicateTerminal bool
	eventType         string
	responseID        string
	usage             Usage
	responseModel     string
	responseConflict  bool
	duration          time.Duration
	firstToken        *int
}

type relayTurnTiming struct {
	startAt               time.Time
	firstTokenMs          *int
	firstResponseModel    string
	terminalResponseModel string
	responseModelConflict bool
}

func Relay(
	ctx context.Context,
	clientConn FrameConn,
	upstreamConn FrameConn,
	firstClientMessage []byte,
	options RelayOptions,
) (RelayResult, *RelayExit) {
	result := RelayResult{RequestModel: strings.TrimSpace(gjson.GetBytes(firstClientMessage, "model").String())}
	if clientConn == nil || upstreamConn == nil {
		return result, &RelayExit{Stage: "relay_init", Err: errors.New("relay connection is nil")}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	nowFn := options.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	writeTimeout := options.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = 2 * time.Minute
	}
	drainTimeout := options.UpstreamDrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = 1200 * time.Millisecond
	}
	firstMessageType := options.FirstMessageType
	if firstMessageType != coderws.MessageBinary {
		firstMessageType = coderws.MessageText
	}
	startAt := nowFn()
	state := &relayState{requestModel: result.RequestModel}
	if isResponseCreateMessage(firstMessageType, firstClientMessage) {
		state.startClientTurn()
	}
	onTrace := options.OnTrace

	relayCtx, relayCancel := context.WithCancel(ctx)
	defer relayCancel()

	lastActivity := atomic.Int64{}
	lastActivity.Store(nowFn().UnixNano())
	markActivity := func() {
		lastActivity.Store(nowFn().UnixNano())
	}

	writeUpstream := func(msgType coderws.MessageType, payload []byte) error {
		writeCtx, cancel := context.WithTimeout(relayCtx, writeTimeout)
		defer cancel()
		return upstreamConn.WriteFrame(writeCtx, msgType, payload)
	}
	writeClientFrameUpstream := func(msgType coderws.MessageType, payload []byte) error {
		if isResponseCreateMessage(msgType, payload) {
			// A client response.create is the authoritative turn boundary. Clear
			// all per-turn terminal/lifecycle markers before forwarding it;
			// otherwise a provider that omits response IDs on the next turn would
			// have its first terminal event mistaken for a duplicate.
			state.startClientTurn()
			state.setRequestModel(strings.TrimSpace(gjson.GetBytes(payload, "model").String()))
		}
		return writeUpstream(msgType, payload)
	}
	writeClient := func(msgType coderws.MessageType, payload []byte) error {
		// 下行写超时故意不挂在 relayCtx 上：coder/websocket 在已武装的 write
		// ctx 被取消时会直接硬关连接（context.AfterFunc 的 stop 不等待执行中
		// 的回调），外部取消若落在一次已成功写入的解除武装窗口内，会连同尚未
		// 发出的 close 帧一起冲掉，客户端只能看到裸 EOF 而收不到关闭码。与读
		// 侧 conn.Read(context.Background()) 同理，取消路径的连接回收由各退出
		// 分支的显式 Close/CloseNow 兜底。
		writeCtx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		return clientConn.WriteFrame(writeCtx, msgType, payload)
	}

	clientToUpstreamFrames := &atomic.Int64{}
	upstreamToClientFrames := &atomic.Int64{}
	droppedDownstreamFrames := &atomic.Int64{}
	emitRelayTrace(onTrace, RelayTraceEvent{
		Stage:        "relay_start",
		PayloadBytes: len(firstClientMessage),
		MessageType:  relayMessageTypeString(firstMessageType),
	})

	if options.FirstMessageSent {
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:        "write_first_message_skipped",
			Direction:    "client_to_upstream",
			MessageType:  relayMessageTypeString(firstMessageType),
			PayloadBytes: len(firstClientMessage),
		})
	} else {
		if err := writeUpstream(firstMessageType, firstClientMessage); err != nil {
			result.Duration = nowFn().Sub(startAt)
			emitRelayTrace(onTrace, RelayTraceEvent{
				Stage:        "write_first_message_failed",
				Direction:    "client_to_upstream",
				MessageType:  relayMessageTypeString(firstMessageType),
				PayloadBytes: len(firstClientMessage),
				Error:        err.Error(),
			})
			return result, &RelayExit{Stage: "write_upstream", Err: err}
		}
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:        "write_first_message_ok",
			Direction:    "client_to_upstream",
			MessageType:  relayMessageTypeString(firstMessageType),
			PayloadBytes: len(firstClientMessage),
		})
	}
	clientToUpstreamFrames.Add(1)
	markActivity()

	exitCh := make(chan relayExitSignal, 3)
	dropDownstreamWrites := atomic.Bool{}
	clientReaderStarted := atomic.Bool{}
	startClientReader := func() {
		if !clientReaderStarted.CompareAndSwap(false, true) {
			return
		}
		go runClientToUpstream(relayCtx, clientConn, options.ReadClientFrame, writeClientFrameUpstream, markActivity, clientToUpstreamFrames, onTrace, exitCh)
	}
	if !options.StartClientAfterFirstDownstream {
		startClientReader()
	}
	go runUpstreamToClient(
		relayCtx,
		upstreamConn,
		writeClient,
		startAt,
		nowFn,
		state,
		options.OnUsageParseFailure,
		options.OnTurnComplete,
		options.BeforeWriteClient,
		options.BeforeClientWrite,
		options.AfterClientWrite,
		func(msgType coderws.MessageType, payload []byte) {
			if options.StartClientAfterFirstDownstream {
				startClientReader()
			}
		},
		&dropDownstreamWrites,
		upstreamToClientFrames,
		droppedDownstreamFrames,
		markActivity,
		onTrace,
		exitCh,
	)
	go runIdleWatchdog(relayCtx, nowFn, options.IdleTimeout, &lastActivity, onTrace, exitCh)

	firstExit := <-exitCh
	// An outer ingress cancellation is a control-plane close, not a graceful
	// upstream disconnect. Leave the client connection open here so the
	// adapter can emit the precise lease/request close code. Internal
	// relayCancel does not cancel ctx and therefore does not take this path.
	if ctx.Err() != nil {
		firstExit.graceful = false
	}
	emitRelayTrace(onTrace, RelayTraceEvent{
		Stage:           "first_exit",
		Direction:       relayDirectionFromStage(firstExit.stage),
		Graceful:        firstExit.graceful,
		WroteDownstream: firstExit.wroteDownstream,
		Error:           relayErrorString(firstExit.err),
	})
	if options.BeforeRelayCancel != nil {
		options.BeforeRelayCancel(RelayExit{
			Stage:           firstExit.stage,
			Err:             firstExit.err,
			Graceful:        firstExit.graceful,
			WroteDownstream: firstExit.wroteDownstream,
		})
	}
	combinedWroteDownstream := firstExit.wroteDownstream
	secondExit := relayExitSignal{graceful: true}
	hasSecondExit := false

	// 客户端断开后尽力继续读取上游短窗口，捕获延迟 usage/terminal 事件用于计费。
	if firstExit.stage == "read_client" && firstExit.graceful {
		dropDownstreamWrites.Store(true)
		secondExit, hasSecondExit = waitRelayExit(exitCh, drainTimeout)
	} else {
		relayCancel()
		_ = upstreamConn.Close()
		if clientReaderStarted.Load() {
			secondExit, hasSecondExit = waitRelayExit(exitCh, 200*time.Millisecond)
		}
	}
	if hasSecondExit {
		combinedWroteDownstream = combinedWroteDownstream || secondExit.wroteDownstream
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:           "second_exit",
			Direction:       relayDirectionFromStage(secondExit.stage),
			Graceful:        secondExit.graceful,
			WroteDownstream: secondExit.wroteDownstream,
			Error:           relayErrorString(secondExit.err),
		})
	}

	relayCancel()
	_ = upstreamConn.Close()

	// A provider may close after an error or after a final snapshot without
	// emitting the corresponding terminal event. Settle any pending fallback
	// before exposing the aggregate result; both operations are idempotent once
	// turnUsage has been cleared.
	emitTurnComplete(options.OnTurnComplete, state, finalizePendingBareError(state, nowFn()))
	finalizeRelayTurnUsage(state)
	enrichResult(&result, state, nowFn().Sub(startAt))
	result.ClientToUpstreamFrames = clientToUpstreamFrames.Load()
	result.UpstreamToClientFrames = upstreamToClientFrames.Load()
	result.DroppedDownstreamFrames = droppedDownstreamFrames.Load()
	if options.FirstMessageSent && firstExit.stage == "read_client" && firstExit.graceful {
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:           "relay_client_closed",
			Graceful:        true,
			WroteDownstream: combinedWroteDownstream,
		})
		return result, nil
	}
	if firstExit.stage == "read_client" && firstExit.graceful {
		stage := "client_disconnected"
		exitErr := firstExit.err
		if hasSecondExit && !secondExit.graceful {
			stage = secondExit.stage
			exitErr = secondExit.err
		}
		if exitErr == nil {
			exitErr = io.EOF
		}
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:           "relay_exit",
			Direction:       relayDirectionFromStage(stage),
			Graceful:        false,
			WroteDownstream: combinedWroteDownstream,
			Error:           relayErrorString(exitErr),
		})
		return result, &RelayExit{
			Stage:           stage,
			Err:             exitErr,
			WroteDownstream: combinedWroteDownstream,
		}
	}
	if firstExit.graceful && (!hasSecondExit || secondExit.graceful) {
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:           "relay_complete",
			Graceful:        true,
			WroteDownstream: combinedWroteDownstream,
		})
		_ = clientConn.Close()
		return result, nil
	}
	if !firstExit.graceful {
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:           "relay_exit",
			Direction:       relayDirectionFromStage(firstExit.stage),
			Graceful:        false,
			WroteDownstream: combinedWroteDownstream,
			Error:           relayErrorString(firstExit.err),
		})
		return result, &RelayExit{
			Stage:           firstExit.stage,
			Err:             firstExit.err,
			WroteDownstream: combinedWroteDownstream,
		}
	}
	if hasSecondExit && !secondExit.graceful {
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:           "relay_exit",
			Direction:       relayDirectionFromStage(secondExit.stage),
			Graceful:        false,
			WroteDownstream: combinedWroteDownstream,
			Error:           relayErrorString(secondExit.err),
		})
		return result, &RelayExit{
			Stage:           secondExit.stage,
			Err:             secondExit.err,
			WroteDownstream: combinedWroteDownstream,
		}
	}
	if options.FirstMessageSent {
		emitRelayTrace(onTrace, RelayTraceEvent{
			Stage:           "relay_client_closed",
			Graceful:        true,
			WroteDownstream: combinedWroteDownstream,
		})
		return result, nil
	}
	emitRelayTrace(onTrace, RelayTraceEvent{
		Stage:           "relay_complete",
		Graceful:        true,
		WroteDownstream: combinedWroteDownstream,
	})
	_ = clientConn.Close()
	return result, nil
}

func runClientToUpstream(
	ctx context.Context,
	clientConn FrameConn,
	readClientFrame func(context.Context, FrameConn) (coderws.MessageType, []byte, error),
	writeUpstream func(msgType coderws.MessageType, payload []byte) error,
	markActivity func(),
	forwardedFrames *atomic.Int64,
	onTrace func(event RelayTraceEvent),
	exitCh chan<- relayExitSignal,
) {
	if readClientFrame == nil {
		readClientFrame = func(ctx context.Context, conn FrameConn) (coderws.MessageType, []byte, error) {
			return conn.ReadFrame(ctx)
		}
	}
	for {
		msgType, payload, err := readClientFrame(ctx, clientConn)
		if err != nil {
			emitRelayTrace(onTrace, RelayTraceEvent{
				Stage:     "read_client_failed",
				Direction: "client_to_upstream",
				Error:     err.Error(),
				Graceful:  isDisconnectError(err),
			})
			exitCh <- relayExitSignal{stage: "read_client", err: err, graceful: isDisconnectError(err)}
			return
		}
		markActivity()
		if err := writeUpstream(msgType, payload); err != nil {
			emitRelayTrace(onTrace, RelayTraceEvent{
				Stage:        "write_upstream_failed",
				Direction:    "client_to_upstream",
				MessageType:  relayMessageTypeString(msgType),
				PayloadBytes: len(payload),
				Error:        err.Error(),
			})
			exitCh <- relayExitSignal{stage: "write_upstream", err: err}
			return
		}
		if forwardedFrames != nil {
			forwardedFrames.Add(1)
		}
		markActivity()
	}
}

func runUpstreamToClient(
	ctx context.Context,
	upstreamConn FrameConn,
	writeClient func(msgType coderws.MessageType, payload []byte) error,
	startAt time.Time,
	nowFn func() time.Time,
	state *relayState,
	onUsageParseFailure func(eventType string, usageRaw string),
	onTurnComplete func(turn RelayTurnResult),
	beforeWriteClient func(msgType coderws.MessageType, payload []byte, wroteDownstream bool) error,
	beforeClientWrite func(msgType coderws.MessageType, payload []byte),
	afterClientWrite func(msgType coderws.MessageType, payload []byte, writeErr error),
	afterWriteClient func(msgType coderws.MessageType, payload []byte),
	dropDownstreamWrites *atomic.Bool,
	forwardedFrames *atomic.Int64,
	droppedFrames *atomic.Int64,
	markActivity func(),
	onTrace func(event RelayTraceEvent),
	exitCh chan<- relayExitSignal,
) {
	wroteDownstream := false
	for {
		msgType, payload, err := upstreamConn.ReadFrame(ctx)
		if err != nil {
			emitTurnComplete(onTurnComplete, state, finalizePendingBareError(state, nowFn()))
			// Preserve a last non-terminal usage snapshot when the upstream closes
			// before its terminal event. A normal terminal has already consumed it.
			finalizeRelayTurnUsage(state)
			emitRelayTrace(onTrace, RelayTraceEvent{
				Stage:           "read_upstream_failed",
				Direction:       "upstream_to_client",
				Error:           err.Error(),
				Graceful:        isDisconnectError(err),
				WroteDownstream: wroteDownstream,
			})
			exitCh <- relayExitSignal{
				stage:           "read_upstream",
				err:             err,
				graceful:        isDisconnectError(err),
				wroteDownstream: wroteDownstream,
			}
			return
		}
		markActivity()
		if beforeWriteClient != nil {
			if err := beforeWriteClient(msgType, payload, wroteDownstream); err != nil {
				emitRelayTrace(onTrace, RelayTraceEvent{
					Stage:           "upstream_message_rejected",
					Direction:       "upstream_to_client",
					MessageType:     relayMessageTypeString(msgType),
					PayloadBytes:    len(payload),
					WroteDownstream: wroteDownstream,
					Error:           err.Error(),
				})
				exitCh <- relayExitSignal{
					stage:           "upstream_message",
					err:             err,
					wroteDownstream: wroteDownstream,
				}
				return
			}
		}
		observedEvent := observedUpstreamEvent{}
		switch msgType {
		case coderws.MessageText:
			eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
			if shouldFinalizePendingBareError(state, payload, eventType) {
				emitTurnComplete(onTurnComplete, state, finalizePendingBareError(state, nowFn()))
			}
			observedEvent = observeUpstreamMessage(state, payload, startAt, nowFn, onUsageParseFailure)
		case coderws.MessageBinary:
			// binary frame 直接透传，不进入 JSON 观测路径（避免无效解析开销）。
		}
		emitTurnComplete(onTurnComplete, state, observedEvent)
		if dropDownstreamWrites != nil && dropDownstreamWrites.Load() {
			if droppedFrames != nil {
				droppedFrames.Add(1)
			}
			emitRelayTrace(onTrace, RelayTraceEvent{
				Stage:           "drop_downstream_frame",
				Direction:       "upstream_to_client",
				MessageType:     relayMessageTypeString(msgType),
				PayloadBytes:    len(payload),
				WroteDownstream: wroteDownstream,
			})
			if observedEvent.terminal {
				exitCh <- relayExitSignal{
					stage:           "drain_terminal",
					graceful:        true,
					wroteDownstream: wroteDownstream,
				}
				return
			}
			markActivity()
			continue
		}
		if beforeClientWrite != nil {
			beforeClientWrite(msgType, payload)
		}
		writeErr := writeClient(msgType, payload)
		if afterClientWrite != nil {
			afterClientWrite(msgType, payload, writeErr)
		}
		if writeErr != nil {
			emitRelayTrace(onTrace, RelayTraceEvent{
				Stage:           "write_client_failed",
				Direction:       "upstream_to_client",
				MessageType:     relayMessageTypeString(msgType),
				PayloadBytes:    len(payload),
				WroteDownstream: wroteDownstream,
				Error:           writeErr.Error(),
			})
			exitCh <- relayExitSignal{stage: "write_client", err: writeErr, wroteDownstream: wroteDownstream}
			return
		}
		wroteDownstream = true
		if afterWriteClient != nil {
			afterWriteClient(msgType, payload)
		}
		if forwardedFrames != nil {
			forwardedFrames.Add(1)
		}
		markActivity()
	}
}

func runIdleWatchdog(
	ctx context.Context,
	nowFn func() time.Time,
	idleTimeout time.Duration,
	lastActivity *atomic.Int64,
	onTrace func(event RelayTraceEvent),
	exitCh chan<- relayExitSignal,
) {
	if idleTimeout <= 0 {
		return
	}
	checkInterval := minDuration(idleTimeout/4, 5*time.Second)
	if checkInterval < time.Second {
		checkInterval = time.Second
	}
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last := time.Unix(0, lastActivity.Load())
			if nowFn().Sub(last) < idleTimeout {
				continue
			}
			emitRelayTrace(onTrace, RelayTraceEvent{
				Stage:     "idle_timeout_triggered",
				Direction: "watchdog",
				Error:     context.DeadlineExceeded.Error(),
			})
			exitCh <- relayExitSignal{stage: "idle_timeout", err: context.DeadlineExceeded}
			return
		}
	}
}

func emitRelayTrace(onTrace func(event RelayTraceEvent), event RelayTraceEvent) {
	if onTrace == nil {
		return
	}
	onTrace(event)
}

func relayMessageTypeString(msgType coderws.MessageType) string {
	switch msgType {
	case coderws.MessageText:
		return "text"
	case coderws.MessageBinary:
		return "binary"
	default:
		return "unknown(" + strconv.Itoa(int(msgType)) + ")"
	}
}

func relayDirectionFromStage(stage string) string {
	switch stage {
	case "read_client", "write_upstream":
		return "client_to_upstream"
	case "read_upstream", "write_client", "drain_terminal":
		return "upstream_to_client"
	case "idle_timeout":
		return "watchdog"
	default:
		return ""
	}
}

func relayErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func observeUpstreamMessage(
	state *relayState,
	message []byte,
	startAt time.Time,
	nowFn func() time.Time,
	onUsageParseFailure func(eventType string, usageRaw string),
) observedUpstreamEvent {
	if state == nil {
		return observedUpstreamEvent{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return observeUpstreamMessageLocked(state, message, startAt, nowFn, onUsageParseFailure)
}

// selectResponseID accepts only canonical Responses identifiers. Upstream
// event and item IDs also commonly appear in the top-level id field, so an
// arbitrary non-empty value must never become the continuation or billing key.
func selectResponseID(candidates ...string) string {
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate)
		if strings.HasPrefix(id, "resp_") {
			return id
		}
	}
	return ""
}

func observeUpstreamMessageLocked(
	state *relayState,
	message []byte,
	startAt time.Time,
	nowFn func() time.Time,
	onUsageParseFailure func(eventType string, usageRaw string),
) observedUpstreamEvent {
	if state == nil || len(message) == 0 {
		return observedUpstreamEvent{}
	}
	values := gjson.GetManyBytes(message, "type", "response.id", "response_id", "id")
	eventType := strings.TrimSpace(values[0].String())
	if eventType == "" {
		return observedUpstreamEvent{}
	}
	responseID := selectResponseID(values[1].String(), values[2].String())
	// 仅 terminal 事件兜底读取顶层 id，避免把 event_id 当成 response_id 关联到 turn。
	if responseID == "" && isTerminalEvent(eventType) {
		responseID = selectResponseID(values[3].String())
	}
	now := nowFn()
	if eventType == "response.created" && responseID != "" {
		state.observeResponseLifecycle(responseID)
	}

	if state.firstTokenMs == nil && isTokenEvent(eventType) {
		ms := int(now.Sub(startAt).Milliseconds())
		if ms >= 0 {
			state.firstTokenMs = &ms
		}
		if state.activeTurn != nil && state.activeTurn.firstTokenMs == nil {
			tms := int(now.Sub(state.activeTurn.startAt).Milliseconds())
			if tms >= 0 {
				state.activeTurn.firstTokenMs = &tms
			}
		}
	}
	terminal := isTerminalEvent(eventType)
	duplicateTerminal := false
	if terminal && eventType != "error" {
		if responseID != "" {
			if state.completedResponseIDs != nil {
				_, duplicateTerminal = state.completedResponseIDs[responseID]
			}
			// A bare error may have already been associated with an ID that was
			// seen in a prior lifecycle frame. Let the authoritative failed event
			// through so it can replace the deferred error snapshot.
			if duplicateTerminal && eventType == "response.failed" && state.pendingBareError != nil &&
				state.pendingBareError.responseID == responseID &&
				!state.terminalSeen.Load() && !state.anonymousTerminalSeen.Load() {
				duplicateTerminal = false
			}
			lifecycleID, lifecycleStarted := state.responseLifecycle()
			if !duplicateTerminal && lifecycleStarted && state.terminalSeen.Load() && lifecycleID != responseID {
				// A response.created event established a response identity for the
				// current client turn. Once that turn has reached a terminal event,
				// a different ID without a new response.created is an ID-mutated
				// replay, even when no intervening data frame was observed.
				duplicateTerminal = true
			}
			if !duplicateTerminal && (state.terminalSeen.Load() || state.anonymousTerminalSeen.Load()) {
				// A client response.create is the only supported way to start a
				// second billable turn.  Once a terminal snapshot has been accepted,
				// every later ID-bearing terminal frame belongs to that same turn,
				// even when the provider (or an injected frame) changes response.id.
				// This closes the no-lifecycle variant of the ID-mutation replay.
				duplicateTerminal = true
			}
			if !duplicateTerminal && state.terminalSeen.Load() && state.nonTerminalAfterTerminal.Load() {
				// A terminal was already accepted for this client turn and an
				// intervening upstream frame followed it. A new response ID is
				// not evidence of a new turn; only a client response.create
				// clears this guard.
				duplicateTerminal = true
			}
		} else {
			// A few compatible upstreams omit response.id/response_id on their
			// terminal snapshots. Treat any later terminal event in the same
			// client turn as a duplicate, even when an upstream adds/removes an
			// ID or changes its usage values.
			duplicateTerminal = state.anonymousTerminalSeen.Load() || state.terminalSeen.Load()
		}
	}
	parsedUsage := Usage{}
	if !duplicateTerminal {
		parsedUsage = parseUsageAndAccumulateLocked(state, message, eventType, onUsageParseFailure)
	}
	observed := observedUpstreamEvent{
		eventType:         eventType,
		responseID:        responseID,
		usage:             parsedUsage,
		duplicateTerminal: duplicateTerminal,
	}
	if duplicateTerminal {
		// A late duplicate must not create/delete turn timing or disturb an
		// active subsequent turn. It is drained for protocol completion only.
		observed.terminal = true
		return observed
	}
	if terminal {
		tier := gjson.GetBytes(message, "response.service_tier")
		if tier.Type == gjson.String {
			observed.serviceTier = tier.String()
		}
	}
	// Mark an ID-less turn terminal before updating state. This closes the race
	// where two anonymous terminal frames arrive back-to-back. ID-bearing
	// terminals use the response-ID set instead, allowing distinct serial turns.
	// A bare error is held until a possible response.failed frame arrives so
	// the authoritative terminal snapshot can replace its fallback usage. It is
	// not itself a billable anonymous terminal and must not consume the marker.
	if terminal && eventType != "error" && responseID == "" && state != nil && !state.anonymousTerminalSeen.CompareAndSwap(false, true) {
		observed.terminal = true
		observed.duplicateTerminal = true
		return observed
	}
	var turnTiming *relayTurnTiming
	if responseID != "" {
		turnTiming = openAIWSRelayGetOrInitTurnTiming(state, responseID, now)
		if turnTiming != nil && turnTiming.firstTokenMs == nil && isTokenEvent(eventType) {
			ms := int(now.Sub(turnTiming.startAt).Milliseconds())
			if ms >= 0 {
				turnTiming.firstTokenMs = &ms
			}
		}
	} else {
		turnTiming = state.activeTurn
	}
	observeRelayTurnResponseModel(turnTiming, firstRelayResponseModel(message), terminal)
	if !terminal {
		// Upstream data is not a turn boundary. Keep terminalSeen set and
		// remember that data followed a terminal so an ID-mutated replay
		// cannot be billed as another response.
		if state.terminalSeen.Load() {
			state.nonTerminalAfterTerminal.Store(true)
		}
		return observed
	}
	if eventType == "error" {
		// Some Responses servers emit a bare error immediately before
		// response.failed. Defer settlement so the authoritative failed usage
		// can replace this fallback instead of billing both snapshots.
		if responseID == "" {
			responseID = openAIWSRelayActiveTurnID(state)
		}
		observed.responseID = responseID
		pending := observed
		state.pendingBareError = &pending
		return observed
	}
	state.pendingBareError = nil
	return finalizeObservedRelayTerminalLocked(state, observed, now)
}

// shouldFinalizePendingBareError decides whether a frame proves that a
// deferred bare error belongs to a completed turn.  response.failed is kept
// in the same turn so its terminal snapshot can replace the error snapshot;
// an unrelated response/lifecycle frame settles the error first.
func shouldFinalizePendingBareError(state *relayState, payload []byte, eventType string) bool {
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return shouldFinalizePendingBareErrorLocked(state, payload, eventType)
}

func shouldFinalizePendingBareErrorLocked(state *relayState, payload []byte, eventType string) bool {
	if state == nil || state.pendingBareError == nil {
		return false
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" || eventType == "error" || eventType == "response.failed" {
		return false
	}
	if isTerminalEvent(eventType) || eventType == "response.created" {
		return true
	}
	values := gjson.GetManyBytes(payload, "response.id", "response_id")
	responseID := selectResponseID(values[0].String(), values[1].String())
	if responseID == "" || state.pendingBareError.responseID == "" {
		return false
	}
	return responseID != state.pendingBareError.responseID
}

func finalizePendingBareError(state *relayState, now time.Time) observedUpstreamEvent {
	if state == nil {
		return observedUpstreamEvent{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return finalizePendingBareErrorLocked(state, now)
}

func finalizePendingBareErrorLocked(state *relayState, now time.Time) observedUpstreamEvent {
	if state == nil || state.pendingBareError == nil {
		return observedUpstreamEvent{}
	}
	observed := *state.pendingBareError
	state.pendingBareError = nil
	return finalizeObservedRelayTerminalLocked(state, observed, now)
}

func finalizeObservedRelayTerminalLocked(state *relayState, observed observedUpstreamEvent, now time.Time) observedUpstreamEvent {
	if state == nil || strings.TrimSpace(observed.eventType) == "" {
		return observedUpstreamEvent{}
	}
	observed.usage = finalizeRelayTurnUsageLocked(state)
	observed.terminal = true
	responseID := strings.TrimSpace(observed.responseID)
	state.lastServiceTier = observed.serviceTier
	if responseID != "" {
		markCompletedResponseID(state, responseID)
		state.lastResponseID = responseID
		if turnTiming, ok := openAIWSRelayDeleteTurnTiming(state, responseID); ok {
			observed.responseModel = relayTurnResponseModel(&turnTiming)
			observed.responseConflict = turnTiming.responseModelConflict
			state.lastResponseModel = observed.responseModel
			state.responseConflict = observed.responseConflict
			duration := now.Sub(turnTiming.startAt)
			if duration < 0 {
				duration = 0
			}
			observed.duration = duration
			observed.firstToken = openAIWSRelayCloneIntPtr(turnTiming.firstTokenMs)
		}
		// Keep the marker for every accepted ID-bearing terminal. A direct
		// sequence of distinct terminal snapshots remains compatible with
		// legacy providers; intervening data is handled by observeUpstreamMessage.
		state.terminalSeen.Store(true)
		state.nonTerminalAfterTerminal.Store(false)
	} else {
		// Mark only an ID-less terminal event for the current turn. ID-bearing
		// events must not poison the anonymous window for a later turn.
		state.anonymousTerminalSeen.Store(true)
		openAIWSRelayDiscardActiveTurnTiming(state)
	}
	state.terminalEventType = observed.eventType
	return observed
}

func openAIWSRelayActiveTurnID(state *relayState) string {
	if state == nil || state.activeTurn == nil {
		return ""
	}
	for responseID, timing := range state.turnTimingByID {
		if timing == state.activeTurn {
			return responseID
		}
	}
	return ""
}

func openAIWSRelayDiscardActiveTurnTiming(state *relayState) {
	if state == nil || state.activeTurn == nil {
		return
	}
	active := state.activeTurn
	for responseID, timing := range state.turnTimingByID {
		if timing == active {
			delete(state.turnTimingByID, responseID)
		}
	}
	state.activeTurn = nil
}

func markCompletedResponseID(state *relayState, responseID string) {
	if state == nil || responseID == "" {
		return
	}
	if state.completedResponseIDs == nil {
		state.completedResponseIDs = make(map[string]struct{}, 8)
	}
	if _, exists := state.completedResponseIDs[responseID]; exists {
		return
	}
	if len(state.completedResponseOrder) >= maxCompletedResponseIDs {
		oldest := state.completedResponseOrder[0]
		state.completedResponseOrder = state.completedResponseOrder[1:]
		delete(state.completedResponseIDs, oldest)
	}
	state.completedResponseIDs[responseID] = struct{}{}
	state.completedResponseOrder = append(state.completedResponseOrder, responseID)
}

func emitTurnComplete(
	onTurnComplete func(turn RelayTurnResult),
	state *relayState,
	observed observedUpstreamEvent,
) {
	if onTurnComplete == nil || !observed.terminal || observed.duplicateTerminal {
		return
	}
	responseID := strings.TrimSpace(observed.responseID)
	if responseID == "" && strings.TrimSpace(observed.eventType) != "error" {
		return
	}
	requestModel := ""
	if state != nil {
		requestModel = state.currentRequestModel()
	}
	onTurnComplete(RelayTurnResult{
		ServiceTier:           observed.serviceTier,
		RequestModel:          requestModel,
		ResponseModel:         observed.responseModel,
		ResponseModelConflict: observed.responseConflict,
		Usage:                 observed.usage,
		RequestID:             responseID,
		TerminalEventType:     observed.eventType,
		Duration:              observed.duration,
		FirstTokenMs:          openAIWSRelayCloneIntPtr(observed.firstToken),
	})
}

func firstRelayResponseModel(message []byte) string {
	if len(message) == 0 {
		return ""
	}
	values := gjson.GetManyBytes(message, "response.model", "model")
	for _, value := range values {
		if value.Type != gjson.String {
			continue
		}
		if model := strings.TrimSpace(value.String()); model != "" {
			return model
		}
	}
	return ""
}

func observeRelayTurnResponseModel(turn *relayTurnTiming, model string, terminal bool) {
	if turn == nil {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	current := relayTurnResponseModel(turn)
	if current != "" && !strings.EqualFold(current, model) {
		turn.responseModelConflict = true
	}
	if terminal {
		turn.terminalResponseModel = model
		return
	}
	if turn.firstResponseModel == "" {
		turn.firstResponseModel = model
	}
}

func relayTurnResponseModel(turn *relayTurnTiming) string {
	if turn == nil {
		return ""
	}
	if turn.terminalResponseModel != "" {
		return turn.terminalResponseModel
	}
	return turn.firstResponseModel
}

func openAIWSRelayGetOrInitTurnTiming(state *relayState, responseID string, now time.Time) *relayTurnTiming {
	if state == nil {
		return nil
	}
	if state.turnTimingByID == nil {
		state.turnTimingByID = make(map[string]*relayTurnTiming, 8)
	}
	timing, ok := state.turnTimingByID[responseID]
	if !ok || timing == nil || timing.startAt.IsZero() {
		timing = &relayTurnTiming{startAt: now}
		state.turnTimingByID[responseID] = timing
		state.activeTurn = timing
		return timing
	}
	return timing
}

func openAIWSRelayDeleteTurnTiming(state *relayState, responseID string) (relayTurnTiming, bool) {
	if state == nil || state.turnTimingByID == nil {
		return relayTurnTiming{}, false
	}
	timing, ok := state.turnTimingByID[responseID]
	if !ok || timing == nil {
		return relayTurnTiming{}, false
	}
	delete(state.turnTimingByID, responseID)
	if state.activeTurn == timing {
		state.activeTurn = nil
	}
	return *timing, true
}

func openAIWSRelayCloneIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func parseUsageAndAccumulate(
	state *relayState,
	message []byte,
	eventType string,
	onParseFailure func(eventType string, usageRaw string),
) Usage {
	if state == nil {
		return Usage{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return parseUsageAndAccumulateLocked(state, message, eventType, onParseFailure)
}

func parseUsageAndAccumulateLocked(
	state *relayState,
	message []byte,
	eventType string,
	onParseFailure func(eventType string, usageRaw string),
) Usage {
	eventType = strings.TrimSpace(eventType)
	if state == nil || len(message) == 0 || !shouldParseUsage(eventType) {
		return Usage{}
	}
	if !gjson.ValidBytes(message) {
		recordUsageParseFailure()
		if onParseFailure != nil {
			onParseFailure(eventType, string(message))
		}
		return Usage{}
	}
	usageResult := gjson.GetBytes(message, "response.usage")
	if !usageResult.Exists() {
		// A few Responses-compatible providers put usage at the event root.
		// Treat both locations identically so a forged root-level snapshot cannot
		// bypass validation while a legitimate provider is not under-billed.
		usageResult = gjson.GetBytes(message, "usage")
	}
	if !usageResult.Exists() {
		return Usage{}
	}
	usageRaw := strings.TrimSpace(usageResult.Raw)
	if usageRaw == "" || !strings.HasPrefix(usageRaw, "{") {
		recordUsageParseFailure()
		if onParseFailure != nil {
			onParseFailure(eventType, usageRaw)
		}
		return Usage{}
	}
	// Validate every known token field before selecting aliases. This prevents
	// gjson.Int from silently truncating a fractional or overflowing nested
	// cache/image value while another alias happens to be present.
	if !validateOpenAIUsageJSON(usageResult) {
		recordUsageParseFailure()
		if onParseFailure != nil {
			onParseFailure(eventType, usageRaw)
		}
		return Usage{}
	}

	inputResult := usageResult.Get("input_tokens")
	if !inputResult.Exists() {
		inputResult = usageResult.Get("prompt_tokens")
	}
	outputResult := usageResult.Get("output_tokens")
	if !outputResult.Exists() {
		outputResult = usageResult.Get("completion_tokens")
	}
	cachedResult := usageResult.Get("input_tokens_details.cached_tokens")
	if !cachedResult.Exists() {
		cachedResult = usageResult.Get("prompt_tokens_details.cached_tokens")
	}
	imageTokens, imageOK := parseUsageIntField(usageResult.Get("output_tokens_details.image_tokens"), false)
	if !imageOK {
		recordUsageParseFailure()
		if onParseFailure != nil {
			onParseFailure(eventType, usageRaw)
		}
		return Usage{}
	}
	if imageTokens == 0 {
		imageTokens, imageOK = parseUsageIntField(usageResult.Get("completion_tokens_details.image_tokens"), false)
		if !imageOK {
			recordUsageParseFailure()
			if onParseFailure != nil {
				onParseFailure(eventType, usageRaw)
			}
			return Usage{}
		}
	}

	// Only response terminal snapshots require both primary totals. Interim
	// events are often partial (for example, input on response.in_progress and
	// output on a later event), so absent fields remain zero and are merged into
	// the current turn snapshot below.
	requireTotals := isTerminalEvent(eventType) && eventType != "error"
	inputTokens, inputOK := parseUsageIntField(inputResult, requireTotals)
	outputTokens, outputOK := parseUsageIntField(outputResult, requireTotals)
	cachedTokens, cachedOK := parseUsageIntField(cachedResult, false)
	if !inputOK || !outputOK || !cachedOK {
		recordUsageParseFailure()
		if onParseFailure != nil {
			onParseFailure(eventType, usageRaw)
		}
		// 解析失败时不做部分字段累加，避免计费 usage 出现“半有效”状态。
		return Usage{}
	}
	cacheCreationTokens, cacheCreationOK := openAICacheCreationTokensFromUsage(usageResult)
	if !cacheCreationOK {
		recordUsageParseFailure()
		if onParseFailure != nil {
			onParseFailure(eventType, usageRaw)
		}
		return Usage{}
	}
	parsedUsage := Usage{
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheCreationInputTokens: cacheCreationTokens,
		CacheReadInputTokens:     cachedTokens,
		ImageOutputTokens:        imageTokens,
	}
	if parsedUsage.InputTokens < 0 || parsedUsage.OutputTokens < 0 ||
		parsedUsage.CacheCreationInputTokens < 0 || parsedUsage.CacheReadInputTokens < 0 ||
		parsedUsage.ImageOutputTokens < 0 ||
		parsedUsage.InputTokens > maxReportedUsageTokens || parsedUsage.OutputTokens > maxReportedUsageTokens ||
		parsedUsage.CacheCreationInputTokens > maxReportedUsageTokens || parsedUsage.CacheReadInputTokens > maxReportedUsageTokens ||
		parsedUsage.ImageOutputTokens > maxReportedUsageTokens {
		recordUsageParseFailure()
		if onParseFailure != nil {
			onParseFailure(eventType, usageRaw)
		}
		return Usage{}
	}

	if isTerminalEvent(eventType) {
		// Terminal usage is authoritative for the turn. An empty terminal
		// snapshot is common on failed/cancelled responses; retain the best
		// interim snapshot instead of erasing already observed usage.
		if relayUsageHasTokens(parsedUsage) || !relayUsageHasTokens(state.turnUsage) {
			state.turnUsage = parsedUsage
		}
	} else {
		// Every non-terminal usage frame is a snapshot. Keep the latest non-zero
		// value for each component, never add snapshots together.
		mergeRelayUsageNonZero(&state.turnUsage, parsedUsage)
		return Usage{}
	}
	return parsedUsage
}

func relayUsageHasTokens(usage Usage) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0 ||
		usage.ImageOutputTokens > 0
}

func mergeRelayUsageNonZero(dst *Usage, src Usage) {
	if dst == nil {
		return
	}
	if src.InputTokens > 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > 0 {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CacheCreationInputTokens > 0 {
		dst.CacheCreationInputTokens = src.CacheCreationInputTokens
	}
	if src.CacheReadInputTokens > 0 {
		dst.CacheReadInputTokens = src.CacheReadInputTokens
	}
	if src.ImageOutputTokens > 0 {
		dst.ImageOutputTokens = src.ImageOutputTokens
	}
}

// finalizeRelayTurnUsage moves exactly one validated turn snapshot into the
// aggregate and clears it. The operation is transactional so a malformed or
// overflowing aggregate cannot partially affect billing.
func finalizeRelayTurnUsage(state *relayState) Usage {
	if state == nil {
		return Usage{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return finalizeRelayTurnUsageLocked(state)
}

func finalizeRelayTurnUsageLocked(state *relayState) Usage {
	if state == nil {
		return Usage{}
	}
	turnUsage := state.turnUsage
	if !relayUsageHasTokens(turnUsage) {
		state.turnUsage = Usage{}
		return Usage{}
	}
	next := state.usage
	if !addUsageWithinLimit(&next.InputTokens, turnUsage.InputTokens) ||
		!addUsageWithinLimit(&next.OutputTokens, turnUsage.OutputTokens) ||
		!addUsageWithinLimit(&next.CacheCreationInputTokens, turnUsage.CacheCreationInputTokens) ||
		!addUsageWithinLimit(&next.CacheReadInputTokens, turnUsage.CacheReadInputTokens) ||
		!addUsageWithinLimit(&next.ImageOutputTokens, turnUsage.ImageOutputTokens) {
		recordUsageParseFailure()
		state.turnUsage = Usage{}
		return Usage{}
	}
	state.usage = next
	state.turnUsage = Usage{}
	return turnUsage
}

func addUsageWithinLimit(current *int, incoming int) bool {
	if current == nil || incoming < 0 || incoming > maxReportedUsageTokens {
		return false
	}
	if *current < 0 || *current > maxReportedUsageTokens-incoming {
		return false
	}
	*current += incoming
	return true
}

func parseUsageIntField(value gjson.Result, required bool) (int, bool) {
	if !value.Exists() {
		return 0, !required
	}
	if value.Type == gjson.Null {
		return 0, !required
	}
	return boundedReportedUsageGJSONInt(value)
}

func openAICacheCreationTokensFromUsage(value gjson.Result) (int, bool) {
	for _, field := range []string{
		"input_tokens_details.cache_write_tokens",
		"prompt_tokens_details.cache_write_tokens",
		"input_tokens_details.cache_creation_tokens",
		"prompt_tokens_details.cache_creation_tokens",
	} {
		result := value.Get(field)
		if result.Exists() && result.Type != gjson.Null {
			parsed, ok := parseUsageIntField(result, false)
			if !ok {
				return 0, false
			}
			// Preserve the existing precedence: an explicit zero in the
			// first supported field wins over later aliases.
			return parsed, true
		}
	}
	for _, field := range []string{
		"cache_write_tokens",
		"cache_creation_input_tokens",
		"cache_write_input_tokens",
		"cache_creation_tokens",
	} {
		result := value.Get(field)
		if result.Exists() && result.Type != gjson.Null {
			tokens, ok := parseUsageIntField(result, false)
			if !ok {
				return 0, false
			}
			if tokens > 0 {
				return tokens, true
			}
		}
	}
	return 0, true
}

func enrichResult(result *RelayResult, state *relayState, duration time.Duration) {
	if result == nil {
		return
	}
	result.Duration = duration
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	enrichResultLocked(result, state)
}

func enrichResultLocked(result *RelayResult, state *relayState) {
	if result == nil || state == nil {
		return
	}
	result.RequestModel = state.currentRequestModel()
	result.ResponseModel = state.lastResponseModel
	result.ServiceTier = state.lastServiceTier
	result.ResponseModelConflict = state.responseConflict
	result.Usage = state.usage
	result.RequestID = state.lastResponseID
	result.TerminalEventType = state.terminalEventType
	result.FirstTokenMs = state.firstTokenMs
}

func (s *relayState) setRequestModel(model string) {
	if s == nil || model == "" {
		return
	}
	s.requestModelMu.Lock()
	s.requestModel = model
	s.requestModelMu.Unlock()
}

func (s *relayState) currentRequestModel() string {
	if s == nil {
		return ""
	}
	s.requestModelMu.RLock()
	defer s.requestModelMu.RUnlock()
	return s.requestModel
}

func isResponseCreateMessage(msgType coderws.MessageType, payload []byte) bool {
	return msgType == coderws.MessageText &&
		strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.create"
}

// startClientTurn clears markers that belong to the previous Responses WS
// turn. Completed response IDs remain in their bounded replay cache so a late
// frame from an older turn cannot be billed again.
func (s *relayState) startClientTurn() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startClientTurnLocked()
}

func (s *relayState) startClientTurnLocked() {
	if s == nil {
		return
	}
	// A client response.create is the authoritative billing boundary. Any
	// snapshot or deferred bare error from the previous turn must be discarded;
	// otherwise the next terminal event could settle stale usage into the new
	// request.
	s.turnUsage = Usage{}
	s.pendingBareError = nil
	// A response.create starts a new billing turn. Drop timing records left by
	// an interrupted/late previous response so its terminal frame cannot be
	// attached to the new turn after the boundary.
	s.turnTimingByID = nil
	s.activeTurn = nil
	s.lifecycleMu.Lock()
	s.lifecycleStarted = false
	s.lifecycleResponseID = ""
	s.lifecycleMu.Unlock()
	s.terminalSeen.Store(false)
	s.nonTerminalAfterTerminal.Store(false)
	s.anonymousTerminalSeen.Store(false)
}

func (s *relayState) observeResponseLifecycle(responseID string) {
	if s == nil || responseID == "" {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.lifecycleStarted {
		// Upstream lifecycle messages are metadata, not billing boundaries.  A
		// forged or repeated response.created must not reopen a completed turn;
		// startClientTurn handles the boundary when the client sends the next
		// response.create frame.
		return
	}
	s.lifecycleStarted = true
	s.lifecycleResponseID = responseID
}

func (s *relayState) responseLifecycle() (string, bool) {
	if s == nil {
		return "", false
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	return s.lifecycleResponseID, s.lifecycleStarted
}

func (s *relayState) resetAnonymousTerminal() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.anonymousTerminalSeen.Store(false)
	s.terminalSeen.Store(false)
	s.nonTerminalAfterTerminal.Store(false)
}

func isDisconnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}
	switch coderws.CloseStatus(err) {
	case coderws.StatusNormalClosure, coderws.StatusGoingAway, coderws.StatusNoStatusRcvd, coderws.StatusAbnormalClosure:
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	return strings.Contains(message, "failed to read frame header: eof") ||
		strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "use of closed network connection") ||
		strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "broken pipe")
}

func isTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "error":
		return true
	default:
		return false
	}
}

func shouldParseUsage(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	if eventType == "error" || isTerminalEvent(eventType) {
		return true
	}
	// Responses-compatible streams may expose cumulative usage on any
	// non-delta response event (for example response.in_progress). Those values
	// are snapshots and are merged into turnUsage without being billed yet.
	return strings.HasPrefix(eventType, "response.") && !strings.HasSuffix(eventType, ".delta")
}

func isTokenEvent(eventType string) bool {
	if eventType == "" {
		return false
	}
	switch eventType {
	case "response.created", "response.in_progress", "response.output_item.added", "response.output_item.done":
		return false
	}
	if strings.Contains(eventType, ".delta") {
		return true
	}
	if strings.HasPrefix(eventType, "response.output_text") {
		return true
	}
	if strings.HasPrefix(eventType, "response.output") {
		return true
	}
	return eventType == "response.completed" || eventType == "response.done"
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func waitRelayExit(exitCh <-chan relayExitSignal, timeout time.Duration) (relayExitSignal, bool) {
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	select {
	case sig := <-exitCh:
		return sig, true
	case <-time.After(timeout):
		return relayExitSignal{}, false
	}
}
