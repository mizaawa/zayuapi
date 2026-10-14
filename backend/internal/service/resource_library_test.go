package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubImageWorkbenchStore struct {
	tasks map[string]*ImageTaskRecord
	all   []*ImageTaskRecord
}

func (s *stubImageWorkbenchStore) Save(context.Context, *ImageTaskRecord, time.Duration) error {
	return nil
}
func (s *stubImageWorkbenchStore) CreateWorkbench(context.Context, *ImageTaskRecord, time.Duration, int) error {
	return nil
}
func (s *stubImageWorkbenchStore) ListWorkbench(context.Context, int64, time.Time) ([]*ImageTaskRecord, error) {
	return nil, nil
}
func (s *stubImageWorkbenchStore) ExpiredWorkbench(context.Context, time.Time) ([]ImageWorkbenchCleanup, error) {
	return nil, nil
}
func (s *stubImageWorkbenchStore) RemoveWorkbenchCleanup(context.Context, ImageWorkbenchCleanup) error {
	return nil
}
func (s *stubImageWorkbenchStore) DeleteWorkbench(context.Context, *ImageTaskRecord) error {
	return nil
}
func (s *stubImageWorkbenchStore) Get(_ context.Context, id string) (*ImageTaskRecord, error) {
	task, ok := s.tasks[id]
	if !ok {
		return nil, ErrImageTaskNotFound
	}
	return task, nil
}
func (s *stubImageWorkbenchStore) ListAllWorkbench(context.Context, time.Time, int) ([]*ImageTaskRecord, error) {
	return s.all, nil
}

type stubResourceRepo struct {
	emails  map[int64]string
	groups  map[int64]string
	keys    map[int64]int64
	billing map[string]ResourceLibraryBilling
	saved   map[string]*ResourceLibrarySaved
	byID    map[string]*ResourceLibrarySaved
	created int
}

func newStubResourceRepo() *stubResourceRepo {
	return &stubResourceRepo{
		emails: map[int64]string{}, groups: map[int64]string{}, keys: map[int64]int64{},
		billing: map[string]ResourceLibraryBilling{}, saved: map[string]*ResourceLibrarySaved{}, byID: map[string]*ResourceLibrarySaved{},
	}
}

func (s *stubResourceRepo) UserEmails(context.Context, []int64) (map[int64]string, error) {
	return s.emails, nil
}
func (s *stubResourceRepo) APIKeyGroups(context.Context, []int64) (map[int64]int64, error) {
	return s.keys, nil
}
func (s *stubResourceRepo) GroupNames(context.Context, []int64) (map[int64]string, error) {
	return s.groups, nil
}
func (s *stubResourceRepo) Billing(context.Context, []string) (map[string]ResourceLibraryBilling, error) {
	return s.billing, nil
}
func (s *stubResourceRepo) SavedSources(context.Context, []string) (map[string]string, error) {
	result := map[string]string{}
	for key, item := range s.saved {
		result[key] = item.ID
	}
	return result, nil
}
func (s *stubResourceRepo) CreateSaved(_ context.Context, item *ResourceLibrarySaved) (bool, error) {
	key := ResourceSourceKey(item.SourceTaskID, item.SourceIndex)
	if _, exists := s.saved[key]; exists {
		return false, nil
	}
	s.created++
	s.saved[key] = item
	s.byID[item.ID] = item
	return true, nil
}
func (s *stubResourceRepo) GetSaved(_ context.Context, id string) (*ResourceLibrarySaved, error) {
	item, ok := s.byID[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	return item, nil
}
func (s *stubResourceRepo) GetSavedBySource(_ context.Context, taskID string, index int) (*ResourceLibrarySaved, error) {
	item, ok := s.saved[ResourceSourceKey(taskID, index)]
	if !ok {
		return nil, ErrResourceNotFound
	}
	return item, nil
}
func (s *stubResourceRepo) ListSaved(context.Context, ResourceLibraryFilter) ([]*ResourceLibrarySaved, int64, error) {
	items := make([]*ResourceLibrarySaved, 0, len(s.byID))
	for _, item := range s.byID {
		items = append(items, item)
	}
	return items, int64(len(items)), nil
}
func (s *stubResourceRepo) SavedFacets(context.Context) ([]string, []ResourceLibraryGroupOption, error) {
	return []string{"gpt-image-1"}, []ResourceLibraryGroupOption{{ID: 2, Name: "A组"}}, nil
}
func (s *stubResourceRepo) DeleteSaved(_ context.Context, id string) (bool, error) {
	if _, ok := s.byID[id]; !ok {
		return false, nil
	}
	delete(s.byID, id)
	return true, nil
}

type stubResourceStorage struct {
	files   map[string][]byte
	deletes int
	written int
}

func newStubResourceStorage() *stubResourceStorage {
	return &stubResourceStorage{files: map[string][]byte{}}
}

func (s *stubResourceStorage) Ready(context.Context) error { return nil }
func (s *stubResourceStorage) Write(_ context.Context, id string, content io.Reader, maxBytes int64) (int64, string, error) {
	data, err := io.ReadAll(io.LimitReader(content, maxBytes+1))
	if err != nil {
		return 0, "", err
	}
	if int64(len(data)) > maxBytes {
		return 0, "", errors.New("too large")
	}
	s.files[id] = data
	s.written++
	return int64(len(data)), "digest-" + id, nil
}
func (s *stubResourceStorage) Open(_ context.Context, id string) (io.ReadSeekCloser, error) {
	data, ok := s.files[id]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return nopSeekCloser{strings.NewReader(string(data))}, nil
}
func (s *stubResourceStorage) Delete(_ context.Context, id string) error {
	s.deletes++
	delete(s.files, id)
	return nil
}

type nopSeekCloser struct{ *strings.Reader }

func (nopSeekCloser) Close() error { return nil }

// stubMemoryImageStorage implements the reader side used by workbench reads.
type stubMemoryImageStorage struct {
	mu    sync.Mutex
	bytes map[string][]byte
}

func (s *stubMemoryImageStorage) Save(context.Context, string, string, []byte) (string, error) {
	return "", nil
}
func (s *stubMemoryImageStorage) Read(_ context.Context, key string, maxBytes int64) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.bytes[key]
	if !ok {
		return nil, "", fs.ErrNotExist
	}
	if int64(len(data)) > maxBytes {
		return nil, "", errors.New("too large")
	}
	return data, "image/png", nil
}
func (s *stubMemoryImageStorage) DeletePrefix(context.Context, string) (int64, error) { return 0, nil }

