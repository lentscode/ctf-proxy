package control

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestManagerReplaceRestoresActiveProxyAfterListenerConflict(t *testing.T) {
	manager, store, original, upstream := startActiveTCPManager(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = occupied.Close() })

	_, err = manager.Replace(original.Name, config.Proxy{
		Active: true, Protocol: "tcp", Listen: occupied.Addr().String(), Upstream: upstream,
	})
	require.ErrorIs(t, err, ErrConflict)

	// The unsuccessful replacement must leave both the durable configuration and
	// the original live listener intact.
	require.Equal(t, original, store.Snapshot().Proxies[0])
	view, err := manager.Get(original.Name)
	require.NoError(t, err)
	require.Equal(t, StateRunning, view.State)
	require.Equal(t, original.Listen, view.Listen)
	assertTCPProxyRoundTrip(t, original.Listen)
}

func TestManagerReplaceRestoresActiveProxyAfterPersistenceFailure(t *testing.T) {
	manager, store, original, upstream := startActiveTCPManager(t)
	changedUpstream := unusedControlTCPAddress(t)
	require.NotEqual(t, upstream, changedUpstream)

	// Removing the configuration directory after startup forces Store.Update to
	// fail after the replacement listener has started, exercising the second
	// rollback path in applyLocked.
	require.NoError(t, os.RemoveAll(filepath.Dir(manager.configPath)))
	_, err := manager.Replace(original.Name, config.Proxy{
		Active: true, Protocol: "tcp", Listen: original.Listen, Upstream: changedUpstream,
	})
	require.True(t, errors.Is(err, ErrPersistence), "expected persistence failure, got %v", err)

	require.Equal(t, original, store.Snapshot().Proxies[0])
	view, err := manager.Get(original.Name)
	require.NoError(t, err)
	require.Equal(t, StateRunning, view.State)
	require.Equal(t, original.Upstream, view.Upstream)
	assertTCPProxyRoundTrip(t, original.Listen)
}

func unusedControlTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

func startActiveTCPManager(t *testing.T) (*Manager, *config.Store, config.Proxy, string) {
	t.Helper()
	upstream := startControlTestEchoServer(t)
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listen := reserved.Addr().String()
	require.NoError(t, reserved.Close())

	path := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	original := config.Proxy{Name: "tcp", Active: true, Protocol: "tcp", Listen: listen, Upstream: upstream}
	require.NoError(t, config.Save(path, config.Config{Version: config.Version, Proxies: []config.Proxy{original}}))
	store, err := config.OpenStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	return manager, store, original, upstream
}

func startControlTestEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()
	return listener.Addr().String()
}

func assertTCPProxyRoundTrip(t *testing.T, address string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, connection.SetDeadline(time.Now().Add(time.Second)))
	_, err = connection.Write([]byte("rollback-check"))
	require.NoError(t, err)
	response := make([]byte, len("rollback-check"))
	_, err = io.ReadFull(connection, response)
	require.NoError(t, err)
	require.Equal(t, "rollback-check", string(response))
}

func tcpYAMLFilter(name, value string) string {
	return "version: 1\nfilters:\n  - name: " + name + "\n    protocol: tcp\n    direction: request\n    action: reject\n    match:\n      all:\n        - field: tcp.body\n          operator: exact\n          value: " + value + "\n"
}
