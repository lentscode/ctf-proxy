package main

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

const dashboardModeEnv = "CTF_PROXY_DASHBOARD"

var loadEmbeddedDashboard = func() (fs.FS, error) {
	return nil, errors.New("embedded dashboard requires a production build")
}

// newServerHandler combines the unauthenticated, loopback-only dashboard with
// the authenticated control API. API routes are handled first so SPA fallback
// can never shadow them.
func newServerHandler(controlHandler http.Handler) (http.Handler, error) {
	if os.Getenv(dashboardModeEnv) == "disabled" {
		return controlHandler, nil
	}
	assets, err := loadEmbeddedDashboard()
	if err != nil {
		return nil, err
	}
	return newServerHandlerWithAssets(controlHandler, assets), nil
}

// newServerHandlerWithAssets provides the routing behavior independently of
// the embedded filesystem for focused tests.
func newServerHandlerWithAssets(controlHandler http.Handler, assets fs.FS) http.Handler {
	static := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			controlHandler.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if _, err := fs.Stat(assets, name); err != nil {
				if path.Ext(name) != "" {
					http.NotFound(w, r)
					return
				}
				request := r.Clone(r.Context())
				request.URL.Path = "/"
				static.ServeHTTP(w, request)
				return
			}
		}
		static.ServeHTTP(w, r)
	})
}
