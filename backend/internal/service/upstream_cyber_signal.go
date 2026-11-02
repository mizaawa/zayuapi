package service

import (
	"bytes"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// anthropicPayloadSignalsCyber reports whether an Anthropic Messages payload
// carries the protocol's own cyber block: Claude refusing the request under its
// cyber safeguards. A refusal is an HTTP 200 whose stop_details.category is
// "cyber" (top level when non-streaming, inside message_delta.delta when
// streaming). With server-side fallback the refusal shows up instead as a
// "fallback" content block whose trigger.category is "cyber". An OpenAI
// cyber_policy error code relayed inside an Anthropic error event counts too.
// Free text never counts: model output that merely mentions cybersecurity sits
// in text deltas, which none of these paths read.
func anthropicPayloadSignalsCyber(payload []byte) bool {
	if len(payload) == 0 || !bytes.Contains(payload, []byte("cyber")) || !gjson.ValidBytes(payload) {
		return false
	}
	for _, path := range []string{
		"stop_details.category",
		"delta.stop_details.category",
		"message.stop_details.category",
	} {
		if isCyberRefusalCategory(gjson.GetBytes(payload, path)) {
			return true
		}
	}
	if isCyberFallbackBlock(gjson.GetBytes(payload, "content_block")) {
		return true
	}
	cyberFallback := false
	gjson.GetBytes(payload, "content").ForEach(func(_, block gjson.Result) bool {
		cyberFallback = isCyberFallbackBlock(block)
		return !cyberFallback
	})
	if cyberFallback {
		return true
	}
	for _, path := range []string{"error.code", "error.type"} {
		if code := gjson.GetBytes(payload, path); code.Type == gjson.String && strings.EqualFold(strings.TrimSpace(code.String()), "cyber_policy") {
			return true
		}
	}
	return false
}

func isCyberRefusalCategory(category gjson.Result) bool {
	return category.Type == gjson.String && strings.EqualFold(strings.TrimSpace(category.String()), "cyber")
}

func isCyberFallbackBlock(block gjson.Result) bool {
	return block.IsObject() && block.Get("type").String() == "fallback" && isCyberRefusalCategory(block.Get("trigger.category"))
}

// observeAnthropicCyberSignal records a cyber block on the current attempt's
// observer for paths that convert the Anthropic stream instead of observing it.
func observeAnthropicCyberSignal(c *gin.Context, payload string) {
	upstreamResponseModelObserverFromContext(c).ObserveAnthropicCyber([]byte(payload))
}

// UpstreamCyberSignalObserved reports whether the current forwarding attempt's
// upstream response carried a protocol-level cyber block that is not an OpenAI
// cyber_policy error (the OpenAI gateway tracks those with MarkOpsCyberPolicy).
// Handlers pass it to usage recording so the group's Cyber auto charge applies.
func UpstreamCyberSignalObserved(c *gin.Context) bool {
	return upstreamResponseModelObserverFromContext(c).CyberSignal()
}
