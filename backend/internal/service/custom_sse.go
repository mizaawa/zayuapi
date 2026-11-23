package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/tidwall/gjson"
)

// Classify the protocol envelope, never URLs or strings inside model output.
func upstreamErrorPayload(payload []byte, event string) bool {
	if event == "error" || event == "response.failed" {
		return true
	}
	switch gjson.GetBytes(payload, "type").String() {
	case "error", "response.failed":
		return true
	}
	return gjson.GetBytes(payload, "error").Type != gjson.Null || gjson.GetBytes(payload, "response.error").Type != gjson.Null
}

// Keep successful SSE frames byte-for-byte. Classify complete events before
// writing any part: an error (or its usage) can span several physical lines.
func relayCustomSSE(src io.Reader, cfg *config.Config, account *Account, observe, emit func([]byte)) error {
	limit := defaultMaxLineSize
	if cfg != nil && cfg.Gateway.MaxLineSize > 0 {
		limit = cfg.Gateway.MaxLineSize
	}
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, min(4096, limit)), limit)
	scanner.Split(scanSSELinesPreservingEndings)
	var raw bytes.Buffer
	var parser openAICompatSSEFrameParser
	flush := func() {
		frame, ok := parser.Finish()
		if !ok || frame.Data == "[DONE]" {
			emit(raw.Bytes())
			raw.Reset()
			return
		}
		payload := []byte(frame.Data)
		observe(payload)
		if upstreamErrorPayload(payload, frame.EventType) {
			// Never echo diagnostic comments/ids or raw continuation lines.
			raw.Reset()
			if frame.EventType == "error" || frame.EventType == "response.failed" {
				_, _ = raw.WriteString("event: " + frame.EventType + "\n")
			}
			_, _ = raw.WriteString("data: ")
			_ = json.Compact(&raw, sanitizeUpstreamClientErrorBody(payload, account))
			_, _ = raw.WriteString("\n\n")
		}
		emit(raw.Bytes())
		raw.Reset()
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) > limit-raw.Len() {
			return errors.New("upstream SSE frame too large")
		}
		_, _ = raw.Write(line)
		if content := trimSSELineEnding(line); len(content) > 0 {
			parser.AddLine(string(content))
		} else {
			flush()
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if raw.Len() > 0 {
		flush()
	}
	return nil
}
