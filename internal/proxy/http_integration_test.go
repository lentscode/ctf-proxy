package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPProxyServeForwardsRequestAndResponse covers the normal HTTP data path.
func TestHTTPProxyServeForwardsRequestAndResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/widgets", r.URL.Path)
		assert.Equal(t, "enabled", r.URL.Query().Get("filter"))
		assert.Equal(t, "request-value", r.Header.Get("X-Request-Header"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "request body", string(body))

		w.Header().Set("X-Response-Header", "response-value")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	var sawRequestBody atomic.Bool
	var sawResponseBody atomic.Bool
	chain, err := filter.NewChain(proxyTestFilter{
		name: "observe-bodies",
		requirements: filter.Requirements{
			Protocols:     []filter.Protocol{filter.ProtocolHTTP},
			NeedsHTTPBody: true,
		},
		evaluate: func(message filter.Message) (filter.Decision, error) {
			switch message.Direction {
			case filter.DirectionRequest:
				sawRequestBody.Store(string(message.HTTP.Body) == "request body")
			case filter.DirectionResponse:
				sawResponseBody.Store(string(message.HTTP.Body) == "upstream response")
			}
			return filter.Decision{Action: filter.ActionAllow}, nil
		},
	})
	require.NoError(t, err)

	proxyAddr, cancel, serveDone := startHTTPProxyWithFilters(t, upstream.URL, make(chan struct{}, 1), chain)

	request, err := http.NewRequest(http.MethodPost, "http://"+proxyAddr+"/widgets?filter=enabled", strings.NewReader("request body"))
	require.NoError(t, err)
	request.Header.Set("X-Request-Header", "request-value")

	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, response.StatusCode)
	assert.Equal(t, "response-value", response.Header.Get("X-Response-Header"))
	assert.Equal(t, "upstream response", string(body))
	assert.True(t, sawRequestBody.Load())
	assert.True(t, sawResponseBody.Load())

	cancel()
	requireProxyStopped(t, serveDone)
}

// TestHTTPProxyServeRejectsRequestsWhenSlotsAreFull protects the request budget.
func TestHTTPProxyServeRejectsRequestsWhenSlotsAreFull(t *testing.T) {
	requestStarted := make(chan struct{})
	allowResponse := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		<-allowResponse
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	proxyAddr, cancel, serveDone := startHTTPProxy(t, upstream.URL, make(chan struct{}, 1))
	client := &http.Client{Timeout: time.Second}

	firstDone := make(chan *http.Response, 1)
	firstErr := make(chan error, 1)
	go func() {
		response, err := client.Get("http://" + proxyAddr + "/slow")
		if err != nil {
			firstErr <- err
			return
		}
		firstDone <- response
	}()

	<-requestStarted

	response, err := client.Get("http://" + proxyAddr + "/rejected")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)

	close(allowResponse)
	select {
	case err := <-firstErr:
		require.NoError(t, err)
	case response := <-firstDone:
		response.Body.Close()
		assert.Equal(t, http.StatusNoContent, response.StatusCode)
	case <-time.After(time.Second):
		t.Fatal("first request did not complete")
	}

	cancel()
	requireProxyStopped(t, serveDone)
}

// TestHTTPProxyRejectsRequestBeforeUpstream verifies request filtering prevents forwarding.
func TestHTTPProxyRejectsRequestBeforeUpstream(t *testing.T) {
	upstreamCalled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalled <- struct{}{}
	}))
	defer upstream.Close()

	chain, err := filter.NewChain(proxyTestFilter{
		name: "reject-admin",
		requirements: filter.Requirements{
			Protocols:  []filter.Protocol{filter.ProtocolHTTP},
			Directions: []filter.Direction{filter.DirectionRequest},
		},
		evaluate: func(message filter.Message) (filter.Decision, error) {
			if message.HTTP.Path == "/admin?debug=true" {
				return filter.Decision{Action: filter.ActionReject}, nil
			}
			return filter.Decision{Action: filter.ActionAllow}, nil
		},
	})
	require.NoError(t, err)

	proxyAddr, cancel, serveDone := startHTTPProxyWithFilters(t, upstream.URL, make(chan struct{}, 1), chain)
	defer cancel()
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + proxyAddr + "/admin?debug=true")
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, "request rejected by proxy", string(body))

	select {
	case <-upstreamCalled:
		t.Fatal("rejected request reached upstream")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	requireProxyStopped(t, serveDone)
}

