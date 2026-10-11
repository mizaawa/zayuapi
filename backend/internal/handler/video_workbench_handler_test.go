package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoWorkbenchInterruptedStreamIsNeverSuccessful(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, length     string
		broken, canceled bool
		valid            bool
	}{
		{name: "complete", length: "5", valid: true},
		{name: "unknown length", valid: true},
		{name: "truncated", length: "10"},
		{name: "excess bytes", length: "3"},
		{name: "invalid length", length: "bad"},
		{name: "stream failure", broken: true},
		{name: "canceled download", canceled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			stream := &videoWorkbenchStreamWriter{header: make(http.Header), pipe: writer}
			c, _ := gin.CreateTestContext(stream)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodGet, "/content", nil).WithContext(ctx)
			stream.header.Set("Content-Length", tc.length)
			if tc.broken {
				_ = c.Error(errors.New("upstream stream failed"))
			}
			if tc.canceled {
				cancel()
			}
			done := make(chan struct{})
			go func() { defer close(done); _, _ = stream.Write([]byte("video")); stream.finish(c) }()
			body, err := io.ReadAll(reader)
			<-done
			require.Equal(t, "video", string(body))
			require.Equal(t, tc.valid, err == nil)
		})
	}
}

func TestVideoWorkbenchMiniMaxRequestValidation(t *testing.T) {
	policy := service.VideoWorkbenchRuntime{MaxReferenceImages: 6, MaxReferenceVideos: 6, MaxReferenceAudios: 6}
	for _, body := range []string{
		`{"model":"MiniMax-Hailuo-2.3","prompt":"waves","duration":5,"ratio":"16:9"}`,
		`{"model":"MiniMax-Hailuo-2.3-Fast","prompt":"waves","duration":6,"ratio":"16:9"}`,
		`{"model":"MiniMax-H3","prompt":"waves","duration":30,"ratio":"16:9"}`,
		`{"model":"MiniMax-H3","prompt":"waves","duration":5,"ratio":"2:1"}`,
	} {
		_, err := parseVideoWorkbenchPayload([]byte(body), policy)
		require.Error(t, err)
	}
	for _, body := range []string{
		`{"model":"MiniMax-H3","prompt":"waves","duration":5,"ratio":"9:16"}`,
		`{"model":"MiniMax-Hailuo-2.3","prompt":"waves","duration":6,"ratio":"16:9"}`,
		`{"model":"other-video","prompt":"waves","duration":60,"ratio":"2:1"}`,
	} {
		_, err := parseVideoWorkbenchPayload([]byte(body), policy)
		require.NoError(t, err)
	}
}

func TestVideoWorkbenchModelUnavailableDoesNotExposeUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &videoHandlerStore{tasks: make(map[string]service.VideoWorkbenchRecord)}
	storage := &videoHandlerStorage{files: make(map[string][]byte)}
	svc := service.NewVideoWorkbenchServiceWithSettings(store, storage, func(context.Context) service.VideoWorkbenchRuntime {
		return service.VideoWorkbenchRuntime{Enabled: true, MaxConcurrent: 3, RetentionMinutes: 15}
	})
	h := NewVideoWorkbenchHandler(svc, nil)
	h.SetAuthRefresh(func(*gin.Context) error { return nil })
	h.execute = func(c *gin.Context, operation string) {
		require.Equal(t, "create", operation)
		c.JSON(503, gin.H{"error": gin.H{"code": "model_not_found", "message": "private upstream https://internal.example sk-secret"}})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video-workbench/tasks", nil)
	task, err := svc.Create(context.Background(), 7, service.VideoWorkbenchMetadata{APIKeyID: 9, Model: "MiniMax-H3", Duration: 5})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = c.Request.WithContext(ctx)
	h.run(task, c, []byte(`{"model":"MiniMax-H3"}`), cancel)
	listed, err := svc.List(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "failed", listed[0].Status)
	require.Equal(t, "model_unavailable", listed[0].ErrorCode)
	data, err := json.Marshal(listed)
	require.NoError(t, err)
	require.NotContains(t, string(data), "internal.example")
	require.NotContains(t, string(data), "sk-secret")
	for _, state := range []string{"FAILURE", "Fail", "failed"} {
		require.Equal(t, "failed", videoWorkbenchResponseState([]byte(`{"status":"`+state+`"}`)))
	}
}

