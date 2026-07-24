package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// TestDefaultControlAddressIsLoopback protects the loopback-only default.
func TestDefaultControlAddressIsLoopback(t *testing.T) {
	testCases := []struct {
		name string
		got  string
		want string
	}{
		{name: "control API", got: defaultControlAddr, want: "127.0.0.1:8081"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("unexpected default control address %q", testCase.got)
			}
		})
	}
}

// TestServerHandlerServesDashboardWithoutShadowingControlRoutes covers the
// static asset, SPA fallback, and control-plane route boundaries.
func TestServerHandlerServesDashboardWithoutShadowingControlRoutes(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("<main>dashboard</main>")},
		"assets/app.js":    &fstest.MapFile{Data: []byte("console.log('app')")},
		"assets/app.css":   &fstest.MapFile{Data: []byte("body {}")},
		"nested/asset.txt": &fstest.MapFile{Data: []byte("asset")},
	}
	control := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Control-Route", r.URL.Path)
		w.WriteHeader(http.StatusUnauthorized)
	})
	handler := newServerHandlerWithAssets(control, assets)

	for _, testCase := range []struct {
		name        string
		path        string
		method      string
		wantStatus  int
		wantBody    string
		controlPath string
	}{
		{name: "dashboard root", path: "/", method: http.MethodGet, wantStatus: http.StatusOK, wantBody: "<main>dashboard</main>"},
		{name: "dashboard asset", path: "/assets/app.js", method: http.MethodGet, wantStatus: http.StatusOK, wantBody: "console.log('app')"},
		{name: "client route fallback", path: "/proxies/new", method: http.MethodGet, wantStatus: http.StatusOK, wantBody: "<main>dashboard</main>"},
		{name: "missing asset", path: "/assets/missing.js", method: http.MethodGet, wantStatus: http.StatusNotFound},
		{name: "api", path: "/api/v1/proxies", method: http.MethodGet, wantStatus: http.StatusUnauthorized, controlPath: "/api/v1/proxies"},
		{name: "health", path: "/healthz", method: http.MethodGet, wantStatus: http.StatusUnauthorized, controlPath: "/healthz"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, testCase.wantStatus)
			}
			if testCase.wantBody != "" && response.Body.String() != testCase.wantBody {
				t.Fatalf("body = %q, want %q", response.Body.String(), testCase.wantBody)
			}
			if testCase.controlPath != "" && response.Header().Get("X-Control-Route") != testCase.controlPath {
				t.Fatalf("control route = %q, want %q", response.Header().Get("X-Control-Route"), testCase.controlPath)
			}
		})
	}
}

// TestNewServerHandlerCanDisableTheEmbeddedDashboard supports Vite-managed
// development without requiring production assets first.
func TestNewServerHandlerCanDisableTheEmbeddedDashboard(t *testing.T) {
	t.Setenv(dashboardModeEnv, "disabled")
	control := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler, err := newServerHandler(control)
	if err != nil {
		t.Fatalf("newServerHandler: %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
