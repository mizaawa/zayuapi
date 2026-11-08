package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

const newAPITestVersion = "rc.15-secfix-nobill-20260709-cachettl-opsledger"

func newAPITestResponse(status int, body string, version string) *http.Response {
	header := http.Header{"Content-Type": []string{"application/json"}}
	if version != "" {
		header.Set(newAPIVersionHeader, version)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func newAPITestAccount(id int64, consensus bool) (*Account, *upstreamBillingProbeAccountRepo) {
	initialRate := 1.0
	account := &Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://upstream.example/v1"},
		Extra: map[string]any{
			UpstreamBillingProbeEnabledExtraKey:    true,
			UpstreamBillingRateSyncEnabledExtraKey: consensus,
		},
		RateMultiplier: &initialRate,
	}
	return account, &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
}

func newAPITestSettings(consensusCount int) *upstreamBillingProbeSettingRepo {
	return &upstreamBillingProbeSettingRepo{values: map[string]string{
		SettingKeyUpstreamBillingProbeSettings: fmt.Sprintf(`{"enabled":true,"interval_seconds":60,"newapi_multiplier_consensus_count":%d}`, consensusCount),
	}}
}

// Official New API: the usage route names no group, and the log route returns
// 1000 rows (far over 64 KiB) with "other" encoded as a JSON string.
func newAPITestLogRows(count int, newest int64, ratio float64) string {
	rows := make([]string, 0, count)
	for i := 0; i < count; i++ {
		other := fmt.Sprintf(`{\"billing_source\":\"wallet\",\"cache_ratio\":0.1,\"completion_ratio\":6,\"group_ratio\":%g,\"model_price\":-1,\"model_ratio\":0.1,\"request_path\":\"/v1/chat/completions\",\"user_group_ratio\":-1}`, ratio)
		rows = append(rows, fmt.Sprintf(`{"id":%d,"user_id":823,"created_at":%d,"type":2,"content":"","username":"miza","token_name":"测试","model_name":"codex-auto-review","quota":1,"prompt_tokens":24,"completion_tokens":5,"use_time":1,"is_stream":false,"channel":0,"channel_name":"","token_id":4498,"group":"gpt-plus","ip":"","request_id":"20261108025255400386468268d9d6CmPZMoJn","other":"%s"}`, i+1, newest-int64(i), other))
	}
	return strings.Join(rows, ",")
}

const newAPITestOfficialUsage = `{"code":true,"data":{"expires_at":0,"model_limits":{},"model_limits_enabled":false,"name":"测试","object":"token_usage","total_available":0,"total_granted":0,"total_used":0,"unlimited_quota":true},"message":"ok"}`

const newAPITestPricing = `{"success":true,"data":[
	{"model_name":"gpt-6-sol","enable_groups":["gpt-plus","gpt-pro"]},
	{"model_name":"gpt-6-astra","enable_groups":["gpt-plus","gpt-pro"]},
	{"model_name":"gpt-6-ultrafast","enable_groups":["gpt-pro"]},
	{"model_name":"claude-sonnet-5","enable_groups":["cc-max","cc-coding"]}
],"group_ratio":{"gpt-plus":0.15,"gpt-pro":0.38,"cc-max":0.85,"cc-coding":0.8},"usable_group":{"gpt-plus":"","gpt-pro":"","cc-max":"","cc-coding":""}}`

func TestNewAPIProbeSyncsOfficialReleaseFromLargeConsumptionLog(t *testing.T) {
	account, repo := newAPITestAccount(201, true)
	logsBody := `{"data":[` + newAPITestLogRows(1000, 1794106376, 0.15) + `],"message":"","success":true}`
	require.Greater(t, len(logsBody), upstreamBillingProbeMaxBodyBytes)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{"error":{"message":"Invalid URL (GET /v1/sub2api/billing)","type":"invalid_request_error"}}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion),
		newAPITestResponse(http.StatusOK, logsBody, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Equal(t, UpstreamBillingProtocolNewAPI, snapshot.Protocol)
	require.Equal(t, newAPITestVersion, snapshot.UpstreamVersion)
	require.Equal(t, newAPIRateSourceConsumptionLog, snapshot.Data["source"])
	require.Equal(t, "gpt-plus", snapshot.Data["group"])
	require.Equal(t, newAPIProbeMaxKeptLogs, snapshot.MultiplierMatchCount)
	require.Equal(t, time.Unix(1794106376, 0).UTC(), *snapshot.ObservedMultiplierAt)
	require.NotNil(t, snapshot.SyncedRateMultiplier)
	require.Equal(t, 0.15, account.BillingRateMultiplier())
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "https://upstream.example/api/log/token", upstream.requests[2].URL.String())
	require.Equal(t, "Bearer sk-test", upstream.requests[2].Header.Get("Authorization"))
}