// TestHTTPProxyRejectsResponseBeforeClient verifies response filtering hides rejected data.
func TestHTTPProxyRejectsResponseBeforeClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Blocked", "true")
		_, _ = w.Write([]byte("upstream secret"))
	}))
	defer upstream.Close()

	chain, err := filter.NewChain(proxyTestFilter{
		name: "reject-response",
		requirements: filter.Requirements{
			Protocols:  []filter.Protocol{filter.ProtocolHTTP},
			Directions: []filter.Direction{filter.DirectionResponse},
		},
		evaluate: func(message filter.Message) (filter.Decision, error) {
			if message.HTTP.Header.Get("X-Blocked") == "true" {
				return filter.Decision{Action: filter.ActionReject}, nil
			}
			return filter.Decision{Action: filter.ActionAllow}, nil
		},
	})
	require.NoError(t, err)

	proxyAddr, cancel, serveDone := startHTTPProxyWithFilters(t, upstream.URL, make(chan struct{}, 1), chain)
	defer cancel()
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + proxyAddr + "/")
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, "request rejected by proxy", string(body))

	cancel()
	requireProxyStopped(t, serveDone)
}

// TestHTTPProxyRejectsRequestBodyBeforeUpstream covers buffered request-body filtering.
func TestHTTPProxyRejectsRequestBodyBeforeUpstream(t *testing.T) {
	upstreamCalled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalled <- struct{}{}
	}))
	defer upstream.Close()

	chain, err := filter.NewChain(proxyTestFilter{
		name: "reject-body",
		requirements: filter.Requirements{
			Protocols:     []filter.Protocol{filter.ProtocolHTTP},
			Directions:    []filter.Direction{filter.DirectionRequest},
			NeedsHTTPBody: true,
		},
		evaluate: func(message filter.Message) (filter.Decision, error) {
			if string(message.HTTP.Body) == "blocked body" {
				return filter.Decision{Action: filter.ActionReject}, nil
			}
			return filter.Decision{Action: filter.ActionAllow}, nil
		},
	})
	require.NoError(t, err)

	proxyAddr, cancel, serveDone := startHTTPProxyWithFilters(t, upstream.URL, make(chan struct{}, 1), chain)
	defer cancel()
	response, err := (&http.Client{Timeout: time.Second}).Post("http://"+proxyAddr+"/", "text/plain", strings.NewReader("blocked body"))
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, "request rejected by proxy", string(body))

	select {
	case <-upstreamCalled:
		t.Fatal("rejected request body reached upstream")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	requireProxyStopped(t, serveDone)
}

// TestHTTPProxyRejectsResponseBodyBeforeClient covers buffered response-body filtering.
func TestHTTPProxyRejectsResponseBodyBeforeClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("upstream secret"))
	}))
	defer upstream.Close()

	chain, err := filter.NewChain(proxyTestFilter{
		name: "reject-response-body",
		requirements: filter.Requirements{
			Protocols:     []filter.Protocol{filter.ProtocolHTTP},
			Directions:    []filter.Direction{filter.DirectionResponse},
			NeedsHTTPBody: true,
		},
		evaluate: func(message filter.Message) (filter.Decision, error) {
			if string(message.HTTP.Body) == "upstream secret" {
				return filter.Decision{Action: filter.ActionReject}, nil
			}
			return filter.Decision{Action: filter.ActionAllow}, nil
		},
	})
	require.NoError(t, err)

	proxyAddr, cancel, serveDone := startHTTPProxyWithFilters(t, upstream.URL, make(chan struct{}, 1), chain)
	defer cancel()
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + proxyAddr + "/")
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, "request rejected by proxy", string(body))

	cancel()
	requireProxyStopped(t, serveDone)
}

