package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type GrokMediaEndpoint string

const (
	GrokMediaEndpointImagesGenerations GrokMediaEndpoint = "images_generations"
	GrokMediaEndpointImagesEdits       GrokMediaEndpoint = "images_edits"
	GrokMediaEndpointVideosGenerations GrokMediaEndpoint = "videos_generations"
	GrokMediaEndpointVideosEdits       GrokMediaEndpoint = "videos_edits"
	GrokMediaEndpointVideosExtensions  GrokMediaEndpoint = "videos_extensions"
	GrokMediaEndpointVideoStatus       GrokMediaEndpoint = "video_status"
	GrokMediaEndpointVideoContent      GrokMediaEndpoint = "video_content"
	SeedanceEndpointCreate             GrokMediaEndpoint = "seedance_create"
	SeedanceEndpointStatus             GrokMediaEndpoint = "seedance_status"
	SeedanceEndpointDelete             GrokMediaEndpoint = "seedance_delete"
)

const OpenAIEndpointCapabilitySeedance OpenAIEndpointCapability = "seedance"

func (e GrokMediaEndpoint) IsSeedance() bool {
	return e == SeedanceEndpointCreate || e == SeedanceEndpointStatus || e == SeedanceEndpointDelete
}

func (e GrokMediaEndpoint) RequiresRequestBody() bool {
	return !e.IsVideoLookupRequest()
}

func (e GrokMediaEndpoint) IsVideoLookupRequest() bool {
	return e == GrokMediaEndpointVideoStatus || e == GrokMediaEndpointVideoContent || e == SeedanceEndpointStatus || e == SeedanceEndpointDelete
}

func (e GrokMediaEndpoint) IsVideoGenerationRequest() bool {
	switch e {
	case GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions, SeedanceEndpointCreate:
		return true
	default:
		return false
	}
}

func (e GrokMediaEndpoint) IsImageGenerationRequest() bool {
	switch e {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		return true
	default:
		return false
	}
}

func (e GrokMediaEndpoint) IsGenerationRequest() bool {
	switch e {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits, GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions, SeedanceEndpointCreate:
		return true
	default:
		return false
	}
}

type GrokMediaRequestInfo struct {
	Model      string
	Prompt     string
	N          int
	Size       string
	SizeTier   string
	Resolution string
	// GrokResolution preserves the historical Grok resolution calculation. It
	// intentionally considers only the original `resolution` field (plus the
	// model/size/default fallback), so accepting resolution_name for Custom
	// requests cannot change Grok's existing billing behavior.
	GrokResolution string
	// RawResolution is the trimmed value selected from the request's
	// resolution/resolution_name fields before model/size based defaults are
	// applied. ResolutionValid reports whether that selected value is one of
	// the supported canonical values or aliases. ResolutionProvided lets
	// callers distinguish an omitted resolution from an explicitly invalid
	// value; both are intentionally represented by ResolutionValid=false.
	RawResolution      string
	ResolutionValid    bool
	ResolutionProvided bool
	grokRawResolution  string
	DurationSeconds    int
	RawDurationSeconds int
	InputImageURLs     []string
	ReferenceVideoURLs []string
	HasReferenceVideo  bool
	MaskImageURL       string
	Uploads            []OpenAIImagesUpload
	MaskUpload         *OpenAIImagesUpload
}

