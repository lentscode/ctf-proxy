// Package config loads, validates, and persistently updates ctf-proxy's
// operator configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lentscode/ctf-proxy/internal/filter"
	"go.yaml.in/yaml/v4"
)

var proxyNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)

// Version is the supported version of the main configuration format.
const Version = 1

// MaxConnectionsLimit prevents a malformed trusted configuration from
// allocating an unbounded number of connection slots per proxy.
const MaxConnectionsLimit = 65_536

const (
	maxTimeout        = 24 * time.Hour
	maxHTTPHeaderSize = 16 << 20
)

// Config is the complete operator-managed configuration. MaxConnections limits
// concurrent client connections for each proxy; zero selects the executable's
// default.
type Config struct {
	Version            int                 `yaml:"version"`
	MaxConnections     int                 `yaml:"max_connections,omitempty"`
	FilterFiles        []string            `yaml:"filter_files,omitempty"`
	ManagedYAMLFilters []ManagedYAMLFilter `yaml:"managed_yaml_filters,omitempty"`
	Metrics            *Metrics            `yaml:"metrics,omitempty"`
	Proxies            []Proxy             `yaml:"proxies"`
}

// Metrics configures bounded, competition-round traffic aggregation.
type Metrics struct {
	CompetitionStart time.Time `yaml:"competition_start"`
	RoundDuration    Duration  `yaml:"round_duration"`
	RetentionRounds  int       `yaml:"retention_rounds"`
}

// ManagedYAMLFilter is an API-managed, single-rule YAML filter document.
// Name duplicates the rule name in YAML so it remains a stable configuration
// identifier even when the source is inspected without compiling it.
type ManagedYAMLFilter struct {
	Name string `yaml:"name"`
	YAML string `yaml:"yaml"`
}

// Proxy describes one public listener and its private upstream service.
type Proxy struct {
	Name     string   `yaml:"name"`
	Active   bool     `yaml:"active"`
	Protocol string   `yaml:"protocol"`
	Listen   string   `yaml:"listen"`
	Upstream string   `yaml:"upstream"`
	Filters  []string `yaml:"filters,omitempty"`
	TCP      *TCP     `yaml:"tcp,omitempty"`
	HTTP     *HTTP    `yaml:"http,omitempty"`
}

// Duration is a human-readable YAML duration such as "5s" or "1m".
// A zero value is omitted and selects the proxy implementation's default.
type Duration time.Duration

// UnmarshalYAML accepts only Go duration strings to keep timeout units explicit.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
		return errors.New("duration must be a string such as 5s")
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", value.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML persists durations in their standard textual form.
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// IsZero allows omitted timeout fields to remain absent when configuration is saved.
func (d Duration) IsZero() bool {
	return d == 0
}

// TCP contains settings valid only for TCP proxies. Zero values select the
// existing unbounded behavior.
type TCP struct {
	DialTimeout  Duration `yaml:"dial_timeout,omitempty"`
	ReadTimeout  Duration `yaml:"read_timeout,omitempty"`
	WriteTimeout Duration `yaml:"write_timeout,omitempty"`
}

// HTTP contains settings valid only for HTTP proxies. Zero values select the
// existing transport and server defaults.
type HTTP struct {
	DialTimeout               Duration `yaml:"dial_timeout,omitempty"`
	KeepAlive                 Duration `yaml:"keep_alive,omitempty"`
	MaxIdleConnections        int      `yaml:"max_idle_connections,omitempty"`
	MaxIdleConnectionsPerHost int      `yaml:"max_idle_connections_per_host,omitempty"`
	MaxConnectionsPerHost     int      `yaml:"max_connections_per_host,omitempty"`
	IdleConnectionTimeout     Duration `yaml:"idle_connection_timeout,omitempty"`
	ResponseHeaderTimeout     Duration `yaml:"response_header_timeout,omitempty"`
	ExpectContinueTimeout     Duration `yaml:"expect_continue_timeout,omitempty"`
	ReadHeaderTimeout         Duration `yaml:"read_header_timeout,omitempty"`
	IdleTimeout               Duration `yaml:"idle_timeout,omitempty"`
	MaxHeaderBytes            int      `yaml:"max_header_bytes,omitempty"`
	ShutdownTimeout           Duration `yaml:"shutdown_timeout,omitempty"`
}

// Store serializes in-process configuration changes and persists each accepted
// version before making it visible to callers. It is intended to be shared by
// a future local control-plane API.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// OpenStore loads path and returns a store initialized with its contents.
func OpenStore(path string) (*Store, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, cfg: clone(cfg)}, nil
}

