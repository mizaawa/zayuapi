package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImageWorkbenchPublicResponseDoesNotExposeProviderData(t *testing.T) {
	for _, storage := range []string{"", ImageWorkbenchStorageLocal} {
		t.Run("storage="+storage, func(t *testing.T) {
			task := &ImageTaskRecord{
				ID: "imgtask_0123456789abcdef0123456789abcdef", Status: ImageTaskStatusFailed,
				HTTPStatus: 502, Workbench: &ImageWorkbenchMetadata{}, WorkbenchStorage: storage,
				Result: json.RawMessage(`{"backend":"http://internal.example","data":[{"url":"https://private-bucket.example/image?signature=secret","revised_prompt":"provider-secret","debug":"account-secret","b64_json":"sensitive","content_type":"image/png","size_bytes":42}]}`),
				Error:  json.RawMessage(`{"message":"sk-provider-secret at http://internal.example","type":"provider-secret","code":"credential-secret"}`),
			}
			public := imageTaskToPublic(task)
			encoded, err := json.Marshal(public)
			require.NoError(t, err)
			for _, hidden := range []string{"secret", "internal.example", "private-bucket", "b64_json", "revised_prompt", "backend", "debug"} {
				require.NotContains(t, string(encoded), hidden)
			}
			require.Equal(t, "/api/v1/image-workbench/tasks/"+task.ID+"/images/0", public.ImageURL)
			require.Contains(t, string(public.Result), `"size_bytes":42`)
			require.Contains(t, string(public.Error), "image generation failed")
		})
	}
	for _, result := range []string{`{"debug":"secret"}`, `{"data":"secret"}`, `{"data":[null]}`} {
		public := workbenchPublicResult("imgtask_test", json.RawMessage(result))
		require.NotContains(t, string(public), "secret")
	}
}

func TestImageWorkbenchSanitizesProviderDataBeforePersistence(t *testing.T) {
	store := &imageTaskMemoryStore{task: &ImageTaskRecord{
		ID: "imgtask_test", UserID: 1, APIKeyID: 2, UserEmail: "owner@example.com",
		Status: ImageTaskStatusProcessing, Workbench: &ImageWorkbenchMetadata{}, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}}
	storage := &fakeImageStorage{}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
	payload := json.RawMessage(`{"debug":"upstream-secret","data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes) + `","url":"http://internal.example","debug":"upstream-secret"}]}`)
	require.NoError(t, svc.Complete(context.Background(), store.task.ID, 200, payload))
	require.Equal(t, ImageTaskStatusCompleted, store.task.Status)
	require.NotContains(t, string(store.task.Result), "upstream-secret")
	require.NotContains(t, string(store.task.Result), "internal.example")
	require.NotContains(t, string(store.task.Result), "b64_json")
	for _, status := range []int{400, 401, 403, 422, 429, 500, 502, 503, 504} {
		store.task.Status = ImageTaskStatusProcessing
		require.NoError(t, svc.Fail(context.Background(), store.task.ID, status, json.RawMessage(`{"message":"upstream-secret"}`)))
		require.NotContains(t, string(store.task.Error), "upstream-secret")
		require.NotEmpty(t, store.task.Error)
	}
}

func TestImageWorkbenchInvalidImageInputUsesSafeDiagnostic(t *testing.T) {
	store := &imageTaskMemoryStore{task: &ImageTaskRecord{
		ID: "imgtask_test", UserID: 1, APIKeyID: 2, UserEmail: "owner@example.com",
		Status: ImageTaskStatusProcessing, Workbench: &ImageWorkbenchMetadata{}, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}}
	svc := NewImageTaskService(store)
	errBody := json.RawMessage(`{"code":"invalid_image_input","message":"private upstream URL https://internal.example and sk-secret"}`)
	require.NoError(t, svc.Fail(context.Background(), store.task.ID, http.StatusBadRequest, errBody))
	require.Contains(t, string(store.task.Error), `"code":"invalid_image_input"`)
	require.Contains(t, string(store.task.Error), "change the model")
	require.NotContains(t, string(store.task.Error), "internal.example")
	require.NotContains(t, string(store.task.Error), "sk-secret")

	public := imageTaskToPublic(store.task)
	require.Equal(t, store.task.Error, public.Error)
	legacy := imageTaskToPublic(&ImageTaskRecord{
		Status: ImageTaskStatusFailed, HTTPStatus: http.StatusBadRequest,
		Workbench: &ImageWorkbenchMetadata{}, Error: errBody,
	})
	require.Equal(t, public.Error, legacy.Error)
}

func TestImageTaskRejectsExpiredRecordBeforeRedisEviction(t *testing.T) {
	store := &imageTaskMemoryStore{task: &ImageTaskRecord{UserID: 1, APIKeyID: 2, ExpiresAt: time.Now().Add(-time.Second).Unix()}}
	svc := NewImageTaskService(store)
	_, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, "imgtask_expired")
	require.ErrorIs(t, err, ErrImageTaskNotFound)
}