func (r GrokMediaRequestInfo) ModerationBody() []byte {
	payload := map[string]any{}
	if prompt := strings.TrimSpace(r.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}

	images := make([]map[string]string, 0, len(r.InputImageURLs)+len(r.Uploads)+1)
	for _, imageURL := range r.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, upload := range r.Uploads {
		if dataURL := upload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if maskURL := strings.TrimSpace(r.MaskImageURL); maskURL != "" {
		images = append(images, map[string]string{"image_url": maskURL})
	}
	if r.MaskUpload != nil {
		if dataURL := r.MaskUpload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

func (e GrokMediaEndpoint) httpMethod() string {
	if e == SeedanceEndpointDelete {
		return http.MethodDelete
	}
	if e.IsVideoLookupRequest() {
		return http.MethodGet
	}
	return http.MethodPost
}

func ExtractGrokMediaModel(contentType string, body []byte) string {
	return ParseGrokMediaRequest(contentType, body).Model
}

func ParseGrokMediaRequest(contentType string, body []byte) GrokMediaRequestInfo {
	info := GrokMediaRequestInfo{N: 1}
	if gjson.ValidBytes(body) {
		parseGrokMediaJSONRequest(body, &info)
	} else {
		parseGrokMediaMultipartRequest(contentType, body, &info)
	}
	info.Model = strings.TrimSpace(info.Model)
	info.Prompt = strings.TrimSpace(info.Prompt)
	info.Size = strings.TrimSpace(info.Size)
	info.SizeTier = NormalizeImageBillingTierOrDefault(info.Size)
	// Keep the client-selected canonical value in Resolution for Custom
	// forwarding. Grok callers use the separate GrokResolution field below so
	// an omitted or invalid Custom value does not silently become 480p.
	if normalized, ok := normalizeVideoBillingResolution(info.RawResolution); ok {
		info.Resolution = normalized
		info.ResolutionValid = true
	} else {
		info.Resolution = NormalizeVideoBillingResolutionForModelOrDefault(info.Model, info.RawResolution, info.Size)
	}
	// Keep this separate from Resolution: Resolution represents the selected
	// Custom-compatible value (which may come from resolution_name), while
	// GrokResolution must retain the pre-existing primary-field behavior.
	info.GrokResolution = NormalizeVideoBillingResolutionForModelOrDefault(info.Model, info.grokRawResolution, info.Size)
	info.RawDurationSeconds = info.DurationSeconds
	info.DurationSeconds = NormalizeVideoBillingDurationSecondsForModelOrDefault(info.Model, info.DurationSeconds)
	if info.N <= 0 {
		info.N = 1
	}
	return info
}

func parseGrokMediaJSONRequest(body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	info.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	resolution := gjson.GetBytes(body, "resolution")
	resolutionName := gjson.GetBytes(body, "resolution_name")
	setGrokMediaResolutionFields(
		info,
		strings.TrimSpace(resolution.String()), resolution.Exists(),
		strings.TrimSpace(resolutionName.String()), resolutionName.Exists(),
	)
	if duration := gjson.GetBytes(body, "duration"); duration.Exists() {
		if parsed, err := strconv.Atoi(strings.TrimSpace(duration.String())); err == nil {
			info.DurationSeconds = parsed
		}
	} else if seconds := gjson.GetBytes(body, "seconds"); seconds.Exists() {
		if seconds.Type == gjson.Number {
			info.DurationSeconds = int(seconds.Int())
		} else if parsed, err := strconv.Atoi(strings.TrimSpace(seconds.String())); err == nil {
			info.DurationSeconds = parsed
		}
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() {
		switch n.Type {
		case gjson.Number:
			info.N = int(n.Int())
		case gjson.String:
			if parsed, err := strconv.Atoi(strings.TrimSpace(n.String())); err == nil {
				info.N = parsed
			}
		}
	}
	appendJSONImageURLs := func(value gjson.Result) {
		if !value.Exists() {
			return
		}
		switch {
		case value.IsArray():
			for _, item := range value.Array() {
				if imageURL := grokMediaJSONImageURL(item); imageURL != "" {
					info.InputImageURLs = append(info.InputImageURLs, imageURL)
					continue
				}
				if item.Type == gjson.String {
					imageURL := strings.TrimSpace(item.String())
					if imageURL == "" {
						continue
					}
					info.InputImageURLs = append(info.InputImageURLs, imageURL)
				}
			}
		default:
			if imageURL := grokMediaJSONImageURL(value); imageURL != "" {
				info.InputImageURLs = append(info.InputImageURLs, imageURL)
				return
			}
			if value.Type == gjson.String {
				imageURL := strings.TrimSpace(value.String())
				if imageURL == "" {
					return
				}
				info.InputImageURLs = append(info.InputImageURLs, imageURL)
			}
		}
	}
	appendJSONImageURLs(gjson.GetBytes(body, "image"))
	appendJSONImageURLs(gjson.GetBytes(body, "images"))
	appendJSONImageURLs(gjson.GetBytes(body, "image_url"))
	appendJSONImageURLs(gjson.GetBytes(body, "image_urls"))
	appendJSONImageURLs(gjson.GetBytes(body, "reference_images"))
	appendJSONImageURLs(gjson.GetBytes(body, "referenceImages"))
	appendJSONImageURLs(gjson.GetBytes(body, "image_refs"))
	appendJSONImageURLs(gjson.GetBytes(body, "input_reference"))
	appendJSONImageURLs(gjson.GetBytes(body, "reference_image_urls"))
	appendJSONImageURLs(gjson.GetBytes(body, "first_frame_image_url"))
	appendJSONImageURLs(gjson.GetBytes(body, "last_frame_image_url"))
	for _, frame := range gjson.GetBytes(body, "keyframes").Array() {
		appendJSONImageURLs(frame.Get("image_url"))
	}
	for _, field := range []string{"referenceVideos", "reference_videos", "video_refs", "reference_video_urls", "reference_video_url"} {
		value := gjson.GetBytes(body, field)
		if !value.Exists() {
			continue
		}
		if value.IsArray() {
			for _, item := range value.Array() {
				if videoURL := grokMediaReferenceURL(item); videoURL != "" {
					info.ReferenceVideoURLs = append(info.ReferenceVideoURLs, videoURL)
				}
			}
		} else if videoURL := grokMediaReferenceURL(value); videoURL != "" {
			info.ReferenceVideoURLs = append(info.ReferenceVideoURLs, videoURL)
		}
	}
	info.HasReferenceVideo = len(info.ReferenceVideoURLs) > 0
	info.MaskImageURL = grokMediaJSONImageURL(gjson.GetBytes(body, "mask"))
}

func grokMediaReferenceURL(value gjson.Result) string {
	if value.Type == gjson.String {
		return strings.TrimSpace(value.String())
	}
	if rawURL := strings.TrimSpace(value.Get("url").String()); rawURL != "" {
		return rawURL
	}
	return strings.TrimSpace(value.Get("video_url").String())
}

func grokMediaJSONImageURL(value gjson.Result) string {
	return imageInputReferenceURL(value)
}

func parseGrokMediaMultipartRequest(contentType string, body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return
	}
	var resolution, resolutionName string
	var resolutionProvided, resolutionNameProvided bool
	defer func() {
		setGrokMediaResolutionFields(
			info,
			resolution, resolutionProvided,
			resolutionName, resolutionNameProvided,
		)
	}()
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		name := strings.TrimSpace(part.FormName())
		switch referenceName := strings.TrimSuffix(name, "[]"); referenceName {
		case "referenceImages", "reference_images", "image_refs", "referenceVideos", "reference_videos", "video_refs", "referenceAudios", "reference_audios", "audio_refs":
			name = referenceName
		}
		if name == "" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize))
		_ = part.Close()
		if err != nil {
			return
		}
		fileName := strings.TrimSpace(part.FileName())
		partContentType := strings.TrimSpace(part.Header.Get("Content-Type"))
		if fileName != "" {
			if name == "referenceVideos" || name == "reference_videos" || name == "video_refs" || strings.HasPrefix(name, "referenceVideos[") || strings.HasPrefix(name, "reference_videos[") {
				info.HasReferenceVideo = true
			}
			upload := OpenAIImagesUpload{
				FieldName:   name,
				FileName:    fileName,
				ContentType: partContentType,
				Data:        data,
			}
			if name == "mask" {
				info.MaskUpload = &upload
				continue
			}
			if name == "image" || name == "input_reference" || name == "referenceImages" || name == "reference_images" || strings.HasPrefix(name, "image[") || strings.HasPrefix(name, "referenceImages[") {
				info.Uploads = append(info.Uploads, upload)
			}
			continue
		}

		value := strings.TrimSpace(string(data))
		switch name {
		case "model":
			info.Model = value
		case "prompt":
			info.Prompt = value
		case "size":
			info.Size = value
		case "resolution":
			resolution = value
			resolutionProvided = true
		case "resolution_name":
			resolutionName = value
			resolutionNameProvided = true
		case "duration":
			if duration, err := strconv.Atoi(value); err == nil {
				info.DurationSeconds = duration
			}
		case "seconds":
			if duration, err := strconv.Atoi(value); err == nil {
				info.DurationSeconds = duration
			}
		case "n":
			if n, err := strconv.Atoi(value); err == nil {
				info.N = n
			}
		case "image", "image_url":
			if value != "" {
				info.InputImageURLs = append(info.InputImageURLs, value)
			}
		case "referenceImages", "reference_images", "image_refs":
			info.InputImageURLs = append(info.InputImageURLs, grokMediaMultipartReferenceURLs(value)...)
		case "referenceVideos", "reference_videos", "video_refs":
			info.ReferenceVideoURLs = append(info.ReferenceVideoURLs, grokMediaMultipartReferenceURLs(value)...)
			info.HasReferenceVideo = info.HasReferenceVideo || len(info.ReferenceVideoURLs) > 0
		case "mask", "mask_image_url":
			info.MaskImageURL = value
		}
	}
}

func grokMediaMultipartReferenceURLs(value string) []string {
	if value == "" {
		return nil
	}
	parsed := gjson.Parse(value)
	if parsed.IsArray() {
		var refs []string
		for _, item := range parsed.Array() {
			if ref := grokMediaReferenceURL(item); ref != "" {
				refs = append(refs, ref)
			}
		}
		return refs
	}
	return []string{value}
}

// setGrokMediaResolutionFields records the effective client-supplied
// resolution without applying the legacy model/size fallback. A valid
// resolution field always wins over resolution_name; when it is absent,
// empty, or invalid, resolution_name is considered next. The caller can then
// apply platform-specific defaults while retaining enough metadata to avoid
// forwarding an invalid value to a transparent Custom upstream.
func setGrokMediaResolutionFields(
	info *GrokMediaRequestInfo,
	resolution string,
	resolutionProvided bool,
	resolutionName string,
	resolutionNameProvided bool,
) {
	if info == nil {
		return
	}
	resolution = strings.TrimSpace(resolution)
	resolutionName = strings.TrimSpace(resolutionName)
	info.grokRawResolution = resolution
	info.ResolutionProvided = resolutionProvided || resolutionNameProvided
	info.RawResolution = resolution
	info.ResolutionValid = false
	if normalized, ok := normalizeVideoBillingResolution(resolution); ok {
		info.RawResolution = resolution
		info.Resolution = normalized
		info.ResolutionValid = true
		return
	}
	if normalized, ok := normalizeVideoBillingResolution(resolutionName); ok {
		info.RawResolution = resolutionName
		info.Resolution = normalized
		info.ResolutionValid = true
		return
	}
	if !resolutionProvided && !resolutionNameProvided {
		if normalized, ok := normalizeVideoBillingResolution(info.Size); ok {
			info.Resolution, info.ResolutionValid = normalized, true
			return
		}
	}
	// Preserve the explicit primary value for diagnostics when present; if it
	// was omitted, retain the alias value instead. Both remain invalid here.
	if !resolutionProvided {
		info.RawResolution = resolutionName
	}
}

