package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

type VideoWorkbenchPayload struct {
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt"`
	Ratio           string   `json:"ratio"`
	Resolution      string   `json:"resolution,omitempty"`
	Duration        int      `json:"duration"`
	ReferenceImages []string `json:"referenceImages,omitempty"`
	ReferenceVideos []string `json:"referenceVideos,omitempty"`
	ReferenceAudios []string `json:"referenceAudios,omitempty"`
}

func miniMaxVideoWorkbenchFamily(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case model == "minimax-h3" || strings.HasPrefix(model, "minimax-h3-"):
		return "h3"
	case model == "minimax-hailuo-2.3", model == "minimax-hailuo-02":
		return "hailuo"
	case model == "minimax-hailuo-2.3-fast":
		return "hailuo-fast"
	default:
		return ""
	}
}

func (p *VideoWorkbenchPayload) ValidateModel() error {
	family := miniMaxVideoWorkbenchFamily(p.Model)
	if family == "" {
		return nil
	}
	if family == "h3" {
		if p.Duration < 4 || p.Duration > 15 {
			return fmt.Errorf("MiniMax H3 duration must be between 4 and 15 seconds")
		}
		switch p.Ratio {
		case "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
		default:
			return fmt.Errorf("MiniMax H3 aspect ratio must be 21:9, 16:9, 4:3, 1:1, 3:4 or 9:16")
		}
		if len(p.ReferenceImages) > 9 || len(p.ReferenceVideos) > 3 || len(p.ReferenceAudios) > 3 {
			return fmt.Errorf("MiniMax H3 accepts at most 9 images, 3 videos and 3 audios")
		}
		return nil
	}
	if p.Duration != 6 && p.Duration != 10 {
		return fmt.Errorf("MiniMax Hailuo duration must be 6 or 10 seconds")
	}
	if len(p.ReferenceVideos) > 0 || len(p.ReferenceAudios) > 0 {
		return fmt.Errorf("MiniMax Hailuo does not support reference videos or audios")
	}
	if len(p.ReferenceImages) > 2 {
		return fmt.Errorf("MiniMax Hailuo accepts at most 2 frame images")
	}
	if family == "hailuo-fast" && len(p.ReferenceImages) == 0 {
		return fmt.Errorf("MiniMax Hailuo Fast requires a reference image")
	}
	return nil
}

func normalizeMiniMaxVideoWorkbenchBody(ctx context.Context, body []byte) ([]byte, error) {
	if ctx == nil || ctx.Value(videoWorkbenchContextKey{}) != true {
		return body, nil
	}
	family := miniMaxVideoWorkbenchFamily(gjson.GetBytes(body, "model").String())
	if family == "" {
		return body, nil
	}
	var p VideoWorkbenchPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("invalid video workbench request")
	}
	if err := p.ValidateModel(); err != nil {
		return nil, err
	}
	metadata := map[string]any{"ratio": p.Ratio}
	if family == "h3" {
		content := []map[string]any{{"type": "text", "text": p.Prompt}}
		for _, media := range []struct {
			values []string
			kind   string
		}{
			{p.ReferenceImages, "image"}, {p.ReferenceVideos, "video"}, {p.ReferenceAudios, "audio"},
		} {
			for _, ref := range media.values {
				content = append(content, map[string]any{
					"type": media.kind + "_url", "role": "reference_" + media.kind,
					media.kind + "_url": map[string]string{"url": ref},
				})
			}
		}
		metadata["content"] = content
	} else {
		if len(p.ReferenceImages) > 0 {
			metadata["first_frame_image"] = p.ReferenceImages[0]
		}
		if len(p.ReferenceImages) > 1 {
			metadata["last_frame_image"] = p.ReferenceImages[1]
		}
	}
	out := map[string]any{
		"model": p.Model, "prompt": p.Prompt, "duration": p.Duration,
		"ratio": p.Ratio, "metadata": metadata,
	}
	if p.Resolution != "" && p.Resolution != "auto" {
		out["resolution"] = p.Resolution
	}
	return json.Marshal(out)
}