func TestImageDownloadRejectsSSRFAndCredentialURLs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()
	for _, target := range []string{
		server.URL, "http://localhost/image", "http://localhost./image", "http://metadata.google.internal/image",
		"http://169.254.169.254/latest/meta-data", "http://10.0.0.1/image", "http://172.16.1.1/image",
		"http://192.168.1.1/image", "http://100.100.100.200/image", "http://0.0.0.0/image",
		"http://[::1]/image", "http://[::ffff:127.0.0.1]/image", "http://[fd00::1]/image",
		"http://[fe80::1%25eth0]/image", "http://224.0.0.1/image", "file:///etc/passwd", "ftp://example.com/image",
		"https://user:password@example.com/image",
	} {
		t.Run(target, func(t *testing.T) {
			uploader := NewImageResultUploader(&fakeImageStorage{}, "images/", 0, nil)
			_, _, err := uploader.download(context.Background(), target)
			require.Error(t, err)
			require.NotContains(t, err.Error(), target)
		})
	}
	require.Zero(t, calls.Load())
}

func TestImageDownloadRedirectPolicy(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1/image", "http://169.254.169.254/image", "https://user:password@example.com/image", "https://public.example/final.png"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := defaultImageDownloadHTTPClient()
			client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(pngBytes))), Request: req}, nil
			})
			uploader := NewImageResultUploader(&fakeImageStorage{}, "images/", 0, client)
			data, contentType, err := uploader.download(context.Background(), "https://public.example/first.png")
			if strings.Contains(target, "public.example") {
				require.NoError(t, err)
				require.Equal(t, pngBytes, data)
				require.Equal(t, "image/png", contentType)
				require.Equal(t, 2, calls)
			} else {
				require.Error(t, err)
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestImageDownloadDoesNotExposeSignedURLInErrors(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("credential-secret")
	})}
	uploader := NewImageResultUploader(&fakeImageStorage{}, "images/", 0, client)
	_, _, err := uploader.download(context.Background(), "https://public.example/image?signature=signature-secret")
	require.EqualError(t, err, "image download request failed")
}

func TestImageUploaderRejectsActiveContentAndOversizedBase64(t *testing.T) {
	for _, content := range []string{"<svg xmlns=\"http://www.w3.org/2000/svg\"><script>alert(1)</script></svg>", "<!DOCTYPE html><script>alert(1)</script>", "not an image"} {
		for _, field := range []string{"b64_json", "url"} {
			encoded := base64.StdEncoding.EncodeToString([]byte(content))
			if field == "url" {
				encoded = "data:image/png;base64," + encoded
			}
			payload, err := json.Marshal(map[string]any{"data": []map[string]string{{field: encoded}}})
			require.NoError(t, err)
			storage := &fakeImageStorage{}
			_, err = NewImageResultUploader(storage, "images/", 0, nil).Rewrite(context.Background(), "imgtask_bad", payload)
			require.Error(t, err)
			require.Empty(t, storage.saved)
		}
	}
	storage := &fakeImageStorage{}
	payload := json.RawMessage(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes) + `"}]}`)
	_, err := NewImageResultUploader(storage, "images/", 8, nil).Rewrite(context.Background(), "imgtask_big", payload)
	require.ErrorContains(t, err, "exceeds 8 bytes")
	require.Empty(t, storage.saved)
}