func GrokMediaVideoRequestSessionHash(requestID string, userID, apiKeyID int64) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	ownerSeed := fmt.Sprintf("%d:%d:%s", userID, apiKeyID, requestID)
	return "grok-video:" + DeriveSessionHashFromSeed(ownerSeed)
}

const mediaVideoRequestBindingTTL = 7 * 24 * time.Hour

// A task may report completed before the provider has finished copying the
// binary into its content store. Retry that narrow window without retrying
// other upstream failures, which could hide a real task/content error.
const grokMediaVideoContentMaxAttempts = 6

var grokMediaVideoContentRetryDelay = 5 * time.Second

const grokMediaVideoBillingPrefix = "grok-video-billing:"

type GrokMediaVideoBillingRecord struct {
	Command   UsageBillingCommand `json:"command"`
	GroupID   int64               `json:"group_id"`
	ExpiresAt time.Time           `json:"expires_at"`
}

// GrokMediaVideoBillingCache is an optional extension implemented by the
// production Redis cache. Keeping it separate preserves GatewayCache test
// doubles while allowing a billed async task to carry its exact refund receipt
// across replicas.
type GrokMediaVideoBillingCache interface {
	SetGrokMediaVideoBillingRecord(ctx context.Context, groupID int64, key string, record *GrokMediaVideoBillingRecord, ttl time.Duration) error
	GetGrokMediaVideoBillingRecord(ctx context.Context, groupID int64, key string) (*GrokMediaVideoBillingRecord, error)
}

func GrokMediaVideoBillingRequestID(requestID string) string {
	return "video:" + strings.TrimSpace(requestID)
}

func GrokMediaVideoRefundRequestID(requestID string) string {
	return "video-refund:" + strings.TrimSpace(requestID)
}

func grokMediaVideoBillingKey(requestID string, userID, apiKeyID int64) string {
	hash := GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID)
	if hash == "" {
		return ""
	}
	return grokMediaVideoBillingPrefix + hash
}

func (s *OpenAIGatewayService) BindGrokMediaVideoBillingRecord(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID int64,
	record *GrokMediaVideoBillingRecord,
) error {
	if s == nil || s.cache == nil || record == nil || strings.TrimSpace(record.Command.RequestID) == "" {
		return fmt.Errorf("grok video billing record is invalid")
	}
	key := grokMediaVideoBillingKey(requestID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("grok video billing record key is invalid")
	}
	ttl := mediaVideoRequestBindingTTL
	if s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		configuredTTL := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second
		if configuredTTL > ttl {
			ttl = configuredTTL
		}
	}
	copyRecord := *record
	copyRecord.GroupID = derefGroupID(groupID)
	copyRecord.ExpiresAt = time.Now().Add(ttl)
	if cache, ok := s.cache.(GrokMediaVideoBillingCache); ok {
		return cache.SetGrokMediaVideoBillingRecord(ctx, copyRecord.GroupID, key, &copyRecord, ttl)
	}
	s.grokMediaVideoBillingRecords.Store(fmt.Sprintf("%d:%s", copyRecord.GroupID, key), copyRecord)
	return nil
}

func (s *OpenAIGatewayService) ResolveGrokMediaVideoBillingRecord(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID int64,
) (*GrokMediaVideoBillingRecord, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("grok video billing cache is unavailable")
	}
	key := grokMediaVideoBillingKey(requestID, userID, apiKeyID)
	group := derefGroupID(groupID)
	if key == "" {
		return nil, fmt.Errorf("grok video billing record key is invalid")
	}
	if cache, ok := s.cache.(GrokMediaVideoBillingCache); ok {
		return cache.GetGrokMediaVideoBillingRecord(ctx, group, key)
	}
	value, ok := s.grokMediaVideoBillingRecords.Load(fmt.Sprintf("%d:%s", group, key))
	if !ok {
		return nil, ErrStickySessionNotFound
	}
	record, ok := value.(GrokMediaVideoBillingRecord)
	if !ok || (!record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt)) {
		s.grokMediaVideoBillingRecords.Delete(fmt.Sprintf("%d:%s", group, key))
		return nil, ErrStickySessionNotFound
	}
	return &record, nil
}

func (s *OpenAIGatewayService) RefundFailedGrokMediaVideoTask(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID int64,
) (*GrokMediaVideoBillingRecord, bool, error) {
	record, err := s.ResolveGrokMediaVideoBillingRecord(ctx, groupID, requestID, userID, apiKeyID)
	if errors.Is(err, ErrStickySessionNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	reverser, ok := s.usageBillingRepo.(UsageBillingReversalRepository)
	if !ok || reverser == nil {
		return record, false, fmt.Errorf("usage billing reversal repository is unavailable")
	}
	result, err := reverser.Reverse(ctx, &UsageBillingReversalCommand{
		ReversalRequestID: GrokMediaVideoRefundRequestID(requestID),
		Original:          record.Command,
	})
	if err != nil {
		return record, false, err
	}
	applied := result != nil && result.Applied
	if !applied {
		return record, false, nil
	}
	s.invalidateGrokMediaVideoBillingCaches(ctx, record)
	return record, true, nil
}

func (s *OpenAIGatewayService) invalidateGrokMediaVideoBillingCaches(ctx context.Context, record *GrokMediaVideoBillingRecord) {
	if s == nil || s.billingCacheService == nil || record == nil {
		return
	}
	if s.billingCacheService != nil {
		if record.Command.BalanceCost > 0 {
			_ = s.billingCacheService.InvalidateUserBalance(ctx, record.Command.UserID)
		}
		if record.Command.SubscriptionCost > 0 && record.Command.SubscriptionID != nil {
			_ = s.billingCacheService.InvalidateSubscription(ctx, record.Command.UserID, record.GroupID)
		}
		if record.Command.APIKeyRateLimitCost > 0 {
			_ = s.billingCacheService.InvalidateAPIKeyRateLimit(ctx, record.Command.APIKeyID)
		}
		if record.Command.UserPlatformQuotaCost > 0 {
			_ = s.billingCacheService.InvalidateUserPlatformQuota(ctx, record.Command.UserID, record.Command.Platform)
		}
	}
}

// ReverseGrokMediaVideoBilling compensates a charge that completed before the
// task's durable billing metadata could be saved. It uses the same idempotent
// reversal path as a later failed task poll.
func (s *OpenAIGatewayService) ReverseGrokMediaVideoBilling(
	ctx context.Context,
	requestID string,
	record *GrokMediaVideoBillingRecord,
) (bool, error) {
	if record == nil {
		return false, nil
	}
	reverser, ok := s.usageBillingRepo.(UsageBillingReversalRepository)
	if !ok || reverser == nil {
		return false, fmt.Errorf("usage billing reversal repository is unavailable")
	}
	result, err := reverser.Reverse(ctx, &UsageBillingReversalCommand{
		ReversalRequestID: GrokMediaVideoRefundRequestID(requestID),
		Original:          record.Command,
	})
	if err != nil {
		return false, err
	}
	applied := result != nil && result.Applied
	if applied {
		s.invalidateGrokMediaVideoBillingCaches(ctx, record)
	}
	return applied, nil
}

func GrokMediaVideoStatusFailed(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	for _, path := range []string{"status", "data.status", "video.status", "state", "data.state"} {
		switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, path).String())) {
		case "failed", "failure", "fail", "error", "cancelled", "canceled", "expired":
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) BindGrokMediaVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID, accountID int64,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("grok video request binding cache is unavailable")
	}
	sessionHash := GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID)
	cacheKey := s.openAISessionCacheKey(sessionHash)
	if cacheKey == "" || accountID <= 0 {
		return fmt.Errorf("grok video request binding is invalid")
	}
	ttl := mediaVideoRequestBindingTTL
	if s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		configuredTTL := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second
		if configuredTTL > ttl {
			ttl = configuredTTL
		}
	}
	return s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, accountID, ttl)
}

