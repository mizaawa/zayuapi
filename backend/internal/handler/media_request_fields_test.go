//go:build unit

package handler

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMediaRequestFieldsRejectParserAmbiguity(t *testing.T) {
	for _, body := range []string{
		`{"model":"cheap","model":"expensive"}`,
		`{"model":"cheap","\u006dodel":"expensive"}`,
		`{"model":"cheap","Model":"expensive"}`,
		`{"n":1,"n":10}`,
		`{"duration":1,"duration":10}`,
		`{"seconds":"1","seconds":"10"}`,
		`{"metadata":{"content":[{"type":"text","type":"video_url"}]}}`,
		`{"model":"test"} {"n":10}`,
		`[]`,
	} {
		require.False(t, mediaRequestFieldsUnambiguous("application/json", []byte(body)), body)
	}
	require.True(t, mediaRequestFieldsUnambiguous("application/json", []byte(`{"model":"test","n":1,"metadata":{"content":[{"type":"text"},{"type":"video_url"}]}}`)))
	for _, duplicate := range []bool{false, true} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-2"))
		if duplicate {
			require.NoError(t, writer.WriteField("Model", "different-model"))
		}
		for i := range 2 {
			file, err := writer.CreateFormFile("image[]", fmt.Sprintf("image-%d.png", i))
			require.NoError(t, err)
			_, err = file.Write([]byte("image fixture"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		require.Equal(t, !duplicate, mediaRequestFieldsUnambiguous(writer.FormDataContentType(), body.Bytes()))
	}
}

func TestMediaRequestsRejectAmbiguousFieldsBeforeCharging(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformCustom} {
		t.Run(platform, func(t *testing.T) {
			upstream := &workbenchBillingUpstream{statuses: []int{http.StatusOK}}
			h, ledger, key := newWorkbenchBillingHandler(t, platform, upstream)
			c, rec := workbenchBillingContext(key, platform, "")
			body, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			c.Request.Body = io.NopCloser(strings.NewReader(strings.TrimSuffix(string(body), "}") + `,"n":10}`))
			if platform == service.PlatformGrok {
				h.GrokImages(c)
			} else {
				h.Images(c)
			}
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Zero(t, upstream.calls)
			require.Empty(t, ledger.reservations)
			require.Empty(t, ledger.commands)
			require.Zero(t, ledger.refunded)
		})
	}
	upstream := &workbenchBillingUpstream{statuses: []int{http.StatusOK}}
	h, ledger, key := newWorkbenchBillingHandler(t, service.PlatformCustom, upstream)
	c, rec := workbenchBillingContext(key, service.PlatformCustom, "")
	c.Request.Body = io.NopCloser(strings.NewReader(`{"model":"cheap","model":"expensive","duration":1,"duration":10,"prompt":"test"}`))
	h.CustomVideoGeneration(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, upstream.calls)
	require.Empty(t, ledger.reservations)
}
