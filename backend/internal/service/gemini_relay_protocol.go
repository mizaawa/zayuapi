package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

// A fixed table bounds memory even when clients supply arbitrary model names.
// A collision is just a cache miss. Credentials are never stored in the table.
type geminiRouteHint struct {
	key   [32]byte
	until time.Time
	path  string
}

var geminiRelayHints struct {
	sync.Mutex
	slots [256]geminiRouteHint
}

func geminiProtocolOrder(key [32]byte) [3]string {
	geminiRelayHints.Lock()
	defer geminiRelayHints.Unlock()
	v := geminiRelayHints.slots[key[0]]
	if v.key == key && time.Now().Before(v.until) {
		if v.path == providerOpenAIResponsesPath {
			return [3]string{v.path, "", providerOpenAIPath}
		}
		if v.path == providerOpenAIPath {
			return [3]string{v.path, "", providerOpenAIResponsesPath}
		}
	}
	return [3]string{"", providerOpenAIPath, providerOpenAIResponsesPath}
}

func rememberGeminiRoute(key [32]byte, path string) {
	geminiRelayHints.Lock()
	defer geminiRelayHints.Unlock()
	slot := &geminiRelayHints.slots[key[0]]
	if path != "" {
		*slot = geminiRouteHint{key, time.Now().Add(10 * time.Minute), path}
	} else if slot.key == key {
		*slot = geminiRouteHint{}
	}
}

func geminiProtocolMismatch(status int) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnsupportedMediaType, http.StatusNotImplemented:
		return true
	}
	return false
}

func geminiProbeThinking(model, effort string) map[string]any {
	model = strings.ToLower(model)
	if effort == "" || isImageGenerationModel(model) {
		return nil
	}
	if strings.HasPrefix(model, "gemini-3") {
		level := "HIGH"
		switch effort {
		case "low", "none", "minimal":
			level = "LOW"
		case "medium":
			if strings.Contains(model, "flash") {
				level = "MEDIUM"
			}
		}
		return map[string]any{"thinkingLevel": level}
	}
	if strings.HasPrefix(model, "gemini-2.5") {
		budget := 8192
		switch effort {
		case "none", "minimal", "low":
			budget = 128
		case "medium":
			budget = 2048
		}
		return map[string]any{"thinkingBudget": budget}
	}
	return nil
}

// geminiRelayRequest preserves the native response contract for all callers.
// Only replay definitive endpoint rejections, never a timeout, a successful
// generation, an auth failure, or a partially consumed response.
func geminiRelayRequest(account *Account, model string, native *http.Request, do func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if account.Platform != PlatformGemini || account.Type != AccountTypeAPIKey || native.GetBody == nil || isOfficialModelsUpstream(native.URL.String()) || isImageGenerationModel(model) {
		return do(native)
	}
	root, action, ok := strings.Cut(native.URL.Path, "/v1beta/models/"+model+":")
	if !ok || (action != "generateContent" && action != "streamGenerateContent") {
		return do(native)
	}
	stream := action == "streamGenerateContent"
	base := *native.URL
	base.Path, base.RawPath, base.RawQuery, base.Fragment = root, "", "", ""
	key := sha256.Sum256([]byte(fmt.Sprintf("account:%d\x00%s\x00%s\x00%s", account.ID, base.String(), model, account.GetCredential("api_key"))))
	buildOA := func(path string) *http.Request {
		r, err := native.GetBody()
		if err != nil {
			return nil
		}
		defer func() { _ = r.Close() }()
		payload, ok := geminiTextToRelayChat(r, model)
		if !ok {
			return nil
		}
		if path == providerOpenAIResponsesPath {
			payload, ok = geminiRelayResponsesRequest(payload)
			if !ok {
				return nil
			}
		}
		alternate, err := http.NewRequestWithContext(native.Context(), http.MethodPost, buildOpenAIEndpointURL(base.String(), path), bytes.NewReader(payload))
		if err != nil {
			return nil
		}
		alternate.Header.Set("Content-Type", "application/json")
		alternate.Header.Set("Accept", "application/json")
		alternate.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
		account.ApplyHeaderOverrides(alternate.Header)
		return alternate
	}
	var rejected *http.Response
	for _, path := range geminiProtocolOrder(key) {
		req := native
		if path != "" {
			req = buildOA(path)
			if req == nil {
				continue
			}
		}
		if rejected != nil {
			if native.Context().Err() != nil {
				return rejected, nil
			}
			_ = rejected.Body.Close()
		}
		resp, err := do(req)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, errors.Join(errGeminiRelayUncertain, err)
		}
		if geminiProtocolMismatch(resp.StatusCode) {
			rejected = resp
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if path != "" {
				resp, err = normalizeGeminiRelayResponse(resp, stream, path)
				if err != nil {
					return nil, err
				}
			}
			rememberGeminiRoute(key, path)
		}
		return resp, nil
	}
	return rejected, nil
}

