package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type VideoWorkbenchRuntime struct {
	Enabled            bool   `json:"enabled"`
	MaxConcurrent      int    `json:"max_concurrent"`
	MaxReferenceImages int    `json:"max_reference_images"`
	MaxReferenceVideos int    `json:"max_reference_videos"`
	MaxReferenceAudios int    `json:"max_reference_audios"`
	RetentionMinutes   int    `json:"retention_minutes"`
	TutorialURL        string `json:"tutorial_url"`
	DocumentationURL   string `json:"documentation_url"`
	Announcement       string `json:"announcement"`
}

var videoWorkbenchSettingKeys = []string{
	SettingKeyVideoWorkbenchEnabled, SettingKeyVideoWorkbenchMaxConcurrent,
	SettingKeyVideoWorkbenchMaxReferenceImages, SettingKeyVideoWorkbenchMaxReferenceVideos,
	SettingKeyVideoWorkbenchMaxReferenceAudios, SettingKeyVideoWorkbenchRetentionMinutes,
	SettingKeyVideoWorkbenchTutorialURL, SettingKeyVideoWorkbenchDocumentationURL,
	SettingKeyVideoWorkbenchAnnouncement,
}

func parseVideoWorkbenchRuntime(vals map[string]string) VideoWorkbenchRuntime {
	r := VideoWorkbenchRuntime{
		Enabled:            vals[SettingKeyVideoWorkbenchEnabled] == "true",
		MaxConcurrent:      parseImageWorkbenchNumber(vals[SettingKeyVideoWorkbenchMaxConcurrent], 3, 100),
		MaxReferenceImages: parseImageWorkbenchNumber(vals[SettingKeyVideoWorkbenchMaxReferenceImages], 6, 100),
		MaxReferenceVideos: parseImageWorkbenchNumber(vals[SettingKeyVideoWorkbenchMaxReferenceVideos], 6, 100),
		MaxReferenceAudios: parseImageWorkbenchNumber(vals[SettingKeyVideoWorkbenchMaxReferenceAudios], 6, 100),
		RetentionMinutes:   parseImageWorkbenchNumber(vals[SettingKeyVideoWorkbenchRetentionMinutes], 30, 10080),
		Announcement:       strings.TrimSpace(vals[SettingKeyVideoWorkbenchAnnouncement]),
	}
	for key, target := range map[string]*string{
		SettingKeyVideoWorkbenchTutorialURL:      &r.TutorialURL,
		SettingKeyVideoWorkbenchDocumentationURL: &r.DocumentationURL,
	} {
		if raw := strings.TrimSpace(vals[key]); validImageWorkbenchTutorialURL(raw) {
			*target = raw
		}
	}
	return r
}

func (s *SettingService) GetVideoWorkbenchRuntime(ctx context.Context) VideoWorkbenchRuntime {
	vals, err := s.settingRepo.GetMultiple(ctx, videoWorkbenchSettingKeys)
	r := parseVideoWorkbenchRuntime(vals)
	if err != nil {
		r.Enabled = false
	}
	return r
}

func (r VideoWorkbenchRuntime) Retention() time.Duration {
	return time.Duration(r.RetentionMinutes) * time.Minute
}

func ValidateVideoWorkbenchSettings(s *SystemSettings) error {
	for _, n := range []int{s.VideoWorkbenchMaxConcurrent, s.VideoWorkbenchMaxReferenceImages, s.VideoWorkbenchMaxReferenceVideos, s.VideoWorkbenchMaxReferenceAudios} {
		if n < 0 || n > 100 {
			return infraerrors.BadRequest("INVALID_VIDEO_WORKBENCH_LIMIT", "video workbench limits must be between 1 and 100")
		}
	}
	if n := s.VideoWorkbenchRetentionMinutes; n < 0 || n > 10080 {
		return infraerrors.BadRequest("INVALID_VIDEO_WORKBENCH_RETENTION", "video retention must be between 1 and 10080 minutes")
	}
	for _, raw := range []string{s.VideoWorkbenchTutorialURL, s.VideoWorkbenchDocumentationURL} {
		if !validImageWorkbenchTutorialURL(strings.TrimSpace(raw)) {
			return infraerrors.BadRequest("INVALID_VIDEO_WORKBENCH_URL", "video workbench links must be HTTP or HTTPS URLs")
		}
	}
	if len([]rune(s.VideoWorkbenchAnnouncement)) > 4000 {
		return infraerrors.BadRequest("INVALID_VIDEO_WORKBENCH_ANNOUNCEMENT", "video workbench announcement exceeds 4000 characters")
	}
	return nil
}
