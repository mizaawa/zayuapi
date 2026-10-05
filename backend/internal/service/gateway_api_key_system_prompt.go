package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type apiKeySystemPromptProtocol string

const (
	apiKeySystemPromptResponses apiKeySystemPromptProtocol = "responses"
	apiKeySystemPromptAnthropic apiKeySystemPromptProtocol = "anthropic"
	apiKeySystemPromptChat      apiKeySystemPromptProtocol = "chat"
)

// Apply at the wire boundary, after protocol conversion and OAuth mimicry.
// Raw JSON patches preserve provider extensions and integer tool arguments.
func applyAPIKeySystemPrompt(c *gin.Context, body []byte, protocol apiKeySystemPromptProtocol) ([]byte, error) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return body, nil
	}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	if !strings.HasSuffix(path, "/responses") && !strings.HasSuffix(path, "/responses/compact") && !strings.HasSuffix(path, "/messages") {
		return body, nil
	}
	key := getAPIKeyFromContext(c)
	if key == nil || !key.CustomSystemPromptEnabled || strings.TrimSpace(key.CustomSystemPrompt) == "" {
		return body, nil
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, fmt.Errorf("inject API key system prompt: request body must be a JSON object")
	}
	if key.CustomSystemPromptForce {
		return appendAPIKeySystemPrompt(body, protocol, key.CustomSystemPrompt)
	}
	sum := sha256.Sum256([]byte(key.CustomSystemPrompt))
	marker := "[sub2api-api-key-prompt:" + hex.EncodeToString(sum[:12]) + "]"
	if bytes.Contains(body, []byte(marker)) {
		return body, nil
	}
	prefix := marker + "\n[Custom System Instructions]\nThe owner of this API key supplied the following system instructions for this request. " +
		"Follow them while retaining the agent's existing tool protocols and workflow.\n\n" + key.CustomSystemPrompt + "\n\n"
	return prependAPIKeyPromptToUserInput(body, protocol, prefix)
}

func appendAPIKeySystemPrompt(body []byte, protocol apiKeySystemPromptProtocol, prompt string) ([]byte, error) {
	switch protocol {
	case apiKeySystemPromptResponses:
		value := gjson.GetBytes(body, "instructions")
		if value.Exists() && value.Type != gjson.String && value.Type != gjson.Null {
			return nil, fmt.Errorf("inject API key system prompt: instructions must be a string")
		}
		existing := value.String()
		if existing == prompt || strings.HasSuffix(existing, "\n\n"+prompt) {
			return body, nil
		}
		if existing != "" {
			prompt = existing + "\n\n" + prompt
		}
		return sjson.SetBytes(body, "instructions", prompt)
	case apiKeySystemPromptAnthropic:
		value := gjson.GetBytes(body, "system")
		if value.Type == gjson.String {
			existing := value.String()
			if existing == prompt || strings.HasSuffix(existing, "\n\n"+prompt) {
				return body, nil
			}
			if existing != "" {
				prompt = existing + "\n\n" + prompt
			}
			return sjson.SetBytes(body, "system", prompt)
		}
		blocks, err := apiKeyPromptRawArray(value)
		if err != nil {
			return nil, err
		}
		if len(blocks) > 0 && gjson.GetBytes(blocks[len(blocks)-1], "type").String() == "text" && gjson.GetBytes(blocks[len(blocks)-1], "text").String() == prompt {
			return body, nil
		}
		block, _ := json.Marshal(map[string]string{"type": "text", "text": prompt})
		return sjson.SetRawBytes(body, "system", buildJSONArrayRaw(append(blocks, block)))
	case apiKeySystemPromptChat:
		messages, err := apiKeyPromptRawArray(gjson.GetBytes(body, "messages"))
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if gjson.GetBytes(message, "role").String() == "system" && gjson.GetBytes(message, "content").Type == gjson.String && gjson.GetBytes(message, "content").String() == prompt {
				return body, nil
			}
		}
		message, _ := json.Marshal(map[string]string{"role": "system", "content": prompt})
		// Keep all system guidance ahead of the conversation without replacing it.
		at := 0
		for at < len(messages) {
			role := gjson.GetBytes(messages[at], "role").String()
			if role != "system" && role != "developer" {
				break
			}
			at++
		}
		messages = append(messages, nil)
		copy(messages[at+1:], messages[at:])
		messages[at] = message
		return sjson.SetRawBytes(body, "messages", buildJSONArrayRaw(messages))
	default:
		return body, nil
	}
}

func prependAPIKeyPromptToUserInput(body []byte, protocol apiKeySystemPromptProtocol, prefix string) ([]byte, error) {
	field, textType := "messages", "text"
	if protocol == apiKeySystemPromptResponses {
		field, textType = "input", "input_text"
	}
	value := gjson.GetBytes(body, field)
	if protocol == apiKeySystemPromptResponses && value.Type == gjson.String {
		return sjson.SetBytes(body, field, prefix+value.String())
	}
	var messages [][]byte
	var err error
	if protocol == apiKeySystemPromptResponses && value.IsObject() {
		messages = [][]byte{[]byte(value.Raw)}
	} else {
		messages, err = apiKeyPromptRawArray(value)
		if err != nil {
			return nil, err
		}
	}
	for i, message := range messages {
		if gjson.GetBytes(message, "role").String() != "user" {
			continue
		}
		content := gjson.GetBytes(message, "content")
		if content.Type == gjson.String {
			messages[i], err = sjson.SetBytes(message, "content", prefix+content.String())
		} else {
			blocks, arrayErr := apiKeyPromptRawArray(content)
			if arrayErr != nil {
				return nil, arrayErr
			}
			block, _ := json.Marshal(map[string]string{"type": textType, "text": prefix})
			at := 0
			// Anthropic requires tool_result blocks before other user content.
			if protocol == apiKeySystemPromptAnthropic {
				for at < len(blocks) && gjson.GetBytes(blocks[at], "type").String() == "tool_result" {
					at++
				}
			}
			blocks = append(blocks, nil)
			copy(blocks[at+1:], blocks[at:])
			blocks[at] = block
			messages[i], err = sjson.SetRawBytes(message, "content", buildJSONArrayRaw(blocks))
		}
		if err != nil {
			return nil, err
		}
		return sjson.SetRawBytes(body, field, buildJSONArrayRaw(messages))
	}
	// Tool-only continuations keep their call/output order. Add guidance after
	// the outputs, never inside output payloads or between calls and outputs.
	message := map[string]string{"role": "user", "content": prefix}
	if protocol == apiKeySystemPromptResponses {
		message["type"] = "message"
	}
	raw, _ := json.Marshal(message)
	return sjson.SetRawBytes(body, field, buildJSONArrayRaw(append(messages, raw)))
}

func apiKeyPromptRawArray(value gjson.Result) ([][]byte, error) {
	if !value.Exists() || value.Type == gjson.Null {
		return nil, nil
	}
	if !value.IsArray() {
		return nil, fmt.Errorf("inject API key system prompt: expected a JSON array")
	}
	items := value.Array()
	raw := make([][]byte, len(items))
	for i, item := range items {
		raw[i] = []byte(item.Raw)
	}
	return raw, nil
}
