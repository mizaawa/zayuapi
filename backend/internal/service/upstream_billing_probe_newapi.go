package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

// New API exposes no billing document like /v1/sub2api/billing, and the
// releases in the wild disagree on what a token key may read:
//
//   - /api/usage/token/ returns the token quota. Official releases never put
//     the token group in it; some patched builds add group and group_ratio.
//   - /api/log/token returns the token's consumption logs, each carrying the
//     group and the group ratio that was billed. Since v0.10.8 the route reads
//     the key from the Authorization header and returns the newest 1000 rows;
//     earlier releases read it from ?key= and return every row, oldest first.
//   - /api/pricing (public) maps the user-selectable groups to their ratios
//     and every model to the groups that serve it, and /v1/models lists the
//     models of the token's group. A token whose model list equals exactly one
//     group's model set belongs to that group, which covers keys that have no
//     consumption log yet.
//
// The probe tries these in that order and keeps whatever identified the
// upstream as New API, so a later step failing never demotes it to unknown.
const (
	newAPIVersionHeader = "X-New-Api-Version"

	newAPIRateSourceTokenUsage     = "token_usage"
	newAPIRateSourceGroupPricing   = "group_pricing"
	newAPIRateSourceConsumptionLog = "consumption_log"
	newAPIRateSourceModelInference = "model_inference"

	// The log route answers with up to 1000 rows (every row before v0.10.8),
	// far beyond the 64 KiB allowed for fixed-size billing documents. Rows are
	// decoded as a stream and only the newest ones are kept.
	newAPIProbeLogMaxBodyBytes = 16 << 20
	newAPIProbeMaxKeptLogs     = upstreamBillingProbeMaxConsensusCount
	// /api/pricing carries model descriptions and easily exceeds 64 KiB.
	newAPIProbeCatalogMaxBodyBytes = 4 << 20
	newAPIProbeMaxVersionLength    = 96
	newAPILogTypeConsume           = 2
	// created_at is in seconds; larger values are millisecond timestamps.
	newAPIMillisecondTimestampFloor = 100_000_000_000
)

// upstreamBillingProbeTarget is the request identity of one probe.
type upstreamBillingProbeTarget struct {
	account  *Account
	baseURL  string
	proxyURL string
	apiKey   string
}

// newAPIProbeEvidence collects what the upstream revealed about itself across
// the requests of one probe.
type newAPIProbeEvidence struct {
	version    string
	recognized bool
}

func (e *newAPIProbeEvidence) observe(headers http.Header) {
	if e == nil || headers == nil {
		return
	}
	if version := sanitizeNewAPIVersion(headers.Get(newAPIVersionHeader)); version != "" {
		e.version = version
		e.recognized = true
	}
}

func (e *newAPIProbeEvidence) protocol() string {
	if e != nil && e.recognized {
		return UpstreamBillingProtocolNewAPI
	}
	return UpstreamBillingProtocolUnknown
}

func sanitizeNewAPIVersion(value string) string {
	value = strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len(value) > newAPIProbeMaxVersionLength {
		value = value[:newAPIProbeMaxVersionLength]
	}
	return value
}

