package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/proxy"
	"github.com/stretchr/testify/require"
)

func TestBuildRunnersLoadsFilterFilesRelativeToConfiguration(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "ctf-proxy.yaml")
	filterPath := filepath.Join(directory, "filters.yaml")
	require.NoError(t, os.WriteFile(filterPath, []byte(`
version: 1
filters:
  - name: only-requests
    protocol: http
    direction: request
    action: reject
    match:
      all:
        - field: http.path
          operator: exact
          value: /private
`), 0o600))

	runners, err := buildRunners(config.Config{
		Version: config.Version,
		Proxies: []config.Proxy{{
			Name: "web", Active: true, Protocol: "http", Listen: ":8080", Upstream: "http://127.0.0.1:18080",
			FilterFiles: []string{"filters.yaml"},
		}},
	}, configPath)
	require.NoError(t, err)
	require.Len(t, runners, 1)
	_, ok := runners[0].runner.(*proxy.HTTPProxy)
	require.True(t, ok)
}

func TestBuildRunnersReportsMissingFilterFile(t *testing.T) {
	_, err := buildRunners(config.Config{
		Version: config.Version,
		Proxies: []config.Proxy{{
			Name: "web", Active: true, Protocol: "http", Listen: ":8080", Upstream: "http://127.0.0.1:18080",
			FilterFiles: []string{"does-not-exist.yaml"},
		}},
	}, filepath.Join(t.TempDir(), "ctf-proxy.yaml"))
	require.ErrorContains(t, err, "load filters for proxy \"web\"")
}

func TestBuildRunnersSkipsInactiveProxies(t *testing.T) {
	runners, err := buildRunners(config.Config{
		Version: config.Version,
		Proxies: []config.Proxy{{
			Name: "staged", Active: false, Protocol: "tcp", Listen: ":31337", Upstream: "127.0.0.1:31338",
		}},
	}, filepath.Join(t.TempDir(), "ctf-proxy.yaml"))
	require.NoError(t, err)
	require.Empty(t, runners)
}