// OpenOrCreateStore opens path, creating a valid empty configuration when the
// file does not exist. Its parent directory must already exist.
func OpenOrCreateStore(path string) (*Store, error) {
	store, err := OpenStore(path)
	if err == nil {
		return store, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	initial := Config{Version: Version, Proxies: []Proxy{}}
	if err := Save(path, initial); err != nil {
		return nil, err
	}
	return &Store{path: path, cfg: initial}, nil
}

// Snapshot returns an independent copy of the current configuration.
func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.cfg)
}

// Update applies change to a copy of the configuration, validates it, and
// atomically saves it. If change or the save fails, the current configuration
// remains unchanged.
func (s *Store) Update(change func(*Config) error) error {
	if s == nil {
		return errors.New("update configuration: nil store")
	}
	if change == nil {
		return errors.New("update configuration: nil change")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.cfg)
	if err := change(&next); err != nil {
		return fmt.Errorf("update configuration: %w", err)
	}
	if err := Save(s.path, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

// Load reads and validates exactly one YAML configuration document.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if err := ensureSingleDocument(decoder); err != nil {
		return Config{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate configuration %q: %w", path, err)
	}
	return cfg, nil
}

// Save validates cfg and atomically replaces path with its YAML representation.
// A failed write leaves the previous configuration intact.
func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}

	directory := filepath.Dir(path)
	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat configuration %q: %w", path, err)
	}
	mode := os.FileMode(0o600)
	if err == nil {
		mode = info.Mode().Perm()
	}

	temporary, err := os.CreateTemp(directory, ".ctf-proxy-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary configuration permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace configuration %q: %w", path, err)
	}
	return nil
}

// Validate checks that cfg can safely be used to construct proxy listeners.
func (c Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("unsupported configuration version %d", c.Version)
	}
	if c.MaxConnections < 0 || c.MaxConnections > MaxConnectionsLimit {
		return fmt.Errorf("max_connections must be between 0 and %d", MaxConnectionsLimit)
	}
	if c.Metrics != nil {
		if c.Metrics.CompetitionStart.IsZero() || c.Metrics.CompetitionStart.Location() != time.UTC || c.Metrics.CompetitionStart.Format(time.RFC3339) == "" {
			return errors.New("metrics.competition_start must be a UTC RFC3339 timestamp")
		}
		d := time.Duration(c.Metrics.RoundDuration)
		if d < time.Minute || d > time.Hour || d%time.Second != 0 {
			return errors.New("metrics.round_duration must be a whole-second duration between 1m and 1h")
		}
		if c.Metrics.RetentionRounds < 1 || c.Metrics.RetentionRounds > 4320 {
			return errors.New("metrics.retention_rounds must be between 1 and 4320")
		}
	}
	for index, path := range c.FilterFiles {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("filter_files at index %d is empty", index)
		}
	}
	managedNames := make(map[string]struct{}, len(c.ManagedYAMLFilters))
	for index, managed := range c.ManagedYAMLFilters {
		if strings.TrimSpace(managed.Name) == "" {
			return fmt.Errorf("managed_yaml_filters at index %d: name is empty", index)
		}
		if strings.TrimSpace(managed.YAML) == "" {
			return fmt.Errorf("managed_yaml_filters at index %d: yaml is empty", index)
		}
		if _, exists := managedNames[managed.Name]; exists {
			return fmt.Errorf("duplicate managed YAML filter name %q", managed.Name)
		}
		compiled, err := filter.CompileYAML([]byte(managed.YAML))
		if err != nil {
			return fmt.Errorf("managed_yaml_filters at index %d: %w", index, err)
		}
		if len(compiled) != 1 {
			return fmt.Errorf("managed_yaml_filters at index %d: YAML must contain exactly one filter", index)
		}
		if compiled[0].Name() != managed.Name {
			return fmt.Errorf("managed_yaml_filters at index %d: YAML filter name %q does not match %q", index, compiled[0].Name(), managed.Name)
		}
		managedNames[managed.Name] = struct{}{}
	}

	names := make(map[string]struct{}, len(c.Proxies))
	listeners := make(map[string]struct{}, len(c.Proxies))
	for index, proxy := range c.Proxies {
		if err := proxy.validate(); err != nil {
			return fmt.Errorf("proxy at index %d: %w", index, err)
		}
		if _, exists := names[proxy.Name]; exists {
			return fmt.Errorf("duplicate proxy name %q", proxy.Name)
		}
		names[proxy.Name] = struct{}{}
		if _, exists := listeners[proxy.Listen]; exists {
			return fmt.Errorf("multiple proxies listen on %q", proxy.Listen)
		}
		listeners[proxy.Listen] = struct{}{}
	}
	return nil
}