func (s *OpenAIGatewayService) ResolveGrokMediaVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID int64,
) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, fmt.Errorf("grok video request binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID))
	if cacheKey == "" {
		return 0, fmt.Errorf("grok video request binding is invalid")
	}
	return s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
}

func (s *OpenAIGatewayService) ResolveGrokMediaVideoRequestAccountDetails(
	ctx context.Context,
	groupID *int64,
	requestID string,
	userID, apiKeyID int64,
) (*Account, error) {
	accountID, err := s.ResolveGrokMediaVideoRequestAccount(ctx, groupID, requestID, userID, apiKeyID)
	if err != nil || accountID <= 0 {
		if repo, ok := s.usageBillingRepo.(MediaBillingRepository); ok {
			if receipt, lookupErr := repo.FindMediaTask(ctx, requestID, userID, apiKeyID); lookupErr == nil && receipt != nil && receipt.GroupID == derefGroupID(groupID) {
				accountID, err = receipt.Quote.Command.AccountID, nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if accountID <= 0 || s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("video request account is unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || (account.Platform != PlatformGrok && account.Platform != PlatformCustom && account.Platform != PlatformOpenAI) {
		return nil, fmt.Errorf("video request account platform is unsupported")
	}
	return account, nil
}

func (s *OpenAIGatewayService) ForwardGrokMedia(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint GrokMediaEndpoint,
	requestID string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("media account is required")
	}
	if account.Platform != PlatformGrok && account.Platform != PlatformCustom {
		return nil, fmt.Errorf("account platform %s is not supported for media", account.Platform)
	}
	if endpoint.IsVideoGenerationRequest() && !validMediaProtocol(account.MediaProtocol()) {
		return nil, fmt.Errorf("%s", mediaProtocolValidationMessage())
	}
	if endpoint.IsVideoGenerationRequest() {
		if err := validateVideoBillingFields(body, contentType); err != nil {
			return nil, err
		}
	}

	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if endpoint == GrokMediaEndpointVideoContent {
		return s.forwardGrokMediaVideoContent(ctx, c, account, token, requestID, startTime)
	}
	targetURL, err := s.buildCompatibleMediaURL(account, endpoint, requestID)
	if err != nil {
		return nil, err
	}

	body, contentType, err = prepareGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	if account.Platform == PlatformCustom && endpoint.IsVideoGenerationRequest() {
		if endpoint == GrokMediaEndpointVideosGenerations && account.MediaProtocol() == "8yes" {
			body, contentType, err = normalizeEightYesVideoRequest(body, contentType)
			if err != nil {
				return nil, err
			}
		} else if protocol := account.MediaProtocol(); endpoint == GrokMediaEndpointVideosGenerations && protocol == "sora" {
			// Normalize explicit provider defaults before parsing usage so
			// forwarding, precharge and final billing agree on the parameters.
			body, err = normalizeVideoProtocolBody(ctx, account, body)
			if err != nil {
				return nil, err
			}
		} else {
			body, contentType, err = normalizeCustomVideoForwardBody(endpoint, body, contentType)
			if err != nil {
				return nil, err
			}
		}
	}
	if account.Platform == PlatformGrok {
		body, contentType, err = normalizeGrokMediaForwardBody(endpoint, body, contentType)
		if err != nil {
			return nil, err
		}
	}
	requestInfo := ParseGrokMediaRequest(contentType, body)
	if account.Platform == PlatformCustom && endpoint.IsVideoGenerationRequest() {
		if account.MediaProtocol() == "8yes" {
			requestInfo = normalizeEightYesVideoRequestInfo(requestInfo)
		} else {
			if account.MediaProtocol() == "sora" {
				requestInfo = normalizeSoraVideoRequestInfo(requestInfo)
			}
			requestInfo = normalizeCustomVideoRequestInfo(requestInfo)
		}
	}
	upstreamModel := requestInfo.Model
	if endpoint.RequiresRequestBody() && account.Platform != PlatformCustom && gjson.ValidBytes(body) {
		if mappedModel := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mappedModel != "" {
			upstreamModel = mappedModel
		}
		if upstreamModel != requestInfo.Model {
			body, err = sjson.SetBytes(body, "model", upstreamModel)
			if err != nil {
				return nil, fmt.Errorf("rewrite grok media account mapped model: %w", err)
			}
		}
	}
	if account.Platform == PlatformGrok {
		body, contentType, err = sanitizeGrokMediaForwardBody(endpoint, body, contentType)
		if err != nil {
			return nil, err
		}
	}
	// Re-parse video requests after account model mapping so resolution,
	// duration constraints, and reference-video pricing follow the model
	// actually sent upstream. Grok image sanitization removes the unsupported
	// size field, but billing must retain the size requested by the client.
	forwardedRequestInfo := requestInfo
	if endpoint.IsVideoGenerationRequest() {
		forwardedRequestInfo = ParseGrokMediaRequest(contentType, body)
		if account.Platform == PlatformCustom {
			if account.MediaProtocol() == "8yes" {
				forwardedRequestInfo = normalizeEightYesVideoRequestInfo(forwardedRequestInfo)
			} else {
				if account.MediaProtocol() == "sora" {
					forwardedRequestInfo = normalizeSoraVideoRequestInfo(forwardedRequestInfo)
				}
				forwardedRequestInfo = normalizeCustomVideoRequestInfo(forwardedRequestInfo)
			}
		}
	}
	if protocol := account.MediaProtocol(); account.Platform == PlatformCustom && endpoint == GrokMediaEndpointVideosGenerations && protocol != "sora" && protocol != "8yes" {
		// MiniMax workbench metadata is built after extracting the original
		// reference and duration metadata used for billing and audit.
		body, err = normalizeVideoProtocolBody(ctx, account, body)
		if err != nil {
			return nil, err
		}
	}

	var bodyReader io.Reader
	if endpoint.RequiresRequestBody() {
		bodyReader = bytes.NewReader(body)
	}
	upstreamCtx, releaseUpstreamCtx := videoMediaUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, endpoint.httpMethod(), targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(targetURL) {
		applyGrokCLIHeaders(upstreamReq.Header)
	}
	if endpoint.RequiresRequestBody() {
		contentType = strings.TrimSpace(contentType)
		if contentType == "" {
			contentType = "application/json"
		}
		upstreamReq.Header.Set("Content-Type", contentType)
	}
	// 账号级请求头覆写最后应用，配置值优先于内置默认头。
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	requestIDHeader := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
	requestModel := requestInfo.Model
	if endpoint == GrokMediaEndpointVideoStatus {
		if err := s.normalizeCompatibleVideoStatusResponse(account, requestID, resp); err != nil {
			return nil, err
		}
	}
	if resp.StatusCode >= 400 {
		return s.handleGrokMediaErrorResponse(ctx, resp, c, account, requestIDHeader, requestModel)
	}

	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if endpoint == GrokMediaEndpointImagesGenerations || endpoint == GrokMediaEndpointImagesEdits {
		if countOpenAIResponseImageOutputsFromJSONBytes(respBody) <= 0 {
			setOpsUpstreamError(c, http.StatusBadGateway, "xAI upstream returned no image output", truncateString(string(respBody), 512))
			failureUsage, _ := extractOpenAIUsageFromJSONBytes(respBody)
			return imageFailureUsageResult(failureUsage, requestModel, upstreamModel), &UpstreamFailoverError{
				StatusCode:      http.StatusBadGateway,
				ResponseBody:    respBody,
				ResponseHeaders: resp.Header.Clone(),
			}
		}
	}
	if endpoint == GrokMediaEndpointVideoStatus && account.Platform == PlatformGrok {
		respBody = rewriteGrokMediaVideoContentURLs(
			respBody,
			requestID,
			grokMediaContentProxyURL(c, requestID),
		)
	}
	usage := grokMediaUsageFromResponse(endpoint, forwardedRequestInfo, respBody)
	if account.MediaProtocol() == "8yes" && endpoint.IsVideoGenerationRequest() {
		// task_id identifies an internal provider task and cannot be polled.
		usage.ResponseID = strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
	}
	result := &OpenAIForwardResult{
		RequestID:            requestIDHeader,
		ResponseID:           usage.ResponseID,
		Usage:                usage.Usage,
		Model:                requestModel,
		BillingModel:         requestModel,
		UpstreamModel:        upstreamModel,
		ResponseHeaders:      resp.Header.Clone(),
		Duration:             time.Since(startTime),
		ImageCount:           usage.ImageCount,
		ImageSize:            usage.ImageSize,
		ImageInputSize:       usage.ImageInputSize,
		ImageOutputSizes:     usage.ImageOutputSizes,
		VideoCount:           usage.VideoCount,
		VideoResolution:      usage.VideoResolution,
		VideoDurationSeconds: usage.VideoDurationSeconds,
		VideoPriceMultiplier: usage.VideoPriceMultiplier,
	}
	if (account.Platform == PlatformCustom || c.GetBool("media_billing_buffer")) && (endpoint.IsVideoGenerationRequest() || endpoint == GrokMediaEndpointVideoStatus) {
		result.BufferedResponseStatus = resp.StatusCode
		result.BufferedResponseHeaders = resp.Header.Clone()
		result.BufferedResponseBody = append([]byte(nil), respBody...)
		return result, nil
	}
	writeGrokMediaResponse(c, resp, respBody, s.responseHeaderFilter)
	return result, nil
}

// WriteBufferedGrokMediaResponse publishes a video creation success only after
// the handler has durably bound and billed the asynchronous task.
func (s *OpenAIGatewayService) WriteBufferedGrokMediaResponse(c *gin.Context, result *OpenAIForwardResult) bool {
	if result == nil || result.BufferedResponseStatus <= 0 {
		return false
	}
	return s.WriteCustomRawUpstreamResponse(
		c,
		result.BufferedResponseStatus,
		result.BufferedResponseHeaders,
		result.BufferedResponseBody,
	)
}

func (s *OpenAIGatewayService) forwardGrokMediaVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, requestID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	upstreamCtx, releaseUpstreamCtx := videoMediaUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	contentURL := ""
	statusRequestID := ""
	signedContent := false
	if account.Platform == PlatformCustom {
		var err error
		contentURL, err = s.buildCompatibleMediaURL(account, GrokMediaEndpointVideoContent, requestID)
		if err != nil {
			return nil, err
		}
	} else {
		statusURL, err := s.buildCompatibleMediaURL(account, GrokMediaEndpointVideoStatus, requestID)
		if err != nil {
			return nil, err
		}
		statusReq, err := http.NewRequestWithContext(
			WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
			http.MethodGet,
			statusURL,
			nil,
		)
		if err != nil {
			return nil, err
		}
		statusReq.Header.Set("Authorization", "Bearer "+token)
		statusReq.Header.Set("Accept", "application/json")
		if account.IsGrokOAuth() && isGrokCLIProxyTarget(statusURL) {
			applyGrokCLIHeaders(statusReq.Header)
		}
		account.ApplyHeaderOverrides(statusReq.Header)

		statusResp, err := s.httpUpstream.Do(statusReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
		}
		statusRequestID = firstNonEmpty(statusResp.Header.Get("x-request-id"), statusResp.Header.Get("xai-request-id"))
		if statusResp.StatusCode >= 300 {
			defer func() { _ = statusResp.Body.Close() }()
			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
			if statusResp.StatusCode < 400 {
				return nil, fmt.Errorf("grok media status redirect is not allowed")
			}
			return s.handleGrokMediaErrorResponse(ctx, statusResp, c, account, statusRequestID, "")
		}
		statusBody, err := ReadUpstreamResponseBody(statusResp.Body, s.cfg, c, openAITooLargeError)
		_ = statusResp.Body.Close()
		if err != nil {
			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
			return nil, err
		}

		contentURL, err = grokMediaSignedVideoContentURL(statusBody, requestID)
		if err != nil {
			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
			return nil, err
		}
		signedContent = contentURL != ""
		if !signedContent {
			contentURL, err = s.buildCompatibleMediaURL(account, GrokMediaEndpointVideoContent, requestID)
			if err != nil {
				SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
				return nil, err
			}
		}
	}

	contentReq, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
		http.MethodGet,
		contentURL,
		nil,
	)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}
	contentReq.Header.Set("Accept", "*/*")
	if c != nil {
		if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
			contentReq.Header.Set("Range", rangeHeader)
		}
	}
	if !signedContent {
		contentReq.Header.Set("Authorization", "Bearer "+token)
		if account.IsGrokOAuth() && isGrokCLIProxyTarget(contentURL) {
			applyGrokCLIHeaders(contentReq.Header)
		}
		account.ApplyHeaderOverrides(contentReq.Header)
	}

	retryCtx := ctx
	if retryCtx == nil {
		retryCtx = context.Background()
	}
	var contentResp *http.Response
	for attempt := 1; attempt <= grokMediaVideoContentMaxAttempts; attempt++ {
		contentResp, err = s.httpUpstream.Do(contentReq, proxyURL, account.ID, account.Concurrency)
		if err != nil || contentResp == nil || contentResp.StatusCode != http.StatusBadGateway || attempt == grokMediaVideoContentMaxAttempts {
			break
		}

		// Drain and close the failed response before retrying so transports can
		// reuse the connection and the response body is never leaked.
		if contentResp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(contentResp.Body, 64<<10))
			_ = contentResp.Body.Close()
		}
		timer := time.NewTimer(grokMediaVideoContentRetryDelay)
		select {
		case <-retryCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, retryCtx.Err()
		case <-timer.C:
		}
	}
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	if contentResp == nil {
		return nil, fmt.Errorf("grok media content upstream returned an empty response")
	}
	if account.Platform == PlatformCustom && account.MediaProtocol() == "sora" && ((contentResp.StatusCode >= 400 && contentResp.StatusCode != http.StatusRequestedRangeNotSatisfiable) || isVideoContentJSONResponse(contentResp)) {
		fallbackResp, fallbackErr := s.customMediaVideoContentFallback(upstreamCtx, account, token, requestID, contentReq, contentResp)
		if fallbackErr != nil {
			_ = contentResp.Body.Close()
			return nil, fallbackErr
		}
		if fallbackResp != nil {
			_ = contentResp.Body.Close()
			contentResp = fallbackResp
		}
	}
	if contentResp.StatusCode >= 300 && contentResp.StatusCode < 400 && account.Platform == PlatformCustom && (account.MediaProtocol() == "8yes" || account.MediaProtocol() == "sora") {
		base := contentReq.URL
		if contentResp.Request != nil {
			base = contentResp.Request.URL
		}
		location, locationErr := resolveVideoContentDownloadURL(contentResp.Header.Get("Location"), base)
		_ = contentResp.Body.Close()
		if locationErr != nil {
			return nil, fmt.Errorf("unsafe video content redirect")
		}
		contentResp, err = s.customMediaVideoContentDownload(upstreamCtx, account, token, contentReq, location.String())
		if err != nil {
			return nil, fmt.Errorf("video content download failed")
		}
	}
	defer func() { _ = contentResp.Body.Close() }()
	contentRequestID := firstNonEmpty(contentResp.Header.Get("x-request-id"), contentResp.Header.Get("xai-request-id"), statusRequestID)
	if contentResp.StatusCode >= 300 && contentResp.StatusCode < 400 {
		return nil, fmt.Errorf("grok media signed content redirect is not allowed")
	}
	if contentResp.StatusCode >= 400 && contentResp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return s.handleGrokMediaErrorResponse(ctx, contentResp, c, account, contentRequestID, "")
	}
	if account.Platform == PlatformCustom && isVideoContentJSONResponse(contentResp) {
		return nil, fmt.Errorf("video content upstream returned JSON instead of a video file")
	}

	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(ctx, account, contentResp.Header, contentResp.StatusCode)
	}
	if err := s.writeMediaTaskContentResponse(ctx, c, contentResp); err != nil {
		return nil, err
	}
	return &OpenAIForwardResult{
		RequestID:       contentRequestID,
		ResponseHeaders: contentResp.Header.Clone(),
		Duration:        time.Since(startTime),
	}, nil
}

