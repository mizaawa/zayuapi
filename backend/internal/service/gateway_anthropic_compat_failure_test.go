//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const anthropicCompatPartialStream = "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_partial","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":11,"cache_read_input_tokens":3}}}` + "\n\n" +
	"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"partial"}}` + "\n\n" +
	"event: message_delta\ndata: " + `{"type":"message_delta","delta":{},"usage":{"output_tokens":2}}` + "\n\n"

func runAnthropicCompatResponse(svc *GatewayService, protocol string, stream bool, response *http.Response, c *gin.Context) (*ForwardResult, error) {
	if protocol == "chat" {
		if stream {
			return svc.handleCCStreamingFromAnthropic(response, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now(), true)
		}
		return svc.handleCCBufferedFromAnthropic(response, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now())
	}
	if stream {
		return svc.handleResponsesStreamingResponse(response, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
	}
	return svc.handleResponsesBufferedStreamingResponse(response, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
}

func TestAnthropicCompatFailuresPreserveProtocolAndRetryBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"responses", "chat"} {
		for _, stream := range []bool{false, true} {
			for _, partial := range []bool{false, true} {
				for _, failure := range []string{"error_event", "read_error", "missing_terminal"} {
					name := protocol + "/buffered/before_output/" + failure
					if stream {
						name = strings.Replace(name, "buffered", "stream", 1)
					}
					if partial {
						name = strings.Replace(name, "before_output", "partial", 1)
					}
					t.Run(name, func(t *testing.T) {
						payload := "event: ping\ndata: {\"type\":\"ping\"}\n\n"
						if partial {
							payload += anthropicCompatPartialStream
						}
						if failure == "error_event" {
							payload += "event:error\ndata:" + `{"type":"error","error":{"type":"overloaded_error","message":"Upstream overloaded"}}` + "\n\n"
						}
						var reader io.Reader = strings.NewReader(payload)
						if failure == "read_error" {
							reader = io.MultiReader(reader, iotest.ErrReader(io.ErrUnexpectedEOF))
						}
						recorder := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(recorder)
						ctx := WithAPIKeyFailoverAttempt(t.Context())
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil).WithContext(ctx)
						response := &http.Response{Header: http.Header{"X-Request-Id": []string{"compat-partial"}}, Body: io.NopCloser(reader)}
						result, err := runAnthropicCompatResponse(&GatewayService{}, protocol, stream, response, c)
						require.Error(t, err)
						require.True(t, APIKeyFailoverAttemptFailed(ctx))
						require.NotContains(t, recorder.Body.String(), "response.completed")
						require.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
						if !stream || !partial {
							require.Nil(t, result)
							var failoverErr *UpstreamFailoverError
							require.ErrorAs(t, err, &failoverErr)
							require.False(t, c.Writer.Written(), "failed attempts must remain replayable before downstream output")
							return
						}
						require.NotNil(t, result)
						require.Equal(t, 11, result.Usage.InputTokens)
						require.Equal(t, 2, result.Usage.OutputTokens)
						require.Equal(t, 3, result.Usage.CacheReadInputTokens)
						require.True(t, IsResponseCommitted(c))
						require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
						if protocol == "responses" {
							require.Contains(t, recorder.Body.String(), "event: response.failed")
							require.Contains(t, recorder.Body.String(), `"id":"msg_partial"`)
						} else {
							require.Contains(t, recorder.Body.String(), `"error":{"message":`)
							require.Contains(t, recorder.Body.String(), "data: [DONE]")
						}
					})
				}
			}
		}
	}
}

func TestAnthropicCompatMissingModelDoesNotRetryAndUsesJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"responses", "chat"} {
		for _, stream := range []bool{false, true} {
			for _, failover := range []bool{false, true} {
				t.Run(protocol+"/"+map[bool]string{false: "buffered", true: "stream"}[stream]+"/"+map[bool]string{false: "normal", true: "failover"}[failover], func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					ctx := t.Context()
					if failover {
						ctx = WithAPIKeyFailoverAttempt(ctx)
					}
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil).WithContext(ctx)
					response := &http.Response{Body: io.NopCloser(strings.NewReader("event: error\ndata: " + `{"type":"error","error":{"type":"not_found_error","message":"model: claude-missing"}}` + "\n\n"))}
					result, err := runAnthropicCompatResponse(&GatewayService{}, protocol, stream, response, c)
					require.Nil(t, result)
					require.Error(t, err)
					if failover {
						require.True(t, APIKeyFailoverAttemptModelUnavailable(ctx))
						require.False(t, c.Writer.Written())
						return
					}
					require.Equal(t, http.StatusNotFound, recorder.Code)
					require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
					require.Contains(t, recorder.Body.String(), "claude-missing")
				})
			}
		}
	}
}

func TestAnthropicCompatTerminalStopReasonAllowsProviderEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"responses", "chat"} {
		for _, stream := range []bool{false, true} {
			t.Run(protocol+"/"+map[bool]string{false: "buffered", true: "stream"}[stream], func(t *testing.T) {
				payload := anthropicCompatPartialStream + "event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n"
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				response := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}
				result, err := runAnthropicCompatResponse(&GatewayService{}, protocol, stream, response, c)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 11, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
			})
		}
	}
}
