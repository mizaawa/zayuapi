package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIStreamTimeoutTestResult struct {
	usage *OpenAIUsage
	err   error
}

type openAIStreamRetryTestWriter struct {
	gin.ResponseWriter
	keepaliveStarted bool
	keepaliveWrites  int
	normalWrites     int
	semanticCommits  int
}

func (w *openAIStreamRetryTestWriter) OpenAICompactKeepaliveStarted() bool {
	return w.keepaliveStarted
}

func (w *openAIStreamRetryTestWriter) WriteOpenAICompactKeepalive(data []byte) (int, error) {
	w.keepaliveStarted = true
	w.keepaliveWrites++
	return w.ResponseWriter.Write(data)
}

func (w *openAIStreamRetryTestWriter) CommitOpenAIStreamOutput() {
	w.semanticCommits++
}

func (w *openAIStreamRetryTestWriter) Write(data []byte) (int, error) {
	w.normalWrites++
	return w.ResponseWriter.Write(data)
}

func runOpenAIStreamTimeoutTest(recorder *openAIResponseFlushRecorder, body io.ReadCloser, gatewayCfg config.GatewayConfig, passthrough bool, setups ...func(*gin.Context)) openAIStreamTimeoutTestResult {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for _, setup := range setups {
		setup(c)
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: gatewayCfg}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	account := &Account{ID: 1, Platform: PlatformOpenAI}
	if passthrough {
		result, err := svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, time.Now(), "model", "model")
		if result == nil {
			return openAIStreamTimeoutTestResult{err: err}
		}
		return openAIStreamTimeoutTestResult{usage: result.usage, err: err}
	}
	result, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "model", "model")
	if result == nil {
		return openAIStreamTimeoutTestResult{err: err}
	}
	return openAIStreamTimeoutTestResult{usage: result.usage, err: err}
}

func runOpenAIStreamTimeoutTestAsync(recorder *openAIResponseFlushRecorder, body io.ReadCloser, gatewayCfg config.GatewayConfig, passthrough bool, setups ...func(*gin.Context)) <-chan openAIStreamTimeoutTestResult {
	done := make(chan openAIStreamTimeoutTestResult, 1)
	go func() { done <- runOpenAIStreamTimeoutTest(recorder, body, gatewayCfg, passthrough, setups...) }()
	return done
}

func awaitOpenAIStreamTimeoutTest(t *testing.T, done <-chan openAIStreamTimeoutTestResult) openAIStreamTimeoutTestResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("stream handler did not return before its deadline")
		return openAIStreamTimeoutTestResult{}
	}
}

func TestOpenAIStreamTerminalEventReturnsWithoutEOF(t *testing.T) {
	for _, tt := range []struct {
		name        string
		passthrough bool
		cfg         config.GatewayConfig
	}{
		{name: "native synchronous"},
		{name: "native asynchronous", cfg: config.GatewayConfig{StreamDataIntervalTimeout: 30}},
		{name: "native guarded", cfg: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 1}},
		{name: "passthrough synchronous", passthrough: true},
		{name: "passthrough asynchronous", passthrough: true, cfg: config.GatewayConfig{StreamDataIntervalTimeout: 30}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = reader.Close() })
			t.Cleanup(func() { _ = writer.Close() })
			recorder := newOpenAIResponseFlushRecorder()
			done := runOpenAIStreamTimeoutTestAsync(recorder, reader, tt.cfg, tt.passthrough)
			payload := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ready\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_terminal\",\"usage\":{\"input_tokens\":7,\"output_tokens\":3},\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ready\"}]}]}}\n\n"
			_, err := writer.Write([]byte(payload))
			require.NoError(t, err)
			result := awaitOpenAIStreamTimeoutTest(t, done)
			require.NoError(t, result.err)
			require.Equal(t, 7, result.usage.InputTokens)
			require.Equal(t, 3, result.usage.OutputTokens)
			body, _ := recorder.snapshot()
			require.Equal(t, payload, body)
		})
	}
}