func grokMediaSignedVideoContentURL(body []byte, requestID string) (string, error) {
	rawURL := strings.TrimSpace(gjson.GetBytes(body, "video.url").String())
	if rawURL == "" {
		return "", nil
	}
	// An upstream Sub2API rewrites protected content URLs to its own proxy
	// endpoint. Treat that as an authenticated relay path, not as a signed URL;
	// the caller will rebuild it against the configured account base URL and
	// attach the upstream API key.
	if isGrokMediaVideoContentURL(rawURL, requestID) {
		return "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") ||
		!strings.EqualFold(parsed.Hostname(), "vidgen.x.ai") ||
		(parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil {
		return "", fmt.Errorf("grok media status returned an unsupported video content URL")
	}
	return parsed.String(), nil
}

func isGrokCLIProxyTarget(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && strings.EqualFold(parsed.Hostname(), "cli-chat-proxy.grok.com")
}

func prepareGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if endpoint != GrokMediaEndpointImagesEdits || gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return body, contentType, nil
	}

	info := ParseGrokMediaRequest(contentType, body)
	payload := make(map[string]any)
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read image edit multipart body: %w", err)
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" || part.FileName() != "" || name == "image" || name == "mask" || strings.HasPrefix(name, "image[") {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return nil, "", fmt.Errorf("read image edit multipart field: %w", err)
		}
		value := strings.TrimSpace(string(data))
		switch name {
		case "n", "output_compression", "partial_images":
			valueInt, err := strconv.Atoi(value)
			if err != nil {
				return nil, "", fmt.Errorf("invalid image edit %s", name)
			}
			payload[name] = valueInt
		case "stream":
			valueBool, err := strconv.ParseBool(value)
			if err != nil {
				return nil, "", fmt.Errorf("invalid image edit stream")
			}
			payload[name] = valueBool
		default:
			payload[name] = value
		}
	}
	if info.Model != "" {
		payload["model"] = info.Model
	}
	if info.Prompt != "" {
		payload["prompt"] = info.Prompt
	}
	if info.N > 1 {
		payload["n"] = info.N
	}
	if info.Size != "" {
		payload["size"] = info.Size
	}

	images := make([]map[string]string, 0, len(info.InputImageURLs)+len(info.Uploads))
	for _, imageURL := range info.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"url": imageURL})
		}
	}
	for _, upload := range info.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, "", err
		}
		images = append(images, map[string]string{"url": dataURL})
	}
	if len(images) > 0 {
		payload["image"] = images[0]
		if len(images) > 1 {
			payload["images"] = images
		}
	}

	maskImageURL := strings.TrimSpace(info.MaskImageURL)
	if info.MaskUpload != nil {
		dataURL, err := openAIImageUploadToDataURL(*info.MaskUpload)
		if err != nil {
			return nil, "", err
		}
		maskImageURL = dataURL
	}
	if maskImageURL != "" {
		payload["mask"] = map[string]string{"url": maskImageURL}
	}

	out, err := marshalOpenAIUpstreamJSON(payload)
	if err != nil {
		return nil, "", err
	}
	return out, "application/json", nil
}

