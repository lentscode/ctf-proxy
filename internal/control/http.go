package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/lentscode/ctf-proxy/internal/config"
)

// ListenLoopback binds addr only if its host resolves exclusively to loopback
// addresses. The management API is authenticated, but loopback binding remains
// a separate defense-in-depth boundary.
func ListenLoopback(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid control address: %w", err)
	}
	if host == "" {
		return nil, errors.New("control address must name a loopback host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return nil, errors.New("control address must be loopback while authentication is disabled")
		}
	} else {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("resolve control address: %w", err)
		}
		for _, ip := range ips {
			if !ip.IsLoopback() {
				return nil, errors.New("control address must resolve only to loopback addresses")
			}
		}
	}
	return net.Listen("tcp", addr)
}

// NewHandler returns the local control-plane HTTP handler.
func NewHandler(manager *Manager, tokens []string) http.Handler {
	return &api{manager: manager, tokens: tokens}
}

type api struct {
	manager *Manager
	tokens  []string
}

type proxyInput struct {
	Name     string   `json:"name"`
	Active   *bool    `json:"active"`
	Protocol string   `json:"protocol"`
	Listen   string   `json:"listen"`
	Upstream string   `json:"upstream"`
	Filters  []string `json:"filters"`
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := a.checkAuthMiddleware(r); err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return
	}

	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if r.URL.Path == "/api/v1/proxies" {
		a.proxies(w, r)
		return
	}
	if r.URL.Path == "/api/v1/filters" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"filters": a.manager.ListFilters()})
		return
	}
	const prefix = "/api/v1/proxies/"
	if strings.HasPrefix(r.URL.Path, prefix) {
		a.proxy(w, r, strings.TrimPrefix(r.URL.Path, prefix))
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "route not found")
}

func (a *api) proxies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"proxies": a.manager.List()})
	case http.MethodPost:
		var input proxyInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		definition := input.definition(true)
		view, err := a.manager.Create(definition)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, view)
	default:
		methodNotAllowed(w)
	}
}

func (a *api) proxy(w http.ResponseWriter, r *http.Request, tail string) {
	parts := strings.Split(tail, "/")
	if len(parts) == 2 && (parts[1] == "activate" || parts[1] == "deactivate") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		view, err := a.manager.SetActive(parts[0], parts[1] == "activate")
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	name := parts[0]
	switch r.Method {
	case http.MethodGet:
		view, err := a.manager.Get(name)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		var input proxyInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		if input.Active == nil {
			writeError(w, http.StatusBadRequest, "validation_error", "active is required for replacement")
			return
		}
		view, err := a.manager.Replace(name, input.definition(false))
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodDelete:
		if err := a.manager.Delete(name); err != nil {
			writeManagerError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w)
	}
}

func (i proxyInput) definition(defaultActive bool) config.Proxy {
	active := defaultActive
	if i.Active != nil {
		active = *i.Active
	}
	return config.Proxy{Name: i.Name, Active: active, Protocol: i.Protocol, Listen: i.Listen, Upstream: i.Upstream, Filters: i.Filters}
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func writeManagerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, ErrPersistence):
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
