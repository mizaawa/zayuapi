package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiBillingChunkReader struct {
	upstream    *geminiCompatHTTPUpstreamStub
	chunks      []string
	terminalErr error
}

func (r *geminiBillingChunkReader) Read(p []byte) (int, error) {
	if err := r.upstream.lastReq.Context().Err(); err != nil {
		return 0, err
	}
	if len(r.chunks) == 0 {
		return 0, r.terminalErr
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if r.chunks[0] == "" {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

func (*geminiBillingChunkReader) Close() error { return nil }

type geminiBillingDisconnectWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *geminiBillingDisconnectWriter) Write(p []byte) (int, error) {
	w.cancel()
	return 0, context.Canceled
}

func (w *geminiBillingDisconnectWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func TestGeminiForwardNativeRetainsUsageAfterDisconnectOrReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		disconnect  bool
		terminalErr error
	}{
		{name: "client disconnect drains final usage", disconnect: true, terminalErr: io.EOF},
		{name: "upstream read error retains usage", terminalErr: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			upstream := &geminiCompatHTTPUpstreamStub{}
			reader := &geminiBillingChunkReader{upstream: upstream, terminalErr: tc.terminalErr, chunks: []string{
				"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n",
				"data: {\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":7}}\n\n",
			}}
			upstream.response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent", nil).WithContext(ctx)
			if tc.disconnect {
				c.Writer = &geminiBillingDisconnectWriter{ResponseWriter: c.Writer, cancel: cancel}
			}
			result, err := svc.ForwardNative(ctx, c, account, "gemini-2.5-flash", "streamGenerateContent", true, []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
			if tc.terminalErr == io.EOF {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.terminalErr)
			}
			require.NotNil(t, result)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 7, result.Usage.OutputTokens)
			if tc.disconnect {
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.NoError(t, upstream.lastReq.Context().Err())
			}
		})
	}
}
