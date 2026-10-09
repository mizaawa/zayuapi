package repository

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func http2KeepAliveTestPoolSettings() poolSettings {
	return poolSettings{
		maxIdleConns:          10,
		maxIdleConnsPerHost:   5,
		maxConnsPerHost:       10,
		idleConnTimeout:       90 * time.Second,
		responseHeaderTimeout: time.Minute,
	}
}

// Codex/OpenAI 上游改走 HTTP/2 后，池化连接被代理/NAT 静默掐断会成为“死连接”：
// 两端都以为连接存活，请求落上去会挂到 TCP 重传超时（分钟级）才失败。Go 的
// http2.Transport 默认 ReadIdleTimeout=0（不发健康 PING），无法检测这种死连接。
// 必须显式启用主动 PING 探测，让死连接被提前剔除，而不是只靠 ResponseHeaderTimeout
// 事后兜底。
func TestEnableOpenAIHTTP2KeepAlive_EnablesPingHealthCheck(t *testing.T) {
	tr := &http.Transport{}

	h2, err := enableOpenAIHTTP2KeepAlive(tr)
	require.NoError(t, err)
	require.NotNil(t, h2, "必须返回已配置的 *http2.Transport")

	require.Positive(t, h2.ReadIdleTimeout, "必须启用空闲 PING 探测以剔除死连接")
	require.Equal(t, openAIHTTP2ReadIdleTimeout, h2.ReadIdleTimeout)
	require.Equal(t, openAIHTTP2PingTimeout, h2.PingTimeout, "PING 无响应必须有超时判定")
	require.NotNil(t, tr.Protocols)
	require.True(t, tr.Protocols.HTTP2(), "http2 必须已挂到底层 http.Transport 上")
}

// openai_h2 模式构建的 Transport 必须带上 H2 PING 健康探测，从源头剔除死连接。
func TestBuildUpstreamTransport_OpenAIH2_EnablesPingHealthCheck(t *testing.T) {
	tr, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeOpenAIH2)
	require.NoError(t, err)
	require.True(t, tr.ForceAttemptHTTP2, "openai_h2 必须启用 HTTP/2")
	require.NotNil(t, tr.Protocols)
	require.True(t, tr.Protocols.HTTP2(), "openai_h2 必须显式配置 http2 以启用 ReadIdleTimeout")
}

// 非 H2 模式（default/h1）不应因本次改动被误配置：default 走 Go 自动 H2（惰性配置，
// 构建时 TLSNextProto 仍为空），h1 模式显式禁用 H2。避免波及 Claude/Gemini 热路径。
func TestBuildUpstreamTransport_NonOpenAIH2_NotEagerlyConfigured(t *testing.T) {
	tr, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeDefault)
	require.NoError(t, err)
	require.Nil(t, tr.TLSNextProto["h2"], "default 模式不应在构建期主动配置 http2 keepalive")
}

// 死连接在经 HTTP 代理（CONNECT 隧道）时最高发，这是带 proxy 账号的真实生产路径：
// 显式 http2 配置须与 Transport.Proxy 同时正确生效，不能相互干扰。
func TestBuildUpstreamTransport_OpenAIH2_WithHTTPProxy_EnablesKeepAlive(t *testing.T) {
	proxyURL, err := url.Parse("http://127.0.0.1:8080")
	require.NoError(t, err)

	tr, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), proxyURL, upstreamProtocolModeOpenAIH2)
	require.NoError(t, err)
	require.True(t, tr.ForceAttemptHTTP2)
	require.NotNil(t, tr.Protocols)
	require.True(t, tr.Protocols.HTTP2(), "经代理的 openai_h2 也必须启用 http2 keepalive")
	require.NotNil(t, tr.Proxy, "HTTP 代理仍须通过 Transport.Proxy 生效")
}

