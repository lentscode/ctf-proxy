package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLoadValidatesAndRejectsUnknownFields ensures trusted YAML stays strict.
func TestLoadValidatesAndRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: 1
max_connections: 10
proxies:
  - name: web
    protocol: http
    listen: ":8080"
    upstream: "http://127.0.0.1:18080"
    unexpected: value
`), 0o600))

	_, err := Load(path)
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected")
}

// TestLoadDefaultsProxyActiveToFalse verifies proxies require an explicit
// active: true declaration before they start.
func TestLoadDefaultsProxyActiveToFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: 1
proxies:
  - name: web
    protocol: http
    listen: ":8080"
    upstream: "http://127.0.0.1:18080"
`), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.False(t, cfg.Proxies[0].Active)
}

// TestLoadProtocolSpecificProxySettings parses textual durations and keeps
// each proxy protocol's settings separate.
func TestLoadProtocolSpecificProxySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: 1
proxies:
  - name: raw
    protocol: tcp
    listen: ":9000"
    upstream: "127.0.0.1:9001"
    tcp:
      dial_timeout: 3s
      read_timeout: 15s
      write_timeout: 2s
  - name: web
    protocol: http
    listen: ":8080"
    upstream: "http://127.0.0.1:18080"
    http:
      dial_timeout: 4s
      keep_alive: 20s
      max_idle_connections: 40
      max_idle_connections_per_host: 12
      max_connections_per_host: 48
      idle_connection_timeout: 25s
      response_header_timeout: 6s
      expect_continue_timeout: 2s
      read_header_timeout: 7s
      idle_timeout: 70s
      max_header_bytes: 2097152
      shutdown_timeout: 8s
`), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, &TCP{DialTimeout: Duration(3 * time.Second), ReadTimeout: Duration(15 * time.Second), WriteTimeout: Duration(2 * time.Second)}, cfg.Proxies[0].TCP)
	require.Nil(t, cfg.Proxies[0].HTTP)
	require.Nil(t, cfg.Proxies[1].TCP)
	require.Equal(t, &HTTP{
		DialTimeout: Duration(4 * time.Second), KeepAlive: Duration(20 * time.Second),
		MaxIdleConnections: 40, MaxIdleConnectionsPerHost: 12, MaxConnectionsPerHost: 48,
		IdleConnectionTimeout: Duration(25 * time.Second), ResponseHeaderTimeout: Duration(6 * time.Second),
		ExpectContinueTimeout: Duration(2 * time.Second), ReadHeaderTimeout: Duration(7 * time.Second),
		IdleTimeout: Duration(70 * time.Second), MaxHeaderBytes: 2 << 20, ShutdownTimeout: Duration(8 * time.Second),
	}, cfg.Proxies[1].HTTP)

	roundTripPath := filepath.Join(t.TempDir(), "round-trip.yaml")
	require.NoError(t, Save(roundTripPath, cfg))
	roundTrip, err := Load(roundTripPath)
	require.NoError(t, err)
	require.Equal(t, cfg, roundTrip)
}

func TestValidateMetricsConfiguration(t *testing.T) {
	valid := &Metrics{CompetitionStart: time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC), RoundDuration: Duration(2 * time.Minute), RetentionRounds: 720}
	for _, testCase := range []struct {
		name    string
		metrics *Metrics
		wantErr string
	}{
		{name: "metrics remain optional", metrics: nil},
		{name: "valid competition schedule", metrics: valid},
		{name: "missing start", metrics: &Metrics{RoundDuration: Duration(time.Minute), RetentionRounds: 1}, wantErr: "competition_start"},
		{name: "duration too short", metrics: &Metrics{CompetitionStart: valid.CompetitionStart, RoundDuration: Duration(59 * time.Second), RetentionRounds: 1}, wantErr: "round_duration"},
		{name: "duration too long", metrics: &Metrics{CompetitionStart: valid.CompetitionStart, RoundDuration: Duration(61 * time.Minute), RetentionRounds: 1}, wantErr: "round_duration"},
		{name: "retention too small", metrics: &Metrics{CompetitionStart: valid.CompetitionStart, RoundDuration: Duration(time.Minute), RetentionRounds: 0}, wantErr: "retention_rounds"},
		{name: "retention too large", metrics: &Metrics{CompetitionStart: valid.CompetitionStart, RoundDuration: Duration(time.Minute), RetentionRounds: 4321}, wantErr: "retention_rounds"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Metrics = testCase.metrics
			err := cfg.Validate()
			if testCase.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}

// TestSaveReplacesConfigurationAndPreservesExistingPermissions covers atomic rewrite behavior.
func TestSaveReplacesConfigurationAndPreservesExistingPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o640))
	cfg := validConfig()

	require.NoError(t, Save(path, cfg))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, cfg, loaded)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

// TestValidateRejectsInvalidConfigurations covers topology and schema validation failures.
func TestValidateRejectsInvalidConfigurations(t *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "conflicting listeners", mutate: func(cfg *Config) {
			cfg.Proxies = append(cfg.Proxies, Proxy{Name: "duplicate-listener", Protocol: "tcp", Listen: ":8080", Upstream: "127.0.0.1:18081"})
		}, wantErr: "multiple proxies listen"},
		{name: "HTTP proxy with TCP upstream", mutate: func(cfg *Config) { cfg.Proxies[0].Upstream = "127.0.0.1:18080" }, wantErr: "absolute http or https URL"},
		{name: "unsupported protocol", mutate: func(cfg *Config) { cfg.Proxies[0].Protocol = "udp" }, wantErr: "protocol"},
		{name: "duplicate proxy name", mutate: func(cfg *Config) {
			cfg.Proxies = append(cfg.Proxies, Proxy{Name: "web", Protocol: "tcp", Listen: ":8081", Upstream: "127.0.0.1:18081"})
		}, wantErr: "duplicate proxy name"},
		{name: "TCP settings on HTTP proxy", mutate: func(cfg *Config) { cfg.Proxies[0].TCP = &TCP{} }, wantErr: "tcp settings are only valid"},
		{name: "HTTP settings on TCP proxy", mutate: func(cfg *Config) {
			cfg.Proxies[0].Protocol = "tcp"
			cfg.Proxies[0].Upstream = "127.0.0.1:18080"
			cfg.Proxies[0].HTTP = &HTTP{}
		}, wantErr: "http settings are only valid"},
		{name: "negative TCP timeout", mutate: func(cfg *Config) {
			cfg.Proxies[0].Protocol = "tcp"
			cfg.Proxies[0].Upstream = "127.0.0.1:18080"
			cfg.Proxies[0].TCP = &TCP{ReadTimeout: Duration(-time.Second)}
		}, wantErr: "read_timeout must be between"},
		{name: "HTTP header size too large", mutate: func(cfg *Config) { cfg.Proxies[0].HTTP = &HTTP{MaxHeaderBytes: maxHTTPHeaderSize + 1} }, wantErr: "max_header_bytes must be between"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := validConfig()
			testCase.mutate(&cfg)
			require.ErrorContains(t, cfg.Validate(), testCase.wantErr)
		})
	}
}

// TestLoadRejectsMoreThanOneDocument ensures configuration files contain one document.
func TestLoadRejectsMoreThanOneDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: 1
proxies:
  - name: web
    protocol: http
    listen: ":8080"
    upstream: "http://127.0.0.1:18080"
---
version: 1
proxies: []
`), 0o600))
	_, err := Load(path)
	require.ErrorContains(t, err, "exactly one YAML document")
}