// probeNewAPI runs the New API steps. handled is false only when nothing in
// the answers identified New API, so the caller may try the next protocol.
func (s *UpstreamBillingProbeService) probeNewAPI(
	ctx context.Context,
	target upstreamBillingProbeTarget,
	evidence *newAPIProbeEvidence,
	interval time.Duration,
	consensusCount int,
	now time.Time,
) (snapshot *UpstreamBillingProbeSnapshot, handled bool, err error) {
	account := target.account
	status, headers, body, requestErr := s.fetchUpstreamBillingProbe(ctx, target, buildNewAPIEndpointURL(target.baseURL, "/api/usage/token/"), true, upstreamBillingProbeMaxBodyBytes)
	evidence.observe(headers)
	if requestErr != "" {
		snapshot, err = s.persistProbeFailureWithEvidence(ctx, account, interval, now, status, requestErr, retryAfter(headers, now), evidence)
		return snapshot, true, err
	}
	var usage *newAPIUsageResponse
	switch {
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		if !evidence.recognized {
			return nil, false, nil
		}
	case status < 200 || status >= 300:
		snapshot, err = s.persistProbeFailureWithEvidence(ctx, account, interval, now, status, classifyUpstreamBillingHTTPFailure(status, headers, body), retryAfter(headers, now), evidence)
		return snapshot, true, err
	default:
		parsed, parseErr := parseNewAPIUsageResponse(body)
		if parseErr != nil && !evidence.recognized {
			return nil, false, nil
		}
		if parseErr == nil {
			usage = parsed
			evidence.recognized = true
		}
	}
	if rate, ok := newAPIUsageGroupMultiplier(usage); ok {
		snapshot, err = s.persistNewAPIDeclaredRate(ctx, account, map[string]any{
			"resolved_rate_multiplier": rate,
			"group":                    usage.Group,
			"source":                   newAPIRateSourceTokenUsage,
		}, rate, status, interval, now, evidence)
		return snapshot, true, err
	}

	logs := s.fetchNewAPIConsumptionLogs(ctx, target, evidence)
	if len(logs.entries) > 0 {
		// Fewer matching rows than the consensus asks for are still enough
		// when the token's current group configuration states the same ratio:
		// two independent sources agree.
		corroboratedBy := ""
		if newAPILogsAwaitConsensus(account, logs.entries, consensusCount) {
			if resolution := s.resolveNewAPIGroupFromCatalog(ctx, target, usage, evidence); resolution.resolved && equalBillingMultiplier(resolution.ratio, logs.entries[0].Ratio) {
				corroboratedBy = resolution.source
			}
		}
		snapshot, err = s.persistNewAPILogProbeResult(ctx, account, logs.entries, logs.status, interval, consensusCount, now, evidence, corroboratedBy)
		return snapshot, true, err
	}

	// An upstream that just failed to answer would only spend the remaining
	// time of a manual probe on the catalog routes as well.
	if logs.failure == "request_failed" {
		snapshot, err = s.persistProbeFailureWithEvidence(ctx, account, interval, now, logs.status, logs.failure, logs.retryAfter, evidence)
		return snapshot, true, err
	}
	resolution := s.resolveNewAPIGroupFromCatalog(ctx, target, usage, evidence)
	if resolution.resolved {
		data := map[string]any{
			"resolved_rate_multiplier": resolution.ratio,
			"group":                    resolution.group,
			"source":                   resolution.source,
		}
		if resolution.modelCount > 0 {
			data["model_count"] = resolution.modelCount
		}
		snapshot, err = s.persistNewAPIDeclaredRate(ctx, account, data, resolution.ratio, status, interval, now, evidence)
		return snapshot, true, err
	}
	if logs.failure != "" {
		snapshot, err = s.persistProbeFailureWithEvidence(ctx, account, interval, now, logs.status, logs.failure, logs.retryAfter, evidence)
		return snapshot, true, err
	}
	reason := "waiting_for_consumption_logs"
	if logs.stale {
		reason = "consumption_logs_truncated"
	}
	snapshot, err = s.persistNewAPIWaitingResult(ctx, account, status, interval, now, reason, resolution.candidates, evidence)
	return snapshot, true, err
}

