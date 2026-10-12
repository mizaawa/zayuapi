package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MediaProtocol is explicit because retrying a creation on another endpoint can
// create a second paid task after the first provider has already accepted it.
func (a *Account) MediaProtocol() string {
	if a == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(a.GetCredential("media_protocol")))
}

func validMediaProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "sora", "8yes":
		return true
	default:
		return false
	}
}

func VideoPrechargeRequestInfo(ctx context.Context, account *Account, body []byte, contentType string) (GrokMediaRequestInfo, error) {
	if !validMediaProtocol(account.MediaProtocol()) {
		return GrokMediaRequestInfo{}, fmt.Errorf("unsupported media protocol; select default, sora or 8yes in account settings")
	}
	if account.MediaProtocol() == "8yes" {
		adapted, adaptedType, err := normalizeEightYesVideoRequest(body, contentType)
		if err != nil {
			return GrokMediaRequestInfo{}, err
		}
		return normalizeEightYesVideoRequestInfo(ParseGrokMediaRequest(adaptedType, adapted)), nil
	}
	adapted, err := normalizeVideoProtocolBody(ctx, account, body)
	if err != nil {
		return GrokMediaRequestInfo{}, err
	}
	if protocol := account.MediaProtocol(); account != nil && account.Platform == PlatformCustom && protocol != "sora" {
		adapted, contentType, err = normalizeCustomVideoForwardBody(GrokMediaEndpointVideosGenerations, adapted, contentType)
		if err != nil {
			return GrokMediaRequestInfo{}, err
		}
	}
	info := ParseGrokMediaRequest(contentType, adapted)
	if account.MediaProtocol() == "sora" && info.RawDurationSeconds == 0 && strings.HasPrefix(strings.ToLower(info.Model), "sora-") {
		info.RawDurationSeconds, info.DurationSeconds = 4, 4
	}
	return info, nil
}

func normalizeVideoProtocolBody(ctx context.Context, account *Account, body []byte) ([]byte, error) {
	protocol := account.MediaProtocol()
	if protocol == "8yes" {
		return normalizeEightYesVideoJSON(body)
	}
	if protocol != "sora" {
		return normalizeMiniMaxVideoWorkbenchBody(ctx, body)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(body, &values); err != nil {
		// Native Sora multipart bodies remain byte-for-byte intact.
		return body, nil
	}
	readString := func(keys ...string) string {
		for _, key := range keys {
			var v string
			if json.Unmarshal(values[key], &v) == nil && v != "" {
				return v
			}
		}
		return ""
	}
	set := func(key string, value any) { values[key], _ = json.Marshal(value) }
	ratio := readString("ratio", "aspect_ratio")
	if ratio == "" {
		parts := strings.Split(readString("size"), "x")
		if len(parts) == 2 {
			w, e1 := strconv.Atoi(parts[0])
			h, e2 := strconv.Atoi(parts[1])
			if e1 == nil && e2 == nil && w > 0 && h > 0 {
				ratio = fmt.Sprintf("%d:%d", w, h)
			}
		}
	}
	if ratio == "" {
		ratio = "1:1"
	}
	for _, field := range []string{"resolution", "resolution_name"} {
		var value string
		if raw := values[field]; len(raw) > 0 && json.Unmarshal(raw, &value) != nil {
			return nil, fmt.Errorf("invalid video resolution")
		}
	}
	resolution := strings.ToLower(strings.TrimSpace(readString("resolution", "resolution_name")))
	delete(values, "resolution_name")
	if resolution == "auto" {
		resolution = "720p"
	}
	if resolution != "" {
		var valid bool
		resolution, valid = normalizeVideoBillingResolution(resolution)
		if !valid {
			return nil, fmt.Errorf("invalid video resolution")
		}
		set("resolution", resolution)
	}
	readSeconds := func(key string) (int, error) {
		raw := values[key]
		if len(raw) == 0 {
			return 0, nil
		}
		value := string(raw)
		if raw[0] == '"' {
			value = readString(key)
		}
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 1 || seconds > 86400 {
			return 0, fmt.Errorf("video duration must be a positive integer no greater than 86400")
		}
		return seconds, nil
	}
	duration, err := readSeconds("duration")
	if err != nil {
		return nil, err
	}
	seconds, err := readSeconds("seconds")
	if err != nil {
		return nil, err
	}
	if duration > 0 && seconds > 0 && duration != seconds {
		return nil, fmt.Errorf("conflicting video duration and seconds")
	}
	if duration == 0 {
		duration = seconds
	}
	if duration == 0 {
		duration = VideoBillingDefaultDurationSeconds
		if protocol == "sora" && strings.HasPrefix(strings.ToLower(readString("model")), "sora-") {
			duration = 4
		}
	}
	set("seconds", strconv.Itoa(duration))
	delete(values, "duration")
	var images []string
	for _, field := range []string{"referenceImages", "reference_images", "image_refs"} {
		if raw := values[field]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &images); err != nil {
				return nil, fmt.Errorf("invalid Sora reference images")
			}
			delete(values, field)
		}
	}
	if len(images) > 1 {
		return nil, fmt.Errorf("sora supports one input reference image")
	}
	if len(images) == 1 {
		set("input_reference", map[string]string{"image_url": images[0]})
	}
	for _, field := range []string{"referenceVideos", "reference_videos", "video_refs", "referenceAudios", "reference_audios", "audio_refs"} {
		if raw := values[field]; len(raw) > 0 && string(raw) != "[]" && string(raw) != "null" {
			return nil, fmt.Errorf("sora creation does not support reference video or audio")
		}
		delete(values, field)
	}
	if readString("size") == "" && resolution != "" {
		size, err := videoProtocolPixelSize(ratio, resolution)
		if err != nil {
			return nil, err
		}
		set("size", size)
	}
	// Relay models (including Grok) accept these extensions even when size
	// is automatic. Native Sora uses only its documented pixel size.
	if strings.HasPrefix(strings.ToLower(readString("model")), "sora-") {
		if readString("size") == "" {
			size, err := videoProtocolPixelSize(ratio, "720p")
			if err != nil {
				return nil, err
			}
			set("size", size)
		}
		switch readString("size") {
		case "720x1280", "1280x720", "1024x1792", "1792x1024", "1080x1920", "1920x1080":
		default:
			return nil, fmt.Errorf("sora requires a supported landscape or portrait size")
		}
		delete(values, "resolution")
		delete(values, "aspect_ratio")
	} else {
		set("aspect_ratio", ratio)
	}
	delete(values, "ratio")
	return json.Marshal(values)
}

func videoProtocolPixelSize(ratio, resolution string) (string, error) {
	parts := strings.Split(ratio, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid aspect ratio")
	}
	w, e1 := strconv.Atoi(parts[0])
	h, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || w < 1 || h < 1 || w > 10000 || h > 10000 {
		return "", fmt.Errorf("invalid aspect ratio")
	}
	short := map[string]int{"480p": 480, "720p": 720, "1080p": 1080, "2k": 1440, "4k": 2160}[resolution]
	if short == 0 {
		return "", fmt.Errorf("invalid resolution")
	}
	width, height := short, short
	if w > h {
		width = int(math.Round(float64(short*w)/float64(h)/2)) * 2
	} else {
		height = int(math.Round(float64(short*h)/float64(w)/2)) * 2
	}
	if width > 16384 || height > 16384 {
		return "", fmt.Errorf("video dimensions exceed provider limits")
	}
	return fmt.Sprintf("%dx%d", width, height), nil
}
