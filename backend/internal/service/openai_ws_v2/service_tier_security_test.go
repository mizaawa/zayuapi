package openai_ws_v2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRelayServiceTierComesFromTerminalAndResetsPerTurn(t *testing.T) {
	state := &relayState{}
	start := time.Now()
	observeUpstreamMessage(state, []byte(`{"type":"response.created","response":{"id":"resp_1","service_tier":"flex"}}`), start, time.Now, nil)
	first := observeUpstreamMessage(state, []byte(`{"type":"response.completed","response":{"id":"resp_1","service_tier":"default","usage":{"input_tokens":1}}}`), start, time.Now, nil)
	var turn RelayTurnResult
	emitTurnComplete(func(result RelayTurnResult) { turn = result }, state, first)
	require.Equal(t, "default", turn.ServiceTier)
	state.startClientTurn()
	second := observeUpstreamMessage(state, []byte(`{"type":"response.completed","response":{"id":"resp_2","usage":{"input_tokens":1}}}`), start, time.Now, nil)
	emitTurnComplete(func(result RelayTurnResult) { turn = result }, state, second)
	require.Empty(t, turn.ServiceTier)
	var result RelayResult
	enrichResultLocked(&result, state)
	require.Empty(t, result.ServiceTier)
}
