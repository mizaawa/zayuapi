package middleware

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFailoverWriterOpenAIStreamOutputCommitsAfterKeepalive(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	keepaliveStarted := &atomic.Bool{}
	writer := newAPIKeyFailoverWriter(c.Writer, keepaliveStarted)
	_, err := writer.WriteOpenAICompactKeepalive([]byte(":\n\n"))
	require.NoError(t, err)
	require.False(t, writer.committed(), "comments must preserve retry capability")
	require.Equal(t, ":\n\n", recorder.Body.String())

	writer.CommitOpenAIStreamOutput()
	require.True(t, writer.committed())
	_, err = writer.Write([]byte("data: model output\n\n"))
	require.NoError(t, err)
	writer.Flush()
	require.Equal(t, ":\n\ndata: model output\n\n", recorder.Body.String())
}
