//go:build live

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageProtocolLiveUpstream struct {
	client      *http.Client
	contentType string
}

func (u *imageProtocolLiveUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.contentType = r.Header.Get("Content-Type")
	return u.client.Do(r)
}

func (u *imageProtocolLiveUpstream) DoWithTLS(r *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, proxy, id, concurrency)
}

// This opt-in test makes paid provider calls; credentials are read only from the environment.
func TestImageProtocolLive(t *testing.T) {
	base, key := os.Getenv("IMAGE_TEST_BASE_URL"), os.Getenv("IMAGE_TEST_API_KEY")
	if base == "" || key == "" {
		t.Skip("IMAGE_TEST_BASE_URL and IMAGE_TEST_API_KEY are required")
	}
	model := os.Getenv("IMAGE_TEST_MODEL")
	if model == "" {
		model = "gpt-image-2"
	}
	reference, err := os.ReadFile(os.Getenv("IMAGE_TEST_REFERENCE"))
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformOpenAI, PlatformCustom} {
		t.Run(platform, func(t *testing.T) {
			upstream := &imageProtocolLiveUpstream{client: &http.Client{Timeout: 4 * time.Minute}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"base_url": base, "api_key": key, "model_mapping": map[string]any{"gpt-image-2": model},
			}}
			payload := map[string]any{
				"model": "gpt-image-2", "prompt": "Change the mug to green. Keep the composition.",
				"size": "1024x1024", "quality": "low", "n": 1,
				"images": []map[string]string{{"image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(reference)}},
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, 1, result.ImageCount)
			require.True(t, strings.HasPrefix(upstream.contentType, "multipart/form-data"))
			encoded := gjson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").String()
			require.NotEmpty(t, encoded)
			image, err := base64.StdEncoding.DecodeString(encoded)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(http.DetectContentType(image), "image/"))
			output := filepath.Join(filepath.Dir(os.Getenv("IMAGE_TEST_REFERENCE")), fmt.Sprintf("gateway-%s.png", platform))
			require.NoError(t, os.WriteFile(output, image, 0600))
			t.Logf("platform=%s model=%s images=%d bytes=%d input_tokens=%d", platform, result.UpstreamModel, result.ImageCount, len(image), result.Usage.InputTokens)
		})
	}
}
