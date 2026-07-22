package control

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/observe"
	"github.com/stretchr/testify/require"
)

func TestAPIStartsEmptyAndManagesInactiveProxy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)

	handler := NewHandler(manager, []string{"test-token"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/proxies", strings.NewReader(`{"name":"staged","active":false,"protocol":"tcp","listen":"127.0.0.1:31337","upstream":"127.0.0.1:31338"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)

	request = httptest.NewRequest(http.MethodGet, "/api/v1/proxies", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Proxies []ProxyView `json:"proxies"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Proxies, 1)
	require.Equal(t, StateInactive, body.Proxies[0].State)

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/proxies/staged", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Empty(t, store.Snapshot().Proxies)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "proxies: []")
}

func TestAPIRejectsUnknownFilterWithoutPersisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/proxies", strings.NewReader(`{"name":"web","active":false,"protocol":"tcp","listen":"127.0.0.1:31337","upstream":"127.0.0.1:31338","filters":["missing"]}`))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	NewHandler(manager, []string{"test-token"}).ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Empty(t, store.Snapshot().Proxies)
}

func TestAPIReportsListenerConflict(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()

	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)

	body := `{"name":"web","protocol":"tcp","listen":"` + occupied.Addr().String() + `","upstream":"127.0.0.1:31338"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/proxies", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	NewHandler(manager, []string{"test-token"}).ServeHTTP(response, request)
	require.Equal(t, http.StatusConflict, response.Code)
	require.Empty(t, store.Snapshot().Proxies)
}

func TestListenLoopback(t *testing.T) {
	listener, err := ListenLoopback("127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	_, err = ListenLoopback("0.0.0.0:0")
	require.ErrorContains(t, err, "loopback")
}

func TestAPIRequiresValidBearerToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	handler := NewHandler(manager, []string{"correct-token"})

	for _, authorization := range []string{"", "Basic correct-token", "Bearer wrong-token"} {
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.Equal(t, "Bearer", response.Header().Get("WWW-Authenticate"))
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("Authorization", "Bearer correct-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
}

func TestTokenFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens")
	token, err := GenerateToken()
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.NoError(t, SaveTokens(path, []string{token}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	tokens, err := LoadTokens(path)
	require.NoError(t, err)
	require.Equal(t, []string{token}, tokens)
}

func TestAPIEventsSnapshotRequiresAuthAndDoesNotConsume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	observer := observe.NewObserver(io.Discard)
	t.Cleanup(observer.Close)
	hub := observer.Hub()
	observer.Report(observe.Event{Level: observe.LevelWarn, Component: observe.ComponentFilter, Kind: observe.KindFilterRejected, Message: "rejected"})
	handler := NewHandler(manager, []string{"test-token"}, hub)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)

	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Events []observe.Event `json:"events"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Events, 1)
	assertSnapshot := hub.Snapshot(1)
	require.Len(t, assertSnapshot, 1)
	require.Equal(t, body.Events[0].ID, assertSnapshot[0].ID)

	request = httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=513", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAPIEventStreamDeliversNewEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	observer := observe.NewObserver(io.Discard)
	t.Cleanup(observer.Close)
	hub := observer.Hub()
	server := httptest.NewServer(NewHandler(manager, []string{"test-token"}, hub))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events/stream", nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer test-token")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))

	reader := bufio.NewReader(response.Body)
	_, err = reader.ReadString('\n') // initial comment
	require.NoError(t, err)
	_, err = reader.ReadString('\n')
	require.NoError(t, err)
	observer.Report(observe.Event{Level: observe.LevelWarn, Component: observe.ComponentFilter, Kind: observe.KindFilterRejected, Message: "rejected"})
	result := make(chan string, 1)
	go func() {
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			if strings.HasPrefix(line, "data: ") {
				result <- line
				return
			}
		}
	}()
	select {
	case line := <-result:
		require.Contains(t, line, `"kind":"filter_rejected"`)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}