func geminiRelayResponsesRequest(chat []byte) ([]byte, bool) {
	var cc map[string]json.RawMessage
	if json.Unmarshal(chat, &cc) != nil || cc["extra_body"] != nil || cc["stop"] != nil {
		return nil, false
	}
	out := map[string]any{"model": cc["model"], "input": cc["messages"], "stream": false, "store": false}
	for source, target := range map[string]string{"temperature": "temperature", "top_p": "top_p", "max_tokens": "max_output_tokens"} {
		if v := cc[source]; v != nil {
			out[target] = v
		}
	}
	if v := cc["reasoning_effort"]; v != nil {
		out["reasoning"] = map[string]any{"effort": v}
	}
	b, err := json.Marshal(out)
	return b, err == nil
}

// Lower only losslessly representable text requests. Never silently drop
// safety settings, tools, media, cachedContent, or a generation constraint.
func geminiTextToRelayChat(r io.Reader, model string) ([]byte, bool) {
	var native map[string]json.RawMessage
	if json.NewDecoder(r).Decode(&native) != nil {
		return nil, false
	}
	for k := range native {
		switch k {
		case "contents", "systemInstruction", "generationConfig":
		default:
			return nil, false
		}
	}
	chat := map[string]any{"model": model, "stream": false}
	var messages []map[string]string
	message := func(raw json.RawMessage, system bool) bool {
		var v struct {
			Role  string
			Parts []map[string]json.RawMessage
		}
		if json.Unmarshal(raw, &v) != nil || len(v.Parts) == 0 {
			return false
		}
		role := "user"
		if system {
			role = "system"
		} else if v.Role == "model" {
			role = "assistant"
		} else if v.Role != "" && v.Role != "user" {
			return false
		}
		var text strings.Builder
		for _, p := range v.Parts {
			var t string
			if len(p) != 1 || json.Unmarshal(p["text"], &t) != nil {
				return false
			}
			_, _ = text.WriteString(t)
		}
		messages = append(messages, map[string]string{"role": role, "content": text.String()})
		return true
	}
	if raw := native["systemInstruction"]; raw != nil && !message(raw, true) {
		return nil, false
	}
	var contents []json.RawMessage
	if json.Unmarshal(native["contents"], &contents) != nil || len(contents) == 0 {
		return nil, false
	}
	for _, raw := range contents {
		if !message(raw, false) {
			return nil, false
		}
	}
	chat["messages"] = messages
	if raw := native["generationConfig"]; raw != nil {
		var cfg map[string]json.RawMessage
		if json.Unmarshal(raw, &cfg) != nil {
			return nil, false
		}
		for k, v := range cfg {
			switch k {
			case "maxOutputTokens":
				chat["max_tokens"] = v
			case "temperature":
				chat["temperature"] = v
			case "topP":
				chat["top_p"] = v
			case "stopSequences":
				chat["stop"] = v
			case "thinkingConfig":
				var tc map[string]json.RawMessage
				if json.Unmarshal(v, &tc) != nil || len(tc) != 1 {
					return nil, false
				}
				var level string
				if json.Unmarshal(tc["thinkingLevel"], &level) == nil {
					chat["reasoning_effort"] = strings.ToLower(level)
				} else if budget, ok := tc["thinkingBudget"]; ok {
					chat["extra_body"] = map[string]any{"google": map[string]any{"thinking_config": map[string]any{"thinking_budget": budget}}}
				} else {
					return nil, false
				}
			default:
				return nil, false
			}
		}
	}
	b, err := json.Marshal(chat)
	return b, err == nil
}