func buildNewAPIEndpointURL(baseURL, endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/" + strings.TrimLeft(endpoint, "/")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(strings.ToLower(path), "/v1") {
		path = path[:len(path)-len("/v1")]
	}
	parsed.Path = strings.TrimRight(path, "/") + "/" + strings.TrimLeft(endpoint, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// classifyUpstreamBillingHTTPFailure tells a Cloudflare block page apart from
// an answer of the upstream itself: the remedy (a proxy for the account) is
// different, and the block page carries no protocol evidence.
func classifyUpstreamBillingHTTPFailure(status int, headers http.Header, body []byte) string {
	if status != http.StatusForbidden && status != http.StatusServiceUnavailable && status != http.StatusTooManyRequests {
		return "http_error"
	}
	if headers == nil || (!strings.EqualFold(strings.TrimSpace(headers.Get("Server")), "cloudflare") && headers.Get("Cf-Ray") == "") {
		return "http_error"
	}
	head := body
	if len(head) > 16<<10 {
		head = head[:16<<10]
	}
	lower := bytes.ToLower(head)
	for _, marker := range []string{"attention required", "you have been blocked", "just a moment", "cf-error-details", "challenge-platform"} {
		if bytes.Contains(lower, []byte(marker)) {
			return "cloudflare_blocked"
		}
	}
	return "http_error"
}

type newAPIUsageResponse struct {
	Group              string
	GroupRatio         *float64
	ModelLimitsEnabled bool
}

func parseNewAPIUsageResponse(body []byte) (*newAPIUsageResponse, error) {
	var response struct {
		Code    any            `json:"code"`
		Success any            `json:"success"`
		Data    map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return nil, err
	}
	object, _ := response.Data["object"].(string)
	if object != "token_usage" || (!newAPITruthy(response.Code) && !newAPITruthy(response.Success)) {
		return nil, fmt.Errorf("unexpected New API token usage schema")
	}
	group, _ := response.Data["group"].(string)
	usage := &newAPIUsageResponse{
		Group:              strings.TrimSpace(group),
		ModelLimitsEnabled: newAPITruthy(response.Data["model_limits_enabled"]),
	}
	if ratio, ok := resolveAccountExtraNumber(response.Data, "group_ratio"); ok && !math.IsNaN(ratio) && !math.IsInf(ratio, 0) {
		usage.GroupRatio = &ratio
	}
	return usage, nil
}

func newAPIUsageGroupMultiplier(usage *newAPIUsageResponse) (float64, bool) {
	if usage == nil || usage.Group == "" || usage.GroupRatio == nil {
		return 0, false
	}
	rate := *usage.GroupRatio
	// A declared zero (or a rate outside the automatic sync range) is still a
	// declaration. Keep it in the snapshot for the account list, but never
	// write it into the account's billing multiplier automatically.
	return rate, rate >= 0 && !math.IsNaN(rate) && !math.IsInf(rate, 0)
}

func newAPITruthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

// newAPIConsumptionLog is one consumption row reduced to what the probe uses.
type newAPIConsumptionLog struct {
	CreatedAt int64
	Group     string
	Ratio     float64
}

type newAPIConsumptionLogFetch struct {
	entries    []newAPIConsumptionLog // newest first
	status     int
	failure    string
	retryAfter time.Duration
	stale      bool
}

// fetchNewAPIConsumptionLogs reads the newest consumption rows of the token.
// A release without the X-New-Api-Version header predates v0.10.8 and only
// answers this route for ?key=, the request its own key lookup page sends.
// That retry carries the key in its query string too, and goes only to the
// account's own upstream.
func (s *UpstreamBillingProbeService) fetchNewAPIConsumptionLogs(
	ctx context.Context,
	target upstreamBillingProbeTarget,
	evidence *newAPIProbeEvidence,
) newAPIConsumptionLogFetch {
	endpoint := buildNewAPIEndpointURL(target.baseURL, "/api/log/token")
	result := s.fetchNewAPIConsumptionLogPage(ctx, target, endpoint, evidence)
	if len(result.entries) > 0 || result.stale || evidence.version != "" {
		return result
	}
	// Without the key those releases answer an empty list, or success:false
	// when logs live in a separate database.
	if result.failure != "" && result.failure != "logs_rejected" {
		return result
	}
	legacy := s.fetchNewAPIConsumptionLogPage(ctx, target, endpoint+"?key="+url.QueryEscape(target.apiKey), evidence)
	if len(legacy.entries) > 0 || legacy.stale {
		return legacy
	}
	return result
}

func (s *UpstreamBillingProbeService) fetchNewAPIConsumptionLogPage(
	ctx context.Context,
	target upstreamBillingProbeTarget,
	endpoint string,
	evidence *newAPIProbeEvidence,
) newAPIConsumptionLogFetch {
	var page *newAPIConsumptionLogPage
	var parseErr error
	var errorBody []byte
	status, headers, requestErr := s.streamUpstreamBillingProbeRequest(ctx, target, endpoint, true, func(status int, body io.Reader) string {
		if status < 200 || status >= 300 {
			errorBody, _ = io.ReadAll(io.LimitReader(body, upstreamBillingProbeMaxBodyBytes))
			return ""
		}
		page, parseErr = decodeNewAPIConsumptionLogPage(body, newAPIProbeLogMaxBodyBytes)
		return ""
	})
	evidence.observe(headers)
	result := newAPIConsumptionLogFetch{status: status, retryAfter: retryAfter(headers, s.currentTime())}
	switch {
	case requestErr != "":
		result.failure = requestErr
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		result.failure = "unsupported_logs_endpoint"
	case status < 200 || status >= 300:
		result.failure = classifyUpstreamBillingHTTPFailure(status, headers, errorBody)
	case parseErr != nil:
		result.failure = "invalid_response"
	case page.rejected:
		result.failure = "logs_rejected"
	case page.truncated && page.ascending:
		// Oldest-first rows cut off before the end: the newest ones, the only
		// ones that tell the current group ratio, were never read.
		result.stale = true
	default:
		result.entries = page.logs
	}
	return result
}

type newAPIConsumptionLogPage struct {
	logs      []newAPIConsumptionLog // newest first, at most newAPIProbeMaxKeptLogs
	rejected  bool                   // success:false and no rows
	truncated bool
	ascending bool
}

// decodeNewAPIConsumptionLogPage streams {"data":[...],"success":true}. Rows
// that are not consumption logs or lack a usable ratio are skipped instead of
// failing the page; a body cut by the size cap or a read deadline keeps the
// rows decoded so far.
func decodeNewAPIConsumptionLogPage(body io.Reader, limit int64) (*newAPIConsumptionLogPage, error) {
	reader := &upstreamBillingProbeLimitedReader{r: body, remaining: limit}
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	type sequencedLog struct {
		newAPIConsumptionLog
		seq int
	}
	var rows []sequencedLog
	rejected := false
	decodeErr := func() error {
		if err := expectNewAPIJSONDelim(decoder, '{'); err != nil {
			return err
		}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, _ := keyToken.(string)
			switch key {
			case "success":
				var value any
				if err := decoder.Decode(&value); err != nil {
					return err
				}
				rejected = !newAPITruthy(value)
			case "data":
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				if token == nil {
					continue
				}
				if delim, ok := token.(json.Delim); !ok || delim != '[' {
					return fmt.Errorf("unexpected New API consumption log schema")
				}
				for decoder.More() {
					var raw json.RawMessage
					if err := decoder.Decode(&raw); err != nil {
						return err
					}
					if entry, ok := newAPIConsumptionLogFromRow(raw); ok {
						rows = append(rows, sequencedLog{newAPIConsumptionLog: entry, seq: len(rows)})
					}
				}
				if err := expectNewAPIJSONDelim(decoder, ']'); err != nil {
					return err
				}
			default:
				var skipped json.RawMessage
				if err := decoder.Decode(&skipped); err != nil {
					return err
				}
			}
		}
		return expectNewAPIJSONDelim(decoder, '}')
	}()
	page := &newAPIConsumptionLogPage{}
	if decodeErr != nil {
		if !reader.cut() {
			return nil, decodeErr
		}
		page.truncated = true
	}
	page.rejected = rejected && len(rows) == 0
	page.ascending = len(rows) > 1 && rows[0].CreatedAt < rows[len(rows)-1].CreatedAt
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CreatedAt != rows[j].CreatedAt {
			return rows[i].CreatedAt > rows[j].CreatedAt
		}
		// Within one second keep the arrival order of the newest-first
		// releases and reverse it for the oldest-first ones.
		if page.ascending {
			return rows[i].seq > rows[j].seq
		}
		return rows[i].seq < rows[j].seq
	})
	if len(rows) > newAPIProbeMaxKeptLogs {
		rows = rows[:newAPIProbeMaxKeptLogs]
	}
	page.logs = make([]newAPIConsumptionLog, len(rows))
	for i := range rows {
		page.logs[i] = rows[i].newAPIConsumptionLog
	}
	return page, nil
}