// normalizeCustomVideoForwardBody applies the small amount of normalization
// needed by transparent Custom video accounts. Unlike Grok, Custom does not
// infer a billing/upstream resolution from the model name or a missing field:
// only an explicitly valid resolution (or resolution_name alias) is sent.
// The explicit auto option selects 720p, independently of the model name.
func normalizeCustomVideoForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if !endpoint.IsVideoGenerationRequest() {
		return body, contentType, nil
	}
	info := ParseGrokMediaRequest(contentType, body)
	if strings.EqualFold(info.RawResolution, "auto") {
		info.Resolution, info.ResolutionValid = "720p", true
	}
	if gjson.ValidBytes(body) {
		out := body
		if info.ResolutionValid {
			var err error
			out, err = sjson.SetBytes(out, "resolution", info.Resolution)
			if err != nil {
				return nil, "", fmt.Errorf("normalize custom video resolution: %w", err)
			}
			// resolution_name is an input alias only; the transparent upstream
			// receives one canonical resolution field.
			out, err = sjson.DeleteBytes(out, "resolution_name")
			if err != nil {
				return nil, "", fmt.Errorf("remove custom video resolution_name: %w", err)
			}
			return out, contentType, nil
		}
		var err error
		for _, field := range []string{"resolution", "resolution_name"} {
			out, err = sjson.DeleteBytes(out, field)
			if err != nil {
				return nil, "", fmt.Errorf("remove custom video %s: %w", field, err)
			}
		}
		return out, contentType, nil
	}

	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return body, contentType, nil
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return body, contentType, nil
	}

	type multipartPart struct {
		header textproto.MIMEHeader
		data   []byte
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	parts := make([]multipartPart, 0)
	hadResolutionField := false
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			// Keep malformed multipart requests transparent; the upstream should
			// remain responsible for reporting their structural error.
			return body, contentType, nil
		}
		// The handler has already applied the aggregate request-body limit. Keep
		// each part byte-for-byte intact here so adding/removing resolution fields
		// cannot truncate an otherwise valid uploaded file.
		data, readErr := io.ReadAll(part)
		_ = part.Close()
		if readErr != nil {
			return body, contentType, nil
		}
		name := strings.TrimSpace(part.FormName())
		// Only text form fields are resolution controls. Preserve a file upload
		// whose field happens to use the same name as an unrelated vendor field.
		if part.FileName() == "" && (name == "resolution" || name == "resolution_name") {
			hadResolutionField = true
			continue
		}
		header := make(textproto.MIMEHeader, len(part.Header))
		for key, values := range part.Header {
			header[key] = append([]string(nil), values...)
		}
		parts = append(parts, multipartPart{header: header, data: data})
	}
	if !hadResolutionField {
		return body, contentType, nil
	}

	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	if err := writer.SetBoundary(boundary); err != nil {
		return body, contentType, nil
	}
	for _, part := range parts {
		created, createErr := writer.CreatePart(part.header)
		if createErr != nil {
			return body, contentType, nil
		}
		if _, writeErr := created.Write(part.data); writeErr != nil {
			return body, contentType, nil
		}
	}
	if info.ResolutionValid {
		if err := writer.WriteField("resolution", info.Resolution); err != nil {
			return body, contentType, nil
		}
	}
	if err := writer.Close(); err != nil {
		return body, contentType, nil
	}
	// SetBoundary above keeps the wire boundary stable. Return the caller's
	// original Content-Type string so charset/parameter formatting is retained.
	return out.Bytes(), contentType, nil
}

