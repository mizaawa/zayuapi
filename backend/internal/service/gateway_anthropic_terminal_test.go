//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type anthropicTerminalTestResult struct {
	result *streamingResult
	err    error
}

func runAnthropicTerminalTestHandler(svc *GatewayService, resp *http.Response, c *gin.Context, passthrough bool) <-chan anthropicTerminalTestResult {
	done := make(chan anthropicTerminalTestResult, 1)
	go func() {
		var result *streamingResult
		var err error
		if passthrough {
			result, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "claude-model", "claude-model")
		} else {
			result, err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "claude-model", "claude-model", false)
		}
		done <- anthropicTerminalTestResult{result: result, err: err}
	}()
	return done
}

func TestAnthropicStreamingTerminalDoesNotWaitForUpstreamEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, disconnected := range []bool{false, true} {
			for _, terminal := range []struct {
				name  string
				block string
			}{
				{name: "message_stop", block: "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
				{name: "data_only_message_stop", block: "data: {\"type\":\"message_stop\"}\n\n"},
				{name: "done_marker", block: "data: [DONE]\n\n"},
			} {
				t.Run(fmt.Sprintf("passthrough=%t/disconnected=%t/%s", passthrough, disconnected, terminal.name), func(t *testing.T) {
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"cache_read_input_tokens\":3}}}\n\n")
						_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n")
						_, _ = io.WriteString(w, terminal.block)
						w.(http.Flusher).Flush()
						<-r.Context().Done()
					}))
					defer upstream.Close()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
					require.NoError(t, err)
					resp, err := upstream.Client().Do(req)
					require.NoError(t, err)
					defer func() { _ = resp.Body.Close() }()

					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
					if disconnected {
						c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
					}
					svc := newMinimalGatewayService()
					svc.cfg.Gateway.StreamDataIntervalTimeout = 1
					done := runAnthropicTerminalTestHandler(svc, resp, c, passthrough)
					select {
					case got := <-done:
						require.NoError(t, got.err)
						require.NotNil(t, got.result)
						require.Equal(t, 11, got.result.usage.InputTokens)
						require.Equal(t, 7, got.result.usage.OutputTokens)
						require.Equal(t, 3, got.result.usage.CacheReadInputTokens)
						require.Equal(t, disconnected, got.result.clientDisconnect)
						if !disconnected {
							require.Contains(t, rec.Body.String(), terminal.block)
							require.True(t, strings.HasSuffix(rec.Body.String(), "\n\n"))
							require.NotContains(t, rec.Body.String(), "stream_timeout")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("complete terminal event must finish while the upstream connection stays open")
					}
				})
			}
		}
	}
}

func TestAnthropicStreamingTerminalWaitsForCompleteEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%t", passthrough), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			defer func() { _ = writer.Close() }()
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
			done := runAnthropicTerminalTestHandler(newMinimalGatewayService(), resp, c, passthrough)
			for _, fragment := range []string{"event: message_stop\n", "data: {\"type\":\"message_stop\"}\n"} {
				_, err := io.WriteString(writer, fragment)
				require.NoError(t, err)
				select {
				case <-done:
					t.Fatal("terminal event ended before its data and blank-line delimiter were received")
				case <-time.After(100 * time.Millisecond):
				}
			}
			_, err := io.WriteString(writer, "\n")
			require.NoError(t, err)
			select {
			case got := <-done:
				require.NoError(t, got.err)
				require.Contains(t, rec.Body.String(), "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			case <-time.After(time.Second):
				t.Fatal("complete terminal event must not require EOF")
			}
		})
	}
}

func TestAnthropicStreamingEventNameWithoutDataIsIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, payload := range []string{"event: message_stop\n", "event: message_stop\n\n", "event: message_stop\ndata:\n\n"} {
			t.Run(fmt.Sprintf("passthrough=%t/%q", passthrough, payload), func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}
				got := <-runAnthropicTerminalTestHandler(newMinimalGatewayService(), resp, c, passthrough)
				require.ErrorContains(t, got.err, "missing terminal event")
			})
		}
	}
}
