package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/soravideo"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// The separate inbound namespace opts into Sora's wire contract. Native
// /videos requests remain governed by the selected account's media protocol.
type soraVideoBridgeContextKey struct{}

func WithSoraVideoBridge(ctx context.Context) context.Context {
	return context.WithValue(ctx, soraVideoBridgeContextKey{}, true)
}

func IsSoraVideoBridge(ctx context.Context) bool {
	return ctx != nil && ctx.Value(soraVideoBridgeContextKey{}) == true
}

type soraBridgeVideoRequest struct {
	Model, Prompt, Size, Resolution, Ratio, Image string
	Seconds                                       int
}

// SoraVideoBridgeValidationError distinguishes local parameter rejection from
// billing admission failures without changing the native protocol's errors.
type SoraVideoBridgeValidationError struct{ Err error }

func (e *SoraVideoBridgeValidationError) Error() string { return e.Err.Error() }
func (e *SoraVideoBridgeValidationError) Unwrap() error { return e.Err }

func ParseSoraVideoBridgeRequestInfo(body []byte, contentType string) (GrokMediaRequestInfo, error) {
	input, err := parseSoraBridgeVideoRequest(body, contentType)
	if err != nil {
		return GrokMediaRequestInfo{}, err
	}
	info := GrokMediaRequestInfo{Model: input.Model, Prompt: input.Prompt, N: 1,
		Size: input.Size, Resolution: input.Resolution, GrokResolution: input.Resolution,
		ResolutionValid: true, DurationSeconds: input.Seconds, RawDurationSeconds: input.Seconds}
	if input.Image != "" {
		info.InputImageURLs = []string{input.Image}
	}
	return info, nil
}

func parseSoraBridgeVideoRequest(body []byte, contentType string) (soraBridgeVideoRequest, error) {
	parsed, err := soravideo.Parse(body, contentType, VideoWorkbenchMaxRequestBytes, ValidVideoReferenceURL)
	if err != nil {
		return soraBridgeVideoRequest{}, err
	}
	resolution, ratio := soraBridgeSize(parsed.Size)
	if resolution == "" {
		return soraBridgeVideoRequest{}, fmt.Errorf("unsupported Sora size; use a supported width x height pixel size")
	}
	return soraBridgeVideoRequest{Model: parsed.Model, Prompt: parsed.Prompt, Size: parsed.Size,
		Image: parsed.Image, Seconds: parsed.Seconds, Resolution: resolution, Ratio: ratio}, nil
}
func soraBridgeSize(size string) (resolution, ratio string) {
	resolution, ok := normalizeVideoBillingResolution(size)
	if !ok || !strings.Contains(size, "x") {
		return "", ""
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", ""
	}
	w, ew := strconv.Atoi(parts[0])
	h, eh := strconv.Atoi(parts[1])
	if ew != nil || eh != nil || w <= 0 || h <= 0 {
		return "", ""
	}
	if w == h {
		return resolution, "1:1"
	}
	if w == 1024 && h == 1792 {
		return resolution, "4:7"
	}
	if w == 1792 && h == 1024 {
		return resolution, "7:4"
	}
	if w > h {
		return resolution, "16:9"
	}
	return resolution, "9:16"
}

// Convert only after model-based account selection. The exact public model is
// retained, while 8yes derives its billed resolution from its model suffix.
func normalizeSoraBridgeVideoRequest(account *Account, body []byte, contentType string) ([]byte, string, error) {
	input, err := parseSoraBridgeVideoRequest(body, contentType)
	if err != nil {
		return nil, "", err
	}
	if account.MediaProtocol() == "sora" {
		return body, contentType, nil
	}
	if account.MediaProtocol() == "8yes" {
		modelResolution := ""
		for _, tier := range []string{"480p", "720p", "1080p", "2k", "4k"} {
			if strings.HasSuffix(videoBillingModelBasename(input.Model), "-"+tier) {
				modelResolution = tier
				break
			}
		}
		if modelResolution != input.Resolution {
			return nil, "", fmt.Errorf("8yes model resolution must match Sora size")
		}
	}
	fields := map[string]any{
		"model": input.Model, "prompt": input.Prompt, "duration": input.Seconds,
		"ratio": input.Ratio, "resolution": input.Resolution,
	}
	if input.Image != "" {
		fields["referenceImages"] = []string{input.Image}
	}
	if account.MediaProtocol() == "" {
		payload := VideoWorkbenchPayload{Model: input.Model, Prompt: input.Prompt, Duration: input.Seconds, Ratio: input.Ratio, Resolution: input.Resolution}
		if input.Image != "" {
			payload.ReferenceImages = []string{input.Image}
		}
		if err := payload.ValidateModel(); err != nil {
			return nil, "", err
		}
	}
	converted, err := json.Marshal(fields)
	return converted, "application/json", err
}

