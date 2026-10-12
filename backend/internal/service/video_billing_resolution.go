package service

import "strings"

const (
	VideoBillingResolution480P  = "480p"
	VideoBillingResolution720P  = "720p"
	VideoBillingResolution1080P = "1080p"
	VideoBillingResolution2K    = "2k"
	VideoBillingResolution4K    = "4k"
)

// xAI 视频生成按秒计费，duration 请求参数允许 1-15 秒；未指定时上游默认生成 8 秒。
// 计费时长必须与上游实际消耗对齐，否则用户可通过拉长 duration 套利（提交时长由用户控制）。
const (
	VideoBillingMinDurationSeconds         = 1
	VideoBillingMaxDurationSeconds         = 15
	VideoBillingExtendedMaxDurationSeconds = 30
	VideoBillingDefaultDurationSeconds     = 8
)

// NormalizeVideoBillingDurationSecondsOrDefault 归一化计费用视频时长：
// 未指定（<=0）按上游默认 8 秒计，超出上游允许区间按边界收敛。
func NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds int) int {
	return normalizeVideoBillingDurationSeconds(durationSeconds, VideoBillingMaxDurationSeconds)
}

// Custom providers accept their own duration ranges. Never apply xAI's short
// clip ceiling to a successfully created Custom task or its billing record.
func NormalizeCustomVideoDurationSeconds(durationSeconds int) int {
	if durationSeconds <= 0 {
		return VideoBillingDefaultDurationSeconds
	}
	return durationSeconds
}

func NormalizeVideoBillingDurationSecondsForModelOrDefault(model string, durationSeconds int) int {
	maxDuration := VideoBillingMaxDurationSeconds
	if strings.HasPrefix(videoBillingModelBasename(model), "wan-3.0") {
		maxDuration = VideoBillingExtendedMaxDurationSeconds
	}
	return normalizeVideoBillingDurationSeconds(durationSeconds, maxDuration)
}

func normalizeVideoBillingDurationSeconds(durationSeconds, maxDuration int) int {
	if durationSeconds <= 0 {
		return VideoBillingDefaultDurationSeconds
	}
	if durationSeconds < VideoBillingMinDurationSeconds {
		return VideoBillingMinDurationSeconds
	}
	if durationSeconds > maxDuration {
		return maxDuration
	}
	return durationSeconds
}

func NormalizeVideoBillingResolutionOrDefault(resolution string) string {
	if normalized, ok := normalizeVideoBillingResolution(resolution); ok {
		return normalized
	}
	return VideoBillingResolution480P
}

func NormalizeVideoBillingResolutionForModelOrDefault(model, resolution, size string) string {
	if normalized, ok := normalizeVideoBillingResolution(resolution); ok {
		return normalized
	}
	modelName := videoBillingModelBasename(model)
	for _, candidate := range []struct {
		suffix     string
		resolution string
	}{
		{suffix: "-1080p", resolution: VideoBillingResolution1080P},
		{suffix: "-720p", resolution: VideoBillingResolution720P},
		{suffix: "-480p", resolution: VideoBillingResolution480P},
		{suffix: "-4k", resolution: VideoBillingResolution4K},
		{suffix: "-2k", resolution: VideoBillingResolution2K},
	} {
		if strings.HasSuffix(modelName, candidate.suffix) {
			return candidate.resolution
		}
	}
	if normalized, ok := normalizeVideoBillingResolution(size); ok {
		return normalized
	}
	return VideoBillingResolution480P
}

func normalizeVideoBillingResolution(resolution string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "480", "480p", "sd", "854x480", "480x854", "480x480":
		return VideoBillingResolution480P, true
	case "720", "720p", "hd", "1280x720", "720x1280", "720x720":
		return VideoBillingResolution720P, true
	case "1080", "1080p", "full_hd", "full-hd", "fhd", "1920x1080", "1080x1920", "1080x1080", "1024x1792", "1792x1024":
		return VideoBillingResolution1080P, true
	case "2k", "1440p", "2560x1440", "1440x2560", "1440x1440", "2048x2048":
		return VideoBillingResolution2K, true
	case "4k", "2160p", "uhd", "3840x2160", "2160x3840", "2160x2160", "4096x4096":
		return VideoBillingResolution4K, true
	default:
		return "", false
	}
}

func VideoReferencePriceMultiplier(model string, hasReferenceVideo bool) float64 {
	if !hasReferenceVideo {
		return 1
	}
	switch modelName := videoBillingModelBasename(model); {
	case strings.HasPrefix(modelName, "seedance-2-5-"):
		return 1.4
	case strings.HasPrefix(modelName, "videos-"):
		return 1.3
	default:
		return 1
	}
}

func videoBillingModelBasename(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = model[slash+1:]
	}
	return model
}
