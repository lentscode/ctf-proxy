package observe

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHubRetainsLatestEventsWithoutConsumption(t *testing.T) {
	hub := NewHub()
	for i := 0; i < HistoryCapacity+1; i++ {
		hub.appendEvent(Event{ID: uint64(i + 1), Level: LevelWarn, Component: ComponentControl, Kind: KindControlConfigurationRejected, Message: fmt.Sprintf("%d", i)})
	}
	first := hub.Snapshot(HistoryCapacity)
	second := hub.Snapshot(HistoryCapacity)
	require.Len(t, first, HistoryCapacity)
	assert.Equal(t, first, second)
	assert.Equal(t, uint64(2), first[0].ID)
	assert.Equal(t, uint64(HistoryCapacity+1), first[len(first)-1].ID)
}

func TestHubDisconnectsOnlySlowSubscriber(t *testing.T) {
	hub := NewHub()
	slow, ok := hub.Subscribe()
	require.True(t, ok)
	healthy, ok := hub.Subscribe()
	require.True(t, ok)
	for i := 0; i < subscriberQueueCapacity; i++ {
		hub.appendEvent(Event{ID: uint64(i + 1), Level: LevelWarn, Component: ComponentFilter, Kind: KindFilterRejected, Message: "rejected"})
	}
	<-healthy.Events()
	hub.appendEvent(Event{ID: uint64(subscriberQueueCapacity + 1), Level: LevelWarn, Component: ComponentFilter, Kind: KindFilterRejected, Message: "rejected"})
	for range slow.Events() {
	}
	select {
	case event := <-healthy.Events():
		assert.NotZero(t, event.ID)
	default:
		t.Fatal("healthy subscriber did not receive an event")
	}
	assert.Greater(t, hub.Dropped(), uint64(0))
}