func expectNewAPIJSONDelim(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); !ok || delim != want {
		return fmt.Errorf("unexpected New API consumption log schema")
	}
	return nil
}

func newAPIConsumptionLogFromRow(raw json.RawMessage) (newAPIConsumptionLog, bool) {
	var row map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&row); err != nil || row == nil {
		return newAPIConsumptionLog{}, false
	}
	logType, ok := resolveAccountExtraNumber(row, "type")
	if !ok || logType != newAPILogTypeConsume {
		return newAPIConsumptionLog{}, false
	}
	createdAt, ok := resolveAccountExtraNumber(row, "created_at")
	if !ok || createdAt <= 0 || math.IsInf(createdAt, 0) {
		return newAPIConsumptionLog{}, false
	}
	if createdAt >= newAPIMillisecondTimestampFloor {
		createdAt /= 1000
	}
	ratio, ok := newAPIConsumptionLogRatio(newAPIConsumptionLogOther(row["other"]))
	if !ok {
		return newAPIConsumptionLog{}, false
	}
	group, _ := row["group"].(string)
	return newAPIConsumptionLog{CreatedAt: int64(createdAt), Group: strings.TrimSpace(group), Ratio: ratio}, true
}

// newAPIConsumptionLogOther decodes "other", which New API stores as a JSON
// string and some forks return as an object.
func newAPIConsumptionLogOther(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case string:
		var other map[string]any
		decoder := json.NewDecoder(strings.NewReader(v))
		decoder.UseNumber()
		if decoder.Decode(&other) != nil {
			return nil
		}
		return other
	default:
		return nil
	}
}

