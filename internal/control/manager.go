// Package control owns the local management API and proxy lifecycle.
package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/lentscode/ctf-proxy/internal/observe"
	"github.com/lentscode/ctf-proxy/internal/proxy"
)

const defaultMaxConnections = 128

var (
	// ErrNotFound indicates that the requested proxy or managed filter is absent.
	ErrNotFound = errors.New("proxy not found")
	// ErrConflict indicates that a requested listener or name conflicts with existing state.
	ErrConflict = errors.New("proxy listener conflict")
	// ErrPersistence indicates that durable configuration could not be updated.
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
	Active        bool     `json:"active"`
	Source        string   `json:"source"`
	Editable      bool     `json:"editable"`
	Protocols     []string `json:"protocols"`
	Directions    []string `json:"directions"`
	NeedsHTTPBody bool     `json:"needs_http_body"`
}

// ManagedFilterView is the API representation of an editable YAML filter.
type ManagedFilterView struct {
	FilterView
	YAML            string   `json:"yaml"`
	AssignedProxies []string `json:"assigned_proxies"`
}

// filterCatalog combines compiled filters with their API metadata and sources.
type filterCatalog struct {
	registry *filter.Registry
	views    []FilterView
	byName   map[string]FilterView
	managed  map[string]config.ManagedYAMLFilter
}

// managedProxy tracks one running data-plane runner and its shutdown channel.
type managedProxy struct {
	definition config.Proxy
	cancel     context.CancelFunc
	done       chan error
}

// Manager serializes configuration changes and reconciles affected listeners.
type Manager struct {
	mu         sync.Mutex
	store      *config.Store
	reporter   observe.Reporter
	configPath string
	catalog    *filterCatalog
	ctx        context.Context
	cancel     context.CancelFunc
	running    map[string]*managedProxy
	states     map[string]State
}