func TestNewAPIProbeReadsLegacyReleaseLogsWithQueryKey(t *testing.T) {
	account, repo := newAPITestAccount(202, true)
	// Releases before v0.10.8 send no version header, ignore the bearer key on
	// the log route and return every row oldest first.
	legacyLogs := `{"data":[
		{"id":1,"created_at":1700000000,"type":2,"group":"default","other":"{\"group_ratio\":1}"},
		{"id":2,"created_at":1700000100,"type":5,"group":"default","other":"{\"group_ratio\":9}"},
		{"id":3,"created_at":1700000200,"type":2,"group":"vip","other":"{\"group_ratio\":0.3,\"user_group_ratio\":-1}"}
	],"message":"","success":true}`
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{"error":{"message":"Invalid URL"}}`, ""),
		newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, ""),
		newAPITestResponse(http.StatusOK, `{"data":[],"message":"","success":true}`, ""),
		newAPITestResponse(http.StatusOK, legacyLogs, ""),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(1))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Equal(t, UpstreamBillingProtocolNewAPI, snapshot.Protocol)
	require.Empty(t, snapshot.UpstreamVersion)
	require.Equal(t, "vip", snapshot.Data["group"])
	require.Equal(t, 0.3, *snapshot.ObservedMultiplier)
	require.Equal(t, 0.3, account.BillingRateMultiplier())
	require.Len(t, upstream.requests, 4)
	require.Equal(t, "https://upstream.example/api/log/token?key=sk-test", upstream.requests[3].URL.String())
}

func TestNewAPIProbeSkipsLegacyLogQueryForVersionedRelease(t *testing.T) {
	account, repo := newAPITestAccount(203, true)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[],"message":"","success":true}`, newAPITestVersion),
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusWaitingLogs, snapshot.Status)
	require.Equal(t, "waiting_for_consumption_logs", snapshot.LastError)
	require.Len(t, upstream.requests, 4)
	for _, req := range upstream.requests {
		require.Empty(t, req.URL.RawQuery)
	}
	require.Equal(t, "https://upstream.example/api/pricing", upstream.requests[3].URL.String())
}

func TestNewAPIProbeInfersGroupFromModelListWithoutLogs(t *testing.T) {
	account, repo := newAPITestAccount(204, true)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[],"message":"","success":true}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestPricing, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[{"id":"gpt-6-astra","object":"model"},{"id":"gpt-6-sol","object":"model"}],"object":"list","success":true}`, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Equal(t, newAPIRateSourceModelInference, snapshot.Data["source"])
	require.Equal(t, "gpt-plus", snapshot.Data["group"])
	require.Equal(t, 2, snapshot.Data["model_count"])
	require.Equal(t, 0.15, account.BillingRateMultiplier())
	require.Len(t, upstream.requests, 5)
	require.Equal(t, "https://upstream.example/api/pricing", upstream.requests[3].URL.String())
	require.Empty(t, upstream.requests[3].Header.Get("Authorization"), "the public pricing route must not receive the account key")
	require.Equal(t, "https://upstream.example/v1/models", upstream.requests[4].URL.String())
	require.Equal(t, "Bearer sk-test", upstream.requests[4].Header.Get("Authorization"))
}

func TestNewAPIProbeLeavesAmbiguousModelInferenceWaiting(t *testing.T) {
	account, repo := newAPITestAccount(205, true)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[],"message":"","success":true}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestPricing, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[{"id":"claude-sonnet-5"}]}`, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusWaitingLogs, snapshot.Status)
	require.Equal(t, UpstreamBillingProtocolNewAPI, snapshot.Protocol)
	require.Equal(t, []newAPIGroupCandidate{{Group: "cc-coding", Ratio: 0.8}, {Group: "cc-max", Ratio: 0.85}}, snapshot.Data["candidates"])
	require.Nil(t, snapshot.SyncedRateMultiplier)
	require.Equal(t, 1.0, account.BillingRateMultiplier())
}