// newAPIConsumptionLogRatio prefers user_group_ratio, the user group's special
// ratio for the group used (-1 when none applies), over group_ratio.
func newAPIConsumptionLogRatio(other map[string]any) (float64, bool) {
	for _, key := range []string{"user_group_ratio", "group_ratio"} {
		value, ok := resolveAccountExtraNumber(other, key)
		if ok && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value, true
		}
	}
	return 0, false
}

type newAPIGroupCandidate struct {
	Group string  `json:"group"`
	Ratio float64 `json:"rate_multiplier"`
}

type newAPIGroupResolution struct {
	resolved   bool
	source     string
	group      string
	ratio      float64
	modelCount int
	candidates []newAPIGroupCandidate
}

type newAPIPricingCatalog struct {
	groupRatios map[string]float64
	groupModels map[string]map[string]struct{}
}

// resolveNewAPIGroupFromCatalog finds the token group ratio without a
// consumption log: a group named by the usage route is looked up in the public
// pricing; otherwise the token's model list must match exactly one public
// group's model set (or groups sharing one ratio). A group the usage route
// named but the pricing does not list is a private group, and guessing a public
// one for it would be wrong.
func (s *UpstreamBillingProbeService) resolveNewAPIGroupFromCatalog(
	ctx context.Context,
	target upstreamBillingProbeTarget,
	usage *newAPIUsageResponse,
	evidence *newAPIProbeEvidence,
) newAPIGroupResolution {
	if usage != nil && usage.Group == "" && usage.ModelLimitsEnabled {
		return newAPIGroupResolution{}
	}
	status, headers, body, requestErr := s.fetchUpstreamBillingProbe(ctx, target, buildNewAPIEndpointURL(target.baseURL, "/api/pricing"), false, newAPIProbeCatalogMaxBodyBytes)
	evidence.observe(headers)
	if requestErr != "" || status < 200 || status >= 300 {
		return newAPIGroupResolution{}
	}
	catalog, err := parseNewAPIPricingCatalog(body)
	if err != nil {
		return newAPIGroupResolution{}
	}
	if usage != nil && usage.Group != "" {
		ratio, ok := catalog.groupRatios[usage.Group]
		if !ok {
			return newAPIGroupResolution{}
		}
		return newAPIGroupResolution{resolved: true, source: newAPIRateSourceGroupPricing, group: usage.Group, ratio: ratio}
	}
	status, headers, body, requestErr = s.fetchUpstreamBillingProbe(ctx, target, buildNewAPIEndpointURL(target.baseURL, "/v1/models"), true, newAPIProbeCatalogMaxBodyBytes)
	evidence.observe(headers)
	if requestErr != "" || status < 200 || status >= 300 {
		return newAPIGroupResolution{}
	}
	models, err := parseNewAPIModelIDs(body)
	if err != nil {
		return newAPIGroupResolution{}
	}
	return inferNewAPITokenGroup(models, catalog)
}