// NewManager loads the immutable startup filter catalog and constructs a
// manager. The supplied store must remain owned by this manager.
func NewManager(store *config.Store, configPath string, reporters ...observe.Reporter) (*Manager, error) {
	if store == nil {
		return nil, errors.New("new control manager: nil configuration store")
	}
	var reporter observe.Reporter = observe.NopReporter{}
	if len(reporters) > 0 && reporters[0] != nil {
		reporter = reporters[0]
	}
	cfg := store.Snapshot()
	manager := &Manager{store: store, reporter: reporter, configPath: configPath, running: make(map[string]*managedProxy), states: make(map[string]State)}
	catalog, err := manager.catalogFor(cfg)
	if err != nil {
		return nil, err
	}
	manager.catalog = catalog
	return manager, nil
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
		if _, err := m.runnerFor(definition, m.catalog, cfg.MaxConnections); err != nil {
			m.cancel()
			return fmt.Errorf("build proxy %q: %w", definition.Name, err)
		}
	}
	for _, definition := range cfg.Proxies {
		if !definition.Active {
			m.states[definition.Name] = StateInactive
			continue
		}
		if err := m.startLocked(definition, m.catalog, cfg.MaxConnections); err != nil {
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

// List returns API views for all configured proxies in configuration order.
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

// Get returns the API view for one configured proxy.
func (m *Manager) Get(name string) (ProxyView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	definition, ok := findProxy(m.store.Snapshot(), name)
	if !ok {
		return ProxyView{}, ErrNotFound
	}
	return m.viewLocked(definition), nil
}

// ListFilters returns metadata for every available filter.
func (m *Manager) ListFilters() []FilterView {
	m.mu.Lock()
	defer m.mu.Unlock()
	views := make([]FilterView, len(m.catalog.views))
	copy(views, m.catalog.views)
	return views
}

// GetManagedFilter returns one editable YAML filter and its assignments.
func (m *Manager) GetManagedFilter(name string) (ManagedFilterView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	managed, ok := m.catalog.managed[name]
	if !ok {
		return ManagedFilterView{}, ErrNotFound
	}
	view := m.catalog.byName[name]
	return ManagedFilterView{FilterView: view, YAML: managed.YAML, AssignedProxies: assignedProxies(m.store.Snapshot(), name)}, nil
}

// ListProxyFilters returns the available filter metadata assigned to a proxy.
func (m *Manager) ListProxyFilters(name string) ([]FilterView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	definition, ok := findProxy(m.store.Snapshot(), name)
	if !ok {
		return nil, ErrNotFound
	}
	views := make([]FilterView, 0, len(definition.Filters))
	for _, filterName := range definition.Filters {
		if view, exists := m.catalog.byName[filterName]; exists {
			views = append(views, view)
		}
	}
	return views, nil
}

// CreateManagedFilter compiles, persists, and assigns a new YAML filter.
func (m *Manager) CreateManagedFilter(proxyName, source string) (ManagedFilterView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	index := proxyIndex(next, proxyName)
	if index < 0 {
		return ManagedFilterView{}, ErrNotFound
	}
	compiled, err := singleYAMLFilter(source)
	if err != nil {
		return ManagedFilterView{}, err
	}
	name := compiled.Name()
	if _, exists := m.catalog.byName[name]; exists {
		return ManagedFilterView{}, fmt.Errorf("%w: filter %q already exists", ErrConflict, name)
	}
	next.ManagedYAMLFilters = append(next.ManagedYAMLFilters, config.ManagedYAMLFilter{Name: name, YAML: source})
	next.Proxies[index].Filters = append(next.Proxies[index].Filters, name)
	if err := m.applyLocked(next); err != nil {
		m.reportControlFailure(err)
		return ManagedFilterView{}, err
	}
	return m.managedViewLocked(name), nil
}

// ReplaceManagedFilter validates and atomically replaces an editable YAML filter.
func (m *Manager) ReplaceManagedFilter(name, source string) (ManagedFilterView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	index := managedFilterIndex(next, name)
	if index < 0 {
		return ManagedFilterView{}, ErrNotFound
	}
	compiled, err := singleYAMLFilter(source)
	if err != nil {
		return ManagedFilterView{}, err
	}
	if compiled.Name() != name {
		return ManagedFilterView{}, fmt.Errorf("YAML filter name %q does not match %q", compiled.Name(), name)
	}
	next.ManagedYAMLFilters[index].YAML = source
	if err := m.applyLocked(next); err != nil {
		m.reportControlFailure(err)
		return ManagedFilterView{}, err
	}
	return m.managedViewLocked(name), nil
}

// DeleteManagedFilter removes an unassigned editable YAML filter.
func (m *Manager) DeleteManagedFilter(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	index := managedFilterIndex(next, name)
	if index < 0 {
		return ErrNotFound
	}
	if assigned := assignedProxies(next, name); len(assigned) > 0 {
		return fmt.Errorf("%w: filter %q is referenced by proxies %s", ErrConflict, name, strings.Join(assigned, ", "))
	}
	next.ManagedYAMLFilters = append(next.ManagedYAMLFilters[:index:index], next.ManagedYAMLFilters[index+1:]...)
	if err := m.applyLocked(next); err != nil {
		m.reportControlFailure(err)
		return err
	}
	return nil
}

// Create validates, persists, and starts a new proxy definition.
func (m *Manager) Create(definition config.Proxy) (ProxyView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	if _, exists := findProxy(next, definition.Name); exists {
		return ProxyView{}, fmt.Errorf("%w: proxy %q already exists", ErrConflict, definition.Name)
	}
	next.Proxies = append(next.Proxies, definition)
	if err := m.applyLocked(next); err != nil {
		m.reportControlFailure(err)
		return ProxyView{}, err
	}
	return m.viewLocked(definition), nil
}

// Replace validates, persists, and reconciles an existing proxy definition.
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
		m.reportControlFailure(err)
		return ProxyView{}, err
	}
	return m.viewLocked(definition), nil
}