func normalizeSoraBridgeVideoResponse(endpoint GrokMediaEndpoint, requestID string, request soraBridgeVideoRequest, body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("video upstream returned invalid task JSON")
	}
	id := requestID
	if endpoint.IsVideoGenerationRequest() {
		id = extractGrokMediaVideoRequestID(body)
	} else {
		// Some providers also expose a private task_id which is not pollable.
		returnedID := firstNonEmpty(gjson.GetBytes(body, "id").String(), gjson.GetBytes(body, "data.id").String(), gjson.GetBytes(body, "video.id").String())
		if returnedID != "" && returnedID != requestID {
			return nil, fmt.Errorf("video upstream returned a different task ID")
		}
	}
	if id == "" {
		return nil, fmt.Errorf("video upstream response is missing a task ID")
	}
	canonicalStatus := soraBridgeVideoStatus(body)
	if canonicalStatus == "" {
		if !endpoint.IsVideoGenerationRequest() {
			return nil, fmt.Errorf("video upstream returned an unsupported task status")
		}
		// A successful creation with an ID may already represent paid work.
		// Bind it before publishing a response, even for an unknown status.
		canonicalStatus = "queued"
	}
	result := map[string]any{"id": id, "object": "video", "status": canonicalStatus}
	if endpoint.IsVideoGenerationRequest() {
		result["model"] = request.Model
	}
	if created := gjson.GetBytes(body, "created_at"); created.Type == gjson.Number && created.Int() > 0 {
		result["created_at"] = created.Int()
	}
	if endpoint.IsVideoGenerationRequest() {
		result["seconds"] = strconv.Itoa(request.Seconds)
		result["size"] = request.Size
	}
	// Reconciliation still sees actual output metadata, including an immediate
	// completion response, instead of settling only the requested estimate.
	for _, path := range []string{"resolution", "video.resolution", "data.resolution", "size"} {
		if value, valid := normalizeVideoBillingResolution(gjson.GetBytes(body, path).String()); valid {
			result["resolution"] = value
			break
		}
	}
	for _, path := range []string{"video.duration", "duration", "seconds", "data.duration", "data.seconds"} {
		if value := gjson.GetBytes(body, path).Int(); value > 0 && value <= 86400 {
			result["duration"] = value
			result["seconds"] = strconv.FormatInt(value, 10)
			break
		}
	}
	if progress := gjson.GetBytes(body, "progress"); progress.Type == gjson.Number && progress.Float() >= 0 && progress.Float() <= 100 {
		result["progress"] = progress.Int()
	}
	if canonicalStatus == "completed" {
		result["content_url"] = "/v1/sora/videos/" + url.PathEscape(id) + "/content"
	}
	if canonicalStatus == "failed" {
		message := firstNonEmpty(gjson.GetBytes(body, "error.message").String(), gjson.GetBytes(body, "message").String(), gjson.GetBytes(body, "reason").String())
		if message == "" {
			message = "Video generation failed"
		}
		code := firstNonEmpty(gjson.GetBytes(body, "error.code").String(), "video_generation_failed")
		result["error"] = map[string]string{"code": truncateString(code, 64), "message": truncateString(sanitizeUpstreamErrorMessage(message), 512)}
	}
	return json.Marshal(result)
}

func soraBridgeVideoStatus(body []byte) string {
	status := firstNonEmpty(
		gjson.GetBytes(body, "status").String(), gjson.GetBytes(body, "data.status").String(),
		gjson.GetBytes(body, "video.status").String(), gjson.GetBytes(body, "state").String(),
		gjson.GetBytes(body, "data.state").String(),
	)
	if GrokMediaVideoStatusFailed(body) {
		status = "failed"
	}
	canonicalStatus := ""
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending", "waiting", "submitted", "created":
		canonicalStatus = "queued"
	case "in_progress", "processing", "running", "generating", "started":
		canonicalStatus = "in_progress"
	case "completed", "succeeded", "success", "done", "finished":
		canonicalStatus = "completed"
	case "failed", "failure", "fail", "error", "cancelled", "canceled", "expired":
		canonicalStatus = "failed"
	}
	return canonicalStatus
}

// Download fallback consumes the original URL fields, while sharing the same
// state aliases as polling. Native requests remain byte-for-byte unaffected.
func soraBridgeDownloadStatus(ctx context.Context, body []byte) []byte {
	if !IsSoraVideoBridge(ctx) {
		return body
	}
	if status := soraBridgeVideoStatus(body); status != "" {
		if out, err := sjson.SetBytes(body, "status", status); err == nil {
			return out
		}
	}
	return body
}