func normalizeCustomVideoRequestInfo(info GrokMediaRequestInfo) GrokMediaRequestInfo {
	info.DurationSeconds = NormalizeCustomVideoDurationSeconds(info.RawDurationSeconds)
	if !info.ResolutionValid {
		info.Resolution = ""
	}
	// Usage extraction is shared by Grok and Custom. Once a request is known to
	// be Custom, make the platform-specific empty/normalized value authoritative
	// for that shared path instead of retaining Grok's model fallback.
	info.GrokResolution = info.Resolution
	return info
}

func normalizeGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if !endpoint.RequiresRequestBody() || !gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	var imageFields []string
	switch endpoint {
	case GrokMediaEndpointImagesEdits:
		var aliases []string
		for _, field := range []string{"image_url", "image_urls"} {
			value := gjson.GetBytes(body, field)
			if !value.Exists() {
				continue
			}
			refs, err := parseImageInputReferences(value, field)
			if err != nil {
				return nil, "", err
			}
			aliases = append(aliases, refs...)
			body, err = sjson.DeleteBytes(body, field)
			if err != nil {
				return nil, "", err
			}
		}
		if len(aliases) > 0 {
			for _, field := range []string{"image", "images"} {
				value := gjson.GetBytes(body, field)
				if !value.Exists() {
					continue
				}
				refs, err := parseImageInputReferences(value, field)
				if err != nil {
					return nil, "", err
				}
				aliases = append(refs, aliases...)
				body, err = sjson.DeleteBytes(body, field)
				if err != nil {
					return nil, "", err
				}
			}
			var err error
			if len(aliases) == 1 {
				body, err = sjson.SetBytes(body, "image", aliases[0])
			} else {
				body, err = sjson.SetBytes(body, "images", aliases)
			}
			if err != nil {
				return nil, "", err
			}
		}
		imageFields = []string{"image", "images", "mask"}
	case GrokMediaEndpointVideosGenerations:
		imageFields = []string{"image", "images", "reference_images"}
	}
	var err error
	body, err = canonicalizeGrokMediaImageURLFields(body, imageFields...)
	if err != nil {
		return nil, "", err
	}
	info := ParseGrokMediaRequest(contentType, body)
	upstreamModel := NormalizeGrokMediaModelForEndpoint(endpoint, info.Model, info.HasInputImage())
	if upstreamModel == "" || upstreamModel == info.Model {
		return body, contentType, nil
	}
	out, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		return nil, "", fmt.Errorf("rewrite grok media model: %w", err)
	}
	return out, contentType, nil
}

func canonicalizeGrokMediaImageURLFields(body []byte, fields ...string) ([]byte, error) {
	out := body
	for _, field := range fields {
		value := gjson.GetBytes(out, field)
		if !value.Exists() {
			continue
		}
		if value.IsArray() {
			for index := range value.Array() {
				var err error
				out, err = canonicalizeGrokMediaImageURLObject(out, fmt.Sprintf("%s.%d", field, index))
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		var err error
		out, err = canonicalizeGrokMediaImageURLObject(out, field)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func canonicalizeGrokMediaImageURLObject(body []byte, path string) ([]byte, error) {
	value := gjson.GetBytes(body, path)
	if value.IsObject() && value.Get("file_id").Exists() {
		return nil, fmt.Errorf("%s.file_id is not supported (use image_url instead)", path)
	}
	if value.Type == gjson.String {
		out, err := sjson.SetBytes(body, path, map[string]string{"url": strings.TrimSpace(value.String())})
		if err != nil {
			return nil, fmt.Errorf("normalize grok media image url: %w", err)
		}
		return out, nil
	}
	legacyPath := path + ".image_url"
	legacy := gjson.GetBytes(body, legacyPath)
	if !legacy.Exists() {
		return body, nil
	}

	out := body
	if strings.TrimSpace(gjson.GetBytes(out, path+".url").String()) == "" {
		var err error
		out, err = sjson.SetBytes(out, path+".url", imageInputReferenceURL(value))
		if err != nil {
			return nil, fmt.Errorf("normalize grok media image url: %w", err)
		}
	}
	out, err := sjson.DeleteBytes(out, legacyPath)
	if err != nil {
		return nil, fmt.Errorf("remove legacy grok media image url: %w", err)
	}
	return out, nil
}

func sanitizeGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if !endpoint.RequiresRequestBody() || !gjson.ValidBytes(body) {
		return body, contentType, nil
	}
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		if !gjson.GetBytes(body, "size").Exists() {
			return body, contentType, nil
		}
		out, err := sjson.DeleteBytes(body, "size")
		if err != nil {
			return nil, "", fmt.Errorf("sanitize grok media size: %w", err)
		}
		return out, contentType, nil
	default:
		return body, contentType, nil
	}
}

func (r GrokMediaRequestInfo) HasInputImage() bool {
	return len(r.InputImageURLs) > 0 || len(r.Uploads) > 0
}

// NormalizeGrokMediaModelForEndpoint resolves the built-in upstream model alias
// for a media endpoint before account-level model mapping and scheduling.
func NormalizeGrokMediaModelForEndpoint(endpoint GrokMediaEndpoint, model string, hasInputImage bool) string {
	model = strings.TrimSpace(model)
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		if model == "grok-imagine" {
			return "grok-imagine-image-quality"
		}
	case GrokMediaEndpointVideosGenerations:
		if model == "grok-imagine-video-1.5" && !hasInputImage {
			return "grok-imagine-video"
		}
	}
	return model
}

type grokMediaUsageMetadata struct {
	ResponseID           string
	Usage                OpenAIUsage
	ImageCount           int
	ImageSize            string
	ImageInputSize       string
	ImageOutputSizes     []string
	VideoCount           int
	VideoResolution      string
	VideoDurationSeconds int
	VideoPriceMultiplier float64
}

func grokMediaUsageFromResponse(endpoint GrokMediaEndpoint, requestInfo GrokMediaRequestInfo, responseBody []byte) grokMediaUsageMetadata {
	usage, _ := extractOpenAIUsageFromJSONBytes(responseBody)
	meta := grokMediaUsageMetadata{Usage: usage}
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		meta.ImageCount = countOpenAIResponseImageOutputsFromJSONBytes(responseBody)
		meta.ImageSize = requestInfo.SizeTier
		meta.ImageInputSize = requestInfo.Size
		meta.ImageOutputSizes = collectOpenAIResponseImageOutputSizesFromJSONBytes(responseBody)
	case GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions:
		meta.ResponseID = extractGrokMediaVideoRequestID(responseBody)
		meta.VideoCount = requestInfo.N
		if meta.VideoCount <= 0 {
			meta.VideoCount = 1
		}
		if meta.VideoCount > maxBillableRequestCount {
			meta.VideoCount = maxBillableRequestCount
		}
		meta.VideoResolution = requestInfo.GrokResolution
		if meta.VideoResolution == "" {
			// Preserve compatibility for callers that construct request metadata
			// directly instead of going through ParseGrokMediaRequest.
			meta.VideoResolution = requestInfo.Resolution
		}
		meta.VideoDurationSeconds = requestInfo.DurationSeconds
		meta.VideoPriceMultiplier = VideoReferencePriceMultiplier(requestInfo.Model, requestInfo.HasReferenceVideo)
		// Keep the legacy media-unit counter populated for existing usage displays.
		meta.ImageCount = 1
	}
	return meta
}

func extractGrokMediaVideoRequestID(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range []string{"id", "data.id", "video.id", "request_id", "data.request_id", "video.request_id", "task_id", "data.task_id", "video.task_id"} {
		if id := strings.TrimSpace(gjson.GetBytes(body, path).String()); id != "" {
			return id
		}
	}
	return ""
}

