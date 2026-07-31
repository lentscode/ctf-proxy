package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lentscode/ctf-proxy/internal/metrics"
	"github.com/stretchr/testify/require"
)

func TestHTTPProxyMetricsCountForwardedTraffic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "ping", string(body))
		_, _ = io.WriteString(w, "pong")
	}))
	defer upstream.Close()
	registry := metrics.New(metrics.Schedule{CompetitionStart: time.Now().UTC().Add(-time.Minute), RoundDuration: time.Minute, RetentionRounds: 2})
	proxy := NewHTTPProxy("unused", upstream.URL, make(chan struct{}, 1), nil)
	proxy.SetMetrics(registry.Register("web", "http"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	request, err := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/", strings.NewReader("ping"))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	_, summaries, current := registry.Current()
	require.True(t, current)
	require.Len(t, summaries, 1)
	require.Equal(t, uint64(1), summaries[0].Metrics.Requests)
	require.Equal(t, uint64(1), summaries[0].Metrics.Responses)
	require.Equal(t, uint64(4), summaries[0].Metrics.ClientToUpstreamBytes)
	require.Equal(t, uint64(4), summaries[0].Metrics.UpstreamToClientBytes)
	cancel()
	requireProxyStopped(t, done)
}

func TestTCPProxyMetricsCountConnectionsChunksAndBytes(t *testing.T) {
	upstream := startEchoServer(t)
	registry := metrics.New(metrics.Schedule{CompetitionStart: time.Now().UTC().Add(-time.Minute), RoundDuration: time.Minute, RetentionRounds: 2})
	proxy := NewTCPProxy("unused", upstream, make(chan struct{}, 1), nil)
	proxy.SetMetrics(registry.Register("echo", "tcp"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	client, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	_, err = client.Write([]byte("ping"))
	require.NoError(t, err)
	require.NoError(t, client.(*net.TCPConn).CloseWrite())
	_, err = io.ReadAll(client)
	require.NoError(t, err)
	require.NoError(t, client.Close())
	require.Eventually(t, func() bool {
		_, summaries, _ := registry.Current()
		return len(summaries) == 1 && summaries[0].Metrics.ConnectionsActive == 0
	}, time.Second, 10*time.Millisecond)
	_, summaries, _ := registry.Current()
	require.Equal(t, uint64(1), summaries[0].Metrics.ConnectionsAccepted)
	require.GreaterOrEqual(t, summaries[0].Metrics.ClientChunks, uint64(1))
	require.GreaterOrEqual(t, summaries[0].Metrics.ServerChunks, uint64(1))
	require.Equal(t, uint64(4), summaries[0].Metrics.ClientToUpstreamBytes)
	require.Equal(t, uint64(4), summaries[0].Metrics.UpstreamToClientBytes)
	cancel()
	requireProxyStopped(t, done)
}
