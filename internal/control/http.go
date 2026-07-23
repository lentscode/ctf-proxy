package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/observe"
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
func NewHandler(manager *Manager, tokens []string, hubs ...*observe.Hub) http.Handler {
	var hub *observe.Hub
	if len(hubs) > 0 {
		hub = hubs[0]
	}
	return &api{manager: manager, tokens: tokens, hub: hub}
}

type api struct {
	manager *Manager
	tokens  []string
	hub     *observe.Hub
}

type proxyInput struct {
	Name     string   `json:"name"`
	Active   *bool    `json:"active"`
	Protocol string   `json:"protocol"`
	Listen   string   `json:"listen"`
	Upstream string   `json:"upstream"`
	Filters  []string `json:"filters"`
}

type yamlFilterInput struct {
	YAML string `json:"yaml"`
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
	const filtersPrefix = "/api/v1/filters/"
	if strings.HasPrefix(r.URL.Path, filtersPrefix) {
		a.filter(w, r, strings.TrimPrefix(r.URL.Path, filtersPrefix))
		return
	}
	if r.URL.Path == "/api/v1/events" {
		a.events(w, r)
		return
	}
	if r.URL.Path == "/api/v1/events/stream" {
		a.eventStream(w, r)
		return
	}
	const prefix = "/api/v1/proxies/"
	if strings.HasPrefix(r.URL.Path, prefix) {
		a.proxy(w, r, strings.TrimPrefix(r.URL.Path, prefix))
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "route not found")
}

func (a *api) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > observe.HistoryCapacity {
			writeError(w, http.StatusBadRequest, "validation_error", fmt.Sprintf("limit must be between 1 and %d", observe.HistoryCapacity))
			return
		}
		limit = parsed
	}
	if a.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []observe.Event{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": a.hub.Snapshot(limit)})
}

func (a *api) eventStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if a.hub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "event streaming is unavailable")
		return
	}
	subscription, ok := a.hub.Subscribe()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "event stream capacity reached")
		return
	}
	defer a.hub.Unsubscribe(subscription)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	controller := http.NewResponseController(w)
	if !writeSSEComment(w, controller, "connected") {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-subscription.Events():
			if !ok || !writeSSEEvent(w, controller, event) {
				return
			}
		case <-ticker.C:
			if !writeSSEComment(w, controller, "heartbeat") {
				return
			}
		}
	}
}

func writeSSEComment(w http.ResponseWriter, controller *http.ResponseController, value string) bool {
	_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := fmt.Fprintf(w, ": %s\n\n", value); err != nil {
		return false
	}
	return controller.Flush() == nil
}

func writeSSEEvent(w http.ResponseWriter, controller *http.ResponseController, event observe.Event) bool {
	data, err := json.Marshal(event)
	if err != nil {
		return false
	}
	_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := fmt.Fprintf(w, "id: %d\nevent: observe\ndata: %s\n\n", event.ID, data); err != nil {
		return false
	}
	return controller.Flush() == nil
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
	if len(parts) == 2 && parts[1] == "filters" {
		a.proxyFilters(w, r, parts[0])
		return
	}
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

func (a *api) proxyFilters(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		filters, err := a.manager.ListProxyFilters(name)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"filters": filters})
	case http.MethodPost:
		var input yamlFilterInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		view, err := a.manager.CreateManagedFilter(name, input.YAML)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, view)
	default:
		methodNotAllowed(w)
	}
}

func (a *api) filter(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" || strings.Contains(name, "/") {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := a.manager.GetManagedFilter(name)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		var input yamlFilterInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		view, err := a.manager.ReplaceManagedFilter(name, input.YAML)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodDelete:
		if err := a.manager.DeleteManagedFilter(name); err != nil {
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
