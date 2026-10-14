package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func resourceError(err error) error {
	if err == nil {
		return nil
	}
	if isResourceNotFound(err) {
		return ErrResourceNotFound
	}
	return err
}

func requireResourceKind(kind string) (string, error) {
	kind, err := NormalizeResourceKind(kind)
	if err != nil {
		return "", err
	}
	if kind == "" {
		return "", ErrResourceInvalidKind
	}
	return kind, nil
}

// item loads one completed live result with resolved provenance.
func (s *ResourceLibraryService) item(ctx context.Context, kind, taskID string, index int) (*ResourceLibraryItem, error) {
	var item *ResourceLibraryItem
	switch kind {
	case ResourceKindImage:
		if s.images == nil {
			return nil, ErrResourceLibraryUnavailable
		}
		task, err := s.images.AdminGetWorkbench(ctx, taskID)
		if err != nil {
			return nil, resourceError(err)
		}
		if task.Status != ImageTaskStatusCompleted {
			return nil, ErrResourceNotFound
		}
		for _, candidate := range imageResourceItems(task) {
			if candidate.Index == index {
				item = candidate
				break
			}
		}
	case ResourceKindVideo:
		if s.videos == nil {
			return nil, ErrResourceLibraryUnavailable
		}
		task, err := s.videos.AdminGet(ctx, taskID)
		if err != nil {
			return nil, resourceError(err)
		}
		if task.Status == "completed" && index == 0 {
			item = videoResourceItem(task)
		}
	default:
		return nil, ErrResourceInvalidKind
	}
	if item == nil {
		return nil, ErrResourceNotFound
	}
	if err := s.enrich(ctx, []*ResourceLibraryItem{item}); err != nil {
		return nil, err
	}
	return item, nil
}

// Get returns one live result.
func (s *ResourceLibraryService) Get(ctx context.Context, kind, taskID string, index int) (*ResourceLibraryItem, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	kind, err := requireResourceKind(kind)
	if err != nil {
		return nil, err
	}
	return s.item(ctx, kind, strings.TrimSpace(taskID), index)
}

// Content opens a live result. Locations are derived from the stored task.
func (s *ResourceLibraryService) Content(ctx context.Context, kind, taskID string, index int) (*ResourceContent, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	kind, err := requireResourceKind(kind)
	if err != nil {
		return nil, err
	}
	return s.content(ctx, kind, strings.TrimSpace(taskID), index)
}

func (s *ResourceLibraryService) content(ctx context.Context, kind, taskID string, index int) (*ResourceContent, error) {
	switch kind {
	case ResourceKindImage:
		if s.images == nil {
			return nil, ErrResourceLibraryUnavailable
		}
		task, err := s.images.AdminGetWorkbench(ctx, taskID)
		if err != nil {
			return nil, resourceError(err)
		}
		data, contentType, err := s.images.AdminReadWorkbenchImage(ctx, task, index)
		if err != nil {
			return nil, resourceError(err)
		}
		return &ResourceContent{
			Body: bytes.NewReader(data), Name: resourceImageName(task.ID, index, contentType),
			ContentType: contentType, ModTime: time.Unix(task.CreatedAt, 0),
		}, nil
	case ResourceKindVideo:
		if s.videos == nil {
			return nil, ErrResourceLibraryUnavailable
		}
		if index != 0 {
			return nil, ErrResourceNotFound
		}
		file, task, err := s.videos.AdminOpen(ctx, taskID)
		if err != nil {
			return nil, resourceError(err)
		}
		contentType := task.ContentType
		if contentType == "" {
			contentType = "video/mp4"
		}
		return &ResourceContent{
			Body: file, Closer: file, Name: resourceVideoName(task.ID, contentType),
			ContentType: contentType, ModTime: time.Unix(task.CreatedAt, 0),
		}, nil
	default:
		return nil, ErrResourceInvalidKind
	}
}

// Delete removes a live result of any user from the workbench and its storage.
func (s *ResourceLibraryService) Delete(ctx context.Context, kind, taskID string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	kind, err := requireResourceKind(kind)
	if err != nil {
		return err
	}
	taskID = strings.TrimSpace(taskID)
	switch kind {
	case ResourceKindImage:
		if s.images == nil {
			return ErrResourceLibraryUnavailable
		}
		return resourceError(s.images.AdminDeleteWorkbench(ctx, taskID))
	default:
		if s.videos == nil {
			return ErrResourceLibraryUnavailable
		}
		return resourceError(s.videos.AdminDelete(ctx, taskID))
	}
}

func newResourceSavedID() string {
	id := uuid.New()
	return "res_" + hex.EncodeToString(id[:])
}