var resourcePNGBytes = append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("payload")...)

func newImageLibraryFixture(t *testing.T) (*ResourceLibraryService, *stubResourceRepo, *stubResourceStorage, string) {
	t.Helper()
	now := time.Now()
	expires := now.Add(30 * time.Minute).Unix()
	completed := now.Unix()
	taskID := "imgtask_0123456789abcdef0123456789abcdef"
	task := &ImageTaskRecord{
		ID: taskID, UserID: 7, APIKeyID: 3, UserEmail: "user@example.com", Status: ImageTaskStatusCompleted,
		CreatedAt: now.Add(-time.Minute).Unix(), CompletedAt: &completed, ExpiresAt: expires,
		GroupID: 2, BillingID: "media:1", WorkbenchStorage: ImageWorkbenchStorageLocal,
		Workbench: &ImageWorkbenchMetadata{Prompt: "draw a cat", Model: "gpt-image-1", Quality: "auto", Size: "1024x1024", Count: 1},
		Result:    json.RawMessage(`{"data":[{"url":"local-image:workbench/x","size_bytes":123,"content_type":"image/png"}]}`),
	}
	store := &stubImageWorkbenchStore{tasks: map[string]*ImageTaskRecord{taskID: task}, all: []*ImageTaskRecord{task}}
	memory := &stubMemoryImageStorage{bytes: map[string][]byte{}}
	images := NewImageTaskService(store)
	images.SetWorkbenchLocalStorage(&stubLocalWorkbenchStorage{storage: memory})
	key := uploaderKeyForEmail("images/", "workbench/", task.ID, task.UserEmail, 0, "image/png")
	memory.bytes[key] = resourcePNGBytes

	repo := newStubResourceRepo()
	repo.emails[7] = "user@example.com"
	repo.groups[2] = "A组"
	repo.keys[3] = 2
	repo.billing["media:1"] = ResourceLibraryBilling{State: "settled", Amount: 0.75, GroupID: 2}
	storage := newStubResourceStorage()
	svc := NewResourceLibraryServiceWith(func(context.Context) bool { return true }, images, nil, repo, storage)
	return svc, repo, storage, taskID
}

// uploaderKeyForEmail mirrors the storage key derivation without touching the
// private uploader fields.
func uploaderKeyForEmail(prefix, extra, taskID, email string, index int, contentType string) string {
	uploader := NewImageResultUploader(nil, prefix, 0, nil)
	workbench := *uploader
	workbench.prefix += extra
	return workbench.buildKey(taskID, email, index, contentType)
}

type stubLocalWorkbenchStorage struct{ storage *stubMemoryImageStorage }

