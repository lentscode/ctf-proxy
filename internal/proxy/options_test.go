package proxy

import (
	"net"
	"testing"
	"time"

	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/stretchr/testify/require"
)

// TestHTTPOptionsDefaultsPreserveExistingLimits protects the behavior selected
// when an HTTP proxy has no http: YAML block.
func TestHTTPOptionsDefaultsPreserveExistingLimits(t *testing.T) {
	got := (HTTPOptions{}).withDefaults()
	require.Equal(t, HTTPOptions{
		DialTimeout:               3 * time.Second,
		KeepAlive:                 30 * time.Second,
		MaxIdleConnections:        64,
		MaxIdleConnectionsPerHost: 16,
		MaxConnectionsPerHost:     64,
		IdleConnectionTimeout:     30 * time.Second,
		ResponseHeaderTimeout:     5 * time.Second,
		ExpectContinueTimeout:     time.Second,
		ReadHeaderTimeout:         5 * time.Second,
		IdleTimeout:               60 * time.Second,
		MaxHeaderBytes:            1 << 20,
		ShutdownTimeout:           5 * time.Second,
	}, got)
}

// TestHTTPTransportAndServerUseConfiguredOptions verifies non-default settings
// are passed to the transport and server constructors.
func TestHTTPTransportAndServerUseConfiguredOptions(t *testing.T) {
	options := HTTPOptions{
		DialTimeout:               2 * time.Second,
		KeepAlive:                 3 * time.Second,
		MaxIdleConnections:        4,
		MaxIdleConnectionsPerHost: 5,
		MaxConnectionsPerHost:     6,
		IdleConnectionTimeout:     7 * time.Second,
		ResponseHeaderTimeout:     8 * time.Second,
		ExpectContinueTimeout:     9 * time.Second,
		ReadHeaderTimeout:         10 * time.Second,
		IdleTimeout:               11 * time.Second,
		MaxHeaderBytes:            12,
		ShutdownTimeout:           13 * time.Second,
	}
	transport := newHTTPTransport(options)
	require.Equal(t, options.MaxIdleConnections, transport.MaxIdleConns)
	require.Equal(t, options.MaxIdleConnectionsPerHost, transport.MaxIdleConnsPerHost)
	require.Equal(t, options.MaxConnectionsPerHost, transport.MaxConnsPerHost)
	require.Equal(t, options.IdleConnectionTimeout, transport.IdleConnTimeout)
	require.Equal(t, options.ResponseHeaderTimeout, transport.ResponseHeaderTimeout)
	require.Equal(t, options.ExpectContinueTimeout, transport.ExpectContinueTimeout)
	server := newHTTPServer(nil, options)
	require.Equal(t, options.ReadHeaderTimeout, server.ReadHeaderTimeout)
	require.Equal(t, options.IdleTimeout, server.IdleTimeout)
	require.Equal(t, options.MaxHeaderBytes, server.MaxHeaderBytes)
}

// TestTCPProxyWithOptionsRetainsConfiguredDeadlines verifies the TCP runner
// keeps all values needed for its dial, read, and write operations.
func TestTCPProxyWithOptionsRetainsConfiguredDeadlines(t *testing.T) {
	options := TCPOptions{DialTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second}
	proxy := NewTCPProxyWithOptions("unused", "127.0.0.1:1", nil, nil, options)
	require.Equal(t, options, proxy.options)
}

// TestTCPReadTimeoutAppliesADeadline verifies an idle client cannot hold a TCP
// forwarding goroutine forever when a read timeout is configured.
func TestTCPReadTimeoutAppliesADeadline(t *testing.T) {
	src, peer := net.Pipe()
	dst, target := net.Pipe()
	t.Cleanup(func() {
		_ = src.Close()
		_ = peer.Close()
		_ = dst.Close()
		_ = target.Close()
	})
	proxy := NewTCPProxyWithOptions("unused", "unused", nil, nil, TCPOptions{ReadTimeout: 20 * time.Millisecond})

	err := proxy.copy(dst, src, filter.DirectionRequest, filter.ConnectionInfo{})
	require.Error(t, err)
	networkErr, ok := err.(net.Error)
	require.True(t, ok)
	require.True(t, networkErr.Timeout())
}

// TestTCPWriteTimeoutAppliesADeadline verifies an unresponsive destination is
// bounded even after the source has delivered data.
func TestTCPWriteTimeoutAppliesADeadline(t *testing.T) {
	src, peer := net.Pipe()
	dst, target := net.Pipe()
	t.Cleanup(func() {
		_ = src.Close()
		_ = peer.Close()
		_ = dst.Close()
		_ = target.Close()
	})
	proxy := NewTCPProxyWithOptions("unused", "unused", nil, nil, TCPOptions{WriteTimeout: 20 * time.Millisecond})
	go func() { _, _ = peer.Write([]byte("payload")) }()

	err := proxy.copy(dst, src, filter.DirectionRequest, filter.ConnectionInfo{})
	require.Error(t, err)
	networkErr, ok := err.(net.Error)
	require.True(t, ok)
	require.True(t, networkErr.Timeout())
}
