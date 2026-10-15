package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageTaskMemoryStore struct {
	task    *ImageTaskRecord
	ttl     time.Duration
	saveErr error
	getErr  error
}

func (s *imageTaskMemoryStore) Save(_ context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	copy := *task
	s.task = &copy
	s.ttl = ttl
	return nil
}

func (s *imageTaskMemoryStore) Get(_ context.Context, _ string) (*ImageTaskRecord, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func TestWorkbenchTaskErrorsExplainTheRejection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		raw    string
		code   string
	}{
		{
			name:   "provider rejects image input",
			status: http.StatusBadRequest,
			raw:    `{"type":"invalid_request_error","code":"invalid_image_input","message":"The image request is invalid.","param":""}`,
			code:   "invalid_image_input",
		},
		{
			name:   "provider moderation",
			status: http.StatusBadRequest,
			raw:    `{"error":{"code":"moderation_blocked","message":"Your request was rejected by the safety system."}}`,
			code:   "content_rejected",
		},
		{
			name:   "provider rejects size",
			status: http.StatusBadRequest,
			raw:    `{"error":{"code":"invalid_image_size","message":"unsupported size 8192x8192"}}`,
			code:   "unsupported_size",
		},
		{
			name:   "channel lacks the model",
			status: http.StatusServiceUnavailable,
			raw:    `{"error":{"code":"model_not_found","message":"模型不存在"}}`,
			code:   "model_unavailable",
		},
		{
			name:   "gateway out of quota",
			status: http.StatusBadRequest,
			raw:    `{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`,
			code:   "insufficient_balance",
		},
		{
			name:   "unknown rejection keeps the status answer",
			status: http.StatusBadRequest,
			raw:    `{"error":{"message":"The image request is invalid."}}`,
			code:   "request_rejected",
		},
		{
			name:   "unknown status keeps the generic answer",
			status: http.StatusBadGateway,
			raw:    `{"error":{"message":"upstream connection reset"}}`,
			code:   "generation_failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			public := workbenchPublicError(tc.status, json.RawMessage(tc.raw))
			require.Equal(t, tc.code, gjson.GetBytes(public, "code").String())
			require.NotEmpty(t, gjson.GetBytes(public, "type").String())
			require.Equal(t, tc.code, gjson.GetBytes(public, "code").String(),
				"re-publishing a public error stays stable across task list reads")
		})
	}
}

func TestWorkbenchTaskErrorNeverExposesProviderText(t *testing.T) {
	raw := json.RawMessage(`{"error":{"code":"internal_route_failure","message":"upstream https://internal.example sk-live-secret of account 42"}}`)
	public := workbenchPublicError(http.StatusBadGateway, raw)
	for _, hidden := range []string{"internal.example", "sk-live-secret", "account 42", "internal_route_failure"} {
		require.NotContains(t, string(public), hidden)
	}
	// A record written before this mapping existed still renders safely.
	legacy := imageTaskToPublic(&ImageTaskRecord{
		ID: "imgtask_legacy", Status: ImageTaskStatusFailed, HTTPStatus: http.StatusBadRequest,
		Workbench: &ImageWorkbenchMetadata{}, ExpiresAt: 1,
		Error: json.RawMessage(`{"message":"sk-live-secret at http://internal.example"}`),
	})
	require.Contains(t, string(legacy.Error), `"code":"request_rejected"`)
	require.NotContains(t, string(legacy.Error), "sk-live-secret")
}

func TestImageTaskServiceLifecycleAndOwnership(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusProcessing, created.Status)
	require.Equal(t, created.ID, created.TaskID)
	require.Equal(t, "image.generation.task", created.Object)
	require.Equal(t, time.Hour, store.ttl)
	require.Equal(t, owner.UserID, store.task.UserID)
	require.Equal(t, owner.APIKeyID, store.task.APIKeyID)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)

	result := json.RawMessage(`{"created":123,"data":[{"url":"https://example.test/image.png"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))

	completed, err := svc.Get(context.Background(), owner, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, completed.Status)
	require.Equal(t, http.StatusOK, completed.HTTPStatus)
	require.Equal(t, "https://example.test/image.png", completed.ImageURL)
	require.JSONEq(t, string(result), string(completed.Result))
	require.NotNil(t, completed.CompletedAt)
}

func TestImageTaskServiceInvalidResultBecomesFailed(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.NoError(t, err)

	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, json.RawMessage(`not-json`)))
	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	require.Equal(t, http.StatusBadGateway, got.HTTPStatus)
	require.Contains(t, string(got.Error), "non-JSON")
}

func TestImageTaskServiceMapsStoreFailures(t *testing.T) {
	store := &imageTaskMemoryStore{saveErr: errors.New("redis down")}
	svc := NewImageTaskService(store)

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

func TestImageTaskServiceResolverUnavailableFailsWithoutPersistingResult(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{name: "disabled", enabled: false},
		{name: "missing uploader", enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &imageTaskMemoryStore{}
			svc := NewImageTaskServiceWithResolver(store, func() (*ImageResultUploader, bool) {
				return nil, tc.enabled
			}, time.Hour, time.Minute)
			created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2, UserEmail: "owner@example.com"})
			require.NoError(t, err)

			b64 := json.RawMessage(`{"data":[{"b64_json":"c2Vuc2l0aXZl"}]}`)
			require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, b64))
			got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2, UserEmail: "owner@example.com"}, created.ID)
			require.NoError(t, err)
			require.Equal(t, ImageTaskStatusFailed, got.Status)
			require.Equal(t, http.StatusServiceUnavailable, got.HTTPStatus)
			require.Empty(t, got.Result)
		})
	}
}