func (s *stubLocalWorkbenchStorage) Save(ctx context.Context, key, contentType string, data []byte) (string, error) {
	return s.storage.Save(ctx, key, contentType, data)
}
func (s *stubLocalWorkbenchStorage) Read(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) {
	return s.storage.Read(ctx, key, maxBytes)
}
func (s *stubLocalWorkbenchStorage) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	return s.storage.DeletePrefix(ctx, prefix)
}
func (s *stubLocalWorkbenchStorage) Ready(context.Context) error { return nil }

func TestResourceLibraryDisabledReturnsDisabled(t *testing.T) {
	disabled := NewResourceLibraryServiceWith(func(context.Context) bool { return false }, nil, nil, newStubResourceRepo(), newStubResourceStorage())
	if _, err := disabled.List(context.Background(), ResourceLibraryFilter{}); !errors.Is(err, ErrResourceLibraryDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if _, _, err := disabled.Save(context.Background(), 1, ResourceKindImage, "imgtask_x", 0); !errors.Is(err, ErrResourceLibraryDisabled) {
		t.Fatalf("expected disabled error on save, got %v", err)
	}
	if disabled.Enabled(context.Background()) {
		t.Fatal("expected Enabled() to report false")
	}
}

func TestResourceLibraryRejectsUnknownKind(t *testing.T) {
	if _, err := NormalizeResourceKind("audio"); !errors.Is(err, ErrResourceInvalidKind) {
		t.Fatalf("expected invalid kind, got %v", err)
	}
	if kind, err := NormalizeResourceKind("  IMAGE "); err != nil || kind != ResourceKindImage {
		t.Fatalf("expected normalized image, got %q %v", kind, err)
	}
	if kind, err := NormalizeResourceKind(""); err != nil || kind != "" {
		t.Fatalf("empty kind should stay unset, got %q %v", kind, err)
	}
}

func TestResourceLibraryFilterNormalization(t *testing.T) {
	filter, err := normalizeResourceFilter(ResourceLibraryFilter{Page: 0, PageSize: 100000, Query: strings.Repeat("x", 300)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filter.Page != 1 || filter.PageSize != resourceLibraryMaxPageSize {
		t.Fatalf("page normalization wrong: %+v", filter)
	}
	if len(filter.Query) != 256 {
		t.Fatalf("query should be bounded, got %d", len(filter.Query))
	}
	if pages := resourcePages(0, 20); pages != 1 {
		t.Fatalf("expected one page for empty result, got %d", pages)
	}
	if pages := resourcePages(41, 20); pages != 3 {
		t.Fatalf("expected three pages, got %d", pages)
	}
}

func TestResourceLibraryListEnrichesAndPaginates(t *testing.T) {
	svc, repo, _, taskID := newImageLibraryFixture(t)
	repo.billing["media:1"] = ResourceLibraryBilling{State: "refunded", Amount: 0.75, GroupID: 2}
	result, err := svc.List(context.Background(), ResourceLibraryFilter{PageSize: 1})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Pages != 1 {
		t.Fatalf("unexpected page: %+v", result)
	}
	item := result.Items[0]
	if item.TaskID != taskID || item.Name != taskID+"-1.png" {
		t.Fatalf("unexpected identity: %+v", item)
	}
	if item.UserEmail != "user@example.com" || item.GroupName != "A组" || item.Model != "gpt-image-1" {
		t.Fatalf("provenance not resolved: %+v", item)
	}
	if item.Cost == nil || *item.Cost != 0 {
		t.Fatalf("refunded content must cost zero, got %v", item.Cost)
	}
	if len(result.Models) != 1 || len(result.Groups) != 1 {
		t.Fatalf("facets missing: %+v", result)
	}
	// Filters that cannot match must return an empty page with exact totals.
	filtered, err := svc.List(context.Background(), ResourceLibraryFilter{Model: "other-model"})
	if err != nil {
		t.Fatalf("filtered list failed: %v", err)
	}
	if filtered.Total != 0 || len(filtered.Items) != 0 {
		t.Fatalf("expected no matches: %+v", filtered)
	}
	searched, err := svc.List(context.Background(), ResourceLibraryFilter{Query: "gpt-image"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if searched.Total != 1 {
		t.Fatalf("search should match the model, got %d", searched.Total)
	}
}

func TestResourceLibrarySaveIsIdempotent(t *testing.T) {
	svc, repo, storage, taskID := newImageLibraryFixture(t)
	saved, created, err := svc.Save(context.Background(), 1, ResourceKindImage, taskID, 0)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if !created || saved == nil || saved.SizeBytes != int64(len(resourcePNGBytes)) {
		t.Fatalf("unexpected save result: created=%v item=%+v", created, saved)
	}
	if saved.SavedBy != 1 || saved.SourceTaskID != taskID || saved.ContentType != "image/png" {
		t.Fatalf("saved metadata incomplete: %+v", saved)
	}
	if _, ok := storage.files[saved.ID]; !ok {
		t.Fatal("preserved file was not written")
	}
	again, created, err := svc.Save(context.Background(), 1, ResourceKindImage, taskID, 0)
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if created || again.ID != saved.ID {
		t.Fatalf("second save should return the existing copy: %+v", again)
	}
	if repo.created != 1 || storage.written != 1 {
		t.Fatalf("archive duplicated: created=%d written=%d", repo.created, storage.written)
	}
	// The live listing marks the source as already preserved.
	list, err := svc.List(context.Background(), ResourceLibraryFilter{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if list.Items[0].SavedID != saved.ID {
		t.Fatalf("listing must expose the saved copy id, got %q", list.Items[0].SavedID)
	}
}

func TestResourceLibrarySaveRejectsMissingResult(t *testing.T) {
	svc, _, storage, taskID := newImageLibraryFixture(t)
	if _, _, err := svc.Save(context.Background(), 1, ResourceKindImage, "imgtask_ffffffffffffffffffffffffffffffff", 0); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if _, _, err := svc.Save(context.Background(), 1, ResourceKindImage, taskID, 3); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected not found for missing index, got %v", err)
	}
	if len(storage.files) != 0 {
		t.Fatal("failed saves must not leave files behind")
	}
}

func TestResourceLibrarySavedContentAndDelete(t *testing.T) {
	svc, _, storage, taskID := newImageLibraryFixture(t)
	saved, _, err := svc.Save(context.Background(), 1, ResourceKindImage, taskID, 0)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	content, err := svc.SavedContent(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("open saved content failed: %v", err)
	}
	body, err := io.ReadAll(content.Body)
	_ = content.Close()
	if err != nil || len(body) != len(resourcePNGBytes) {
		t.Fatalf("unexpected content: %d bytes err=%v", len(body), err)
	}
	if err := svc.DeleteSaved(context.Background(), saved.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, ok := storage.files[saved.ID]; ok {
		t.Fatal("file should be removed with the row")
	}
	if err := svc.DeleteSaved(context.Background(), saved.ID); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("second delete should report not found, got %v", err)
	}
	if _, err := svc.SavedContent(context.Background(), "res_zzzz"); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("invalid id should be rejected, got %v", err)
	}
}

func TestResourceLibraryLiveContentUsesDerivedKey(t *testing.T) {
	svc, _, _, taskID := newImageLibraryFixture(t)
	content, err := svc.Content(context.Background(), ResourceKindImage, taskID, 0)
	if err != nil {
		t.Fatalf("content failed: %v", err)
	}
	defer func() { _ = content.Close() }()
	if content.ContentType != "image/png" || content.Name != taskID+"-1.png" {
		t.Fatalf("unexpected content identity: %+v", content)
	}
	data, err := io.ReadAll(content.Body)
	if err != nil || len(data) != len(resourcePNGBytes) {
		t.Fatalf("unexpected bytes: %d err=%v", len(data), err)
	}
}

func TestVideoResourceItemNamingAndDetails(t *testing.T) {
	task := &VideoWorkbenchRecord{
		VideoWorkbenchTask: VideoWorkbenchTask{
			ID: "vidtask_0123456789abcdef0123456789abcdef", Status: "completed", CreatedAt: 10, CompletedAt: 20,
			ExpiresAt: 30, SizeBytes: 2048, ContentType: "video/webm",
			Workbench: VideoWorkbenchMetadata{APIKeyID: 3, Model: "veo-3", Prompt: "a dog", Ratio: "16:9", Resolution: "1080p", Duration: 8},
		},
		UserID: 7, GroupID: 2, BillingID: "media:2",
	}
	item := videoResourceItem(task)
	if item == nil || item.Name != task.ID+".webm" || item.Kind != ResourceKindVideo {
		t.Fatalf("unexpected video item: %+v", item)
	}
	if item.SizeBytes != 2048 || item.Details["duration"] != 8 || item.Details["resolution"] != "1080p" {
		t.Fatalf("video details missing: %+v", item.Details)
	}
	if videoResourceItem(nil) != nil {
		t.Fatal("nil task must not produce an item")
	}
	if name := resourceVideoName("vidtask_x", "video/mp4"); name != "vidtask_x.mp4" {
		t.Fatalf("unexpected default extension: %s", name)
	}
	if name := resourceVideoName("vidtask_x", "video/quicktime"); name != "vidtask_x.mov" {
		t.Fatalf("unexpected mov extension: %s", name)
	}
}