func TestVideoWorkbenchGenerationOwnershipAndDownloadHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	policy := service.VideoWorkbenchRuntime{Enabled: true, MaxConcurrent: 3, MaxReferenceImages: 6, MaxReferenceVideos: 6, MaxReferenceAudios: 6, RetentionMinutes: 30}
	store := &videoHandlerStore{tasks: make(map[string]service.VideoWorkbenchRecord)}
	storage := &videoHandlerStorage{files: make(map[string][]byte)}
	svc := service.NewVideoWorkbenchServiceWithSettings(store, storage, func(context.Context) service.VideoWorkbenchRuntime { return policy })
	h := NewVideoWorkbenchHandler(svc, nil)
	h.pollInterval = time.Millisecond
	h.SetAuthRefresh(func(*gin.Context) error { return nil })
	mp4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	h.execute = func(c *gin.Context, operation string) {
		switch operation {
		case "create":
			c.JSON(200, gin.H{"id": "upstream-private-id", "status": "queued"})
		case "status":
			c.JSON(200, gin.H{"id": "upstream-private-id", "status": "completed", "url": "https://private.example/result?secret=hidden"})
		case "content":
			c.Data(200, "video/mp4", mp4)
		}
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		id := int64(7)
		if c.GetHeader("Authorization") == "other" {
			id = 8
		}
		if c.GetHeader("Authorization") != "" {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: id, User: &service.User{ID: id}, Group: &service.Group{Platform: service.PlatformComposite, VideoWorkbenchEnabled: true}})
		}
		c.Next()
	})
	router.POST("/v1/video-workbench/tasks", h.Submit)
	router.GET("/api/v1/video-workbench/tasks", h.List)
	router.GET("/api/v1/video-workbench/tasks/:task_id/content", h.Download)
	router.DELETE("/api/v1/video-workbench/tasks/:task_id", h.Delete)
	request := func(method, path, authorization, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", authorization)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	created := request("POST", "/v1/video-workbench/tasks", "owner", `{"model":"custom-video","prompt":"waves","duration":60,"ratio":"16:9"}`)
	require.Equal(t, 202, created.Code, created.Body.String())
	var task service.VideoWorkbenchTask
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &task))
	require.Eventually(t, func() bool {
		tasks, err := svc.List(context.Background(), 7)
		return err == nil && len(tasks) == 1 && tasks[0].Status == "completed"
	}, 5*time.Second, 10*time.Millisecond)
	path := "/api/v1/video-workbench/tasks/" + task.ID + "/content"
	require.Equal(t, 401, request("GET", path, "", "").Code)
	require.Equal(t, 404, request("GET", path, "other", "").Code)
	download := request("GET", path, "owner", "")
	require.Equal(t, 200, download.Code)
	require.Equal(t, mp4, download.Body.Bytes())
	require.Contains(t, download.Header().Get("Content-Disposition"), "attachment")
	listed := request("GET", "/api/v1/video-workbench/tasks", "owner", "")
	require.NotContains(t, listed.Body.String(), "upstream-private-id")
	require.NotContains(t, listed.Body.String(), "private.example")
	require.NotContains(t, listed.Body.String(), "secret")
	require.Equal(t, 200, request("DELETE", "/api/v1/video-workbench/tasks/"+task.ID, "owner", "").Code)
	require.Equal(t, 404, request("GET", path, "owner", "").Code)
}

type videoHandlerStore struct {
	service.VideoWorkbenchStore
	mu    sync.Mutex
	tasks map[string]service.VideoWorkbenchRecord
}

func (s *videoHandlerStore) Create(_ context.Context, task *service.VideoWorkbenchRecord, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = *task
	return nil
}

func (s *videoHandlerStore) Get(_ context.Context, id string) (*service.VideoWorkbenchRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, service.ErrVideoTaskNotFound
	}
	return &task, nil
}

func (s *videoHandlerStore) List(_ context.Context, userID int64, _ time.Time) ([]*service.VideoWorkbenchRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tasks []*service.VideoWorkbenchRecord
	for _, task := range s.tasks {
		if task.UserID == userID {
			tasks = append(tasks, &task)
		}
	}
	return tasks, nil
}

func (s *videoHandlerStore) TryStart(_ context.Context, task *service.VideoWorkbenchRecord, now time.Time, _ time.Duration, _ int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.tasks[task.ID]
	if !ok || current.Status != "queued" {
		return false, service.ErrVideoTaskNotFound
	}
	task.Status, task.StartedAt = "processing", now.Unix()
	s.tasks[task.ID] = *task
	return true, nil
}

func (s *videoHandlerStore) Heartbeat(context.Context, *service.VideoWorkbenchRecord, time.Time) error {
	return nil
}

func (s *videoHandlerStore) Finish(_ context.Context, task *service.VideoWorkbenchRecord, _ time.Duration, _ int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.ID]; !ok {
		return false, service.ErrVideoTaskNotFound
	}
	s.tasks[task.ID] = *task
	return true, nil
}

