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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	proxyAddr, cancel, serveDone := startHTTPProxy(t, upstream.URL, make(chan struct{}, 1))

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

	cancel()
	requireProxyStopped(t, serveDone)
}

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

func startHTTPProxy(t *testing.T, upstreamURL string, slots chan struct{}) (string, context.CancelFunc, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	proxy := NewHTTPProxy("unused", upstreamURL, slots)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- proxy.serve(ctx, listener)
	}()

	return listener.Addr().String(), cancel, serveDone
}

func requireProxyStopped(t *testing.T, serveDone <-chan error) {
	t.Helper()

	select {
	case err := <-serveDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop")
	}
}
