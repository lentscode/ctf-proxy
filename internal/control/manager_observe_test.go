package control

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/observe"
	"github.com/stretchr/testify/require"
)

func TestManagerReportsRejectedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	observer := observe.NewObserver(io.Discard)
	t.Cleanup(observer.Close)
	manager, err := NewManager(store, path, observer)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)

	_, err = manager.Create(config.Proxy{
		Name: "web", Active: false, Protocol: "tcp", Listen: "127.0.0.1:31337", Upstream: "127.0.0.1:31338", Filters: []string{"not-registered"},
	})
	require.Error(t, err)
	events := observer.Hub().Snapshot(1)
	require.Len(t, events, 1)
	require.Equal(t, observe.KindControlConfigurationRejected, events[0].Kind)
}

func TestManagerReconcilesActiveProxyWhenManagedFilterChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listen := reserved.Addr().String()
	require.NoError(t, reserved.Close())
	initial := tcpYAMLFilter("block-ping", "ping")
	require.NoError(t, config.Save(path, config.Config{
		Version:            config.Version,
		ManagedYAMLFilters: []config.ManagedYAMLFilter{{Name: "block-ping", YAML: initial}},
		Proxies:            []config.Proxy{{Name: "tcp", Active: true, Protocol: "tcp", Listen: listen, Upstream: "127.0.0.1:31338", Filters: []string{"block-ping"}}},
	}))
	store, err := config.OpenStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)

	manager.mu.Lock()
	previous := manager.running["tcp"]
	manager.mu.Unlock()
	_, err = manager.ReplaceManagedFilter("block-ping", tcpYAMLFilter("block-ping", "pong"))
	require.NoError(t, err)
	manager.mu.Lock()
	current := manager.running["tcp"]
	manager.mu.Unlock()
	require.NotSame(t, previous, current)
	require.Contains(t, store.Snapshot().ManagedYAMLFilters[0].YAML, "pong")
}

func tcpYAMLFilter(name, value string) string {
	return "version: 1\nfilters:\n  - name: " + name + "\n    protocol: tcp\n    direction: request\n    action: reject\n    match:\n      all:\n        - field: tcp.body\n          operator: exact\n          value: " + value + "\n"
}