func TestNewAPIProbeLooksUpUsageGroupInPublicPricing(t *testing.T) {
	account, repo := newAPITestAccount(206, true)
	usage := `{"code":true,"data":{"object":"token_usage","name":"","group":"gpt-pro","total_used":0,"total_available":0},"message":"ok"}`
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, usage, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[],"message":"","success":true}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestPricing, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Equal(t, newAPIRateSourceGroupPricing, snapshot.Data["source"])
	require.Equal(t, "gpt-pro", snapshot.Data["group"])
	require.Equal(t, 0.38, account.BillingRateMultiplier())
	require.Len(t, upstream.requests, 4, "a group named by the usage route needs no model list")
}

func TestNewAPIProbeDoesNotGuessForPrivateUsageGroup(t *testing.T) {
	account, repo := newAPITestAccount(207, true)
	usage := `{"code":true,"data":{"object":"token_usage","name":"k","group":"svip","total_used":0,"total_available":0},"message":"ok"}`
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, usage, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[],"message":"","success":true}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestPricing, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusWaitingLogs, snapshot.Status)
	require.Nil(t, snapshot.Data)
	require.Len(t, upstream.requests, 4)
	require.Equal(t, 1.0, account.BillingRateMultiplier())
}

func TestNewAPIProbeCorroboratesFewLogsWithCurrentGroupRatio(t *testing.T) {
	for _, tc := range []struct {
		name       string
		models     string
		wantStatus string
		wantRate   float64
	}{
		{name: "model list agrees", models: `{"data":[{"id":"gpt-6-sol"},{"id":"gpt-6-astra"}]}`, wantStatus: UpstreamBillingProbeStatusOK, wantRate: 0.15},
		{name: "model list disagrees", models: `{"data":[{"id":"gpt-6-sol"},{"id":"gpt-6-astra"},{"id":"gpt-6-ultrafast"}]}`, wantStatus: UpstreamBillingProbeStatusWaitingConsensus, wantRate: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account, repo := newAPITestAccount(208, true)
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
				newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion),
				newAPITestResponse(http.StatusOK, `{"data":[`+newAPITestLogRows(1, 1794106376, 0.15)+`],"message":"","success":true}`, newAPITestVersion),
				newAPITestResponse(http.StatusOK, newAPITestPricing, newAPITestVersion),
				newAPITestResponse(http.StatusOK, tc.models, newAPITestVersion),
			}}
			svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

			snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, snapshot.Status)
			require.Equal(t, 1, snapshot.MultiplierMatchCount)
			require.Equal(t, 0.15, *snapshot.ObservedMultiplier)
			require.Equal(t, tc.wantRate, account.BillingRateMultiplier())
			if tc.wantStatus == UpstreamBillingProbeStatusOK {
				require.Equal(t, newAPIRateSourceModelInference, snapshot.Data["corroborated_by"])
			} else {
				require.NotContains(t, snapshot.Data, "corroborated_by")
			}
		})
	}
}