func TestOpenAIStreamPreOutputNetworkErrorsAreSanitized(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			networkErr := &net.OpError{
				Op: "read", Net: "tcp",
				Source: &net.TCPAddr{IP: net.ParseIP("10.18.2.3"), Port: 54321},
				Addr:   &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 443},
				Err:    syscall.ECONNRESET,
			}
			body := &openAIResponseFlushReadError{err: networkErr}
			result := runOpenAIStreamTimeoutTest(newOpenAIResponseFlushRecorder(), body, config.GatewayConfig{}, passthrough)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, result.err, &failoverErr)
			require.Contains(t, string(failoverErr.ResponseBody), "connection reset by peer")
			require.NotContains(t, string(failoverErr.ResponseBody), "10.18.2.3")
			require.NotContains(t, string(failoverErr.ResponseBody), "203.0.113.5")
			require.NotContains(t, string(failoverErr.ResponseBody), "54321")
		})
	}
}

func TestOpenAIPassthroughIdleTimeoutClosesUpstreamAndAllowsFailover(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	body := &firstOutputCloseTrackingBody{ReadCloser: reader, closed: make(chan struct{})}
	recorder := newOpenAIResponseFlushRecorder()
	done := runOpenAIStreamTimeoutTestAsync(recorder, body, config.GatewayConfig{StreamDataIntervalTimeout: 1}, true)
	result := awaitOpenAIStreamTimeoutTest(t, done)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, result.err, &failoverErr)
	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "stream data interval timeout")
	bodyText, flushes := recorder.snapshot()
	require.Empty(t, bodyText)
	require.Empty(t, flushes)
	select {
	case <-body.closed:
	default:
		t.Fatal("idle timeout did not close the blocked upstream reader")
	}
}

func TestOpenAIPassthroughKeepalivePreservesPreOutputFailover(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	recorder := newOpenAIResponseFlushRecorder()
	done := runOpenAIStreamTimeoutTestAsync(recorder, reader, config.GatewayConfig{StreamKeepaliveInterval: 1}, true)
	_, err := writer.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_wait\"}}\n\n"))
	require.NoError(t, err)
	waitOpenAIResponseFlushCount(t, recorder, 1)
	body, _ := recorder.snapshot()
	require.Equal(t, ":\n\n", body)
	_, err = writer.Write([]byte("data: {\"type\":\"response.failed\",\"error\":{\"code\":\"server_error\",\"message\":\"upstream unavailable\"}}\n\n"))
	// Early failover can close the reader after consuming the terminal data line.
	require.True(t, err == nil || errors.Is(err, io.ErrClosedPipe))
	result := awaitOpenAIStreamTimeoutTest(t, done)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, result.err, &failoverErr)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	body, _ = recorder.snapshot()
	require.Equal(t, ":\n\n", body)
}

func TestOpenAIPassthroughPreambleStagingIsBounded(t *testing.T) {
	line := ": " + strings.Repeat("x", 64*1024) + "\n\n"
	payload := strings.Repeat(line, openAIFirstOutputStageMaxBytes/len(line)+1)
	recorder := newOpenAIResponseFlushRecorder()
	result := runOpenAIStreamTimeoutTest(recorder, io.NopCloser(strings.NewReader(payload)), config.GatewayConfig{}, true)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, result.err, &failoverErr)
	require.Contains(t, string(failoverErr.ResponseBody), "staging limit exceeded")
	body, flushes := recorder.snapshot()
	require.Empty(t, body)
	require.Empty(t, flushes)
}

func TestOpenAIPassthroughKeepaliveDoesNotSplitOpenEvent(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	recorder := newOpenAIResponseFlushRecorder()
	done := runOpenAIStreamTimeoutTestAsync(recorder, reader, config.GatewayConfig{StreamKeepaliveInterval: 1}, true)
	firstLine := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ready\"}\n"
	_, err := writer.Write([]byte(firstLine))
	require.NoError(t, err)
	select {
	case <-recorder.flushEvents:
		t.Fatal("keepalive flushed an incomplete SSE event")
	case <-time.After(1200 * time.Millisecond):
	}
	tail := "\ndata: [DONE]\n\n"
	_, err = writer.Write([]byte(tail))
	require.NoError(t, err)
	result := awaitOpenAIStreamTimeoutTest(t, done)
	require.NoError(t, result.err)
	body, _ := recorder.snapshot()
	require.Equal(t, firstLine+tail, body)
}

