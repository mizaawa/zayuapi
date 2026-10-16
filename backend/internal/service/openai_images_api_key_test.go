package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIImagesAPIKeyEditBody(t *testing.T) {
	pngData := encodeOpenAIImageTestPNG(t, 2, 2)
	jpegData := encodeOpenAIImageTestJPEG(t, 2, 2)
	body := []byte(fmt.Sprintf(`{
		"model":"opaque/upstream-image-model","prompt":"  edit\nwith details  ",
		"n":2,"size":"1536x1024","quality":"high","background":"transparent",
		"output_format":"webp","output_compression":0,"moderation":"auto",
		"input_fidelity":"high","response_format":"b64_json","stream":true,
		"partial_images":0,"user":"customer-42","seed":9007199254740993,
		"provider_scale":1.00,"provider_enabled":false,"provider_label":"",
		"images":[{"image_url":"data:image/png;base64,%s"},{"image_url":"data:image/jpeg;base64,%s"}],
		"mask":{"image_url":"data:image/png;base64,%s"}
	}`, pngData, jpegData, pngData))

	converted, contentType, err := normalizeOpenAIImagesAPIKeyEditBody(body, "application/json; charset=utf-8")
	require.NoError(t, err)
	form := readInlineOpenAIImagesTestForm(t, converted, contentType)
	require.Equal(t, map[string][]string{
		"model": {"opaque/upstream-image-model"}, "prompt": {"  edit\nwith details  "},
		"n": {"2"}, "size": {"1536x1024"}, "quality": {"high"}, "background": {"transparent"},
		"output_format": {"webp"}, "output_compression": {"0"}, "moderation": {"auto"},
		"input_fidelity": {"high"}, "response_format": {"b64_json"}, "stream": {"true"},
		"partial_images": {"0"}, "user": {"customer-42"}, "seed": {"9007199254740993"},
		"provider_scale": {"1.00"}, "provider_enabled": {"false"}, "provider_label": {""},
	}, form.Value)
	require.Len(t, form.File, 2)
	require.Len(t, form.File["image[]"], 2)
	require.Len(t, form.File["mask"], 1)
	for _, tc := range []struct {
		field, name, contentType, encoded string
		index                             int
	}{
		{"image[]", "image-1.png", "image/png", pngData, 0},
		{"image[]", "image-2.jpg", "image/jpeg", jpegData, 1},
		{"mask", "mask.png", "image/png", pngData, 0},
	} {
		file := form.File[tc.field][tc.index]
		require.Equal(t, tc.name, file.Filename)
		require.Equal(t, tc.contentType, file.Header.Get("Content-Type"))
		reader, err := file.Open()
		require.NoError(t, err)
		actual, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		expected, err := base64.StdEncoding.DecodeString(tc.encoded)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

func TestNormalizeOpenAIImagesAPIKeyEditBodyReferenceShapes(t *testing.T) {
	dataURL := "data:image/png;base64," + encodeOpenAIImageTestPNG(t, 1, 1)
	for _, field := range []string{"images", "image", "image_urls", "image_url"} {
		for _, reference := range []any{dataURL, map[string]any{"url": dataURL}, map[string]any{"image_url": dataURL}, map[string]any{"image_url": map[string]any{"url": dataURL}}} {
			t.Run(fmt.Sprintf("%s/%T", field, reference), func(t *testing.T) {
				value := reference
				if field == "images" || field == "image_urls" {
					value = []any{reference}
				}
				body, err := json.Marshal(map[string]any{"model": "gpt-image-2", field: value})
				require.NoError(t, err)
				converted, contentType, err := normalizeOpenAIImagesAPIKeyEditBody(body, "application/json")
				require.NoError(t, err)
				form := readInlineOpenAIImagesTestForm(t, converted, contentType)
				require.Len(t, form.File["image[]"], 1)
			})
		}
	}
}

func TestNormalizeOpenAIImagesAPIKeyEditBodyPreservesProviderRequests(t *testing.T) {
	dataURL := "data:image/png;base64," + encodeOpenAIImageTestPNG(t, 1, 1)
	for name, suffix := range map[string]string{
		"remote":                 `"images":[{"image_url":"https://cdn.example/image.png"}]`,
		"file id":                `"images":[{"file_id":"file-provider-id"}]`,
		"mixed references":       fmt.Sprintf(`"images":[{"image_url":%q},{"image_url":"https://cdn.example/image.png"}]`, dataURL),
		"remote mask":            fmt.Sprintf(`"images":[%q],"mask":"https://cdn.example/mask.png"`, dataURL),
		"structured option":      fmt.Sprintf(`"images":[%q],"provider":{"steps":20}`, dataURL),
		"array option":           fmt.Sprintf(`"images":[%q],"provider":["mode"]`, dataURL),
		"reference extra field":  fmt.Sprintf(`"images":[{"image_url":%q,"detail":"high"}]`, dataURL),
		"nested extra field":     fmt.Sprintf(`"images":[{"image_url":{"url":%q,"detail":"high"}}]`, dataURL),
		"null option":            fmt.Sprintf(`"images":[%q],"seed":null`, dataURL),
		"duplicate top field":    fmt.Sprintf(`"images":[%q],"user":"a","user":"b"`, dataURL),
		"duplicate nested field": fmt.Sprintf(`"images":[{"image_url":%q,"image_url":%q}]`, dataURL, dataURL),
		"empty images":           `"images":[]`,
		"generation":             `"prompt":"generate"`,
	} {
		t.Run(name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2",` + suffix + `}`)
			converted, contentType, err := normalizeOpenAIImagesAPIKeyEditBody(body, "application/json; charset=utf-8")
			require.NoError(t, err)
			require.Equal(t, body, converted)
			require.Equal(t, "application/json; charset=utf-8", contentType)
		})
	}

	t.Run("native multipart", func(t *testing.T) {
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		require.NoError(t, writer.WriteField("model", "gpt-image-2"))
		part, err := writer.CreateFormFile("image[]", "source.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("existing-provider-bytes"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		converted, contentType, err := normalizeOpenAIImagesAPIKeyEditBody(buffer.Bytes(), writer.FormDataContentType())
		require.NoError(t, err)
		require.Equal(t, buffer.Bytes(), converted)
		require.Equal(t, writer.FormDataContentType(), contentType)
	})
}

func TestDecodeInlineOpenAIImageValidation(t *testing.T) {
	pngData := encodeOpenAIImageTestPNG(t, 1, 1)
	for name, dataURL := range map[string]string{
		"missing comma":    "data:image/png;base64",
		"missing encoding": "data:image/png," + pngData,
		"invalid base64":   "data:image/png;base64,%%%",
		"empty bytes":      "data:image/png;base64,",
		"text bytes":       "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image")),
		"mime mismatch":    "data:image/jpeg;base64," + pngData,
		"unsupported mime": "data:image/svg+xml;base64," + pngData,
		"invalid mime":     "data:image/png;invalid parameter;base64," + pngData,
		"duplicate marker": "data:image/png;base64;base64," + pngData,
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"images": []string{dataURL}})
			require.NoError(t, err)
			converted, contentType, err := normalizeOpenAIImagesAPIKeyEditBody(body, "application/json")
			require.Error(t, err)
			require.Nil(t, converted)
			require.Empty(t, contentType)
		})
	}
	t.Run("oversized payload", func(t *testing.T) {
		dataURL := "data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(openAIImagesMaxInputImageBytes+1))
		_, _, err := decodeInlineOpenAIImage(dataURL)
		require.ErrorContains(t, err, "exceeds")
	})
	t.Run("larger than legacy upload limit", func(t *testing.T) {
		data := make([]byte, openAIImageMaxUploadPartSize+1)
		pngBytes, err := base64.StdEncoding.DecodeString(pngData)
		require.NoError(t, err)
		copy(data, pngBytes)
		decoded, contentType, err := decodeInlineOpenAIImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(data))
		require.NoError(t, err)
		require.Equal(t, "image/png", contentType)
		require.Equal(t, data, decoded)
	})
	t.Run("webp", func(t *testing.T) {
		dataURL := "data:image/webp;base64," + encodeOpenAIImageTestWebPVP8X(2, 2)
		_, contentType, err := decodeInlineOpenAIImage(dataURL)
		require.NoError(t, err)
		require.Equal(t, "image/webp", contentType)
	})
}

