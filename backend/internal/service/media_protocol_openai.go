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