func TestOpenAIPassthroughIdleTimeoutPreservesCollectedUsage(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	recorder := newOpenAIResponseFlushRecorder()
	done := runOpenAIStreamTimeoutTestAsync(recorder, reader, config.GatewayConfig{StreamDataIntervalTimeout: 1}, true)
	payload := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n"
	_, err := writer.Write([]byte(payload))
	require.NoError(t, err)
	result := awaitOpenAIStreamTimeoutTest(t, done)
	require.ErrorContains(t, result.err, "stream data interval timeout")
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(result.err, &failoverErr))
	require.Equal(t, 5, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	body, _ := recorder.snapshot()
	require.NotContains(t, body, "stream_timeout", "timeout errors must not split an incomplete SSE event")
}

func TestOpenAIStreamIncompleteTerminalEventTimesOut(t *testing.T) {
	for _, tt := range []struct {
		name        string
		terminal    string
		passthrough bool
		guarded     bool
	}{
		{name: "native completed", terminal: "data: {\"type\":\"response.completed\",\"response\":{}}\n"},
		{name: "native guarded completed", terminal: "data: {\"type\":\"response.completed\",\"response\":{}}\n", guarded: true},
		{name: "passthrough completed", terminal: "data: {\"type\":\"response.completed\",\"response\":{}}\n", passthrough: true},
		{name: "native done", terminal: "data: [DONE]\n"},
		{name: "passthrough done", terminal: "data: [DONE]\n", passthrough: true},
		{name: "native terminal header", terminal: "event: response.completed\n\n"},
		{name: "passthrough terminal header", terminal: "event: response.completed\n\n", passthrough: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = reader.Close() })
			t.Cleanup(func() { _ = writer.Close() })
			recorder := newOpenAIResponseFlushRecorder()
			cfg := config.GatewayConfig{StreamDataIntervalTimeout: 1}
			if tt.guarded {
				cfg.OpenAIFirstOutputTimeoutSeconds = 5
			}
			done := runOpenAIStreamTimeoutTestAsync(recorder, reader, cfg, tt.passthrough)
			_, err := writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ready\"}\n\n" + tt.terminal))
			require.NoError(t, err)
			result := awaitOpenAIStreamTimeoutTest(t, done)
			require.ErrorContains(t, result.err, "stream data interval timeout")
		})
	}
}

func TestOpenAIStreamDoneEventReturnsWithoutEOF(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		reader, writer := io.Pipe()
		recorder := newOpenAIResponseFlushRecorder()
		done := runOpenAIStreamTimeoutTestAsync(recorder, reader, config.GatewayConfig{StreamDataIntervalTimeout: 30}, passthrough)
		_, err := writer.Write([]byte("data: [DONE]\n\n"))
		require.NoError(t, err)
		result := awaitOpenAIStreamTimeoutTest(t, done)
		require.NoError(t, result.err)
		body, _ := recorder.snapshot()
		require.Equal(t, "data: [DONE]\n\n", body)
		_ = reader.Close()
		_ = writer.Close()
	}
}

func TestOpenAIStreamKeepaliveDoesNotConvertFailedEventToJSON(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = reader.Close() })
			t.Cleanup(func() { _ = writer.Close() })
			recorder := newOpenAIResponseFlushRecorder()
			cfg := config.GatewayConfig{StreamKeepaliveInterval: 1, OpenAIFirstOutputTimeoutSeconds: 5}
			done := runOpenAIStreamTimeoutTestAsync(recorder, reader, cfg, passthrough, func(c *gin.Context) {
				rule := newNonFailoverPassthroughRule(http.StatusBadRequest, "context_length_exceeded", http.StatusBadRequest, "")
				rule.Platforms = []string{PlatformOpenAI}
				rule.PassthroughBody = true
				ruleSvc := &ErrorPassthroughService{}
				ruleSvc.setLocalCache([]*model.ErrorPassthroughRule{rule})
				BindErrorPassthroughService(c, ruleSvc)
			})
			waitOpenAIResponseFlushCount(t, recorder, 1)
			_, err := writer.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"invalid_request_error\",\"code\":\"context_length_exceeded\",\"message\":\"input exceeds context window\"}}}\n\n"))
			require.NoError(t, err)
			result := awaitOpenAIStreamTimeoutTest(t, done)
			require.Error(t, result.err)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(result.err, &failoverErr))
			body, _ := recorder.snapshot()
			require.True(t, strings.HasPrefix(body, ":\n\n"))
			require.Contains(t, body, "data: {")
			require.Contains(t, body, "response.failed")
			require.NotContains(t, body, "\n{\"error\"")
		})
	}
}

