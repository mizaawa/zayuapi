//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type monitorChunk struct {
	after time.Duration
	data  string
}
type monitorScriptedBody struct {
	chunks    []monitorChunk
	remaining string
}

func (r *monitorScriptedBody) Read(p []byte) (int, error) {
	if r.remaining == "" {
		if len(r.chunks) == 0 {
			return 0, io.EOF
		}
		c := r.chunks[0]
		r.chunks = r.chunks[1:]
		time.Sleep(c.after)
		r.remaining = c.data
	}
	n := copy(p, r.remaining)
	r.remaining = r.remaining[n:]
	return n, nil
}

type monitorTestTransport func(*http.Request) (*http.Response, error)

func (f monitorTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMonitorStreamMeasuresFirstGeneratedTokenNotLastByte(t *testing.T) {
	for _, tc := range []struct{ name, provider, mode, prelude, token, answer, done string }{
		{"chat", MonitorProviderOpenAI, "", `{"choices":[{"delta":{"role":"assistant","content":""}}]}`, `{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`, `{"choices":[{"delta":{"content":"42"}}]}`, "[DONE]"},
		{"responses", MonitorProviderCustom, MonitorAPIModeResponses, `{"type":"response.created"}`, `{"type":"response.output_text.delta","delta":"4"}`, `{"type":"response.output_text.delta","delta":"2"}`, `{"type":"response.completed"}`},
		{"anthropic", MonitorProviderAnthropic, "", `{"type":"ping"}`, `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"thinking"}}`, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"42"}}`, `{"type":"message_stop"}`},
		{"gemini", MonitorProviderGemini, "", `{"usageMetadata":{"promptTokenCount":4}}`, `{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]}}]}`, `{"candidates":[{"content":{"parts":[{"text":"42"}]}}]}`, `{"candidates":[{"finishReason":"STOP"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				orig := monitorHTTPClient
				defer func() { monitorHTTPClient = orig }()
				monitorHTTPClient = &http.Client{Timeout: monitorRequestTimeout, Transport: monitorTestTransport(func(r *http.Request) (*http.Response, error) {
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					_ = r.Body.Close()
					if tc.provider == MonitorProviderGemini {
						require.Contains(t, r.URL.Path, ":streamGenerateContent")
						require.Equal(t, "sse", r.URL.Query().Get("alt"))
					} else {
						require.Equal(t, true, body["stream"])
					}
					chunks := []monitorChunk{{time.Second, ": keepalive\n\ndata: " + tc.prelude + "\n\n"}, {time.Second, "data: " + tc.token + "\n\n"}, {70 * time.Second, "data: " + tc.answer + "\n\n"}, {time.Second, "data: " + tc.done + "\n\n"}}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}}, Body: io.NopCloser(&monitorScriptedBody{chunks: chunks})}, nil
				})}
				opts := &CheckOptions{APIMode: tc.mode, BodyOverrideMode: MonitorBodyOverrideModeReplace, BodyOverride: map[string]any{"instructions": "test", "input": "test", "model": "test", "messages": []any{"test"}}}
				res := runCheckForModel(context.Background(), tc.provider, "https://monitor.invalid", "key", "test", opts)
				require.Equal(t, MonitorStatusOperational, res.Status, res.Message)
				require.Equal(t, 2000, *res.LatencyMs)
				require.Equal(t, 73000, *res.DurationMs)
			})
		})
	}
}

func TestMonitorStreamRequiresAValidCompleteAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, frames       string
		wantToken, wantErr bool
	}{
		{"metadata only", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: [DONE]\n\n", false, false},
		{"error after token", "data: {\"choices\":[{\"delta\":{\"content\":\"42\"}}]}\n\ndata: {\"error\":{\"message\":\"failed upstream\"}}\n\n", true, true},
		{"truncated stream", "data: {\"choices\":[{\"delta\":{\"content\":\"42\"}}]}\n\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &io.LimitedReader{R: strings.NewReader(tc.frames), N: monitorResponseMaxBytes + 1}
			reply, err := readMonitorStream(r, monitorReply{}, MonitorProviderOpenAI, "", time.Now())
			require.Equal(t, tc.wantToken, reply.firstTokenMs != nil)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Empty(t, reply.text)
				require.False(t, validateChallenge(reply.text, "42"))
			}
		})
	}
}