// clone returns a configuration copy whose slices can be changed independently.
func clone(cfg Config) Config {
	copy := cfg
	copy.FilterFiles = append([]string(nil), cfg.FilterFiles...)
	copy.ManagedYAMLFilters = append([]ManagedYAMLFilter(nil), cfg.ManagedYAMLFilters...)
	if cfg.Metrics != nil {
		metrics := *cfg.Metrics
		copy.Metrics = &metrics
	}
	copy.Proxies = make([]Proxy, len(cfg.Proxies))
	for index, proxy := range cfg.Proxies {
		copy.Proxies[index] = proxy
		copy.Proxies[index].Filters = append([]string(nil), proxy.Filters...)
		if proxy.TCP != nil {
			tcp := *proxy.TCP
			copy.Proxies[index].TCP = &tcp
		}
		if proxy.HTTP != nil {
			http := *proxy.HTTP
			copy.Proxies[index].HTTP = &http
		}
	}
	return copy
}

// validate checks the protocol-specific listener and upstream topology.
func (p Proxy) validate() error {
	if !proxyNamePattern.MatchString(p.Name) {
		return errors.New("name must match [A-Za-z0-9][A-Za-z0-9_-]{0,62}")
	}
	if p.Protocol != "tcp" && p.Protocol != "http" {
		return fmt.Errorf("protocol must be tcp or http, got %q", p.Protocol)
	}
	if err := validateAddress(p.Listen, "listen"); err != nil {
		return err
	}
	if p.Protocol == "tcp" {
		if p.HTTP != nil {
			return errors.New("http settings are only valid for http proxies")
		}
		if err := validateAddress(p.Upstream, "upstream"); err != nil {
			return err
		}
		if p.TCP != nil {
			if err := p.TCP.validate(); err != nil {
				return fmt.Errorf("tcp settings: %w", err)
			}
		}
	} else {
		if p.TCP != nil {
			return errors.New("tcp settings are only valid for tcp proxies")
		}
		if err := validateHTTPUpstream(p.Upstream); err != nil {
			return err
		}
		if p.HTTP != nil {
			if err := p.HTTP.validate(); err != nil {
				return fmt.Errorf("http settings: %w", err)
			}
		}
	}
	for index, name := range p.Filters {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("filters at index %d is empty", index)
		}
	}
	return nil
}

func (t TCP) validate() error {
	for name, value := range map[string]Duration{
		"dial_timeout":  t.DialTimeout,
		"read_timeout":  t.ReadTimeout,
		"write_timeout": t.WriteTimeout,
	} {
		if err := validateTimeout(name, value); err != nil {
			return err
		}
	}
	return nil
}

func (h HTTP) validate() error {
	for name, value := range map[string]Duration{
		"dial_timeout":            h.DialTimeout,
		"keep_alive":              h.KeepAlive,
		"idle_connection_timeout": h.IdleConnectionTimeout,
		"response_header_timeout": h.ResponseHeaderTimeout,
		"expect_continue_timeout": h.ExpectContinueTimeout,
		"read_header_timeout":     h.ReadHeaderTimeout,
		"idle_timeout":            h.IdleTimeout,
		"shutdown_timeout":        h.ShutdownTimeout,
	} {
		if err := validateTimeout(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]int{
		"max_idle_connections":          h.MaxIdleConnections,
		"max_idle_connections_per_host": h.MaxIdleConnectionsPerHost,
		"max_connections_per_host":      h.MaxConnectionsPerHost,
	} {
		if value < 0 || value > MaxConnectionsLimit {
			return fmt.Errorf("%s must be between 0 and %d", name, MaxConnectionsLimit)
		}
	}
	if h.MaxHeaderBytes < 0 || h.MaxHeaderBytes > maxHTTPHeaderSize {
		return fmt.Errorf("max_header_bytes must be between 0 and %d", maxHTTPHeaderSize)
	}
	return nil
}

func validateTimeout(name string, value Duration) error {
	if value < 0 || time.Duration(value) > maxTimeout {
		return fmt.Errorf("%s must be between 0s and %s", name, maxTimeout)
	}
	return nil
}

// validateAddress accepts the host:port form used by TCP endpoints.
func validateAddress(address, field string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s must be host:port: %w", field, err)
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("%s must use a port from 1 through 65535", field)
	}
	return nil
}

// validateHTTPUpstream accepts only a bare HTTP or HTTPS origin URL.
func validateHTTPUpstream(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("upstream must be an absolute http or https URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("upstream must use http or https, got %q", u.Scheme)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("upstream must contain only scheme and host")
	}
	return nil
}

// ensureSingleDocument rejects trailing YAML documents after the first one.
func ensureSingleDocument(decoder *yaml.Decoder) error {
	var extra yaml.Node
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("configuration must contain exactly one YAML document")
}
