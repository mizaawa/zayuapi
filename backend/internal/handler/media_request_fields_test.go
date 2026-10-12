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

func TestMediaMultipartReferenceListsAndScalarBoundaries(t *testing.T) {
	for _, name := range []string{"referenceImages", "reference_images[]", "image_refs", "referenceVideos", "reference_videos[]", "video_refs", "referenceAudios", "reference_audios[]", "audio_refs"} {
		t.Run(name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "seedance-2-5-720p"))
			require.NoError(t, writer.WriteField(name, "https://cdn.example.com/first"))
			require.NoError(t, writer.WriteField(name, "https://cdn.example.com/second"))
			require.NoError(t, writer.Close())
			require.True(t, mediaRequestFieldsUnambiguous(writer.FormDataContentType(), body.Bytes()))
		})
	}
	for _, name := range []string{"model", "Model", "n", "duration", "seconds", "resolution", "quality", "model[]", " duration "} {
		for _, file := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/file=%t", name, file), func(t *testing.T) {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField(strings.ToLower(strings.TrimSpace(strings.TrimSuffix(name, "[]"))), "1"))
				if file {
					part, err := writer.CreateFormFile(name, "field.txt")
					require.NoError(t, err)
					_, err = io.WriteString(part, "10")
					require.NoError(t, err)
				} else {
					require.NoError(t, writer.WriteField(name, "10"))
				}
				require.NoError(t, writer.Close())
				require.False(t, mediaRequestFieldsUnambiguous(writer.FormDataContentType(), body.Bytes()))
			})
		}
	}
}
