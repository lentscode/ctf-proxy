package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestLoadDefaultsProxyActiveToTrue(t *testing.T) {
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
	require.True(t, cfg.Proxies[0].Active)
}

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

func TestValidateRejectsConflictingListenersAndInvalidUpstreams(t *testing.T) {
	cfg := validConfig()
	cfg.Proxies = append(cfg.Proxies, Proxy{
		Name: "duplicate-listener", Protocol: "tcp", Listen: ":8080", Upstream: "127.0.0.1:18081",
	})
	require.ErrorContains(t, cfg.Validate(), "multiple proxies listen")

	cfg = validConfig()
	cfg.Proxies[0].Upstream = "127.0.0.1:18080"
	require.ErrorContains(t, cfg.Validate(), "absolute http or https URL")
}

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

func TestOpenOrCreateStoreCreatesAnEmptyValidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := OpenOrCreateStore(path)
	require.NoError(t, err)
	require.Empty(t, store.Snapshot().Proxies)
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, Config{Version: Version, Proxies: []Proxy{}}, loaded)
}

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
