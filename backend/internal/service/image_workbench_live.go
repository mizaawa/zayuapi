//go:build live

package service

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type imageWorkbenchLiveUpstream struct{ client *http.Client }

func (u *imageWorkbenchLiveUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(r)
}

func (u *imageWorkbenchLiveUpstream) DoWithTLS(r *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, proxy, id, concurrency)
}

// NewImageWorkbenchLiveGatewayFromEnvironment constructs the real Images
// forwarding service without database-backed scheduling or billing. It is
// available only in explicitly requested live builds.
func NewImageWorkbenchLiveGatewayFromEnvironment() (*OpenAIGatewayService, *Account, error) {
	base := strings.TrimSpace(os.Getenv("IMAGE_TEST_BASE_URL"))
	key := strings.TrimSpace(os.Getenv("IMAGE_TEST_API_KEY"))
	if base == "" || key == "" {
		return nil, nil, errors.New("IMAGE_TEST_BASE_URL and IMAGE_TEST_API_KEY are required")
	}
	model := strings.TrimSpace(os.Getenv("IMAGE_TEST_MODEL"))
	if model == "" {
		model = "gpt-image-2"
	}
	platform := strings.TrimSpace(os.Getenv("IMAGE_TEST_PLATFORM"))
	if platform == "" {
		platform = PlatformOpenAI
	}
	if platform != PlatformOpenAI && platform != PlatformCustom {
		return nil, nil, errors.New("IMAGE_TEST_PLATFORM must be openai or custom")
	}
	mapping := map[string]any{}
	for _, alias := range []string{"gpt-image-2", "gptimage2", "gpt-image2", "gptimage-2", model} {
		mapping[alias] = model
	}
	gateway := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: &imageWorkbenchLiveUpstream{client: &http.Client{Timeout: 5 * time.Minute}},
	}
	account := &Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"base_url": base, "api_key": key, "model_mapping": mapping},
	}
	return gateway, account, nil
}
