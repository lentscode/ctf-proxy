// Package control owns the local management API and proxy lifecycle.
package control

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/lentscode/ctf-proxy/internal/proxy"
)

const defaultMaxConnections = 128

var (
	ErrNotFound    = errors.New("proxy not found")
	ErrConflict    = errors.New("proxy listener conflict")
	ErrPersistence = errors.New("configuration persistence failed")
)

// State describes a proxy's current data-plane state.
type State string

const (
	StateRunning  State = "running"
	StateInactive State = "inactive"
	StateFailed   State = "failed"
)

// ProxyView is the API representation of a configured proxy.
type ProxyView struct {
	Name     string   `json:"name"`
	Active   bool     `json:"active"`
	Protocol string   `json:"protocol"`
	Listen   string   `json:"listen"`
	Upstream string   `json:"upstream"`
	Filters  []string `json:"filters"`
	State    State    `json:"state"`
}

// FilterView describes a filter available for proxy selection.
type FilterView struct {
	Name          string   `json:"name"`
	Source        string   `json:"source"`
	Protocols     []string `json:"protocols"`
	Directions    []string `json:"directions"`
	NeedsHTTPBody bool     `json:"needs_http_body"`
}

type managedProxy struct {
	definition config.Proxy
	cancel     context.CancelFunc
	done       chan error
}

// Manager serializes configuration changes and reconciles affected listeners.
type Manager struct {
	mu       sync.Mutex
	store    *config.Store
	registry *filter.Registry
	filters  []FilterView
	ctx      context.Context
	cancel   context.CancelFunc
	running  map[string]*managedProxy
	states   map[string]State
}

// NewManager loads the immutable startup filter catalog and constructs a
// manager. The supplied store must remain owned by this manager.
func NewManager(store *config.Store, configPath string) (*Manager, error) {
	if store == nil {
		return nil, errors.New("new control manager: nil configuration store")
	}
	registry := filter.NewRegistry()
	if err := filter.RegisterBuiltins(registry); err != nil {
		return nil, err
	}
	cfg := store.Snapshot()
	paths := make([]string, len(cfg.FilterFiles))
	for i, path := range cfg.FilterFiles {
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(configPath), path)
		}
		paths[i] = path
	}
	yamlFilters, err := filter.LoadYAMLFiles(paths)
	if err != nil {
		return nil, fmt.Errorf("load global YAML filters: %w", err)
	}
	views := make([]FilterView, 0, len(yamlFilters))
	for _, current := range yamlFilters {
		compiled := current
		if err := registry.Register(compiled.Name(), func() (filter.Filter, error) { return compiled, nil }); err != nil {
			return nil, fmt.Errorf("register YAML filter %q: %w", compiled.Name(), err)
		}
		views = append(views, describeFilter(compiled, "yaml"))
	}
	return &Manager{store: store, registry: registry, filters: views, running: make(map[string]*managedProxy), states: make(map[string]State)}, nil
}

// Start starts all active configured proxies. It is valid for cfg to contain no
// proxies, in which case only the control plane is started.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx != nil {
		return errors.New("control manager already started")
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	cfg := m.store.Snapshot()
	for _, definition := range cfg.Proxies {
		if _, err := m.runnerFor(definition); err != nil {
			m.cancel()
			return fmt.Errorf("build proxy %q: %w", definition.Name, err)
		}
	}
	for _, definition := range cfg.Proxies {
		if !definition.Active {
			m.states[definition.Name] = StateInactive
			continue
		}
		if err := m.startLocked(definition); err != nil {
			m.stopAllLocked()
			return fmt.Errorf("start proxy %q: %w", definition.Name, err)
		}
	}
	return nil
}

// Close stops every managed listener.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopAllLocked()
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) List() []ProxyView {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.store.Snapshot()
	views := make([]ProxyView, 0, len(cfg.Proxies))
	for _, definition := range cfg.Proxies {
		views = append(views, m.viewLocked(definition))
	}
	return views
}

func (m *Manager) Get(name string) (ProxyView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	definition, ok := findProxy(m.store.Snapshot(), name)
	if !ok {
		return ProxyView{}, ErrNotFound
	}
	return m.viewLocked(definition), nil
}

func (m *Manager) ListFilters() []FilterView {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]FilterView(nil), m.filters...)
}

func (m *Manager) Create(definition config.Proxy) (ProxyView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	if _, exists := findProxy(next, definition.Name); exists {
		return ProxyView{}, fmt.Errorf("%w: proxy %q already exists", ErrConflict, definition.Name)
	}
	next.Proxies = append(next.Proxies, definition)
	if err := m.applyLocked(next); err != nil {
		return ProxyView{}, err
	}
	return m.viewLocked(definition), nil
}

func (m *Manager) Replace(name string, definition config.Proxy) (ProxyView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	index := proxyIndex(next, name)
	if index < 0 {
		return ProxyView{}, ErrNotFound
	}
	definition.Name = name
	next.Proxies[index] = definition
	if err := m.applyLocked(next); err != nil {
		return ProxyView{}, err
	}
	return m.viewLocked(definition), nil
}

func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	index := proxyIndex(next, name)
	if index < 0 {
		return ErrNotFound
	}
	next.Proxies = append(next.Proxies[:index:index], next.Proxies[index+1:]...)
	return m.applyLocked(next)
}

func (m *Manager) SetActive(name string, active bool) (ProxyView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	index := proxyIndex(next, name)
	if index < 0 {
		return ProxyView{}, ErrNotFound
	}
	next.Proxies[index].Active = active
	// Retrying a failed active proxy must construct a new runner even though its
	// persisted definition did not change.
	if active && m.states[name] == StateFailed {
		delete(m.running, name)
	}
	if err := m.applyLocked(next); err != nil {
		return ProxyView{}, err
	}
	return m.viewLocked(next.Proxies[index]), nil
}

