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

func TestAPIManagesYAMLFiltersAndPreservesThemAfterProxyDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	handler := NewHandler(manager, []string{"test-token"})

	proxyBody := `{"name":"web","active":false,"protocol":"http","listen":"127.0.0.1:31337","upstream":"http://127.0.0.1:31338"}`
	response := serveAPI(handler, http.MethodPost, "/api/v1/proxies", proxyBody)
	require.Equal(t, http.StatusCreated, response.Code)

	source := yamlFilterDocument("block-admin", "/admin")
	response = serveAPI(handler, http.MethodPost, "/api/v1/proxies/web/filters", `{"yaml":`+jsonString(source)+`}`)
	require.Equal(t, http.StatusCreated, response.Code)
	var created ManagedFilterView
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	require.Equal(t, "block-admin", created.Name)
	require.True(t, created.Editable)
	require.Equal(t, source, created.YAML)
	require.Equal(t, []string{"web"}, created.AssignedProxies)
	require.Equal(t, []string{"block-admin"}, store.Snapshot().Proxies[0].Filters)
	require.Equal(t, source, store.Snapshot().ManagedYAMLFilters[0].YAML)

	response = serveAPI(handler, http.MethodGet, "/api/v1/proxies/web/filters", "")
	require.Equal(t, http.StatusOK, response.Code)
	var assigned struct {
		Filters []FilterView `json:"filters"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &assigned))
	require.Len(t, assigned.Filters, 1)
	require.True(t, assigned.Filters[0].Editable)

	updated := yamlFilterDocument("block-admin", "/private")
	response = serveAPI(handler, http.MethodPut, "/api/v1/filters/block-admin", `{"yaml":`+jsonString(updated)+`}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, updated, store.Snapshot().ManagedYAMLFilters[0].YAML)

	response = serveAPI(handler, http.MethodDelete, "/api/v1/filters/block-admin", "")
	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "web")

	response = serveAPI(handler, http.MethodDelete, "/api/v1/proxies/web", "")
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Len(t, store.Snapshot().ManagedYAMLFilters, 1)
	response = serveAPI(handler, http.MethodGet, "/api/v1/filters/block-admin", "")
	require.Equal(t, http.StatusOK, response.Code)

	response = serveAPI(handler, http.MethodDelete, "/api/v1/filters/block-admin", "")
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Empty(t, store.Snapshot().ManagedYAMLFilters)
}

func TestAPIManagedYAMLFilterValidation(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		arrange    func(t *testing.T, handler http.Handler)
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
		assert     func(t *testing.T, store *config.Store)
	}{
		{
			name: "invalid JSON", method: http.MethodPost, path: "/api/v1/proxies/web/filters", body: `{`, wantStatus: http.StatusBadRequest, wantBody: "invalid JSON",
		},
		{
			name: "unknown JSON field", method: http.MethodPost, path: "/api/v1/proxies/web/filters", body: `{"yaml":"version: 1\nfilters: []\n","extra":true}`, wantStatus: http.StatusBadRequest, wantBody: "unknown field",
		},
		{
			name: "zero YAML rules", method: http.MethodPost, path: "/api/v1/proxies/web/filters", body: `{"yaml":"version: 1\nfilters: []\n"}`, wantStatus: http.StatusBadRequest, wantBody: "exactly one filter",
		},
		{
			name: "missing proxy", method: http.MethodPost, path: "/api/v1/proxies/missing/filters", body: `{"yaml":` + jsonString(yamlFilterDocument("new-rule", "/admin")) + `}`, wantStatus: http.StatusNotFound,
		},
		{
			name:    "duplicate managed name",
			arrange: arrangeManagedFilter("stable"), method: http.MethodPost, path: "/api/v1/proxies/web/filters", body: `{"yaml":` + jsonString(yamlFilterDocument("stable", "/private")) + `}`, wantStatus: http.StatusConflict, wantBody: "already exists",
			assert: func(t *testing.T, store *config.Store) { require.Len(t, store.Snapshot().ManagedYAMLFilters, 1) },
		},
		{
			name:    "renamed rule",
			arrange: arrangeManagedFilter("stable"), method: http.MethodPut, path: "/api/v1/filters/stable", body: `{"yaml":` + jsonString(yamlFilterDocument("changed", "/admin")) + `}`, wantStatus: http.StatusBadRequest, wantBody: "does not match",
			assert: func(t *testing.T, store *config.Store) {
				require.Equal(t, "stable", store.Snapshot().ManagedYAMLFilters[0].Name)
			},
		},
		{
			name: "file or built-in style unknown filter detail", method: http.MethodGet, path: "/api/v1/filters/missing", wantStatus: http.StatusNotFound,
		},
		{
			name: "missing filter delete", method: http.MethodDelete, path: "/api/v1/filters/missing", wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store, handler := newFilterAPITestServer(t)
			if testCase.arrange != nil {
				testCase.arrange(t, handler)
			}
			response := serveAPI(handler, testCase.method, testCase.path, testCase.body)
			require.Equal(t, testCase.wantStatus, response.Code)
			if testCase.wantBody != "" {
				require.Contains(t, response.Body.String(), testCase.wantBody)
			}
			if testCase.assert != nil {
				testCase.assert(t, store)
			}
		})
	}
}

func newFilterAPITestServer(t *testing.T) (*config.Store, http.Handler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	handler := NewHandler(manager, []string{"test-token"})
	response := serveAPI(handler, http.MethodPost, "/api/v1/proxies", `{"name":"web","active":false,"protocol":"tcp","listen":"127.0.0.1:31337","upstream":"127.0.0.1:31338"}`)
	require.Equal(t, http.StatusCreated, response.Code)
	return store, handler
}

