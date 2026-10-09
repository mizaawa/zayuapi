package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexModelsMappingProjectsAvailableAliases(t *testing.T) {
	body := []byte(`{"models":[{"slug":"gpt-5.6","display_name":"GPT","metadata":{"keep":true}},{"slug":"other"}],"extra":"keep"}`)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	account.Credentials["model_mapping"] = map[string]any{"public-alias": "gpt-5.6", "missing": "unavailable"}
	projected, err := projectCodexModelsManifestForAccount(body, account)
	require.NoError(t, err)
	require.Equal(t, int64(1), gjson.GetBytes(projected, "models.#").Int())
	require.Equal(t, "public-alias", gjson.GetBytes(projected, "models.0.slug").String())
	require.Equal(t, "public-alias", gjson.GetBytes(projected, "models.0.display_name").String())
	require.True(t, gjson.GetBytes(projected, "models.0.metadata.keep").Bool())
	require.Equal(t, "keep", gjson.GetBytes(projected, "extra").String())
	require.Equal(t, "gpt-5.6", gjson.GetBytes(body, "models.0.slug").String())
}

func TestCodexModelsMappingPreservesIdentityBody(t *testing.T) {
	body := []byte(`{ "models": [ { "slug": "gpt-5.6", "extra": 1 }, { "slug": "gpt-5.6-codex" } ] }`)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	account.Credentials["model_mapping"] = map[string]any{"gpt-5.6": "gpt-5.6", "gpt-5.6-codex": "gpt-5.6-codex"}
	projected, err := projectCodexModelsManifestForAccount(body, account)
	require.NoError(t, err)
	require.Equal(t, body, projected)
}

func TestFetchCodexModelsMappingKeepsRawCacheAndRevalidatesLocalETag(t *testing.T) {
	requests := 0
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		requests++
		require.Empty(t, req.Header.Get("If-None-Match"), "client projection ETag must not reach upstream")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"ETag": []string{`"upstream"`}}, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"gpt-5.6"},{"slug":"other"}]}`))}, nil
	}}
	svc := newCodexModelsAPIKeyTestService(upstream)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-5.6"}
	first, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	require.NoError(t, err)
	require.NotEqual(t, `"upstream"`, first.ETag)
	second, err := svc.FetchCodexModelsManifest(context.Background(), account, "0.144.0", first.ETag)
	require.NoError(t, err)
	require.True(t, second.NotModified)
	changedAccount := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	changedAccount.Credentials["model_mapping"] = map[string]any{"renamed": "gpt-5.6"}
	third, err := svc.FetchCodexModelsManifest(context.Background(), changedAccount, "0.144.0", first.ETag)
	require.NoError(t, err)
	require.False(t, third.NotModified)
	require.Equal(t, "renamed", gjson.GetBytes(third.Body, "models.0.slug").String())
	unmapped := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	raw, err := svc.FetchCodexModelsManifest(context.Background(), unmapped, "0.144.0", "")
	require.NoError(t, err)
	require.Equal(t, int64(2), gjson.GetBytes(raw.Body, "models.#").Int())
	require.Equal(t, "gpt-5.6", gjson.GetBytes(raw.Body, "models.0.slug").String())
	require.Equal(t, 1, requests)
}