// ValidResourceSavedID reports whether id has the server-generated form.
func ValidResourceSavedID(id string) bool {
	if len(id) != 36 || !strings.HasPrefix(id, "res_") {
		return false
	}
	for _, c := range id[4:] {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

// Save preserves a live result. Saving the same result again returns the
// existing copy (created=false) instead of duplicating its file.
func (s *ResourceLibraryService) Save(ctx context.Context, actorID int64, kind, taskID string, index int) (*ResourceLibrarySaved, bool, error) {
	if err := s.ready(ctx); err != nil {
		return nil, false, err
	}
	if s.storage == nil {
		return nil, false, ErrResourceLibraryUnavailable
	}
	kind, err := requireResourceKind(kind)
	if err != nil {
		return nil, false, err
	}
	taskID = strings.TrimSpace(taskID)
	existing, err := s.repo.GetSavedBySource(ctx, taskID, index)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrResourceNotFound) {
		return nil, false, ErrResourceLibraryUnavailable.WithCause(err)
	}
	item, err := s.item(ctx, kind, taskID, index)
	if err != nil {
		return nil, false, err
	}
	content, err := s.content(ctx, kind, taskID, index)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = content.Close() }()
	if err := s.storage.Ready(ctx); err != nil {
		return nil, false, ErrResourceLibraryUnavailable.WithCause(err)
	}
	limit := resourceLibraryMaxImageBytes
	if kind == ResourceKindVideo {
		limit = resourceLibraryMaxVideoBytes
	}
	id := newResourceSavedID()
	size, digest, err := s.storage.Write(ctx, id, content.Body, limit)
	if err != nil {
		return nil, false, ErrResourceLibraryUnavailable.WithCause(err)
	}
	actorEmail := ""
	if emails, err := s.repo.UserEmails(ctx, []int64{actorID}); err == nil {
		actorEmail = emails[actorID]
	}
	details := map[string]any{}
	for key, value := range item.Details {
		details[key] = value
	}
	details["completed_at"] = item.CompletedAt
	details["billing_state"] = item.BillingState
	expires := time.Unix(item.ExpiresAt, 0).UTC()
	saved := &ResourceLibrarySaved{
		ID: id, Kind: kind, SourceTaskID: item.TaskID, SourceIndex: item.Index, Name: item.Name,
		ContentType: content.ContentType, SizeBytes: size, SHA256: digest,
		UserID: item.UserID, UserEmail: item.UserEmail, APIKeyID: item.APIKeyID,
		GroupID: item.GroupID, GroupName: item.GroupName, Model: item.Model, Prompt: item.Prompt,
		BillingID: item.BillingID, Cost: item.Cost, Details: details,
		GeneratedAt: time.Unix(item.CreatedAt, 0).UTC(), SourceExpiresAt: &expires,
		SavedBy: actorID, SavedByEmail: actorEmail, CreatedAt: s.now().UTC(),
	}
	created, err := s.repo.CreateSaved(ctx, saved)
	if err != nil || !created {
		s.removeSavedFile(id)
	}
	if err != nil {
		return nil, false, ErrResourceLibraryUnavailable.WithCause(err)
	}
	if !created {
		existing, err = s.repo.GetSavedBySource(ctx, taskID, index)
		if err != nil {
			return nil, false, ErrResourceLibraryUnavailable.WithCause(err)
		}
		return existing, false, nil
	}
	return saved, true, nil
}

func (s *ResourceLibraryService) removeSavedFile(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.storage.Delete(ctx, id); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logger.L().Warn("resource_library.file_cleanup_failed", zap.String("id", id), zap.Error(err))
	}
}

// ListSaved pages preserved copies newest first.
func (s *ResourceLibraryService) ListSaved(ctx context.Context, filter ResourceLibraryFilter) (*ResourceLibrarySavedListResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	filter, err := normalizeResourceFilter(filter)
	if err != nil {
		return nil, err
	}
	items, total, err := s.repo.ListSaved(ctx, filter)
	if err != nil {
		return nil, ErrResourceLibraryUnavailable.WithCause(err)
	}
	models, groups, err := s.repo.SavedFacets(ctx)
	if err != nil {
		return nil, ErrResourceLibraryUnavailable.WithCause(err)
	}
	return &ResourceLibrarySavedListResult{
		Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize,
		Pages: resourcePages(total, filter.PageSize), Models: models, Groups: groups,
	}, nil
}

// GetSaved returns one preserved copy.
func (s *ResourceLibraryService) GetSaved(ctx context.Context, id string) (*ResourceLibrarySaved, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if !ValidResourceSavedID(id) {
		return nil, ErrResourceNotFound
	}
	saved, err := s.repo.GetSaved(ctx, id)
	if errors.Is(err, ErrResourceNotFound) {
		return nil, ErrResourceNotFound
	}
	if err != nil {
		return nil, ErrResourceLibraryUnavailable.WithCause(err)
	}
	return saved, nil
}

// SavedContent opens the preserved file of a saved copy.
func (s *ResourceLibraryService) SavedContent(ctx context.Context, id string) (*ResourceContent, error) {
	saved, err := s.GetSaved(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.storage == nil {
		return nil, ErrResourceLibraryUnavailable
	}
	file, err := s.storage.Open(ctx, saved.ID)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrResourceNotFound) {
		return nil, ErrResourceNotFound
	}
	if err != nil {
		return nil, ErrResourceLibraryUnavailable.WithCause(err)
	}
	return &ResourceContent{Body: file, Closer: file, Name: saved.Name, ContentType: saved.ContentType, ModTime: saved.CreatedAt}, nil
}

// DeleteSaved permanently removes a preserved copy and its file.
func (s *ResourceLibraryService) DeleteSaved(ctx context.Context, id string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if !ValidResourceSavedID(id) {
		return ErrResourceNotFound
	}
	deleted, err := s.repo.DeleteSaved(ctx, id)
	if err != nil {
		return ErrResourceLibraryUnavailable.WithCause(err)
	}
	if !deleted {
		return ErrResourceNotFound
	}
	if s.storage != nil {
		s.removeSavedFile(id)
	}
	return nil
}