func TestBuildUpstreamTransport_OpenAIH2_NegotiatesHTTP2(t *testing.T) {
	for _, useProxy := range []bool{false, true} {
		t.Run(fmt.Sprintf("proxy=%t", useProxy), func(t *testing.T) {
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, r.Proto)
			}))
			upstream.EnableHTTP2 = true
			upstream.StartTLS()
			defer upstream.Close()

			var proxyURL *url.URL
			var connectCount atomic.Int64
			if useProxy {
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodConnect || r.Host != upstream.Listener.Addr().String() {
						http.Error(w, "unexpected CONNECT target", http.StatusBadRequest)
						return
					}
					target, dialErr := net.DialTimeout("tcp", upstream.Listener.Addr().String(), time.Second)
					if dialErr != nil {
						http.Error(w, dialErr.Error(), http.StatusBadGateway)
						return
					}
					defer func() { _ = target.Close() }()
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						http.Error(w, "CONNECT hijacking unavailable", http.StatusInternalServerError)
						return
					}
					client, rw, hijackErr := hijacker.Hijack()
					if hijackErr != nil {
						return
					}
					defer func() { _ = client.Close() }()
					if _, writeErr := rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); writeErr != nil {
						return
					}
					if flushErr := rw.Flush(); flushErr != nil {
						return
					}
					connectCount.Add(1)
					done := make(chan struct{})
					go func() {
						_, _ = io.Copy(target, client)
						_ = target.Close()
						close(done)
					}()
					_, _ = io.Copy(client, target)
					_ = client.Close()
					<-done
				}))
				defer proxy.Close()
				var err error
				proxyURL, err = url.Parse(proxy.URL)
				require.NoError(t, err)
			}

			tr, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), proxyURL, upstreamProtocolModeOpenAIH2)
			require.NoError(t, err)
			trustedTransport, ok := upstream.Client().Transport.(*http.Transport)
			require.True(t, ok)
			tr.TLSClientConfig = trustedTransport.TLSClientConfig.Clone()
			defer tr.CloseIdleConnections()
			client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
			response, err := client.Get(upstream.URL)
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, 2, response.ProtoMajor)
			require.Equal(t, "HTTP/2.0", string(body))
			if useProxy {
				require.EqualValues(t, 1, connectCount.Load(), "请求必须经过 CONNECT 代理")
			}
		})
	}
}

// Verify the configured x/net transport is still linked to net/http in Go 1.27:
// changing its keepalive values after ConfigureTransports must cause wire PINGs
// and close a connection whose peer deliberately never acknowledges them.
func TestEnableOpenAIHTTP2KeepAlive_UnansweredPingClosesConnection(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certificateServer.TLS.Certificates[0]
	trustedTransport, ok := certificateServer.Client().Transport.(*http.Transport)
	require.True(t, ok)
	clientTLS := trustedTransport.TLSClientConfig.Clone()
	certificateServer.Close()

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{"h2"},
		MinVersion:   tls.VersionTLS12,
	})
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	type peerResult struct {
		pingReceived bool
		err          error
	}
	peerDone := make(chan peerResult, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			peerDone <- peerResult{err: acceptErr}
			return
		}
		defer func() { _ = conn.Close() }()
		if deadlineErr := conn.SetDeadline(time.Now().Add(3 * time.Second)); deadlineErr != nil {
			peerDone <- peerResult{err: deadlineErr}
			return
		}
		preface := make([]byte, len(http2.ClientPreface))
		if _, readErr := io.ReadFull(conn, preface); readErr != nil {
			peerDone <- peerResult{err: readErr}
			return
		}
		if string(preface) != http2.ClientPreface {
			peerDone <- peerResult{err: errors.New("HTTP/2 client preface missing")}
			return
		}
		framer := http2.NewFramer(conn, conn)
		if writeErr := framer.WriteSettings(); writeErr != nil {
			peerDone <- peerResult{err: writeErr}
			return
		}
		pingReceived := false
		for {
			frame, readErr := framer.ReadFrame()
			if readErr != nil {
				peerDone <- peerResult{pingReceived: pingReceived, err: readErr}
				return
			}
			switch frame := frame.(type) {
			case *http2.SettingsFrame:
				if !frame.IsAck() {
					if writeErr := framer.WriteSettingsAck(); writeErr != nil {
						peerDone <- peerResult{err: writeErr}
						return
					}
				}
			case *http2.PingFrame:
				if !frame.IsAck() {
					pingReceived = true // Deliberately do not acknowledge this PING.
				}
			}
		}
	}()

	tr := &http.Transport{TLSClientConfig: clientTLS}
	h2, err := enableOpenAIHTTP2KeepAlive(tr)
	require.NoError(t, err)
	h2.ReadIdleTimeout = 50 * time.Millisecond
	h2.PingTimeout = 50 * time.Millisecond
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	_, err = client.Get("https://" + listener.Addr().String())
	require.Error(t, err, "未响应 PING 的连接必须失败并关闭")
	result := <-peerDone
	require.True(t, result.pingReceived, "底层 HTTP/2 transport 必须按配置发送 PING")
	require.ErrorIs(t, result.err, io.EOF, "必须由客户端关闭连接，不能依赖测试 deadline")
}
