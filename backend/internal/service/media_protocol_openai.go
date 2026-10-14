package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON video relays use numeric duration and reference URL arrays, while
// native Sora uses seconds and a single input reference.
func normalizeOpenAIVideoJSON(body []byte) ([]byte, error) {
	if err := validateVideoBillingFields(body, "application/json"); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, fmt.Errorf("OpenAI video requires a JSON request object")
	}
	readString := func(key string) (string, error) {
		if _, exists := fields[key]; !exists {
			return "", nil
		}
		var value string
		if json.Unmarshal(fields[key], &value) != nil {
			return "", fmt.Errorf("invalid video %s", key)
		}
		return strings.TrimSpace(value), nil
	}
	set := func(key string, value any) { fields[key], _ = json.Marshal(value) }
	info := ParseGrokMediaRequest("application/json", body)
	if info.N > 1 {
		return nil, fmt.Errorf("video creation accepts one task per request")
	}
	duration := info.RawDurationSeconds
	if duration == 0 {
		duration = 5
		if isGrokOpenAIVideoModel(info.Model) {
			duration = 8
		}
	}
	set("duration", duration)
	delete(fields, "seconds")
	resolution, err := readString("resolution")
	if err != nil {
		return nil, err
	}
	alias, err := readString("resolution_name")
	if err != nil {
		return nil, err
	}
	if resolution == "" {
		resolution = alias
	}
	if resolution == "" || strings.EqualFold(resolution, "auto") {
		resolution = "720p"
	}
	normalized, valid := normalizeVideoBillingResolution(resolution)
	if !valid {
		return nil, fmt.Errorf("invalid video resolution")
	}
	if alias != "" && !strings.EqualFold(alias, "auto") {
		other, ok := normalizeVideoBillingResolution(alias)
		if !ok || other != normalized {
			return nil, fmt.Errorf("conflicting video resolutions")
		}
	}
	set("resolution", normalized)
	delete(fields, "resolution_name")
	ratio, err := readString("ratio")
	if err != nil {
		return nil, err
	}
	aspect, err := readString("aspect_ratio")
	if err != nil {
		return nil, err
	}
	if ratio != "" && aspect != "" && ratio != aspect {
		return nil, fmt.Errorf("conflicting video aspect ratios")
	}
	if ratio == "" {
		ratio = aspect
	}
	if ratio != "" && ratio != "auto" {
		if _, err := videoProtocolPixelSize(ratio, normalized); err != nil {
			return nil, err
		}
		set("aspect_ratio", ratio)
	}
	delete(fields, "ratio")
	if isGrokOpenAIVideoModel(info.Model) {
		if err := normalizeGrokOpenAIVideoReferences(fields); err != nil {
			return nil, err
		}
		return json.Marshal(fields)
	}
	referenceModes := 0
	for _, mode := range [][]string{
		{"input_reference"},
		{"first_frame_image_url", "last_frame_image_url"},
	} {
		present := false
		for _, key := range mode {
			if _, exists := fields[key]; !exists {
				continue
			}
			value, err := readString(key)
			if err != nil || value == "" {
				return nil, fmt.Errorf("invalid video %s", key)
			}
			set(key, value)
			present = true
		}
		if present {
			referenceModes++
		}
	}
	for _, ref := range []struct {
		field   string
		aliases []string
	}{
		{"reference_image_urls", []string{"reference_image_urls", "referenceImages", "reference_images", "image_refs"}},
		{"reference_video_urls", []string{"reference_video_urls", "referenceVideos", "reference_videos", "video_refs"}},
		{"reference_audio_urls", []string{"reference_audio_urls", "referenceAudios", "reference_audios", "audio_refs"}},
	} {
		var selected []string
		for _, key := range ref.aliases {
			raw, exists := fields[key]
			if !exists {
				continue
			}
			var urls []string
			if json.Unmarshal(raw, &urls) != nil {
				return nil, fmt.Errorf("invalid video %s", key)
			}
			if len(selected) > 0 && len(urls) > 0 {
				return nil, fmt.Errorf("conflicting video reference fields")
			}
			for _, value := range urls {
				if strings.TrimSpace(value) == "" {
					return nil, fmt.Errorf("invalid video reference URL")
				}
			}
			selected = append(selected, urls...)
			delete(fields, key)
		}
		if len(selected) > 0 {
			set(ref.field, selected)
			referenceModes++
		}
	}
	if referenceModes > 1 {
		return nil, fmt.Errorf("video creation accepts only one reference mode: single image, first/last frames, image URLs, video URLs or audio URLs")
	}
	return json.Marshal(fields)
}

func isGrokOpenAIVideoModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "grok-imagine-video" || strings.HasPrefix(model, "grok-imagine-video-")
}

// Grok's JSON endpoint uses images/mode; frames and voice IDs may accompany
// reference images, while an edit accepts a single reference_video_url.
func normalizeGrokOpenAIVideoReferences(fields map[string]json.RawMessage) error {
	set := func(key string, value any) { fields[key], _ = json.Marshal(value) }
	readString := func(key string) (string, error) {
		raw, exists := fields[key]
		if !exists {
			return "", nil
		}
		var value string
		if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("invalid video %s", key)
		}
		return strings.TrimSpace(value), nil
	}
	readArray := func(keys ...string) ([]string, error) {
		var selected []string
		for _, key := range keys {
			raw, exists := fields[key]
			if !exists {
				continue
			}
			var values []string
			if json.Unmarshal(raw, &values) != nil {
				return nil, fmt.Errorf("invalid video %s", key)
			}
			if len(selected) > 0 && len(values) > 0 {
				return nil, fmt.Errorf("conflicting video reference fields")
			}
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					return nil, fmt.Errorf("invalid video reference URL")
				}
				selected = append(selected, strings.TrimSpace(value))
			}
			delete(fields, key)
		}
		return selected, nil
	}
	images, err := readArray("images", "reference_image_urls", "referenceImages", "reference_images", "image_refs")
	if err != nil {
		return err
	}
	if raw, exists := fields["input_reference"]; exists {
		var reference string
		if json.Unmarshal(raw, &reference) != nil {
			var input struct {
				ImageURL string `json:"image_url"`
			}
			if json.Unmarshal(raw, &input) != nil {
				return fmt.Errorf("invalid video input_reference")
			}
			reference = input.ImageURL
		}
		if strings.TrimSpace(reference) == "" || len(images) > 0 {
			return fmt.Errorf("invalid or conflicting video input_reference")
		}
		images = []string{strings.TrimSpace(reference)}
		delete(fields, "input_reference")
	}
	videos, err := readArray("reference_video_urls", "referenceVideos", "reference_videos", "video_refs")
	if err != nil {
		return err
	}
	video, err := readString("reference_video_url")
	if err != nil {
		return err
	}
	if len(videos) > 1 || (video != "" && len(videos) > 0) {
		return fmt.Errorf("video edit supports one reference video")
	}
	if len(videos) == 1 {
		video = videos[0]
	}
	audios, err := readArray("reference_audio_urls", "referenceAudios", "reference_audios", "audio_refs")
	if err != nil {
		return err
	}
	if len(audios) > 0 {
		return fmt.Errorf("grok video accepts reference_voice_ids instead of reference audio URLs")
	}
	additions := false
	for _, key := range []string{"first_frame_image_url", "last_frame_image_url"} {
		value, err := readString(key)
		if err != nil {
			return err
		}
		if value != "" {
			set(key, value)
			additions = true
		}
	}
	for _, key := range []string{"keyframes", "reference_voice_ids"} {
		if raw, exists := fields[key]; exists {
			var values []json.RawMessage
			if json.Unmarshal(raw, &values) != nil {
				return fmt.Errorf("invalid video %s", key)
			}
			additions = additions || len(values) > 0
		}
	}
	mode, err := readString("mode")
	if err != nil {
		return err
	}
	if mode == "" {
		switch {
		case video != "":
			mode = "video-edit"
		case additions || len(images) > 1:
			mode = "reference-images"
		case len(images) == 1:
			mode = "image-to-video"
		}
	}
	switch mode {
	case "":
	case "image-to-video":
		if len(images) != 1 || video != "" || additions {
			return fmt.Errorf("image-to-video requires one reference image")
		}
	case "reference-images":
		if video != "" || (len(images) == 0 && !additions) {
			return fmt.Errorf("invalid video reference-images input")
		}
	case "video-edit":
		if video == "" || len(images) > 0 || additions {
			return fmt.Errorf("video-edit requires one reference video without image references")
		}
	default:
		return fmt.Errorf("unsupported video reference mode")
	}
	if mode != "" {
		set("mode", mode)
	}
	if len(images) > 0 {
		set("images", images)
	}
	if video != "" {
		set("reference_video_url", video)
	}
	return nil
}
