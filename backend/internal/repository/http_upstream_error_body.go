package repository

import (
	"io"
	"net/http"
	"sync"
	"time"
)

const upstreamErrorBodyTimeout = 10 * time.Second

type upstreamErrorBodyTransport struct {
	base    http.RoundTripper
	timeout time.Duration
}

func (t *upstreamErrorBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	boundUpstreamErrorBody(resp, t.timeout)
	return resp, err
}

// Error and redirect bodies must not block failover or the client's redirect drain.
func boundUpstreamErrorBody(resp *http.Response, timeout time.Duration) {
	if resp == nil || resp.StatusCode < http.StatusMultipleChoices || resp.Body == nil {
		return
	}
	body := &upstreamErrorBody{body: resp.Body}
	body.timer = time.AfterFunc(timeout, body.closeBody)
	resp.Body = body
}

type upstreamErrorBody struct {
	body      io.ReadCloser
	timer     *time.Timer
	closeOnce sync.Once
	closeErr  error
}

func (b *upstreamErrorBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err != nil {
		b.timer.Stop()
	}
	return n, err
}

func (b *upstreamErrorBody) closeBody() {
	b.closeOnce.Do(func() { b.closeErr = b.body.Close() })
}

func (b *upstreamErrorBody) Close() error {
	b.timer.Stop()
	b.closeBody()
	return b.closeErr
}