// Delete removes a proxy definition and stops its runner.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store.Snapshot()
	index := proxyIndex(next, name)
	if index < 0 {
		return ErrNotFound
	}
	next.Proxies = append(next.Proxies[:index:index], next.Proxies[index+1:]...)
	err := m.applyLocked(next)
	if err != nil {
		m.reportControlFailure(err)
	}
	return err
}

// SetActive changes a proxy's persisted activation state and runner lifecycle.
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
		m.reportControlFailure(err)
		return ProxyView{}, err
	}
	return m.viewLocked(next.Proxies[index]), nil
}

// applyLocked validates a complete next configuration before committing it.
func (m *Manager) applyLocked(next config.Config) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if m.ctx == nil {
		return errors.New("control manager is not started")
	}
	nextCatalog, err := m.catalogFor(next)
	if err != nil {
		return err
	}
	for _, definition := range next.Proxies {
		if _, err := m.runnerFor(definition, nextCatalog, next.MaxConnections); err != nil {
			return fmt.Errorf("build proxy %q: %w", definition.Name, err)
		}
	}

	previous := m.store.Snapshot()
	changedFilters := changedManagedFilters(previous, next)
	stopped := make([]config.Proxy, 0)
	for name, current := range m.running {
		desired, exists := findProxy(next, name)
		if !exists || !desired.Active || !reflect.DeepEqual(current.definition, desired) || referencesAny(current.definition, changedFilters) {
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
		if err := m.startLocked(definition, nextCatalog, next.MaxConnections); err != nil {
			for _, name := range started {
				m.stopLocked(name)
			}
			m.restoreLocked(stopped, m.catalog, previous.MaxConnections)
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
		m.restoreLocked(stopped, m.catalog, previous.MaxConnections)
		return fmt.Errorf("%w: %v", ErrPersistence, err)
	}
	m.catalog = nextCatalog
	return nil
}

// restoreLocked attempts to restart runners stopped during a failed update.
func (m *Manager) restoreLocked(definitions []config.Proxy, catalog *filterCatalog, maxConnections int) {
	for _, definition := range definitions {
		if err := m.startLocked(definition, catalog, maxConnections); err != nil {
			m.states[definition.Name] = StateFailed
			m.reporter.Report(observe.Event{Level: observe.LevelError, Component: observe.ComponentProxy, Kind: observe.KindProxyRestoreFailed, Proxy: definition.Name, Message: "proxy restore failed after configuration update"})
		}
	}
}

// startLocked binds and tracks one active proxy runner.
func (m *Manager) startLocked(definition config.Proxy, catalog *filterCatalog, maxConnections int) error {
	runner, err := m.runnerFor(definition, catalog, maxConnections)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", definition.Listen)
	if err != nil {
		m.reporter.Report(observe.Event{Level: observe.LevelError, Component: observe.ComponentProxy, Kind: observe.KindProxyListenerFailed, Proxy: definition.Name, Message: "proxy listener could not be started"})
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
				m.reporter.Report(observe.Event{Level: observe.LevelError, Component: observe.ComponentProxy, Kind: observe.KindProxyStoppedUnexpectedly, Proxy: definition.Name, Message: "proxy stopped unexpectedly"})
			}
		}
	}()
	return nil
}

// stopLocked cancels one runner and waits for its serve loop to finish.
func (m *Manager) stopLocked(name string) {
	current, exists := m.running[name]
	if !exists {
		return
	}
	delete(m.running, name)
	current.cancel()
	<-current.done
}

// stopAllLocked stops every currently tracked runner.
func (m *Manager) stopAllLocked() {
	for name := range m.running {
		m.stopLocked(name)
	}
}