func TestOpenAIStreamKeepaliveBytesDoNotCountAsModelOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for i := 0; i < 2; i++ {
		n, err := c.Writer.Write([]byte(":\n\n"))
		require.NoError(t, err)
		recordOpenAIStreamKeepaliveBytes(c, n)
		require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
		require.False(t, openAIStreamClientOutputStarted(c, false))
	}
	_, err := c.Writer.Write([]byte("data: real\n\n"))
	require.NoError(t, err)
	require.Equal(t, len("data: real\n\n"), OpenAICompactKeepaliveAdjustedWrittenSize(c))
	require.True(t, openAIStreamClientOutputStarted(c, false))
}

func TestOpenAIStreamKeepalivePreservesAPIKeyRetryWriter(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			name := "native"
			if passthrough {
				name = "passthrough"
			}
			if fail {
				name += " failover"
			} else {
				name += " output"
			}
			t.Run(name, func(t *testing.T) {
				reader, writer := io.Pipe()
				t.Cleanup(func() { _ = reader.Close() })
				t.Cleanup(func() { _ = writer.Close() })
				recorder := newOpenAIResponseFlushRecorder()
				var retryWriter *openAIStreamRetryTestWriter
				cfg := config.GatewayConfig{StreamKeepaliveInterval: 1, OpenAIFirstOutputTimeoutSeconds: 5}
				done := runOpenAIStreamTimeoutTestAsync(recorder, reader, cfg, passthrough, func(c *gin.Context) {
					retryWriter = &openAIStreamRetryTestWriter{ResponseWriter: c.Writer}
					c.Writer = retryWriter
				})
				waitOpenAIResponseFlushCount(t, recorder, 1)
				payload := "data: {\"type\":\"response.failed\",\"error\":{\"code\":\"server_error\",\"message\":\"unavailable\"}}\n\n"
				if !fail {
					payload = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ready\"}\n\ndata: [DONE]\n\n"
				}
				_, err := writer.Write([]byte(payload))
				require.True(t, err == nil || errors.Is(err, io.ErrClosedPipe))
				result := awaitOpenAIStreamTimeoutTest(t, done)
				require.Positive(t, retryWriter.keepaliveWrites)
				if fail {
					var failoverErr *UpstreamFailoverError
					require.ErrorAs(t, result.err, &failoverErr)
					require.True(t, failoverErr.SafeToFailoverAfterWrite)
					require.Zero(t, retryWriter.normalWrites)
					require.Zero(t, retryWriter.semanticCommits)
				} else {
					require.NoError(t, result.err)
					require.Positive(t, retryWriter.semanticCommits)
					require.Positive(t, retryWriter.normalWrites)
					body, _ := recorder.snapshot()
					require.Equal(t, ":\n\n"+payload, body)
				}
			})
		}
	}
}

func TestOpenAIStreamInheritedAPIKeyKeepaliveDoesNotCountAsOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Writer.WriteHeaderNow()
	c.Writer = &openAIStreamRetryTestWriter{ResponseWriter: c.Writer, keepaliveStarted: true}
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
	require.False(t, openAIStreamClientOutputStarted(c, false))
	svc := &OpenAIGatewayService{}
	failoverErr := svc.newOpenAIStreamFailoverError(c, &Account{ID: 1, Platform: PlatformOpenAI}, true, "", nil, "upstream unavailable")
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
}
