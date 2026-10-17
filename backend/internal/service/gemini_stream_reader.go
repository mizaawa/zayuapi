package service

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var errGeminiStreamIdleTimeout = errors.New("gemini stream data interval timeout")

type geminiStreamLine struct {
	line string
	err  error
}

// The request goroutine owns heartbeats; the reader only reports upstream lines.
type geminiStreamReader struct {
	body            io.ReadCloser
	reader          *bufio.Reader
	events          chan geminiStreamLine
	done            chan struct{}
	closeOnce       sync.Once
	lastReadAt      atomic.Int64
	idleInterval    time.Duration
	idleTimer       *time.Timer
	keepaliveTicker *time.Ticker
	onKeepalive     func() error
	eventInProgress bool
	sawDone         bool
	terminal        bool
}

func newGeminiStreamReader(body io.ReadCloser, cfg *config.Config, onKeepalive func() error) *geminiStreamReader {
	r := &geminiStreamReader{body: body, onKeepalive: onKeepalive}
	var keepaliveInterval time.Duration
	if cfg != nil {
		r.idleInterval = time.Duration(cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		keepaliveInterval = time.Duration(cfg.Gateway.StreamKeepaliveInterval) * time.Second
	}
	if r.idleInterval <= 0 && (keepaliveInterval <= 0 || onKeepalive == nil) {
		r.reader = bufio.NewReader(body)
		return r
	}
	r.events = make(chan geminiStreamLine, 1)
	r.done = make(chan struct{})
	r.lastReadAt.Store(time.Now().UnixNano())
	if r.idleInterval > 0 {
		r.idleTimer = time.NewTimer(r.idleInterval)
	}
	if keepaliveInterval > 0 && onKeepalive != nil {
		r.keepaliveTicker = time.NewTicker(keepaliveInterval)
	}
	go func() {
		defer close(r.events)
		reader := bufio.NewReader(&geminiStreamActivityReader{body: body, lastReadAt: &r.lastReadAt})
		for {
			line, err := reader.ReadString('\n')
			select {
			case r.events <- geminiStreamLine{line: line, err: err}:
			case <-r.done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *geminiStreamReader) ReadString() (string, error) {
	if r.terminal {
		return "", io.EOF
	}
	if r.reader != nil {
		line, err := r.reader.ReadString('\n')
		r.observeLine(line)
		return line, err
	}
	var idleCh, keepaliveCh <-chan time.Time
	if r.idleTimer != nil {
		idleCh = r.idleTimer.C
	}
	if r.keepaliveTicker != nil {
		keepaliveCh = r.keepaliveTicker.C
	}
	consume := func(event geminiStreamLine, ok bool) (string, error) {
		if !ok {
			return "", io.EOF
		}
		r.observeLine(event.line)
		return event.line, event.err
	}
	for {
		// Drain queued data before checking timeouts after a slow downstream write.
		select {
		case event, ok := <-r.events:
			return consume(event, ok)
		default:
		}
		select {
		case event, ok := <-r.events:
			return consume(event, ok)
		case <-idleCh:
			select {
			case event, ok := <-r.events:
				r.idleTimer.Reset(r.idleInterval)
				return consume(event, ok)
			default:
			}
			remaining := r.idleInterval - time.Since(time.Unix(0, r.lastReadAt.Load()))
			if remaining > 0 {
				r.idleTimer.Reset(remaining)
				continue
			}
			r.Close()
			return "", errGeminiStreamIdleTimeout
		case <-keepaliveCh:
			if !r.eventInProgress {
				if err := r.onKeepalive(); err != nil {
					// Continue bounded draining for usage after a downstream disconnect.
					r.keepaliveTicker.Stop()
					keepaliveCh = nil
				}
			}
		}
	}
}

func (r *geminiStreamReader) observeLine(line string) {
	trimmed := strings.TrimRight(line, "\r\n")
	r.eventInProgress = trimmed != ""
	if trimmed == "" && r.sawDone {
		r.terminal = true
	}
	if strings.HasPrefix(trimmed, "data:") && strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")) == "[DONE]" {
		r.sawDone = true
	}
}

func (r *geminiStreamReader) Close() {
	r.closeOnce.Do(func() {
		if r.idleTimer != nil {
			r.idleTimer.Stop()
		}
		if r.keepaliveTicker != nil {
			r.keepaliveTicker.Stop()
		}
		if r.done != nil {
			close(r.done)
		}
		_ = r.body.Close()
	})
}

type geminiStreamActivityReader struct {
	body       io.Reader
	lastReadAt *atomic.Int64
}

func (r *geminiStreamActivityReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.lastReadAt.Store(time.Now().UnixNano())
	}
	return n, err
}
