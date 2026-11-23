package service

import (
	"bytes"
	"errors"
	"io"
	"time"

	"github.com/tidwall/gjson"
)

// Reuse the protocol decoders used by degradation probes for answer validation.
// Time actual streamed output separately: reasoning counts as a generated token,
// but a role, heartbeat, usage report or final buffered answer is not a delta.
func readMonitorStream(body *io.LimitedReader, reply monitorReply, provider, apiMode string, start time.Time) (monitorReply, error) {
	protocol := DegradationProtocolOpenAIChat
	switch {
	case provider == MonitorProviderGemini:
		protocol = DegradationProtocolGemini
	case provider == MonitorProviderAnthropic:
		protocol = DegradationProtocolAnthropic
	case apiMode == MonitorAPIModeResponses:
		protocol = DegradationProtocolOpenAIResponses
	}
	state := newDegradationStreamState(start)
	err := readServerSentEvents(body, func(event string, data []byte) error {
		if !bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) && !gjson.ValidBytes(data) {
			return errors.New("malformed monitor stream event")
		}
		if reply.firstTokenMs == nil && monitorEventHasToken(protocol, event, data) {
			ms := int(time.Since(start).Milliseconds())
			reply.firstTokenMs = &ms
		}
		if err := state.handle(protocol, event, data); err != nil {
			return err
		}
		if state.failure != "" {
			return errors.New(state.failure)
		}
		return nil
	})
	if err != nil {
		return reply, err
	}
	if body.N == 0 {
		return reply, errors.New("monitor response exceeds size limit")
	}
	if !state.completed {
		return reply, errors.New("monitor stream ended before completion")
	}
	reply.text = state.output.String()
	return reply, nil
}

func monitorEventHasToken(protocol, event string, data []byte) bool {
	root := gjson.ParseBytes(data)
	if kind := root.Get("type").String(); kind != "" {
		event = kind
	}
	switch protocol {
	case DegradationProtocolOpenAIResponses:
		switch event {
		case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta", "response.refusal.delta":
			return root.Get("delta").String() != ""
		}
	case DegradationProtocolAnthropic:
		if event == "content_block_start" {
			return root.Get("content_block.text").String() != "" || root.Get("content_block.thinking").String() != ""
		}
		if event == "content_block_delta" {
			return root.Get("delta.text").String() != "" || root.Get("delta.thinking").String() != "" || root.Get("delta.partial_json").String() != ""
		}
	case DegradationProtocolGemini:
		for _, part := range root.Get("candidates.0.content.parts").Array() {
			if part.Get("text").String() != "" || part.Get("functionCall.name").String() != "" {
				return true
			}
		}
	default:
		delta := root.Get("choices.0.delta")
		return delta.Get("content").String() != "" || delta.Get("reasoning_content").String() != "" || delta.Get("reasoning").String() != "" || delta.Get("tool_calls.0.function.arguments").String() != "" || delta.Get("tool_calls.0.function.name").String() != ""
	}
	return false
}
