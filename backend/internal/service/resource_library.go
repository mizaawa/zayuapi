package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// The resource library lets administrators review image/video workbench
// results of every user and preserve selected results beyond retention.

const (
	ResourceKindImage = "image"
	ResourceKindVideo = "video"

	// Each kind enumerates at most this many of the latest retained results.
	ResourceLibraryLiveLimit = 5000

	resourceLibraryMaxImageBytes int64 = 64 << 20
	resourceLibraryMaxVideoBytes int64 = 1 << 30
	resourceLibraryMaxPageSize         = 100
)

var (
	ErrResourceLibraryDisabled    = infraerrors.New(http.StatusNotFound, "RESOURCE_LIBRARY_DISABLED", "resource library is disabled")
	ErrResourceNotFound           = infraerrors.New(http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found")
	ErrResourceLibraryUnavailable = infraerrors.New(http.StatusServiceUnavailable, "RESOURCE_LIBRARY_UNAVAILABLE", "resource library storage is unavailable")
	ErrResourceInvalidKind        = infraerrors.New(http.StatusBadRequest, "INVALID_RESOURCE_KIND", "kind must be image or video")
)

// ResourceLibraryItem is one retained workbench result. Prompts and owner
// identities are intentionally returned in full to administrators.
type ResourceLibraryItem struct {
	Kind         string         `json:"kind"`
	TaskID       string         `json:"task_id"`
	Index        int            `json:"index"`
	Name         string         `json:"name"`
	UserID       int64          `json:"user_id"`
	UserEmail    string         `json:"user_email"`
	APIKeyID     int64          `json:"api_key_id"`
	GroupID      int64          `json:"group_id"`
	GroupName    string         `json:"group_name"`
	Model        string         `json:"model"`
	Prompt       string         `json:"prompt"`
	SizeBytes    int64          `json:"size_bytes"`
	ContentType  string         `json:"content_type"`
	CreatedAt    int64          `json:"created_at"`
	CompletedAt  int64          `json:"completed_at,omitempty"`
	ExpiresAt    int64          `json:"expires_at"`
	BillingID    string         `json:"billing_id,omitempty"`
	Cost         *float64       `json:"cost"`
	BillingState string         `json:"billing_state,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
	SavedID      string         `json:"saved_id,omitempty"`
}

// ResourceLibrarySaved is an administrator-preserved copy of a result.
type ResourceLibrarySaved struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	SourceTaskID    string         `json:"source_task_id"`
	SourceIndex     int            `json:"source_index"`
	Name            string         `json:"name"`
	ContentType     string         `json:"content_type"`
	SizeBytes       int64          `json:"size_bytes"`
	SHA256          string         `json:"sha256"`
	UserID          int64          `json:"user_id"`
	UserEmail       string         `json:"user_email"`
	APIKeyID        int64          `json:"api_key_id"`
	GroupID         int64          `json:"group_id"`
	GroupName       string         `json:"group_name"`
	Model           string         `json:"model"`
	Prompt          string         `json:"prompt"`
	BillingID       string         `json:"billing_id,omitempty"`
	Cost            *float64       `json:"cost"`
	Details         map[string]any `json:"details,omitempty"`
	GeneratedAt     time.Time      `json:"generated_at"`
	SourceExpiresAt *time.Time     `json:"source_expires_at,omitempty"`
	SavedBy         int64          `json:"saved_by"`
	SavedByEmail    string         `json:"saved_by_email"`
	CreatedAt       time.Time      `json:"created_at"`
}

type ResourceLibraryFilter struct {
	Kind     string
	Model    string
	GroupID  int64
	Query    string
	Page     int
	PageSize int
}

type ResourceLibraryGroupOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ResourceLibraryListResult struct {
	Items     []*ResourceLibraryItem       `json:"items"`
	Total     int64                        `json:"total"`
	Page      int                          `json:"page"`
	PageSize  int                          `json:"page_size"`
	Pages     int                          `json:"pages"`
	Models    []string                     `json:"models"`
	Groups    []ResourceLibraryGroupOption `json:"groups"`
	Truncated bool                         `json:"truncated"`
}

type ResourceLibrarySavedListResult struct {
	Items    []*ResourceLibrarySaved      `json:"items"`
	Total    int64                        `json:"total"`
	Page     int                          `json:"page"`
	PageSize int                          `json:"page_size"`
	Pages    int                          `json:"pages"`
	Models   []string                     `json:"models"`
	Groups   []ResourceLibraryGroupOption `json:"groups"`
}

// ResourceLibraryBilling is the billing outcome of a media reservation.
type ResourceLibraryBilling struct {
	State   string
	Amount  float64
	GroupID int64
}

type ResourceLibraryRepository interface {
	UserEmails(ctx context.Context, ids []int64) (map[int64]string, error)
	APIKeyGroups(ctx context.Context, ids []int64) (map[int64]int64, error)
	GroupNames(ctx context.Context, ids []int64) (map[int64]string, error)
	Billing(ctx context.Context, ids []string) (map[string]ResourceLibraryBilling, error)
	SavedSources(ctx context.Context, taskIDs []string) (map[string]string, error)
	CreateSaved(ctx context.Context, item *ResourceLibrarySaved) (bool, error)
	GetSaved(ctx context.Context, id string) (*ResourceLibrarySaved, error)
	GetSavedBySource(ctx context.Context, taskID string, index int) (*ResourceLibrarySaved, error)
	ListSaved(ctx context.Context, filter ResourceLibraryFilter) ([]*ResourceLibrarySaved, int64, error)
	SavedFacets(ctx context.Context) ([]string, []ResourceLibraryGroupOption, error)
	DeleteSaved(ctx context.Context, id string) (bool, error)
}

// ResourceLibraryStorage keeps preserved files outside workbench retention.
type ResourceLibraryStorage interface {
	Ready(ctx context.Context) error
	Write(ctx context.Context, id string, content io.Reader, maxBytes int64) (int64, string, error)
	Open(ctx context.Context, id string) (io.ReadSeekCloser, error)
	Delete(ctx context.Context, id string) error
}

// ResourceContent is a readable resource body with its download identity.
type ResourceContent struct {
	Body        io.ReadSeeker
	Closer      io.Closer
	Name        string
	ContentType string
	ModTime     time.Time
}

func (c *ResourceContent) Close() error {
	if c == nil || c.Closer == nil {
		return nil
	}
	return c.Closer.Close()
}

type ResourceLibraryService struct {
	enabled func(context.Context) bool
	images  *ImageTaskService
	videos  *VideoWorkbenchService
	repo    ResourceLibraryRepository
	storage ResourceLibraryStorage
	now     func() time.Time
}

func NewResourceLibraryService(settings *SettingService, images *ImageTaskService, videos *VideoWorkbenchService, repo ResourceLibraryRepository, storage ResourceLibraryStorage) *ResourceLibraryService {
	return NewResourceLibraryServiceWith(settings.IsResourceLibraryEnabled, images, videos, repo, storage)
}

func NewResourceLibraryServiceWith(enabled func(context.Context) bool, images *ImageTaskService, videos *VideoWorkbenchService, repo ResourceLibraryRepository, storage ResourceLibraryStorage) *ResourceLibraryService {
	return &ResourceLibraryService{enabled: enabled, images: images, videos: videos, repo: repo, storage: storage, now: time.Now}
}

func (s *ResourceLibraryService) Enabled(ctx context.Context) bool {
	return s != nil && s.enabled != nil && s.enabled(ctx)
}

func (s *ResourceLibraryService) ready(ctx context.Context) error {
	if !s.Enabled(ctx) {
		return ErrResourceLibraryDisabled
	}
	if s.repo == nil {
		return ErrResourceLibraryUnavailable
	}
	return nil
}

func NormalizeResourceKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "":
		return "", nil
	case ResourceKindImage:
		return ResourceKindImage, nil
	case ResourceKindVideo:
		return ResourceKindVideo, nil
	default:
		return "", ErrResourceInvalidKind
	}
}

func normalizeResourceFilter(filter ResourceLibraryFilter) (ResourceLibraryFilter, error) {
	kind, err := NormalizeResourceKind(filter.Kind)
	if err != nil {
		return filter, err
	}
	filter.Kind = kind
	filter.Model = strings.TrimSpace(filter.Model)
	filter.Query = strings.TrimSpace(filter.Query)
	if len(filter.Query) > 256 {
		filter.Query = filter.Query[:256]
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > resourceLibraryMaxPageSize {
		filter.PageSize = resourceLibraryMaxPageSize
	}
	return filter, nil
}

func resourcePages(total int64, pageSize int) int {
	pages := int(math.Ceil(float64(total) / float64(pageSize)))
	if pages < 1 {
		return 1
	}
	return pages
}

// Upstream image responses may omit the byte size; it is never negative.
func resourceSize(size int64) int64 {
	if size < 0 {
		return 0
	}
	return size
}

func resourceImageName(taskID string, index int, contentType string) string {
	return taskID + "-" + strconv.Itoa(index+1) + extensionForContentType(contentType)
}

func resourceVideoName(taskID, contentType string) string {
	switch contentType {
	case "video/webm":
		return taskID + ".webm"
	case "video/quicktime":
		return taskID + ".mov"
	default:
		return taskID + ".mp4"
	}
}

func imageResourceItems(task *ImageTaskRecord) []*ResourceLibraryItem {
	var result imageWorkbenchResult
	if task == nil || task.Workbench == nil || json.Unmarshal(task.Result, &result) != nil {
		return nil
	}
	items := make([]*ResourceLibraryItem, 0, len(result.Data))
	for index, data := range result.Data {
		if data.URL == "" || !supportedImageContentType(data.ContentType) {
			continue
		}
		item := &ResourceLibraryItem{
			Kind: ResourceKindImage, TaskID: task.ID, Index: index,
			Name:   resourceImageName(task.ID, index, data.ContentType),
			UserID: task.UserID, UserEmail: task.UserEmail, APIKeyID: task.APIKeyID,
			GroupID: task.GroupID, Model: task.Workbench.Model, Prompt: task.Workbench.Prompt,
			SizeBytes: resourceSize(data.SizeBytes), ContentType: data.ContentType,
			CreatedAt: task.CreatedAt, ExpiresAt: task.ExpiresAt, BillingID: task.BillingID,
			Details: map[string]any{"quality": task.Workbench.Quality, "size": task.Workbench.Size, "count": task.Workbench.Count},
		}
		if task.CompletedAt != nil {
			item.CompletedAt = *task.CompletedAt
		}
		items = append(items, item)
	}
	return items
}

func videoResourceItem(task *VideoWorkbenchRecord) *ResourceLibraryItem {
	if task == nil {
		return nil
	}
	w := task.Workbench
	return &ResourceLibraryItem{
		Kind: ResourceKindVideo, TaskID: task.ID, Name: resourceVideoName(task.ID, task.ContentType),
		UserID: task.UserID, APIKeyID: w.APIKeyID, GroupID: task.GroupID, Model: w.Model, Prompt: w.Prompt,
		SizeBytes: task.SizeBytes, ContentType: task.ContentType, CreatedAt: task.CreatedAt,
		CompletedAt: task.CompletedAt, ExpiresAt: task.ExpiresAt, BillingID: task.BillingID,
		Details: map[string]any{
			"ratio": w.Ratio, "resolution": w.Resolution, "duration": w.Duration,
			"reference_images": w.ReferenceImages, "reference_videos": w.ReferenceVideos, "reference_audios": w.ReferenceAudios,
		},
	}
}

// liveItems enumerates retained results of both workbenches.
func (s *ResourceLibraryService) liveItems(ctx context.Context, kind string) ([]*ResourceLibraryItem, bool, error) {
	now := s.now().UTC()
	items := []*ResourceLibraryItem{}
	truncated := false
	if s.images != nil && kind != ResourceKindVideo {
		tasks, err := s.images.AdminListWorkbench(ctx, now, ResourceLibraryLiveLimit)
		if err != nil {
			return nil, false, err
		}
		truncated = truncated || len(tasks) >= ResourceLibraryLiveLimit
		for _, task := range tasks {
			items = append(items, imageResourceItems(task)...)
		}
	}
	if s.videos != nil && kind != ResourceKindImage {
		tasks, err := s.videos.AdminList(ctx, now, ResourceLibraryLiveLimit)
		if err != nil {
			return nil, false, err
		}
		truncated = truncated || len(tasks) >= ResourceLibraryLiveLimit
		for _, task := range tasks {
			items = append(items, videoResourceItem(task))
		}
	}
	return items, truncated, nil
}

// enrich resolves owner emails, groups, billing outcomes and saved copies.
func (s *ResourceLibraryService) enrich(ctx context.Context, items []*ResourceLibraryItem) error {
	if len(items) == 0 {
		return nil
	}
	userIDs, keyIDs, billingIDs, taskIDs := []int64{}, []int64{}, []string{}, []string{}
	for _, item := range items {
		if item.UserEmail == "" && item.UserID > 0 {
			userIDs = append(userIDs, item.UserID)
		}
		if item.GroupID <= 0 && item.APIKeyID > 0 {
			keyIDs = append(keyIDs, item.APIKeyID)
		}
		if item.BillingID != "" {
			billingIDs = append(billingIDs, item.BillingID)
		}
		taskIDs = append(taskIDs, item.TaskID)
	}
	emails, err := s.repo.UserEmails(ctx, uniqueInt64s(userIDs))
	if err != nil {
		return ErrResourceLibraryUnavailable.WithCause(err)
	}
	billing, err := s.repo.Billing(ctx, uniqueStrings(billingIDs))
	if err != nil {
		return ErrResourceLibraryUnavailable.WithCause(err)
	}
	keyGroups, err := s.repo.APIKeyGroups(ctx, uniqueInt64s(keyIDs))
	if err != nil {
		return ErrResourceLibraryUnavailable.WithCause(err)
	}
	saved, err := s.repo.SavedSources(ctx, uniqueStrings(taskIDs))
	if err != nil {
		return ErrResourceLibraryUnavailable.WithCause(err)
	}
	groupIDs := []int64{}
	for _, item := range items {
		if item.UserEmail == "" {
			item.UserEmail = emails[item.UserID]
		}
		if outcome, ok := billing[item.BillingID]; ok && item.BillingID != "" {
			cost := outcome.Amount
			item.BillingState = outcome.State
			if outcome.State == "refunded" {
				cost = 0
			}
			item.Cost = &cost
			if item.GroupID <= 0 {
				item.GroupID = outcome.GroupID
			}
		}
		if item.GroupID <= 0 {
			item.GroupID = keyGroups[item.APIKeyID]
		}
		if item.GroupID > 0 {
			groupIDs = append(groupIDs, item.GroupID)
		}
		item.SavedID = saved[ResourceSourceKey(item.TaskID, item.Index)]
	}
	names, err := s.repo.GroupNames(ctx, uniqueInt64s(groupIDs))
	if err != nil {
		return ErrResourceLibraryUnavailable.WithCause(err)
	}
	for _, item := range items {
		item.GroupName = names[item.GroupID]
	}
	return nil
}

// ResourceSourceKey identifies one result inside a workbench task.
func ResourceSourceKey(taskID string, index int) string {
	return taskID + "#" + strconv.Itoa(index)
}

func resourceMatches(item *ResourceLibraryItem, filter ResourceLibraryFilter) bool {
	if filter.Kind != "" && item.Kind != filter.Kind {
		return false
	}
	if filter.Model != "" && item.Model != filter.Model {
		return false
	}
	if filter.GroupID > 0 && item.GroupID != filter.GroupID {
		return false
	}
	if filter.Query == "" {
		return true
	}
	query := strings.ToLower(filter.Query)
	for _, field := range []string{item.Name, item.TaskID, item.UserEmail, item.Prompt, item.Model} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// List pages retained results newest first, with facets for the filters.
func (s *ResourceLibraryService) List(ctx context.Context, filter ResourceLibraryFilter) (*ResourceLibraryListResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	filter, err := normalizeResourceFilter(filter)
	if err != nil {
		return nil, err
	}
	items, truncated, err := s.liveItems(ctx, filter.Kind)
	if err != nil {
		return nil, err
	}
	if err := s.enrich(ctx, items); err != nil {
		return nil, err
	}
	models, groups := map[string]bool{}, map[int64]string{}
	matched := make([]*ResourceLibraryItem, 0, len(items))
	for _, item := range items {
		if item.Model != "" {
			models[item.Model] = true
		}
		if item.GroupID > 0 {
			groups[item.GroupID] = item.GroupName
		}
		if resourceMatches(item, filter) {
			matched = append(matched, item)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].CreatedAt != matched[j].CreatedAt {
			return matched[i].CreatedAt > matched[j].CreatedAt
		}
		if matched[i].TaskID != matched[j].TaskID {
			return matched[i].TaskID > matched[j].TaskID
		}
		return matched[i].Index < matched[j].Index
	})
	total := int64(len(matched))
	start := min((filter.Page-1)*filter.PageSize, len(matched))
	end := min(start+filter.PageSize, len(matched))
	return &ResourceLibraryListResult{
		Items: matched[start:end], Total: total, Page: filter.Page, PageSize: filter.PageSize,
		Pages: resourcePages(total, filter.PageSize), Models: sortedResourceModels(models),
		Groups: sortedGroupOptions(groups), Truncated: truncated,
	}, nil
}

func sortedResourceModels(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedGroupOptions(groups map[int64]string) []ResourceLibraryGroupOption {
	result := make([]ResourceLibraryGroupOption, 0, len(groups))
	for id, name := range groups {
		result = append(result, ResourceLibraryGroupOption{ID: id, Name: name})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]bool, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value > 0 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func isResourceNotFound(err error) bool {
	return errors.Is(err, ErrImageTaskNotFound) || errors.Is(err, ErrVideoTaskNotFound) || errors.Is(err, ErrResourceNotFound)
}