func TestOpenAIImagesAPIKeyInlineEditsUseMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformOpenAI, PlatformCustom} {
		t.Run(platform, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-image-2","prompt":"edit","images":[{"image_url":"data:image/png;base64,%s"}],"n":1}`, encodeOpenAIImageTestPNG(t, 1, 1)))
			req := httptest.NewRequest(http.MethodPost, openAIImagesEditsEndpoint, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"url":"https://cdn.example/image.png"}]}`)),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := &Account{
				ID: 7, Platform: platform, Type: AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key": "test-api-key", "base_url": "https://image-upstream.example/v1",
					"model_mapping": map[string]any{"channel-image": "opaque-image-2"},
				},
			}
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "channel-image")
			require.NoError(t, err)
			require.Equal(t, "opaque-image-2", result.UpstreamModel)
			require.Equal(t, openAIImagesEditsEndpoint, upstream.lastReq.URL.Path)
			form := readInlineOpenAIImagesTestForm(t, upstream.lastBody, upstream.lastReq.Header.Get("Content-Type"))
			require.Equal(t, []string{"opaque-image-2"}, form.Value["model"])
			require.Equal(t, []string{"edit"}, form.Value["prompt"])
			require.Len(t, form.File["image[]"], 1)
		})
	}
}

func readInlineOpenAIImagesTestForm(t *testing.T, body []byte, contentType string) *multipart.Form {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, form.RemoveAll()) })
	return form
}
