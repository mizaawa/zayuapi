package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiStreamTimeoutTestResult struct {
	usage *ClaudeUsage
	err   error
}

func startGeminiStalledStream(t *testing.T, payload string) (*http.Response, <-chan struct{}) {
	t.Helper()
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
		require.NoError(t, http.NewResponseController(w).Flush())
		<-r.Context().Done()
		close(closed)
	}))
	t.Cleanup(server.Close)
	resp, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, closed
}

func runGeminiStreamTimeoutHandler(path string, recorder *httptest.ResponseRecorder, resp *http.Response, cfg config.GatewayConfig) <-chan geminiStreamTimeoutTestResult {
	done := make(chan geminiStreamTimeoutTestResult, 1)
	go func() {
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/gemini-stream", nil)
		svc := &GeminiMessagesCompatService{cfg: &config.Config{Gateway: cfg}}
		var result *geminiStreamResult
		var err error
		if path == "native" {
			nativeResult, nativeErr := svc.handleNativeStreamingResponse(c, resp, time.Now(), false)
			if nativeResult != nil {
				done <- geminiStreamTimeoutTestResult{usage: nativeResult.usage, err: nativeErr}
			} else {
				done <- geminiStreamTimeoutTestResult{err: nativeErr}
			}
			return
		}
		if path == "messages" {
			result, err = svc.handleStreamingResponse(c, resp, time.Now(), "gemini-test")
		} else {
			result, err = svc.handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-test", false, true)
		}
		if result == nil {
			done <- geminiStreamTimeoutTestResult{err: err}
			return
		}
		done <- geminiStreamTimeoutTestResult{usage: result.usage, err: err}
	}()
	return done
}

func awaitGeminiStreamTimeoutHandler(t *testing.T, done <-chan geminiStreamTimeoutTestResult) geminiStreamTimeoutTestResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(4 * time.Second):
		t.Fatal("Gemini stream handler did not return before its deadline")
		return geminiStreamTimeoutTestResult{}
	}
}

func requireGeminiUpstreamClosed(t *testing.T, closed <-chan struct{}) {
	t.Helper()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Gemini stream handler did not close its stalled upstream body")
	}
}

func TestGeminiStreamIdleTimeoutRetainsUsageAfterFinishReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const payload = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}]}\n\n" +
		"data: {\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":7}}\n\n"
	for _, path := range []string{"native", "messages", "chat"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			resp, closed := startGeminiStalledStream(t, payload)
			recorder := httptest.NewRecorder()
			done := runGeminiStreamTimeoutHandler(path, recorder, resp, config.GatewayConfig{StreamDataIntervalTimeout: 1})
			result := awaitGeminiStreamTimeoutHandler(t, done)
			require.ErrorIs(t, result.err, errGeminiStreamIdleTimeout)
			require.NotNil(t, result.usage)
			require.Equal(t, 12, result.usage.InputTokens)
			require.Equal(t, 7, result.usage.OutputTokens)
			require.Contains(t, recorder.Body.String(), "hello")
			requireGeminiUpstreamClosed(t, closed)
		})
	}
}

func TestGeminiStreamKeepaliveRespectsEventBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"native", "messages", "chat"} {
		for _, payload := range []string{"", "event: upstream\n"} {
			name := path + "/idle"
			if payload != "" {
				name = path + "/open-event"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				resp, closed := startGeminiStalledStream(t, payload)
				recorder := httptest.NewRecorder()
				done := runGeminiStreamTimeoutHandler(path, recorder, resp, config.GatewayConfig{
					StreamDataIntervalTimeout: 2,
					StreamKeepaliveInterval:   1,
				})
				result := awaitGeminiStreamTimeoutHandler(t, done)
				require.ErrorIs(t, result.err, errGeminiStreamIdleTimeout)
				if payload == "" {
					require.Contains(t, recorder.Body.String(), ":\n\n")
				} else {
					require.NotContains(t, recorder.Body.String(), ":\n\n")
				}
				requireGeminiUpstreamClosed(t, closed)
			})
		}
	}
}

func TestGeminiStreamDoneReturnsWithoutEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const payload = "data: {\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":7}}\n\n" +
		"data: [DONE]\n\n"
	for _, path := range []string{"native", "messages", "chat"} {
		for _, timeout := range []int{0, 1} {
			name := path + "/synchronous"
			if timeout > 0 {
				name = path + "/asynchronous"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				resp, closed := startGeminiStalledStream(t, payload)
				recorder := httptest.NewRecorder()
				done := runGeminiStreamTimeoutHandler(path, recorder, resp, config.GatewayConfig{StreamDataIntervalTimeout: timeout})
				result := awaitGeminiStreamTimeoutHandler(t, done)
				require.NoError(t, result.err)
				require.NotNil(t, result.usage)
				require.Equal(t, 12, result.usage.InputTokens)
				require.Equal(t, 7, result.usage.OutputTokens)
				switch path {
				case "native":
					require.Equal(t, payload, recorder.Body.String())
				case "messages":
					require.Contains(t, recorder.Body.String(), "event: message_stop\n")
				default:
					require.True(t, strings.HasSuffix(recorder.Body.String(), "data: [DONE]\n\n"))
				}
				requireGeminiUpstreamClosed(t, closed)
			})
		}
	}
}

func TestGeminiStreamReaderActivePartialLineRefreshesTimeout(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	stream := newGeminiStreamReader(reader, &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}}, nil)
	defer stream.Close()
	writeDone := make(chan error, 1)
	go func() {
		for index, part := range []string{"data: {\"text\":\"", "active", " line\"}\n\n", "data: [DONE]\n\n"} {
			if index == 1 || index == 2 {
				time.Sleep(650 * time.Millisecond)
			}
			if _, err := io.WriteString(writer, part); err != nil {
				writeDone <- err
				return
			}
		}
		writeDone <- nil
	}()
	var output strings.Builder
	for {
		line, err := stream.ReadString()
		_, _ = output.WriteString(line)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	require.Equal(t, "data: {\"text\":\"active line\"}\n\ndata: [DONE]\n\n", output.String())
	require.NoError(t, <-writeDone)
}