func (s *OpenAIGatewayService) handleGrokMediaErrorResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestIDHeader string,
	requestedModel string,
) (result *OpenAIForwardResult, resultErr error) {
	body := s.readUpstreamErrorBody(resp)
	defer func() {
		if result == nil {
			usage, _ := extractOpenAIUsageFromJSONBytes(body)
			result = imageFailureUsageResult(usage, requestedModel, account.GetMappedModel(requestedModel))
		}
	}()
	// Reconcile Grok quota state before configurable passthrough branches can
	// return. Custom accounts use the normal OpenAI-compatible failover policy.
	if account.Platform == PlatformGrok {
		s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body)
	}
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("media upstream returned status %d", resp.StatusCode)
	}

	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
	if account.Platform == PlatformCustom {
		canonicalModel := canonicalOpenAIAccountSchedulingModel(account, requestedModel)
		shouldDisable := s.handleOpenAIAccountUpstreamError(
			ctx, account, resp.StatusCode, resp.Header, body, canonicalModel,
		)
		shouldFailover := shouldFailoverOpenAIPassthroughResponse(account, resp.StatusCode, body)
		kind := "http_error"
		if shouldFailover {
			kind = "failover"
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:             account.Platform,
			AccountID:            account.ID,
			AccountName:          account.Name,
			UpstreamStatusCode:   resp.StatusCode,
			UpstreamRequestID:    requestIDHeader,
			Passthrough:          true,
			Kind:                 kind,
			Message:              upstreamMsg,
			Detail:               upstreamDetail,
			UpstreamResponseBody: upstreamDetail,
		})
		if shouldFailover {
			failoverErr := newOpenAIUpstreamFailoverError(
				resp.StatusCode,
				resp.Header,
				body,
				upstreamMsg,
				!shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			)
			failoverErr.RawResponsePassthrough = true
			return nil, failoverErr
		}
		s.WriteCustomRawUpstreamResponse(c, resp.StatusCode, resp.Header, body)
		return nil, fmt.Errorf("custom media upstream error: %d", resp.StatusCode)
	}
	if account.Platform == PlatformGrok && isGrokContentPolicyRejection(resp.StatusCode, body) {
		clientMsg := grokContentPolicyClientMessage(body)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestIDHeader,
			Kind:               "http_error",
			Message:            clientMsg,
			Detail:             upstreamDetail,
		})
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, http.StatusForbidden, "invalid_request_error", clientMsg)
		return nil, fmt.Errorf("grok content policy rejection: %s", clientMsg)
	}

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c,
		account.Platform,
		resp.StatusCode,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, status, errType, errMsg)
		return nil, fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", resp.StatusCode, upstreamMsg)
	}

	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestIDHeader,
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})
		MarkResponseCommitted(c)
		writeGrokMediaErrorResponse(c, http.StatusInternalServerError, "upstream_error", "Upstream gateway error")
		return nil, fmt.Errorf("upstream error: %d (not in custom error codes) message=%s", resp.StatusCode, upstreamMsg)
	}

	kind := "http_error"
	shouldFailover := s.shouldFailoverUpstreamError(resp.StatusCode)
	if account.Platform == PlatformGrok {
		shouldFailover = s.shouldFailoverGrokUpstreamError(resp.StatusCode, body)
	}
	if shouldFailover {
		kind = "failover"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  requestIDHeader,
		Kind:               kind,
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	if kind == "failover" {
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           body,
			ResponseHeaders:        resp.Header.Clone(),
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
		}
	}

	MarkResponseCommitted(c)
	writeGrokMediaErrorResponse(c, resp.StatusCode, grokMediaErrorType(resp.StatusCode), upstreamMsg)
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func grokMediaErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "upstream_error"
	}
}

func writeGrokMediaErrorResponse(c *gin.Context, statusCode int, errType, message string) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    strings.TrimSpace(errType),
			"message": strings.TrimSpace(message),
		},
	})
}

func writeGrokMediaResponse(c *gin.Context, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if c == nil || resp == nil {
		return
	}
	writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, filter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, body)
}

// A 200 response alone cannot distinguish a complete download from a stream
// that failed after its headers were committed.
const GrokMediaContentCompleteKey = "grok_media_content_complete"

func writeGrokMediaContentResponse(c *gin.Context, resp *http.Response) error {
	if c == nil || resp == nil || resp.Body == nil {
		return fmt.Errorf("grok media content response is incomplete")
	}

	for _, name := range []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"Content-Disposition",
	} {
		if value := strings.TrimSpace(resp.Header.Get(name)); value != "" {
			c.Header(name, value)
		}
	}
	if strings.TrimSpace(c.Writer.Header().Get("Content-Length")) == "" && resp.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	if strings.TrimSpace(c.Writer.Header().Get("Content-Type")) == "" {
		c.Header("Content-Type", "application/octet-stream")
	}
	c.Status(resp.StatusCode)
	MarkResponseCommitted(c)
	_, err := io.Copy(c.Writer, resp.Body)
	if err == nil {
		c.Set(GrokMediaContentCompleteKey, true)
	}
	return err
}

func rewriteGrokMediaVideoContentURLs(body []byte, requestID, proxyURL string) []byte {
	if len(body) == 0 || strings.TrimSpace(requestID) == "" || strings.TrimSpace(proxyURL) == "" || !gjson.ValidBytes(body) {
		return body
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return body
	}
	changed := rewriteGrokMediaKnownVideoURL(&value, proxyURL)
	if rewriteGrokMediaVideoContentURLValue(&value, requestID, proxyURL) {
		changed = true
	}
	if !changed {
		return body
	}
	rewritten, err := json.Marshal(value)
	if err != nil {
		return body
	}
	return rewritten
}

func rewriteGrokMediaKnownVideoURL(value *any, proxyURL string) bool {
	if value == nil {
		return false
	}
	root, ok := (*value).(map[string]any)
	if !ok {
		return false
	}
	video, ok := root["video"].(map[string]any)
	if !ok {
		return false
	}
	rawURL, ok := video["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return false
	}
	video["url"] = proxyURL
	return true
}

func rewriteGrokMediaVideoContentURLValue(value *any, requestID, proxyURL string) bool {
	if value == nil {
		return false
	}
	switch typed := (*value).(type) {
	case map[string]any:
		changed := false
		for key, child := range typed {
			childValue := child
			if rewriteGrokMediaVideoContentURLValue(&childValue, requestID, proxyURL) {
				typed[key] = childValue
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for index, child := range typed {
			childValue := child
			if rewriteGrokMediaVideoContentURLValue(&childValue, requestID, proxyURL) {
				typed[index] = childValue
				changed = true
			}
		}
		return changed
	case string:
		if isGrokMediaVideoContentURL(typed, requestID) {
			*value = proxyURL
			return true
		}
	}
	return false
}

func isGrokMediaVideoContentURL(rawURL, requestID string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Path == "" {
		return false
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(segments) < 3 {
		return false
	}
	requestID = strings.Trim(requestID, "/")
	decodedID, err := url.PathUnescape(segments[len(segments)-2])
	if err != nil {
		return false
	}
	return segments[len(segments)-3] == "videos" &&
		decodedID == requestID &&
		segments[len(segments)-1] == "content"
}

func grokMediaContentProxyURL(c *gin.Context, requestID string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || strings.TrimSpace(requestID) == "" {
		return ""
	}
	pathPrefix := ""
	if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
		pathPrefix = "/v1"
	}
	return pathPrefix + "/videos/" + url.PathEscape(strings.Trim(requestID, "/")) + "/content"
}