func TestNewAPIProbeSkipsCatalogOnceLogsReachConsensus(t *testing.T) {
	account, repo := newAPITestAccount(209, true)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"data":[`+newAPITestLogRows(3, 1794106376, 0.15)+`],"message":"","success":true}`, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Equal(t, 0.15, account.BillingRateMultiplier())
	require.Len(t, upstream.requests, 3)
}

func TestNewAPIProbeKeepsNewAPIProtocolWhenLaterStepsFail(t *testing.T) {
	account, repo := newAPITestAccount(210, true)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion),
		newAPITestResponse(http.StatusBadGateway, `bad gateway`, newAPITestVersion),
		newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusFailed, snapshot.Status)
	require.Equal(t, UpstreamBillingProtocolNewAPI, snapshot.Protocol)
	require.Equal(t, newAPITestVersion, snapshot.UpstreamVersion)
	require.Equal(t, "http_error", snapshot.LastError)
	require.Equal(t, http.StatusBadGateway, snapshot.HTTPStatus)
}

func TestNewAPIProbeStopsAfterLogRequestFails(t *testing.T) {
	account, repo := newAPITestAccount(213, true)
	upstream := &newAPISequenceUpstream{steps: []newAPISequenceStep{
		{resp: newAPITestResponse(http.StatusNotFound, `{}`, newAPITestVersion)},
		{resp: newAPITestResponse(http.StatusOK, newAPITestOfficialUsage, newAPITestVersion)},
		{err: context.DeadlineExceeded},
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusFailed, snapshot.Status)
	require.Equal(t, "request_failed", snapshot.LastError)
	require.Equal(t, UpstreamBillingProtocolNewAPI, snapshot.Protocol)
	require.Equal(t, 3, upstream.calls, "no catalog request after the log request timed out")
}

// newAPISequenceUpstream answers each request with the next step, which may
// be a transport error.
type newAPISequenceUpstream struct {
	steps []newAPISequenceStep
	calls int
}

type newAPISequenceStep struct {
	resp *http.Response
	err  error
}

func (u *newAPISequenceUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	if u.calls >= len(u.steps) {
		return nil, fmt.Errorf("unexpected request %d", u.calls+1)
	}
	step := u.steps[u.calls]
	u.calls++
	return step.resp, step.err
}

func (u *newAPISequenceUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func TestNewAPIProbeReportsCloudflareBlock(t *testing.T) {
	account, repo := newAPITestAccount(211, true)
	block := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Server": []string{"cloudflare"}, "Cf-Ray": []string{"a571295d4e5af532-AMS"}, "Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(`<!DOCTYPE html><title>Attention Required! | Cloudflare</title><h1>Sorry, you have been blocked</h1>`)),
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusNotFound, `{}`, "v1.0.0-rc.7"),
		block,
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusFailed, snapshot.Status)
	require.Equal(t, "cloudflare_blocked", snapshot.LastError)
	require.Equal(t, UpstreamBillingProtocolNewAPI, snapshot.Protocol)
	require.Equal(t, "v1.0.0-rc.7", snapshot.UpstreamVersion)
}

func TestNewAPIProbeTreatsNewAPIErrorOnSub2APIRouteAsNewAPI(t *testing.T) {
	account, repo := newAPITestAccount(212, false)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newAPITestResponse(http.StatusUnauthorized, `{"error":{"message":"invalid token"}}`, newAPITestVersion),
		newAPITestResponse(http.StatusOK, `{"code":true,"data":{"object":"token_usage","group":"gpt-plus","group_ratio":"0.15"},"message":"ok"}`, newAPITestVersion),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, newAPITestSettings(3))

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Equal(t, newAPIRateSourceTokenUsage, snapshot.Data["source"])
	require.Equal(t, 0.15, *snapshot.ObservedMultiplier)
	require.Nil(t, snapshot.SyncedRateMultiplier, "rate sync is off for this account")
}

func TestDecodeNewAPIConsumptionLogPage(t *testing.T) {
	t.Run("skips rows it cannot use", func(t *testing.T) {
		body := `{"data":[
			{"id":1,"created_at":1794106376000,"type":2,"group":"a","other":{"group_ratio":0.5,"user_group_ratio":0.25}},
			"not a row",
			{"id":2,"created_at":"1794106375","type":"2","group":"b","other":"{\"group_ratio\":\"0.4\"}"},
			{"id":3,"created_at":1794106374,"type":2,"group":"c","other":"not json"},
			{"id":4,"created_at":1794106373,"type":2,"group":"d","other":"{\"group_ratio\":-1}"},
			{"id":5,"created_at":1794106372,"type":1,"group":"e","other":"{\"group_ratio\":3}"}
		],"success":true}`
		page, err := decodeNewAPIConsumptionLogPage(strings.NewReader(body), 1<<20)
		require.NoError(t, err)
		require.False(t, page.truncated)
		require.Equal(t, []newAPIConsumptionLog{
			{CreatedAt: 1794106376, Group: "a", Ratio: 0.25},
			{CreatedAt: 1794106375, Group: "b", Ratio: 0.4},
		}, page.logs)
	})

	t.Run("cut newest-first page keeps its head", func(t *testing.T) {
		body := `{"data":[` + newAPITestLogRows(50, 1794106376, 0.2) + `],"success":true}`
		page, err := decodeNewAPIConsumptionLogPage(strings.NewReader(body), int64(len(body)/2))
		require.NoError(t, err)
		require.True(t, page.truncated)
		require.False(t, page.ascending)
		require.NotEmpty(t, page.logs)
		require.Equal(t, int64(1794106376), page.logs[0].CreatedAt)
	})

	t.Run("cut oldest-first page is flagged", func(t *testing.T) {
		rows := make([]string, 0, 50)
		for i := 0; i < 50; i++ {
			rows = append(rows, fmt.Sprintf(`{"id":%d,"created_at":%d,"type":2,"group":"g","other":"{\"group_ratio\":0.2}"}`, i+1, 1700000000+i))
		}
		body := `{"data":[` + strings.Join(rows, ",") + `],"success":true}`
		page, err := decodeNewAPIConsumptionLogPage(strings.NewReader(body), int64(len(body)/2))
		require.NoError(t, err)
		require.True(t, page.truncated)
		require.True(t, page.ascending)
	})

	t.Run("rejection and null data", func(t *testing.T) {
		page, err := decodeNewAPIConsumptionLogPage(strings.NewReader(`{"message":"record not found","success":false}`), 1<<20)
		require.NoError(t, err)
		require.True(t, page.rejected)

		page, err = decodeNewAPIConsumptionLogPage(strings.NewReader(`{"data":null,"message":"","success":true}`), 1<<20)
		require.NoError(t, err)
		require.False(t, page.rejected)
		require.Empty(t, page.logs)
	})

	t.Run("malformed body is an error", func(t *testing.T) {
		_, err := decodeNewAPIConsumptionLogPage(strings.NewReader(`{"data":{"rows":[]},"success":true}`), 1<<20)
		require.Error(t, err)
		_, err = decodeNewAPIConsumptionLogPage(strings.NewReader(`<html>`), 1<<20)
		require.Error(t, err)
	})
}

func TestInferNewAPITokenGroupResolvesSharedRatio(t *testing.T) {
	catalog, err := parseNewAPIPricingCatalog([]byte(`{"success":true,"data":[
		{"model_name":"m1","enable_groups":["a","b"]},
		{"model_name":"m2","enable_groups":["a","b","c"]}
	],"group_ratio":{"a":0.5,"b":"0.5","c":2}}`))
	require.NoError(t, err)

	resolution := inferNewAPITokenGroup([]string{"m2", "m1"}, catalog)
	require.True(t, resolution.resolved)
	require.Equal(t, "a / b", resolution.group)
	require.Equal(t, 0.5, resolution.ratio)

	only := inferNewAPITokenGroup([]string{"m2"}, catalog)
	require.True(t, only.resolved)
	require.Equal(t, "c", only.group)
	require.Equal(t, 2.0, only.ratio)

	require.False(t, inferNewAPITokenGroup([]string{"m1", "m3"}, catalog).resolved)
	require.False(t, inferNewAPITokenGroup(nil, catalog).resolved)
}

func TestClassifyUpstreamBillingHTTPFailure(t *testing.T) {
	cloudflare := http.Header{"Server": []string{"cloudflare"}}
	require.Equal(t, "cloudflare_blocked", classifyUpstreamBillingHTTPFailure(http.StatusForbidden, cloudflare, []byte("<title>Attention Required! | Cloudflare</title>")))
	require.Equal(t, "cloudflare_blocked", classifyUpstreamBillingHTTPFailure(http.StatusServiceUnavailable, http.Header{"Cf-Ray": []string{"x"}}, []byte("<title>Just a moment...</title>")))
	require.Equal(t, "http_error", classifyUpstreamBillingHTTPFailure(http.StatusForbidden, cloudflare, []byte(`{"message":"forbidden"}`)))
	require.Equal(t, "http_error", classifyUpstreamBillingHTTPFailure(http.StatusForbidden, http.Header{}, []byte("Attention Required")))
	require.Equal(t, "http_error", classifyUpstreamBillingHTTPFailure(http.StatusInternalServerError, cloudflare, []byte("Attention Required")))
}

func TestSanitizeNewAPIVersion(t *testing.T) {
	require.Equal(t, "v1.0.0-rc.7", sanitizeNewAPIVersion(" v1.0.0-rc.7 "))
	require.Equal(t, "rc.15", sanitizeNewAPIVersion("rc.15\u0000\u200b"))
	require.Len(t, sanitizeNewAPIVersion(strings.Repeat("x", 300)), newAPIProbeMaxVersionLength)
}