func (m *Manager) applyLocked(next config.Config) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if m.ctx == nil {
		return errors.New("control manager is not started")
	}
	for _, definition := range next.Proxies {
		if _, err := m.runnerFor(definition); err != nil {
			return fmt.Errorf("build proxy %q: %w", definition.Name, err)
		}
	}

	previous := m.store.Snapshot()
	stopped := make([]config.Proxy, 0)
	for name, current := range m.running {
		desired, exists := findProxy(next, name)
		if !exists || !desired.Active || !reflect.DeepEqual(current.definition, desired) {
			stopped = append(stopped, current.definition)
			m.stopLocked(name)
		}
	}

	started := make([]string, 0)
	for _, definition := range next.Proxies {
		if !definition.Active {
			m.states[definition.Name] = StateInactive
			continue
		}
		if _, exists := m.running[definition.Name]; exists {
			continue
		}
		if err := m.startLocked(definition); err != nil {
			for _, name := range started {
				m.stopLocked(name)
			}
			m.restoreLocked(stopped)
			if errors.Is(err, ErrConflict) {
				return err
			}
			return fmt.Errorf("start proxy %q: %w", definition.Name, err)
		}
		started = append(started, definition.Name)
	}
	if err := m.store.Update(func(cfg *config.Config) error { *cfg = next; return nil }); err != nil {
		for _, name := range started {
			m.stopLocked(name)
		}
		m.restoreLocked(stopped)
		return fmt.Errorf("%w: %v", ErrPersistence, err)
	}
	_ = previous // retained for clarity: Store remains unchanged until commit.
	return nil
}

func (m *Manager) restoreLocked(definitions []config.Proxy) {
	for _, definition := range definitions {
		if err := m.startLocked(definition); err != nil {
			m.states[definition.Name] = StateFailed
			log.Printf("restore proxy %q after failed configuration update: %v", definition.Name, err)
		}
	}
}

func (m *Manager) startLocked(definition config.Proxy) error {
	runner, err := m.runnerFor(definition)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", definition.Listen)
	if err != nil {
		return fmt.Errorf("%w: listen on %s: %v", ErrConflict, definition.Listen, err)
	}
	ctx, cancel := context.WithCancel(m.ctx)
	current := &managedProxy{definition: definition, cancel: cancel, done: make(chan error, 1)}
	m.running[definition.Name] = current
	m.states[definition.Name] = StateRunning
	go func() {
		err := runner.Serve(ctx, listener)
		current.done <- err
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.running[definition.Name] == current {
			delete(m.running, definition.Name)
			if ctx.Err() == nil && err != nil {
				m.states[definition.Name] = StateFailed
				log.Printf("proxy %q stopped unexpectedly: %v", definition.Name, err)
			}
		}
	}()
	return nil
}

func (m *Manager) stopLocked(name string) {
	current, exists := m.running[name]
	if !exists {
		return
	}
	delete(m.running, name)
	current.cancel()
	<-current.done
}

func (m *Manager) stopAllLocked() {
	for name := range m.running {
		m.stopLocked(name)
	}
}

func (m *Manager) runnerFor(definition config.Proxy) (proxy.Runner, error) {
	filters, err := m.registry.Build(definition.Filters)
	if err != nil {
		return nil, fmt.Errorf("resolve filters: %w", err)
	}
	chain, err := filter.NewChain(filters...)
	if err != nil {
		return nil, err
	}
	maxConnections := m.store.Snapshot().MaxConnections
	if maxConnections == 0 {
		maxConnections = defaultMaxConnections
	}
	slots := make(chan struct{}, maxConnections)
	switch definition.Protocol {
	case "tcp":
		return proxy.NewTCPProxy(definition.Listen, definition.Upstream, slots, chain), nil
	case "http":
		return proxy.NewHTTPProxy(definition.Listen, definition.Upstream, slots, chain), nil
	default:
		return nil, fmt.Errorf("unsupported protocol %q", definition.Protocol)
	}
}

func (m *Manager) viewLocked(definition config.Proxy) ProxyView {
	state := m.states[definition.Name]
	if !definition.Active {
		state = StateInactive
	} else if state == "" {
		state = StateFailed
	}
	return ProxyView{Name: definition.Name, Active: definition.Active, Protocol: definition.Protocol, Listen: definition.Listen, Upstream: definition.Upstream, Filters: append([]string{}, definition.Filters...), State: state}
}

func findProxy(cfg config.Config, name string) (config.Proxy, bool) {
	for _, definition := range cfg.Proxies {
		if definition.Name == name {
			return definition, true
		}
	}
	return config.Proxy{}, false
}

func proxyIndex(cfg config.Config, name string) int {
	for i, definition := range cfg.Proxies {
		if definition.Name == name {
			return i
		}
	}
	return -1
}

func describeFilter(current filter.Filter, source string) FilterView {
	requirements := current.Requirements()
	view := FilterView{Name: current.Name(), Source: source, NeedsHTTPBody: requirements.NeedsHTTPBody}
	for _, protocol := range requirements.Protocols {
		if protocol == filter.ProtocolTCP {
			view.Protocols = append(view.Protocols, "tcp")
		}
		if protocol == filter.ProtocolHTTP {
			view.Protocols = append(view.Protocols, "http")
		}
	}
	for _, direction := range requirements.Directions {
		if direction == filter.DirectionRequest {
			view.Directions = append(view.Directions, "request")
		}
		if direction == filter.DirectionResponse {
			view.Directions = append(view.Directions, "response")
		}
	}
	return view
}
