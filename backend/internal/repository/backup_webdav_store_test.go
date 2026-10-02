package repository

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestWebDAVBackupStoreUploadDownloadDeleteAndConnectionTest(t *testing.T) {
	const username = "backup-user"
	const password = "backup-password"
	const expectedRoot = "/remote.php/dav/files/backup-user"
	const expectedPrefix = expectedRoot + "/个人兴趣/服务器自动备份/"
	var mu sync.Mutex
	objects := make(map[string][]byte)
	var methods []string
	methodPaths := make(map[string][]string)
	probeGets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPassword, ok := r.BasicAuth()
		if !ok || gotUser != username || gotPassword != password {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		methods = append(methods, r.Method)
		methodPaths[r.Method] = append(methodPaths[r.Method], r.URL.EscapedPath())
		mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, expectedRoot+"/") {
			http.Error(w, "wrong path", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodPut:
			if !strings.HasPrefix(r.URL.Path, expectedPrefix) {
				http.Error(w, "wrong object path", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Content-Type"); got != "application/gzip" && got != "application/octet-stream" {
				http.Error(w, "wrong content type", http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(r.Body)
			mu.Lock()
			objects[strings.TrimPrefix(r.URL.Path, expectedPrefix)] = data
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			if !strings.HasPrefix(r.URL.Path, expectedPrefix) {
				http.Error(w, "wrong object path", http.StatusBadRequest)
				return
			}
			key := strings.TrimPrefix(r.URL.Path, expectedPrefix)
			mu.Lock()
			if strings.HasPrefix(key, "zayuapi-connection-test-") && probeGets == 0 {
				probeGets++
				mu.Unlock()
				http.NotFound(w, r)
				return
			}
			data, found := objects[key]
			if strings.HasPrefix(key, "zayuapi-connection-test-") {
				probeGets++
			}
			mu.Unlock()
			if !found {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		case http.MethodDelete:
			if !strings.HasPrefix(r.URL.Path, expectedPrefix) {
				http.Error(w, "wrong object path", http.StatusBadRequest)
				return
			}
			key := strings.TrimPrefix(r.URL.Path, expectedPrefix)
			mu.Lock()
			delete(objects, key)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	factory := NewS3BackupStoreFactory()
	store, err := factory(context.Background(), &service.BackupS3Config{
		StorageType:    "webdav",
		WebDAVURL:      server.URL + "/remote.php/dav/files/backup-user",
		WebDAVUsername: username,
		WebDAVPassword: password,
		WebDAVPath:     "个人兴趣/服务器自动备份",
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "2026/10/01/数据库转储.sql.gz"
	content := []byte("compressed database bytes")
	size, err := store.Upload(ctx, key, strings.NewReader(string(content)), "application/gzip")
	require.NoError(t, err)
	require.Equal(t, int64(len(content)), size)
	body, err := store.Download(ctx, key)
	require.NoError(t, err)
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, content, got)
	require.NoError(t, store.Delete(ctx, key))

	// The same authenticated adapter is used by the configuration test endpoint.
	require.NoError(t, store.HeadBucket(ctx))
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, methods)
	require.Contains(t, methods, "MKCOL")
	require.Contains(t, methods, http.MethodPut)
	require.Contains(t, methods, http.MethodGet)
	require.Contains(t, methods, http.MethodDelete)
	require.Equal(t, 2, probeGets, "connection probe retries once when the PUT is not immediately visible")
	expectedEscapedObjectPath := (&url.URL{Path: expectedPrefix + key}).EscapedPath()
	require.Contains(t, methodPaths[http.MethodPut], expectedEscapedObjectPath)
	require.Contains(t, methodPaths[http.MethodGet], expectedEscapedObjectPath)
	require.Contains(t, methodPaths[http.MethodDelete], expectedEscapedObjectPath)
	require.Contains(t, methodPaths["MKCOL"], (&url.URL{Path: expectedRoot + "/个人兴趣"}).EscapedPath())
	require.Empty(t, objects)
}

func TestWebDAVBackupStoreFollowsPikPakCDNRedirectWithoutCredentials(t *testing.T) {
	const username = "backup-user"
	const password = "backup-password"
	const downloaded = "database backup"
	var authMu sync.Mutex
	var sourceAuthValid bool
	var cdnSawCredentials bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "dav.pikpak.ai":
			gotUser, gotPassword, ok := r.BasicAuth()
			authMu.Lock()
			sourceAuthValid = ok && gotUser == username && gotPassword == password
			authMu.Unlock()
			http.Redirect(w, r, "https://dl-a10b-1194.mypikpak.net/download/signed", http.StatusFound)
		case "dl-a10b-1194.mypikpak.net":
			_, _, hasBasicAuth := r.BasicAuth()
			authMu.Lock()
			cdnSawCredentials = hasBasicAuth || r.Header.Get("Cookie") != "" || r.Header.Get("Proxy-Authorization") != ""
			authMu.Unlock()
			_, _ = io.WriteString(w, downloaded)
		default:
			http.Error(w, "unexpected host", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	store, err := newWebDAVBackupStore(&service.BackupS3Config{
		WebDAVURL:      "https://dav.pikpak.ai/dav",
		WebDAVUsername: username,
		WebDAVPassword: password,
		WebDAVPath:     "backups",
	})
	require.NoError(t, err)

	certPool := x509.NewCertPool()
	certPool.AddCert(server.Certificate())
	dialer := &net.Dialer{}
	store.client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: certPool, ServerName: "example.com", MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}

	body, err := store.Download(context.Background(), "dump.sql.gz")
	require.NoError(t, err)
	defer func() { require.NoError(t, body.Close()) }()
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, downloaded, string(got))
	authMu.Lock()
	gotSourceAuthValid, gotCDNSawCredentials := sourceAuthValid, cdnSawCredentials
	authMu.Unlock()
	require.True(t, gotSourceAuthValid, "the WebDAV request must retain its configured Basic Auth")
	require.False(t, gotCDNSawCredentials, "WebDAV credentials must not reach the PikPak download CDN")
}

func TestWebDAVConnectionTestRetriesTemporaryPikPakCDN405(t *testing.T) {
	const username = "backup-user"
	const password = "backup-password"
	const probeContent = "zayuapi-webdav-connection-test"
	var mu sync.Mutex
	cdnGETs := 0
	var cdnSawCredentials bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "dav.pikpak.ai":
			gotUser, gotPassword, ok := r.BasicAuth()
			if !ok || gotUser != username || gotPassword != password {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			switch r.Method {
			case "MKCOL":
				w.WriteHeader(http.StatusMethodNotAllowed)
			case http.MethodPut:
				w.WriteHeader(http.StatusCreated)
			case http.MethodGet:
				http.Redirect(w, r, "https://dl-a10b-1194.mypikpak.net/download/?signature=redacted", http.StatusFound)
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			default:
				http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
			}
		case "dl-a10b-1194.mypikpak.net":
			_, _, hasBasicAuth := r.BasicAuth()
			mu.Lock()
			cdnGETs++
			cdnSawCredentials = cdnSawCredentials || hasBasicAuth || r.Header.Get("Cookie") != "" || r.Header.Get("Proxy-Authorization") != ""
			attempt := cdnGETs
			mu.Unlock()
			if attempt == 1 {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			_, _ = io.WriteString(w, probeContent)
		default:
			http.Error(w, "unexpected host", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	store, err := newWebDAVBackupStore(&service.BackupS3Config{
		WebDAVURL:      "https://dav.pikpak.ai/dav",
		WebDAVUsername: username,
		WebDAVPassword: password,
		WebDAVPath:     "backups",
	})
	require.NoError(t, err)

	certPool := x509.NewCertPool()
	certPool.AddCert(server.Certificate())
	dialer := &net.Dialer{}
	store.client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: certPool, ServerName: "example.com", MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, store.HeadBucket(ctx))
	mu.Lock()
	require.Equal(t, 2, cdnGETs, "the connection test should retry one temporary 405 from PikPak CDN")
	require.False(t, cdnSawCredentials, "WebDAV credentials must not reach the PikPak download CDN")
	mu.Unlock()
}

func TestDescribeWebDAVGetFailureOmitsSignedURL(t *testing.T) {
	webDAVURL, err := url.Parse("https://dav.pikpak.ai/dav")
	require.NoError(t, err)
	resp := &http.Response{
		Status: "405 Method Not Allowed",
		Request: &http.Request{URL: &url.URL{
			Scheme:   "https",
			Host:     "dl-a10b-1194.mypikpak.net",
			Path:     "/download/",
			RawQuery: "signature=must-not-be-logged",
		}},
	}
	require.Equal(t, "WebDAV GET failed: 405 Method Not Allowed (response from PikPak download CDN)", describeWebDAVGetFailure(resp, webDAVURL))
	require.False(t, retryableWebDAVProbeStatus(&http.Response{
		StatusCode: http.StatusMethodNotAllowed,
		Request:    &http.Request{URL: &url.URL{Scheme: "https", Host: "dav.pikpak.ai"}},
	}, webDAVURL), "a persistent GET rejection from the WebDAV server is not transient CDN readiness")
}

func TestCheckWebDAVRedirectEnforcesPikPakDownloadPolicy(t *testing.T) {
	newRequest := func(method, rawURL string) *http.Request {
		req, err := http.NewRequest(method, rawURL, nil)
		require.NoError(t, err)
		return req
	}
	origin := newRequest(http.MethodGet, "https://dav.pikpak.ai/backups/dump")
	webDAVURL, err := url.Parse("https://dav.pikpak.ai/dav")
	require.NoError(t, err)

	t.Run("allows five HTTPS download redirects", func(t *testing.T) {
		via := []*http.Request{origin}
		for i := 0; i < 5; i++ {
			req := newRequest(http.MethodGet, "https://dl-a10b-1194.mypikpak.net/download")
			if i == 0 {
				req.Header.Set("Authorization", "Basic sensitive")
				req.Header.Set("Cookie", "session=sensitive")
				req.Header.Set("Cookie2", "session2=sensitive")
				req.Header.Set("Proxy-Authorization", "Basic sensitive")
				req.Header.Set("Range", "bytes=0-10")
			}
			require.NoError(t, checkWebDAVRedirect(req, via, webDAVURL))
			if i == 0 {
				require.Empty(t, req.Header.Get("Authorization"))
				require.Empty(t, req.Header.Get("Cookie"))
				require.Empty(t, req.Header.Get("Cookie2"))
				require.Empty(t, req.Header.Get("Proxy-Authorization"))
				require.Equal(t, "bytes=0-10", req.Header.Get("Range"))
			}
			via = append(via, req)
		}
		req := newRequest(http.MethodGet, "https://dl-a10b-1194.mypikpak.net/download/too-many")
		require.ErrorContains(t, checkWebDAVRedirect(req, via, webDAVURL), "too many")
	})

	tests := []struct {
		name        string
		method      string
		origin      string
		destination string
	}{
		{name: "blocks HTTP downgrade", method: http.MethodGet, origin: "https://dav.pikpak.ai/file", destination: "http://dl-a10b-1194.mypikpak.net/file"},
		{name: "blocks an untrusted host", method: http.MethodGet, origin: "https://dav.pikpak.ai/file", destination: "https://attacker.example/file"},
		{name: "blocks an IP address", method: http.MethodGet, origin: "https://dav.pikpak.ai/file", destination: "https://127.0.0.1/file"},
		{name: "blocks nonstandard ports", method: http.MethodGet, origin: "https://dav.pikpak.ai/file", destination: "https://dl-a10b-1194.mypikpak.net:8443/file"},
		{name: "blocks embedded credentials", method: http.MethodGet, origin: "https://dav.pikpak.ai/file", destination: "https://user:password@dl-a10b-1194.mypikpak.net/file"},
		{name: "blocks writes", method: http.MethodPut, origin: "https://dav.pikpak.ai/file", destination: "https://dl-a10b-1194.mypikpak.net/file"},
		{name: "blocks untrusted WebDAV origins", method: http.MethodGet, origin: "https://dav.example.test/file", destination: "https://dl-a10b-1194.mypikpak.net/file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from := newRequest(tt.method, tt.origin)
			to := newRequest(tt.method, tt.destination)
			require.Error(t, checkWebDAVRedirect(to, []*http.Request{from}, webDAVURL))
		})
	}
}

func TestNewWebDAVBackupStoreRejectsUnsafeURLAndPath(t *testing.T) {
	tests := []service.BackupS3Config{
		{WebDAVURL: "https://user:password@dav.example.test/dav", WebDAVUsername: "u", WebDAVPassword: "p"},
		{WebDAVURL: "https://dav.example.test/dav?token=x", WebDAVUsername: "u", WebDAVPassword: "p"},
		{WebDAVURL: "https://dav.example.test/dav", WebDAVUsername: "u", WebDAVPassword: "p", WebDAVPath: "backups/../private"},
	}
	for _, cfg := range tests {
		_, err := newWebDAVBackupStore(&cfg)
		require.Error(t, err)
	}
}