// runnerFor builds a protocol-specific runner and immutable filter chain.
func (m *Manager) runnerFor(definition config.Proxy, catalog *filterCatalog, maxConnections int) (proxy.Runner, error) {
	filters, err := catalog.registry.Build(definition.Filters)
	if err != nil {
		return nil, fmt.Errorf("resolve filters: %w", err)
	}
	chain, err := filter.NewChainWithEventSink(observe.FilterSink(m.reporter, definition.Name), filters...)
	if err != nil {
		return nil, err
	}
	if maxConnections == 0 {
		maxConnections = defaultMaxConnections
	}
	slots := make(chan struct{}, maxConnections)
	switch definition.Protocol {
	case "tcp":
		return proxy.NewTCPProxy(definition.Listen, definition.Upstream, slots, chain, observe.WithProxy(m.reporter, definition.Name)), nil
	case "http":
		return proxy.NewHTTPProxy(definition.Listen, definition.Upstream, slots, chain, observe.WithProxy(m.reporter, definition.Name)), nil
	default:
		return nil, fmt.Errorf("unsupported protocol %q", definition.Protocol)
	}
}

// reportControlFailure emits a sanitized event for a rejected management change.
func (m *Manager) reportControlFailure(err error) {
	kind, message := observe.KindControlConfigurationRejected, "configuration update rejected"
	if errors.Is(err, ErrPersistence) {
		kind, message = observe.KindControlConfigurationPersistenceFailed, "configuration persistence failed"
	}
	m.reporter.Report(observe.Event{Level: observe.LevelError, Component: observe.ComponentControl, Kind: kind, Message: message})
}

// viewLocked combines persisted definition and in-memory runner state.
func (m *Manager) viewLocked(definition config.Proxy) ProxyView {
	state := m.states[definition.Name]
	if !definition.Active {
		state = StateInactive
	} else if state == "" {
		state = StateFailed
	}
	return ProxyView{Name: definition.Name, Active: definition.Active, Protocol: definition.Protocol, Listen: definition.Listen, Upstream: definition.Upstream, Filters: append([]string{}, definition.Filters...), State: state}
}

// findProxy locates a proxy definition by its stable name.
func findProxy(cfg config.Config, name string) (config.Proxy, bool) {
	for _, definition := range cfg.Proxies {
		if definition.Name == name {
			return definition, true
		}
	}
	return config.Proxy{}, false
}

// proxyIndex returns a proxy's configuration index or -1 when absent.
func proxyIndex(cfg config.Config, name string) int {
	for i, definition := range cfg.Proxies {
		if definition.Name == name {
			return i
		}
	}
	return -1
}

// managedFilterIndex returns a managed filter's configuration index or -1.
func managedFilterIndex(cfg config.Config, name string) int {
	for i, definition := range cfg.ManagedYAMLFilters {
		if definition.Name == name {
			return i
		}
	}
	return -1
}

// assignedProxies lists proxies that reference filterName.
func assignedProxies(cfg config.Config, filterName string) []string {
	assigned := make([]string, 0)
	for _, definition := range cfg.Proxies {
		for _, current := range definition.Filters {
			if current == filterName {
				assigned = append(assigned, definition.Name)
				break
			}
		}
	}
	return assigned
}

// referencesAny reports whether a proxy uses one of the changed filter names.
func referencesAny(definition config.Proxy, names map[string]struct{}) bool {
	for _, name := range definition.Filters {
		if _, exists := names[name]; exists {
			return true
		}
	}
	return false
}

// changedManagedFilters returns names whose YAML source differs between configs.
func changedManagedFilters(previous, next config.Config) map[string]struct{} {
	previousByName := make(map[string]string, len(previous.ManagedYAMLFilters))
	for _, current := range previous.ManagedYAMLFilters {
		previousByName[current.Name] = current.YAML
	}
	nextByName := make(map[string]string, len(next.ManagedYAMLFilters))
	for _, current := range next.ManagedYAMLFilters {
		nextByName[current.Name] = current.YAML
	}
	changed := make(map[string]struct{})
	for name, source := range previousByName {
		if nextByName[name] != source {
			changed[name] = struct{}{}
		}
	}
	for name, source := range nextByName {
		if previousByName[name] != source {
			changed[name] = struct{}{}
		}
	}
	return changed
}

