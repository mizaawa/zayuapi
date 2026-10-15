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
//
// "openai" was retired: it described the same video contract as the default
// passthrough protocol, so the account picker no longer offers it. Accounts
// that still store it fall back to the default protocol instead of failing
// validation on their next edit.
func (a *Account) MediaProtocol() string {
	if a == nil {
		return ""
	}
	protocol := strings.ToLower(strings.TrimSpace(a.GetCredential("media_protocol")))
	if protocol == legacyOpenAIMediaProtocol {
		return ""
	}
	return protocol
}

const legacyOpenAIMediaProtocol = "openai"

func validMediaProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "sora", "8yes":
		return true
	default:
		// Stored legacy values still read back as the default protocol.
		return strings.EqualFold(strings.TrimSpace(protocol), legacyOpenAIMediaProtocol)
	}
}

func mediaProtocolValidationMessage() string {
	return "unsupported media protocol; select default, sora or 8yes in account settings"
}

func VideoPrechargeRequestInfo(ctx context.Context, account *Account, body []byte, contentType string) (GrokMediaRequestInfo, error) {
	if err := validateVideoBillingFields(body, contentType); err != nil {
		return GrokMediaRequestInfo{}, err
	}
	if !validMediaProtocol(account.MediaProtocol()) {
		return GrokMediaRequestInfo{}, fmt.Errorf("%s", mediaProtocolValidationMessage())
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
	if account.Platform == PlatformCustom && account.MediaProtocol() != "sora" {
		adapted, contentType, err = normalizeCustomVideoForwardBody(GrokMediaEndpointVideosGenerations, adapted, contentType)
		if err != nil {
			return GrokMediaRequestInfo{}, err
		}
	}
	info := ParseGrokMediaRequest(contentType, adapted)
	if account.MediaProtocol() == "sora" {
		info = normalizeSoraVideoRequestInfo(info)
	}
	return info, nil
}

func normalizeSoraVideoRequestInfo(info GrokMediaRequestInfo) GrokMediaRequestInfo {
	if info.RawDurationSeconds == 0 && strings.HasPrefix(strings.ToLower(info.Model), "sora-") {
		info.RawDurationSeconds, info.DurationSeconds = 4, 4
	}
	return info
}

// Each remaining protocol rewrites the request into the shape its provider
// documents. The default protocol stays a transparent passthrough, so a relay
// keeps receiving exactly the fields its documentation describes.
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
	resolution := strings.ToLower(strings.TrimSpace(readString("resolution")))
	resolutionName := strings.ToLower(strings.TrimSpace(readString("resolution_name")))
	autoResolution := resolution == "auto" || resolutionName == "auto"
	delete(values, "resolution_name")
	for _, candidate := range []string{resolution, resolutionName} {
		if candidate == "" || candidate == "auto" {
			continue
		}
		if _, valid := normalizeVideoBillingResolution(candidate); !valid {
			return nil, fmt.Errorf("invalid video resolution")
		}
	}
	if resolution != "" && resolution != "auto" && resolutionName != "" && resolutionName != "auto" {
		primary, _ := normalizeVideoBillingResolution(resolution)
		alias, _ := normalizeVideoBillingResolution(resolutionName)
		if primary != alias {
			return nil, fmt.Errorf("conflicting video resolutions")
		}
	}
	if resolution == "" || resolution == "auto" {
		resolution = resolutionName
	}
	if resolution == "auto" || (resolution == "" && autoResolution) {
		resolution = "720p"
	}
	if resolution != "" {
		resolution, _ = normalizeVideoBillingResolution(resolution)
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
			var refs []string
			if err := json.Unmarshal(raw, &refs); err != nil {
				return nil, fmt.Errorf("invalid Sora reference images")
			}
			if len(images) > 0 && len(refs) > 0 {
				return nil, fmt.Errorf("conflicting Sora reference image fields")
			}
			images = append(images, refs...)
			delete(values, field)
		}
	}
	if len(images) > 1 {
		return nil, fmt.Errorf("sora supports one input reference image")
	}
	if len(images) == 1 {
		if _, exists := values["input_reference"]; exists {
			return nil, fmt.Errorf("conflicting Sora reference image fields")
		}
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
