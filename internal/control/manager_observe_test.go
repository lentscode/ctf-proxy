package control

import (
	"context"
	"io"
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