func arrangeManagedFilter(name string) func(t *testing.T, handler http.Handler) {
	return func(t *testing.T, handler http.Handler) {
		t.Helper()
		response := serveAPI(handler, http.MethodPost, "/api/v1/proxies/web/filters", `{"yaml":`+jsonString(yamlFilterDocument(name, "/admin"))+`}`)
		require.Equal(t, http.StatusCreated, response.Code)
	}
}

func serveAPI(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func yamlFilterDocument(name, path string) string {
	return "version: 1\nfilters:\n  - name: " + name + "\n    protocol: http\n    direction: request\n    action: reject\n    match:\n      all:\n        - field: http.path\n          operator: prefix\n          value: " + path + "\n"
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

func TestAPIProxyRoutesSupportRetrievalReplacementAndActivation(t *testing.T) {
	store, handler := newControlAPITestServer(t)
	listen := unusedTCPAddress(t)

	created := serveAPI(handler, http.MethodPost, "/api/v1/proxies", `{"name":"web","active":false,"protocol":"tcp","listen":"`+listen+`","upstream":"127.0.0.1:31338"}`)
	require.Equal(t, http.StatusCreated, created.Code)

	response := serveAPI(handler, http.MethodGet, "/api/v1/proxies/web", "")
	require.Equal(t, http.StatusOK, response.Code)
	var view ProxyView
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	require.Equal(t, StateInactive, view.State)
	require.Equal(t, "web", view.Name)

	response = serveAPI(handler, http.MethodPut, "/api/v1/proxies/web", `{"protocol":"tcp","listen":"`+listen+`","upstream":"127.0.0.1:31339"}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "active is required")

	// The URL name is authoritative: a replacement must not be able to rename
	// a configured proxy through its request body.
	response = serveAPI(handler, http.MethodPut, "/api/v1/proxies/web", `{"name":"attempted-rename","active":false,"protocol":"tcp","listen":"`+listen+`","upstream":"127.0.0.1:31339","filters":[]}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	require.Equal(t, "web", view.Name)
	require.Equal(t, "127.0.0.1:31339", view.Upstream)

	response = serveAPI(handler, http.MethodPost, "/api/v1/proxies/web/activate", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	require.True(t, view.Active)
	require.Equal(t, StateRunning, view.State)

	response = serveAPI(handler, http.MethodPost, "/api/v1/proxies/web/deactivate", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	require.False(t, view.Active)
	require.Equal(t, StateInactive, view.State)
	require.False(t, store.Snapshot().Proxies[0].Active)
}

func TestAPIRouteAndMethodValidation(t *testing.T) {
	_, handler := newControlAPITestServer(t)

	for _, testCase := range []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "health only allows GET", method: http.MethodPost, path: "/healthz", want: http.StatusMethodNotAllowed},
		{name: "proxies only allow GET or POST", method: http.MethodPut, path: "/api/v1/proxies", want: http.StatusMethodNotAllowed},
		{name: "proxy filters only allow GET or POST", method: http.MethodDelete, path: "/api/v1/proxies/web/filters", want: http.StatusMethodNotAllowed},
		{name: "filters only allow GET", method: http.MethodPost, path: "/api/v1/filters", want: http.StatusMethodNotAllowed},
		{name: "events only allow GET", method: http.MethodPost, path: "/api/v1/events", want: http.StatusMethodNotAllowed},
		{name: "stream only allows GET", method: http.MethodPost, path: "/api/v1/events/stream", want: http.StatusMethodNotAllowed},
		{name: "unknown route", method: http.MethodGet, path: "/api/v1/unknown", want: http.StatusNotFound},
		{name: "malformed proxy route", method: http.MethodGet, path: "/api/v1/proxies/web/extra", want: http.StatusNotFound},
		{name: "malformed filter route", method: http.MethodGet, path: "/api/v1/filters/a/b", want: http.StatusNotFound},
		{name: "missing proxy", method: http.MethodGet, path: "/api/v1/proxies/missing", want: http.StatusNotFound},
		{name: "cannot activate missing proxy", method: http.MethodPost, path: "/api/v1/proxies/missing/activate", want: http.StatusNotFound},
		{name: "filters for missing proxy", method: http.MethodGet, path: "/api/v1/proxies/missing/filters", want: http.StatusNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := serveAPI(handler, testCase.method, testCase.path, "")
			require.Equal(t, testCase.want, response.Code)
			if testCase.want == http.StatusMethodNotAllowed {
				require.Contains(t, response.Body.String(), `"method_not_allowed"`)
			}
		})
	}

	response := serveAPI(handler, http.MethodGet, "/api/v1/filters", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"filters":[]}`, response.Body.String())
}

func TestAPIEventsValidatesLimitAndWorksWithoutEventHub(t *testing.T) {
	_, handler := newControlAPITestServer(t)

	for _, rawLimit := range []string{"0", "-1", "not-a-number", "513"} {
		t.Run(rawLimit, func(t *testing.T) {
			response := serveAPI(handler, http.MethodGet, "/api/v1/events?limit="+rawLimit, "")
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Contains(t, response.Body.String(), "limit must be between 1 and 512")
		})
	}

	response := serveAPI(handler, http.MethodGet, "/api/v1/events?limit=512", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"events":[]}`, response.Body.String())

	response = serveAPI(handler, http.MethodGet, "/api/v1/events/stream", "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}

func newControlAPITestServer(t *testing.T) (*config.Store, http.Handler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	return store, NewHandler(manager, []string{"test-token"})
}

func unusedTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
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
