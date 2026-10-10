package service

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type ImageWorkbenchRuntime struct {
	Enabled                bool
	MaxConcurrent          int
	AdminExempt            bool
	CustomRetentionEnabled bool
	RetentionMinutes       int
	TutorialURL            string
}

func (r ImageWorkbenchRuntime) Limit(isAdmin bool) int {
	if isAdmin && r.AdminExempt {
		return 0
	}
	return r.MaxConcurrent
}

func (r ImageWorkbenchRuntime) Retention() time.Duration {
	return time.Duration(r.RetentionMinutes) * time.Minute
}

var imageWorkbenchSettingKeys = []string{
	SettingKeyImageWorkbenchEnabled, SettingKeyImageWorkbenchMaxConcurrent,
	SettingKeyImageWorkbenchAdminExempt, SettingKeyImageWorkbenchCustomRetentionEnabled,
	SettingKeyImageWorkbenchRetentionMinutes, SettingKeyImageWorkbenchTutorialURL,
}

func parseImageWorkbenchNumber(raw string, fallback, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > max {
		return fallback
	}
	return n
}

func imageWorkbenchNumberOrDefault(n, fallback int) int {
	if n == 0 {
		return fallback
	}
	return n
}

func parseImageWorkbenchRuntime(vals map[string]string) ImageWorkbenchRuntime {
	r := ImageWorkbenchRuntime{
		Enabled:                !isFalseSettingValue(vals[SettingKeyImageWorkbenchEnabled]),
		MaxConcurrent:          parseImageWorkbenchNumber(vals[SettingKeyImageWorkbenchMaxConcurrent], ImageWorkbenchMaxConcurrent, 100),
		AdminExempt:            vals[SettingKeyImageWorkbenchAdminExempt] == "true",
		CustomRetentionEnabled: vals[SettingKeyImageWorkbenchCustomRetentionEnabled] == "true",
		RetentionMinutes:       15,
	}
	if r.CustomRetentionEnabled {
		r.RetentionMinutes = parseImageWorkbenchNumber(vals[SettingKeyImageWorkbenchRetentionMinutes], 15, 1440)
	}
	if raw := strings.TrimSpace(vals[SettingKeyImageWorkbenchTutorialURL]); validImageWorkbenchTutorialURL(raw) {
		r.TutorialURL = raw
	}
	return r
}

// Read directly so access checks and new task deadlines follow saved settings.
func (s *SettingService) GetImageWorkbenchRuntime(ctx context.Context) ImageWorkbenchRuntime {
	vals, err := s.settingRepo.GetMultiple(ctx, imageWorkbenchSettingKeys)
	r := parseImageWorkbenchRuntime(vals)
	if err != nil {
		r.Enabled = false
	}
	return r
}

func validImageWorkbenchTutorialURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && len(raw) <= 2048
}

func ValidateImageWorkbenchSettings(settings *SystemSettings) error {
	if n := settings.ImageWorkbenchMaxConcurrent; n < 0 || n > 100 {
		return infraerrors.BadRequest("INVALID_IMAGE_WORKBENCH_CONCURRENCY", "image workbench concurrency must be between 1 and 100")
	}
	if n := settings.ImageWorkbenchRetentionMinutes; n < 0 || n > 1440 {
		return infraerrors.BadRequest("INVALID_IMAGE_WORKBENCH_RETENTION", "image workbench retention must be between 1 and 1440 minutes")
	}
	if !validImageWorkbenchTutorialURL(strings.TrimSpace(settings.ImageWorkbenchTutorialURL)) {
		return infraerrors.BadRequest("INVALID_IMAGE_WORKBENCH_TUTORIAL_URL", "image workbench tutorial must be an HTTP or HTTPS URL")
	}
	return nil
}
