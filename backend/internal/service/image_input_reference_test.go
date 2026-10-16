//go:build unit

package service

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIImageEditReferenceAliasesReachResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, refs := range []string{
		`"images":[{"image_url":"https://example.com/source.png"}]`,
		`"images":["https://example.com/source.png"]`,
		`"image":"https://example.com/source.png"`,
		`"image":{"url":"https://example.com/source.png"}`,
		`"image":{"image_url":{"url":"https://example.com/source.png"}}`,
		`"image":[{"image_url":"https://example.com/source.png"}]`,
		`"image_url":"https://example.com/source.png"`,
		`"image_urls":["https://example.com/source.png"]`,
	} {
		t.Run(refs, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","prompt":"Change background","input_fidelity":"high","mask":"https://example.com/mask.png",` + refs + `}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.Equal(t, []string{"https://example.com/source.png"}, parsed.InputImageURLs)
			require.Equal(t, "https://example.com/mask.png", parsed.MaskImageURL)
			upstream, err := buildOpenAIImagesResponsesRequest(parsed, "gpt-image-2")
			require.NoError(t, err)
			require.Equal(t, "https://example.com/source.png", gjson.GetBytes(upstream, "input.0.content.1.image_url").String())
			require.Equal(t, "https://example.com/mask.png", gjson.GetBytes(upstream, "tools.0.input_image_mask.image_url").String())
			require.False(t, gjson.GetBytes(upstream, "tools.0.input_fidelity").Exists())
			require.Len(t, gjson.GetBytes(parsed.ModerationBody(), "images").Array(), 2)
		})
	}
}

func TestOpenAIImageEditRejectsMalformedReferences(t *testing.T) {
	for _, refs := range []string{
		`"images":[{"image_url":"https://example.com/valid.png"},{}]`,
		`"images":[{"image_url":42}]`,
		`"images":[null]`,
		`"images":[{"file_id":"file-private"}]`,
		`"images":[{"image_url":"https://example.com/source.png","file_id":"file-private"}]`,
		`"images":[{"url":"https://example.com/a.png","image_url":"https://example.com/b.png"}]`,
		`"image_urls":"https://example.com/source.png"`,
		`"image":{"image_url":{"url":42}}`,
		`"image":"https://example.com/source.png","mask":{"file_id":"file-private"}`,
		`"image":"https://example.com/source.png","mask":{"url":"https://example.com/a.png","image_url":"https://example.com/b.png"}`,
		`"image":"https://example.com/source.png","mask":["https://example.com/mask.png"]`,
	} {
		t.Run(refs, func(t *testing.T) {
			parsed := &OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint}
			err := parseOpenAIImagesJSONRequest([]byte(`{"model":"gpt-image-2","prompt":"Change background",`+refs+`}`), parsed)
			require.Error(t, err)
		})
	}
}

func TestOpenAIImageEditMultipartUploadLimit(t *testing.T) {
	for _, size := range []int{20<<20 + 1, 50<<20 + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("image", "source.png")
			require.NoError(t, err)
			chunk := make([]byte, 1<<20)
			for remaining := size; remaining > 0; {
				n := min(remaining, len(chunk))
				_, err = part.Write(chunk[:n])
				require.NoError(t, err)
				remaining -= n
			}
			require.NoError(t, writer.Close())
			parsed := &OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint}
			err = parseOpenAIImagesMultipartRequest(body.Bytes(), writer.FormDataContentType(), parsed)
			if size > 50<<20 {
				require.ErrorContains(t, err, "50 MiB")
				return
			}
			require.NoError(t, err)
			require.Len(t, parsed.Uploads, 1)
			require.Len(t, parsed.Uploads[0].Data, size)
		})
	}
}

func TestGrokImageEditReferenceAliasesNormalizeAndRemainAuditable(t *testing.T) {
	for _, refs := range []string{
		`"images":[{"image_url":"https://example.com/source.png"}]`,
		`"images":["https://example.com/source.png"]`,
		`"image":"https://example.com/source.png"`,
		`"image":{"image_url":{"url":"https://example.com/source.png"}}`,
		`"image_url":"https://example.com/source.png"`,
		`"image_urls":["https://example.com/source.png"]`,
	} {
		t.Run(refs, func(t *testing.T) {
			body := []byte(`{"model":"grok-imagine-image","prompt":"Change background","mask":"https://example.com/mask.png",` + refs + `}`)
			parsed := ParseGrokMediaRequest("application/json", body)
			require.Equal(t, []string{"https://example.com/source.png"}, parsed.InputImageURLs)
			require.Len(t, gjson.GetBytes(parsed.ModerationBody(), "images").Array(), 2)
			upstream, _, err := normalizeGrokMediaForwardBody(GrokMediaEndpointImagesEdits, body, "application/json")
			require.NoError(t, err)
			ref := gjson.GetBytes(upstream, "image.url").String()
			if ref == "" {
				ref = gjson.GetBytes(upstream, "images.0.url").String()
			}
			require.Equal(t, "https://example.com/source.png", ref)
			require.Equal(t, "https://example.com/mask.png", gjson.GetBytes(upstream, "mask.url").String())
			require.False(t, gjson.GetBytes(upstream, "image_urls").Exists())
			require.False(t, gjson.GetBytes(upstream, "image_url").Exists())
		})
	}
}

func TestGrokImageEditRejectsUnsupportedFileReferences(t *testing.T) {
	for _, body := range []string{
		`{"images":[{"file_id":"file-private"}]}`,
		`{"image":{"image_url":"https://example.com/source.png"},"mask":{"file_id":"file-private"}}`,
	} {
		_, _, err := normalizeGrokMediaForwardBody(GrokMediaEndpointImagesEdits, []byte(body), "application/json")
		require.ErrorContains(t, err, "file_id is not supported")
	}
}

func TestGrokImageEditMultipartPreservesOutputOptions(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"model": "grok-imagine-image", "prompt": "Change background", "quality": "high",
		"response_format": "b64_json", "output_format": "webp", "background": "opaque",
		"output_compression": "80", "partial_images": "2", "stream": "true", "n": "1",
	} {
		require.NoError(t, writer.WriteField(key, value))
	}
	image, err := writer.CreateFormFile("image[]", "source.png")
	require.NoError(t, err)
	_, err = image.Write([]byte("test-image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	upstream, contentType, err := prepareGrokMediaForwardBody(GrokMediaEndpointImagesEdits, body.Bytes(), writer.FormDataContentType())
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "high", gjson.GetBytes(upstream, "quality").String())
	require.Equal(t, "b64_json", gjson.GetBytes(upstream, "response_format").String())
	require.Equal(t, "webp", gjson.GetBytes(upstream, "output_format").String())
	require.Equal(t, int64(80), gjson.GetBytes(upstream, "output_compression").Int())
	require.Equal(t, gjson.Number, gjson.GetBytes(upstream, "output_compression").Type)
	require.Equal(t, int64(2), gjson.GetBytes(upstream, "partial_images").Int())
	require.Equal(t, gjson.True, gjson.GetBytes(upstream, "stream").Type)
	require.NotEmpty(t, gjson.GetBytes(upstream, "image.url").String())
}