var errGeminiRelayReceipt = errors.New("gemini relay returned an invalid or unmetered Chat Completions response")
var errGeminiRelayUncertain = errors.New("gemini relay generation outcome is unknown")

type geminiRelayBody struct {
	io.ReadCloser
	path string
}

func geminiNegotiatedEndpoint(resp *http.Response) string {
	if body, ok := resp.Body.(geminiRelayBody); ok {
		return body.path
	}
	return ""
}

func (s *GeminiMessagesCompatService) doGeminiGeneration(account *Account, model string, req *http.Request, proxy string) (*http.Response, error) {
	return geminiRelayRequest(account, model, req, func(r *http.Request) (*http.Response, error) {
		return s.httpUpstream.Do(r, proxy, account.ID, account.Concurrency)
	})
}

// Buffer an OA-only relay's receipt before releasing text. A caller disabling
// usage reporting (or disconnecting) must not control whether a bill is issued.
func normalizeGeminiRelayResponse(resp *http.Response, stream bool, path string) (*http.Response, error) {
	defer func() { _ = resp.Body.Close() }()
	const limit = 16 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(b) > limit || !gjson.ValidBytes(b) {
		return nil, errGeminiRelayReceipt
	}
	get := func(path string) (int, bool) { return readOptionalBoundedUsageGJSONInt(gjson.GetBytes(b, path)) }
	input, output := "usage.prompt_tokens", "usage.completion_tokens"
	if path == providerOpenAIResponsesPath {
		input, output = "usage.input_tokens", "usage.output_tokens"
	}
	p, ok1 := get(input)
	o, ok2 := get(output)
	c, ok3 := get(input + "_details.cached_tokens")
	t, ok4 := get(output + "_details.reasoning_tokens")
	if !ok1 || !ok2 || !ok3 || !ok4 || gjson.GetBytes(b, input).Type == gjson.Null || gjson.GetBytes(b, output).Type == gjson.Null || c > p || t > o || p+o == 0 {
		return nil, errGeminiRelayReceipt
	}
	choice := gjson.GetBytes(b, "choices.0")
	text := choice.Get("message.content")
	reason := choice.Get("finish_reason").String()
	if path == providerOpenAIResponsesPath {
		text = gjson.Result{Type: gjson.String, Str: extractOpenAIResponsesText(b)}
		switch gjson.GetBytes(b, "status").String() {
		case "completed":
			if text.Str != "" {
				reason = "stop"
			}
		case "incomplete":
			switch gjson.GetBytes(b, "incomplete_details.reason").String() {
			case "max_output_tokens":
				reason = "length"
			case "content_filter":
				reason = "content_filter"
			}
		}
	}
	switch reason {
	case "stop":
		reason = "STOP"
	case "length":
		reason = "MAX_TOKENS"
	case "content_filter":
		reason = "SAFETY"
	default:
		return nil, errGeminiRelayReceipt
	}
	if text.Type != gjson.String && reason != "SAFETY" {
		return nil, errGeminiRelayReceipt
	}
	data, _ := json.Marshal(map[string]any{"modelVersion": gjson.GetBytes(b, "model").String(), "candidates": []any{map[string]any{"index": 0, "finishReason": reason, "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": text.String()}}}}}, "usageMetadata": map[string]int{"promptTokenCount": p, "cachedContentTokenCount": c, "candidatesTokenCount": o - t, "thoughtsTokenCount": t, "totalTokenCount": p + o}})
	out := new(http.Response)
	*out = *resp
	out.Header = resp.Header.Clone()
	if out.Header == nil {
		out.Header = make(http.Header)
	}
	if !extractGeminiUsage(data).hasObservedTokens() {
		return nil, errGeminiRelayReceipt
	}
	out.Header.Del("Content-Length")
	out.Header.Set("Content-Type", "application/json")
	if stream {
		data = append(append([]byte("data: "), data...), '\n', '\n')
		out.Header.Set("Content-Type", "text/event-stream")
	}
	out.Body = geminiRelayBody{io.NopCloser(bytes.NewReader(data)), path}
	out.ContentLength = int64(len(data))
	return out, nil
}
