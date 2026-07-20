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
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v4"
)

// Version is the supported version of the main configuration format.
const Version = 1

// MaxConnectionsLimit prevents a malformed trusted configuration from
// allocating an unbounded number of connection slots per proxy.
const MaxConnectionsLimit = 65_536

// Config is the complete operator-managed configuration. MaxConnections limits
// concurrent client connections for each proxy; zero selects the executable's
// default.
type Config struct {
	Version        int     `yaml:"version"`
	MaxConnections int     `yaml:"max_connections,omitempty"`
	Proxies        []Proxy `yaml:"proxies"`
}

// Proxy describes one public listener and its private upstream service.
type Proxy struct {
	Name        string   `yaml:"name"`
	Active      bool     `yaml:"active"`
	Protocol    string   `yaml:"protocol"`
	Listen      string   `yaml:"listen"`
	Upstream    string   `yaml:"upstream"`
	FilterFiles []string `yaml:"filter_files,omitempty"`
}

// UnmarshalYAML defaults Active to true when it is omitted. A pointer is used
// while decoding so an explicit active: false remains distinguishable from an
// absent field.
func (p *Proxy) UnmarshalYAML(node *yaml.Node) error {
	var decoded struct {
		Name        string   `yaml:"name"`
		Active      *bool    `yaml:"active"`
		Protocol    string   `yaml:"protocol"`
		Listen      string   `yaml:"listen"`
		Upstream    string   `yaml:"upstream"`
		FilterFiles []string `yaml:"filter_files,omitempty"`
	}
	if err := node.Load(&decoded, yaml.WithKnownFields()); err != nil {
		return err
	}

	p.Name = decoded.Name
	p.Active = true
	if decoded.Active != nil {
		p.Active = *decoded.Active
	}
	p.Protocol = decoded.Protocol
	p.Listen = decoded.Listen
	p.Upstream = decoded.Upstream
	p.FilterFiles = decoded.FilterFiles
	return nil
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
	if len(c.Proxies) == 0 {
		return errors.New("at least one proxy is required")
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

func clone(cfg Config) Config {
	copy := cfg
	copy.Proxies = make([]Proxy, len(cfg.Proxies))
	for index, proxy := range cfg.Proxies {
		copy.Proxies[index] = proxy
		copy.Proxies[index].FilterFiles = append([]string(nil), proxy.FilterFiles...)
	}
	return copy
}

func (p Proxy) validate() error {
	if p.Name == "" {
		return errors.New("name is required")
	}
	if p.Protocol != "tcp" && p.Protocol != "http" {
		return fmt.Errorf("protocol must be tcp or http, got %q", p.Protocol)
	}
	if err := validateAddress(p.Listen, "listen"); err != nil {
		return err
	}
	if p.Protocol == "tcp" {
		if err := validateAddress(p.Upstream, "upstream"); err != nil {
			return err
		}
	} else if err := validateHTTPUpstream(p.Upstream); err != nil {
		return err
	}
	for index, path := range p.FilterFiles {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("filter_files at index %d is empty", index)
		}
	}
	return nil
}

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
