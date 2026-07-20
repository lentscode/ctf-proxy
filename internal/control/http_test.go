package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lentscode/ctf-proxy/internal/config"
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

	handler := NewHandler(manager)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/proxies", strings.NewReader(`{"name":"staged","active":false,"protocol":"tcp","listen":"127.0.0.1:31337","upstream":"127.0.0.1:31338"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)

	request = httptest.NewRequest(http.MethodGet, "/api/v1/proxies", nil)
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
	response := httptest.NewRecorder()
	NewHandler(manager).ServeHTTP(response, request)
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
	response := httptest.NewRecorder()
	NewHandler(manager).ServeHTTP(response, request)
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
