package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWSPassthroughGPT6SolReasoningEffortMatchesWirePerTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	upstream := newStagedPassthroughConn()
	type turnOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	outcomes := make(chan turnOutcome, 4)
	hooks := &OpenAIWSIngressHooks{
		MaxReasoningEffort: "xhigh",
		ReasoningEffortMappings: []ReasoningEffortMapping{
			{From: "max", To: "xhigh"},
		},
		AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
			outcomes <- turnOutcome{result: result, err: err}
		},
	}
	server, serverErr := startPassthroughHookRecordingServer(
		t, controlCtx,
		newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
		passthroughLifecycleAccount(), hooks,
	)
	t.Cleanup(server.Close)
	t.Cleanup(func() { cancelControl(context.Canceled) })
	clientConn := dialReasoningEffortPassthroughClient(t, server)
	t.Cleanup(func() { _ = clientConn.CloseNow() })

	for turn, effort := range []string{"xhigh", "max", "ultra", "xhigh"} {
		body, err := json.Marshal(map[string]any{
			"type": "response.create", "model": "gpt-6-sol",
			"input": "hello", "reasoning": map[string]any{"effort": effort},
		})
		require.NoError(t, err)
		writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
		err = clientConn.Write(writeCtx, coderws.MessageText, body)
		cancelWrite()
		require.NoError(t, err)

		wire := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
		require.Equal(t, "gpt-6-sol", gjson.GetBytes(wire, "model").String())
		require.Equal(t, effort, gjson.GetBytes(wire, "reasoning.effort").String())
		upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_effort_%d","model":"gpt-6-sol","usage":{"input_tokens":1,"output_tokens":1}}}`, turn))
		event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
		require.NoError(t, err)
		require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())

		select {
		case outcome := <-outcomes:
			require.NoError(t, outcome.err)
			require.NotNil(t, outcome.result)
			require.NotNil(t, outcome.result.ReasoningEffort)
			require.Equal(t, effort, *outcome.result.ReasoningEffort)
		case <-time.After(3 * time.Second):
			t.Fatal("missing reasoning effort usage result")
		}
	}

	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("reasoning effort passthrough did not exit")
	}
}

func dialReasoningEffortPassthroughClient(t *testing.T, server *httptest.Server) *coderws.Conn {
	t.Helper()
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDial()
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	return clientConn
}