// TestHTTPProxyPreservesOversizedRequestBodyWhenFiltering protects oversized request replay.
func TestHTTPProxyPreservesOversizedRequestBodyWhenFiltering(t *testing.T) {
	payload := strings.Repeat("x", int(DefaultMaxFilterBodyBytes)+1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, payload, string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	var skipped atomic.Bool
	chain, err := filter.NewChain(proxyTestFilter{
		name: "body-filter",
		requirements: filter.Requirements{
			Protocols:     []filter.Protocol{filter.ProtocolHTTP},
			Directions:    []filter.Direction{filter.DirectionRequest},
			NeedsHTTPBody: true,
		},
		evaluate: func(message filter.Message) (filter.Decision, error) {
			skipped.Store(message.HTTP.BodySkipped)
			return filter.Decision{Action: filter.ActionAllow}, nil
		},
	})
	require.NoError(t, err)

	proxyAddr, cancel, serveDone := startHTTPProxyWithFilters(t, upstream.URL, make(chan struct{}, 1), chain)
	defer cancel()
	response, err := (&http.Client{Timeout: time.Second}).Post("http://"+proxyAddr+"/", "application/octet-stream", strings.NewReader(payload))
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
	assert.True(t, skipped.Load())

	cancel()
	requireProxyStopped(t, serveDone)
}

// TestHTTPProxyPreservesOversizedResponseBodyWhenFiltering protects oversized response replay.
func TestHTTPProxyPreservesOversizedResponseBodyWhenFiltering(t *testing.T) {
	payload := strings.Repeat("x", int(DefaultMaxFilterBodyBytes)+1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer upstream.Close()

	var skipped atomic.Bool
	chain, err := filter.NewChain(proxyTestFilter{
		name: "response-body-filter",
		requirements: filter.Requirements{
			Protocols:     []filter.Protocol{filter.ProtocolHTTP},
			Directions:    []filter.Direction{filter.DirectionResponse},
			NeedsHTTPBody: true,
		},
		evaluate: func(message filter.Message) (filter.Decision, error) {
			skipped.Store(message.HTTP.BodySkipped)
			return filter.Decision{Action: filter.ActionAllow}, nil
		},
	})
	require.NoError(t, err)

	proxyAddr, cancel, serveDone := startHTTPProxyWithFilters(t, upstream.URL, make(chan struct{}, 1), chain)
	defer cancel()
	response, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + proxyAddr + "/")
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, payload, string(body))
	assert.True(t, skipped.Load())

	cancel()
	requireProxyStopped(t, serveDone)
}

// startHTTPProxy starts an HTTP proxy without filters for integration tests.
func startHTTPProxy(t *testing.T, upstreamURL string, slots chan struct{}) (string, context.CancelFunc, <-chan error) {
	return startHTTPProxyWithFilters(t, upstreamURL, slots, nil)
}

// startHTTPProxyWithFilters starts an HTTP proxy with the supplied chain.
func startHTTPProxyWithFilters(t *testing.T, upstreamURL string, slots chan struct{}, filters *filter.Chain) (string, context.CancelFunc, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	proxy := NewHTTPProxy("unused", upstreamURL, slots, filters)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- proxy.serve(ctx, listener)
	}()

	return listener.Addr().String(), cancel, serveDone
}

// requireProxyStopped waits for clean shutdown of an integration proxy.
func requireProxyStopped(t *testing.T, serveDone <-chan error) {
	t.Helper()

	select {
	case err := <-serveDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop")
	}
}
