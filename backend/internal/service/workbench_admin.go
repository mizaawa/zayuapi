package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// Administrator access to workbench results. These paths deliberately skip the
// per-user ownership checks, but they never accept storage keys or file paths
// from the caller: every object location is still derived from the stored task.

// ImageWorkbenchAdminStore enumerates retained workbench tasks across users.
type ImageWorkbenchAdminStore interface {
	ListAllWorkbench(ctx context.Context, now time.Time, limit int) ([]*ImageTaskRecord, error)
}

// VideoWorkbenchAdminStore enumerates retained workbench videos across users.
type VideoWorkbenchAdminStore interface {
	ListAll(ctx context.Context, now time.Time, limit int) ([]*VideoWorkbenchRecord, error)
}

// ValidImageWorkbenchTaskID reports whether id has the server-generated form.
func ValidImageWorkbenchTaskID(id string) bool {
	if len(id) != 40 || !strings.HasPrefix(id, "imgtask_") {
		return false
	}
	for _, c := range id[8:] {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

// AdminListWorkbench returns completed, unexpired workbench images of all users.
func (s *ImageTaskService) AdminListWorkbench(ctx context.Context, now time.Time, limit int) ([]*ImageTaskRecord, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	store, ok := s.store.(ImageWorkbenchAdminStore)
	if !ok {
		return nil, ErrImageTaskUnavailable
	}
	records, err := store.ListAllWorkbench(ctx, now, limit)
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	result := make([]*ImageTaskRecord, 0, len(records))
	for _, task := range records {
		if task != nil && task.Workbench != nil && task.Status == ImageTaskStatusCompleted && task.ExpiresAt > now.Unix() {
			result = append(result, task)
		}
	}
	return result, nil
}

// AdminGetWorkbench loads any unexpired workbench task by its server ID.
func (s *ImageTaskService) AdminGetWorkbench(ctx context.Context, id string) (*ImageTaskRecord, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	if !ValidImageWorkbenchTaskID(id) {
		return nil, ErrImageTaskNotFound
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, ErrImageTaskNotFound
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.Workbench == nil || task.ExpiresAt <= time.Now().Unix() {
		return nil, ErrImageTaskNotFound
	}
	return task, nil
}

// AdminReadWorkbenchImage reads one generated image of a completed task.
func (s *ImageTaskService) AdminReadWorkbenchImage(ctx context.Context, task *ImageTaskRecord, index int) ([]byte, string, error) {
	if task == nil || task.Workbench == nil || task.Status != ImageTaskStatusCompleted {
		return nil, "", ErrImageTaskNotFound
	}
	return s.readWorkbenchImage(ctx, task, index)
}

// AdminDeleteWorkbench removes a task and its stored images for any owner.
func (s *ImageTaskService) AdminDeleteWorkbench(ctx context.Context, id string) error {
	task, err := s.AdminGetWorkbench(ctx, id)
	if err != nil {
		return err
	}
	store, ok := s.store.(ImageWorkbenchStore)
	if !ok {
		return ErrImageTaskUnavailable
	}
	return s.deleteWorkbenchTask(ctx, store, task)
}

// AdminList returns completed, unexpired workbench videos of all users.
func (s *VideoWorkbenchService) AdminList(ctx context.Context, now time.Time, limit int) ([]*VideoWorkbenchRecord, error) {
	if s == nil || s.store == nil {
		return nil, ErrVideoTaskUnavailable
	}
	store, ok := s.store.(VideoWorkbenchAdminStore)
	if !ok {
		return nil, ErrVideoTaskUnavailable
	}
	records, err := store.ListAll(ctx, now, limit)
	if err != nil {
		return nil, ErrVideoTaskUnavailable.WithCause(err)
	}
	result := make([]*VideoWorkbenchRecord, 0, len(records))
	for _, task := range records {
		if task != nil && task.Status == "completed" && task.ExpiresAt > now.Unix() {
			result = append(result, task)
		}
	}
	return result, nil
}

// AdminGet loads any unexpired workbench video task by its server ID.
func (s *VideoWorkbenchService) AdminGet(ctx context.Context, id string) (*VideoWorkbenchRecord, error) {
	if s == nil || s.store == nil {
		return nil, ErrVideoTaskUnavailable
	}
	if !ValidVideoWorkbenchTaskID(id) {
		return nil, ErrVideoTaskNotFound
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.ExpiresAt <= time.Now().Unix() {
		return nil, ErrVideoTaskNotFound
	}
	return task, nil
}

// AdminOpen opens the stored file of a completed video for any owner.
func (s *VideoWorkbenchService) AdminOpen(ctx context.Context, id string) (io.ReadSeekCloser, *VideoWorkbenchRecord, error) {
	task, err := s.AdminGet(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if task.Status != "completed" || s.storage == nil {
		return nil, nil, ErrVideoTaskNotFound
	}
	file, err := s.storage.Open(ctx, task.ID)
	if err != nil {
		return nil, nil, ErrVideoTaskNotFound
	}
	return file, task, nil
}

// AdminDelete removes a video task and its file for any owner.
func (s *VideoWorkbenchService) AdminDelete(ctx context.Context, id string) error {
	task, err := s.AdminGet(ctx, id)
	if err != nil {
		return err
	}
	if s.storage == nil {
		return ErrVideoTaskUnavailable
	}
	return s.deleteTask(ctx, task)
}