func (s *videoHandlerStore) Delete(_ context.Context, task *service.VideoWorkbenchRecord, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, task.ID)
	return nil
}

func (s *videoHandlerStore) AcknowledgeCleanup(context.Context, string) error { return nil }

type videoHandlerStorage struct {
	service.VideoWorkbenchLocalStorage
	mu    sync.Mutex
	files map[string][]byte
}

func (s *videoHandlerStorage) Ready(context.Context) error { return nil }

func (s *videoHandlerStorage) Write(_ context.Context, id string, reader io.Reader, _ time.Duration) (int64, string, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return 0, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = body
	return int64(len(body)), "video/mp4", nil
}

type videoHandlerFile struct{ *bytes.Reader }

func (videoHandlerFile) Close() error { return nil }

func (s *videoHandlerStorage) Open(_ context.Context, id string) (io.ReadSeekCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.files[id]
	if !ok {
		return nil, service.ErrVideoTaskNotFound
	}
	return videoHandlerFile{bytes.NewReader(body)}, nil
}

func (s *videoHandlerStorage) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, id)
	return nil
}

func TestVideoWorkbenchPayloadEnforcesMediaLimitsAndSafeFields(t *testing.T) {
	policy := service.VideoWorkbenchRuntime{MaxReferenceImages: 1, MaxReferenceVideos: 1, MaxReferenceAudios: 1}
	base := `"model":"custom-video","prompt":"waves","ratio":"16:9","duration":60`
	valid, err := parseVideoWorkbenchPayload([]byte("{"+base+"}"), policy)
	require.NoError(t, err)
	require.Equal(t, 60, valid.Duration)
	for _, extra := range []string{
		`,"referenceImages":["http://127.0.0.1/admin"]`,
		`,"referenceVideos":["file:///etc/passwd"]`,
		`,"referenceAudios":["https://public.example/redirect"]`,
		`,"referenceImages":["data:image/svg+xml;base64,PHN2Zy8+"]`,
		`,"referenceVideos":["data:video/mp4;base64,PGh0bWw+c2VjcmV0PC9odG1sPg=="]`,
		`,"callback_url":"http://127.0.0.1"`,
	} {
		_, err := parseVideoWorkbenchPayload([]byte("{"+base+extra+"}"), policy)
		require.Error(t, err)
	}
	const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
	_, err = parseVideoWorkbenchPayload([]byte(fmt.Sprintf(`{%s,"referenceImages":[%q]}`, base, png)), policy)
	require.NoError(t, err)
	_, err = parseVideoWorkbenchPayload([]byte(fmt.Sprintf(`{%s,"referenceImages":[%q,%q]}`, base, png, png)), policy)
	require.Error(t, err)
	mp4 := base64.StdEncoding.EncodeToString([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'})
	require.True(t, validVideoWorkbenchReference("data:video/mp4;base64,"+mp4, "video"))
	for _, duration := range []string{"0", "-1", "1.5", "86401", "null", `"60"`} {
		_, err := parseVideoWorkbenchPayload([]byte(strings.Replace("{"+base+"}", `"duration":60`, `"duration":`+duration, 1)), policy)
		require.Error(t, err)
	}
	_, err = parseVideoWorkbenchPayload([]byte("{"+base+"}{}"), policy)
	require.Error(t, err)
}

func TestVideoWorkbenchResponseKeepsUpstreamURLsAndErrorsPrivate(t *testing.T) {
	require.Equal(t, "completed", videoWorkbenchResponseState([]byte(`{"data":{"status":"succeeded","url":"http://127.0.0.1/secret"}}`)))
	require.Equal(t, "failed", videoWorkbenchResponseState([]byte(`{"status":"failed","error":"sk-private"}`)))
	for _, id := range []string{"../private", "https://example.com/private", "task/../content", "task?token=secret"} {
		require.Empty(t, videoWorkbenchResponseID([]byte(fmt.Sprintf(`{"id":%q}`, id))))
	}
	require.Equal(t, "task-standard-id", videoWorkbenchResponseID([]byte(`{"id":"task-standard-id","request_id":"other"}`)))
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video-workbench/tasks?callback=bad", nil)
	c.Request.Header.Set("Range", "bytes=1-2")
	c.Request.Header.Set("Idempotency-Key", "user-controlled")
	cloned := videoWorkbenchGatewayContext(c, nil, "content", "task-123", httptest.NewRecorder())
	require.Equal(t, "/v1/videos/task-123/content", cloned.Request.URL.Path)
	require.Empty(t, cloned.Request.URL.RawQuery)
	require.Empty(t, cloned.GetHeader("Range"))
	require.Empty(t, cloned.GetHeader("Idempotency-Key"))
}