// singleYAMLFilter compiles exactly one managed YAML filter document.
func singleYAMLFilter(source string) (filter.Filter, error) {
	if strings.TrimSpace(source) == "" {
		return nil, errors.New("YAML is required")
	}
	compiled, err := filter.CompileYAML([]byte(source))
	if err != nil {
		return nil, err
	}
	if len(compiled) != 1 {
		return nil, errors.New("YAML must contain exactly one filter")
	}
	return compiled[0], nil
}

// managedViewLocked builds the API representation for a managed filter.
func (m *Manager) managedViewLocked(name string) ManagedFilterView {
	managed := m.catalog.managed[name]
	return ManagedFilterView{FilterView: m.catalog.byName[name], YAML: managed.YAML, AssignedProxies: assignedProxies(m.store.Snapshot(), name)}
}

// catalogFor builds the filter registry and metadata for a complete config.
func (m *Manager) catalogFor(cfg config.Config) (*filterCatalog, error) {
	registry := filter.NewRegistry()
	if err := filter.RegisterBuiltins(registry); err != nil {
		return nil, err
	}
	catalog := &filterCatalog{registry: registry, byName: make(map[string]FilterView), managed: make(map[string]config.ManagedYAMLFilter)}
	for _, name := range registry.Names() {
		builtins, err := registry.Build([]string{name})
		if err != nil {
			return nil, fmt.Errorf("describe built-in filter %q: %w", name, err)
		}
		view := describeFilter(builtins[0], "builtin")
		catalog.views = append(catalog.views, view)
		catalog.byName[name] = view
	}
	register := func(current filter.Filter, source string, editable bool) error {
		compiled := current
		if err := registry.Register(compiled.Name(), func() (filter.Filter, error) { return compiled, nil }); err != nil {
			return err
		}
		view := describeFilter(compiled, source)
		view.Editable = editable
		catalog.views = append(catalog.views, view)
		catalog.byName[view.Name] = view
		return nil
	}
	paths := make([]string, len(cfg.FilterFiles))
	for i, path := range cfg.FilterFiles {
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(m.configPath), path)
		}
		paths[i] = path
	}
	fileFilters, err := filter.LoadYAMLFiles(paths)
	if err != nil {
		return nil, fmt.Errorf("load global YAML filters: %w", err)
	}
	for _, current := range fileFilters {
		if err := register(current, "yaml", false); err != nil {
			return nil, fmt.Errorf("register YAML filter %q: %w", current.Name(), err)
		}
	}
	for _, managed := range cfg.ManagedYAMLFilters {
		current, err := singleYAMLFilter(managed.YAML)
		if err != nil {
			return nil, fmt.Errorf("compile managed YAML filter %q: %w", managed.Name, err)
		}
		if current.Name() != managed.Name {
			return nil, fmt.Errorf("managed YAML filter name %q does not match compiled name %q", managed.Name, current.Name())
		}
		if err := register(current, "yaml", true); err != nil {
			return nil, fmt.Errorf("register managed YAML filter %q: %w", current.Name(), err)
		}
		catalog.managed[managed.Name] = managed
	}
	return catalog, nil
}

// describeFilter converts filter requirements into dashboard metadata.
func describeFilter(current filter.Filter, source string) FilterView {
	requirements := current.Requirements()
	if current, ok := current.(interface{ DeclaredRequirements() filter.Requirements }); ok {
		requirements = current.DeclaredRequirements()
	}
	view := FilterView{Name: current.Name(), Active: true, Source: source, NeedsHTTPBody: requirements.NeedsHTTPBody}
	if current, ok := current.(interface{ Active() bool }); ok {
		view.Active = current.Active()
	}
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