// TestStoreUpdatesOnlyAfterPersistingValidConfiguration protects the store's commit ordering.
func TestStoreUpdatesOnlyAfterPersistingValidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	require.NoError(t, Save(path, validConfig()))
	store, err := OpenStore(path)
	require.NoError(t, err)

	require.NoError(t, store.Update(func(cfg *Config) error {
		cfg.Proxies[0].Listen = ":8081"
		return nil
	}))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, ":8081", loaded.Proxies[0].Listen)

	require.Error(t, store.Update(func(cfg *Config) error {
		cfg.Proxies[0].Protocol = "udp"
		return nil
	}))
	require.Equal(t, ":8081", store.Snapshot().Proxies[0].Listen)
	require.Equal(t, "http", store.Snapshot().Proxies[0].Protocol)
}

// TestOpenOrCreateStoreCreatesAnEmptyValidConfiguration covers first-run initialization.
func TestOpenOrCreateStoreCreatesAnEmptyValidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := OpenOrCreateStore(path)
	require.NoError(t, err)
	require.Empty(t, store.Snapshot().Proxies)
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, Config{Version: Version, Proxies: []Proxy{}}, loaded)
}

// TestManagedYAMLFiltersRoundTripAndValidate covers managed filter persistence and checks.
func TestManagedYAMLFiltersRoundTripAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	source := "version: 1\nfilters:\n  - name: block-admin\n    active: true\n    protocol: http\n    direction: request\n    action: reject\n    match:\n      all:\n        - field: http.path\n          operator: prefix\n          value: /admin\n"
	cfg := validConfig()
	cfg.ManagedYAMLFilters = []ManagedYAMLFilter{{Name: "block-admin", YAML: source}}
	require.NoError(t, Save(path, cfg))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, source, loaded.ManagedYAMLFilters[0].YAML)

	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "name mismatch", mutate: func(cfg *Config) { cfg.ManagedYAMLFilters[0].Name = "different" }, want: "does not match"},
		{name: "zero rules", mutate: func(cfg *Config) { cfg.ManagedYAMLFilters[0].YAML = "version: 1\nfilters: []\n" }, want: "exactly one filter"},
		{name: "duplicate names", mutate: func(cfg *Config) { cfg.ManagedYAMLFilters = append(cfg.ManagedYAMLFilters, cfg.ManagedYAMLFilters[0]) }, want: "duplicate"},
		{name: "empty name", mutate: func(cfg *Config) { cfg.ManagedYAMLFilters[0].Name = "" }, want: "name is empty"},
		{name: "empty YAML", mutate: func(cfg *Config) { cfg.ManagedYAMLFilters[0].YAML = " " }, want: "yaml is empty"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			invalid := cfg
			invalid.ManagedYAMLFilters = append([]ManagedYAMLFilter(nil), cfg.ManagedYAMLFilters...)
			testCase.mutate(&invalid)
			require.ErrorContains(t, invalid.Validate(), testCase.want)
		})
	}
}

// validConfig returns the smallest valid configuration used by validation tests.
func validConfig() Config {
	return Config{
		Version:        Version,
		MaxConnections: 10,
		FilterFiles:    []string{"filters/common.yaml"},
		Proxies: []Proxy{{
			Name: "web", Active: true, Protocol: "http", Listen: ":8080", Upstream: "http://127.0.0.1:18080",
			Filters: []string{"reject-debug-path"},
		}},
	}
}