func parseNewAPIPricingCatalog(body []byte) (*newAPIPricingCatalog, error) {
	var response struct {
		Success    any               `json:"success"`
		Data       []json.RawMessage `json:"data"`
		GroupRatio map[string]any    `json:"group_ratio"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return nil, err
	}
	if !newAPITruthy(response.Success) || len(response.GroupRatio) == 0 {
		return nil, fmt.Errorf("unexpected New API pricing schema")
	}
	catalog := &newAPIPricingCatalog{
		groupRatios: make(map[string]float64, len(response.GroupRatio)),
		groupModels: make(map[string]map[string]struct{}),
	}
	for group := range response.GroupRatio {
		ratio, ok := resolveAccountExtraNumber(response.GroupRatio, group)
		if ok && ratio >= 0 && !math.IsNaN(ratio) && !math.IsInf(ratio, 0) {
			catalog.groupRatios[group] = ratio
		}
	}
	for _, raw := range response.Data {
		var model struct {
			ModelName    string `json:"model_name"`
			EnableGroups []any  `json:"enable_groups"`
		}
		if json.Unmarshal(raw, &model) != nil || model.ModelName == "" {
			continue
		}
		for _, value := range model.EnableGroups {
			group, ok := value.(string)
			if !ok {
				continue
			}
			if catalog.groupModels[group] == nil {
				catalog.groupModels[group] = make(map[string]struct{})
			}
			catalog.groupModels[group][model.ModelName] = struct{}{}
		}
	}
	return catalog, nil
}

func parseNewAPIModelIDs(body []byte) ([]string, error) {
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(response.Data))
	for _, model := range response.Data {
		if model.ID != "" {
			models = append(models, model.ID)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("empty model list")
	}
	return models, nil
}

// inferNewAPITokenGroup accepts only groups whose model set equals the
// token's model list. Several such groups resolve only when they share one
// ratio; otherwise they are returned as candidates for display.
func inferNewAPITokenGroup(tokenModels []string, catalog *newAPIPricingCatalog) newAPIGroupResolution {
	if catalog == nil {
		return newAPIGroupResolution{}
	}
	tokenSet := make(map[string]struct{}, len(tokenModels))
	for _, model := range tokenModels {
		tokenSet[model] = struct{}{}
	}
	if len(tokenSet) == 0 {
		return newAPIGroupResolution{}
	}
	var exact []newAPIGroupCandidate
	for group, ratio := range catalog.groupRatios {
		models := catalog.groupModels[group]
		if len(models) != len(tokenSet) {
			continue
		}
		matches := true
		for model := range tokenSet {
			if _, ok := models[model]; !ok {
				matches = false
				break
			}
		}
		if matches {
			exact = append(exact, newAPIGroupCandidate{Group: group, Ratio: ratio})
		}
	}
	if len(exact) == 0 {
		return newAPIGroupResolution{}
	}
	sort.Slice(exact, func(i, j int) bool { return exact[i].Group < exact[j].Group })
	for _, candidate := range exact[1:] {
		if !equalBillingMultiplier(candidate.Ratio, exact[0].Ratio) {
			return newAPIGroupResolution{candidates: exact}
		}
	}
	groups := make([]string, len(exact))
	for i, candidate := range exact {
		groups[i] = candidate.Group
	}
	return newAPIGroupResolution{
		resolved:   true,
		source:     newAPIRateSourceModelInference,
		group:      strings.Join(groups, " / "),
		ratio:      exact[0].Ratio,
		modelCount: len(tokenSet),
	}
}

// persistNewAPIDeclaredRate stores a ratio that reflects the token's current
// group configuration (usage route, public pricing or model match) and syncs
// it at once, unlike consumption logs that need consensus.
func (s *UpstreamBillingProbeService) persistNewAPIDeclaredRate(
	ctx context.Context,
	account *Account,
	data map[string]any,
	rate float64,
	status int,
	interval time.Duration,
	now time.Time,
	evidence *newAPIProbeEvidence,
) (*UpstreamBillingProbeSnapshot, error) {
	observedAt := now.UTC()
	snapshot := &UpstreamBillingProbeSnapshot{
		Status:               UpstreamBillingProbeStatusOK,
		Protocol:             UpstreamBillingProtocolNewAPI,
		UpstreamVersion:      evidence.version,
		ObservedMultiplier:   &rate,
		ObservedMultiplierAt: &observedAt,
		MultiplierMatchCount: 1,
		Data:                 data,
		ReceivedAt:           &observedAt,
		FreshUntil:           probeTimePtr(now.Add(2 * interval)),
		LastAttemptAt:        now,
		NextProbeAt:          now.Add(interval),
		HTTPStatus:           status,
	}
	var syncRate *float64
	if upstreamBillingRateSyncEnabled(account) {
		if candidate, valid := upstreamBillingProbeSyncRate(snapshot.Data); valid && !equalBillingMultiplier(account.BillingRateMultiplier(), candidate) {
			syncRate = &candidate
			snapshot.SyncedRateMultiplier = &candidate
		}
	}
	if err := s.updateSnapshot(ctx, account, snapshot, syncRate); err != nil {
		return nil, err
	}
	if syncRate != nil {
		slog.Info("upstream_billing_rate_sync_applied", "source", "newapi_"+fmt.Sprint(data["source"]), "account_id", account.ID, "new_rate_multiplier", *syncRate)
	}
	return snapshot, nil
}

// newAPILogMatchingCount counts the newest rows that share the newest ratio.
func newAPILogMatchingCount(logs []newAPIConsumptionLog) int {
	count := 0
	for _, log := range logs {
		if !equalBillingMultiplier(log.Ratio, logs[0].Ratio) {
			break
		}
		count++
	}
	return count
}

// newAPILogsAwaitConsensus reports whether the logs alone leave a syncable
// ratio unwritten for want of matching rows.
func newAPILogsAwaitConsensus(account *Account, logs []newAPIConsumptionLog, consensusCount int) bool {
	if len(logs) == 0 || !upstreamBillingRateSyncEnabled(account) {
		return false
	}
	if consensusCount <= 0 {
		consensusCount = upstreamBillingProbeDefaultConsensusCount
	}
	value, valid := upstreamBillingProbeSyncRate(map[string]any{"resolved_rate_multiplier": logs[0].Ratio})
	return valid && newAPILogMatchingCount(logs) < consensusCount && !equalBillingMultiplier(account.BillingRateMultiplier(), value)
}

// persistNewAPILogProbeResult syncs the newest log ratio once consensusCount
// newest rows agree, or at once when corroboratedBy names a catalog source
// that resolved the same ratio.
func (s *UpstreamBillingProbeService) persistNewAPILogProbeResult(
	ctx context.Context,
	account *Account,
	logs []newAPIConsumptionLog,
	status int,
	interval time.Duration,
	consensusCount int,
	now time.Time,
	evidence *newAPIProbeEvidence,
	corroboratedBy string,
) (*UpstreamBillingProbeSnapshot, error) {
	if len(logs) == 0 {
		return s.persistNewAPIWaitingResult(ctx, account, status, interval, now, "waiting_for_consumption_logs", nil, evidence)
	}
	if consensusCount <= 0 {
		consensusCount = upstreamBillingProbeDefaultConsensusCount
	}
	rate := logs[0].Ratio
	observedAt := time.Unix(logs[0].CreatedAt, 0).UTC()
	matchingCount := newAPILogMatchingCount(logs)
	var syncRate *float64
	if (matchingCount >= consensusCount || corroboratedBy != "") && upstreamBillingRateSyncEnabled(account) {
		if value, valid := upstreamBillingProbeSyncRate(map[string]any{"resolved_rate_multiplier": rate}); valid && !equalBillingMultiplier(account.BillingRateMultiplier(), value) {
			syncRate = &value
		}
	}
	state := UpstreamBillingProbeStatusOK
	reason := ""
	if upstreamBillingRateSyncEnabled(account) && !equalBillingMultiplier(account.BillingRateMultiplier(), rate) && syncRate == nil {
		state = UpstreamBillingProbeStatusWaitingConsensus
		reason = "waiting_for_multiplier_consensus"
	}
	snapshot := &UpstreamBillingProbeSnapshot{
		Status: state, Protocol: UpstreamBillingProtocolNewAPI, UpstreamVersion: evidence.version,
		LastAttemptAt: now, NextProbeAt: now.Add(interval), HTTPStatus: status, LastError: reason,
		ObservedMultiplier: &rate, ObservedMultiplierAt: &observedAt, MultiplierMatchCount: matchingCount,
		Data: map[string]any{
			"resolved_rate_multiplier": rate,
			"group":                    logs[0].Group,
			"source":                   newAPIRateSourceConsumptionLog,
			"consensus_required":       consensusCount,
		},
		ReceivedAt:           &observedAt,
		FreshUntil:           probeTimePtr(now.Add(2 * interval)),
		SyncedRateMultiplier: syncRate,
	}
	if corroboratedBy != "" {
		snapshot.Data["corroborated_by"] = corroboratedBy
	}
	if err := s.updateSnapshot(ctx, account, snapshot, syncRate); err != nil {
		return nil, err
	}
	if syncRate != nil {
		slog.Info("upstream_billing_rate_sync_applied", "source", "newapi_consumption_log", "account_id", account.ID, "new_rate_multiplier", *syncRate)
	}
	return snapshot, nil
}

func (s *UpstreamBillingProbeService) persistNewAPIWaitingResult(
	ctx context.Context,
	account *Account,
	status int,
	interval time.Duration,
	now time.Time,
	reason string,
	candidates []newAPIGroupCandidate,
	evidence *newAPIProbeEvidence,
) (*UpstreamBillingProbeSnapshot, error) {
	snapshot := &UpstreamBillingProbeSnapshot{
		Status: UpstreamBillingProbeStatusWaitingLogs, Protocol: UpstreamBillingProtocolNewAPI, UpstreamVersion: evidence.version,
		LastAttemptAt: now, NextProbeAt: now.Add(interval), HTTPStatus: status, LastError: reason,
	}
	if len(candidates) > 0 {
		snapshot.Data = map[string]any{"source": newAPIRateSourceModelInference, "candidates": candidates}
	}
	if err := s.updateSnapshot(ctx, account, snapshot, nil); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *UpstreamBillingProbeService) persistProbeFailureWithEvidence(
	ctx context.Context,
	account *Account,
	interval time.Duration,
	now time.Time,
	statusCode int,
	reason string,
	retryAfterDuration time.Duration,
	evidence *newAPIProbeEvidence,
) (*UpstreamBillingProbeSnapshot, error) {
	return s.persistProbeFailureDetailed(ctx, account, interval, now, statusCode, reason, retryAfterDuration, evidence.protocol(), evidence.version)
}
