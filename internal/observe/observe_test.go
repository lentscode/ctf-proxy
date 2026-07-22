package observe

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFanoutReporterIsolatesPanics(t *testing.T) {
	var received int
	reporter := NewFanoutReporter(panicReporter{}, ReporterFunc(func(Event) { received++ }))
	reporter.Report(Event{})
	assert.Equal(t, 1, received)
}

func TestFilterSinkMapsOnlySafeMetadata(t *testing.T) {
	var received Event
	sink := FilterSink(ReporterFunc(func(event Event) { received = event }), "web")
	sink.TryReport(filter.Event{Kind: filter.EventKindRejected, Filter: "block", Protocol: filter.ProtocolHTTP, Direction: filter.DirectionRequest})
	assert.Equal(t, Event{
		Level: LevelWarn, Component: ComponentFilter, Kind: KindFilterRejected,
		Proxy: "web", Filter: "block", Protocol: "http", Direction: "request", Message: "traffic rejected by filter",
	}, received)
}

func TestSanitizeMessageBoundsAndFlattens(t *testing.T) {
	message := SanitizeMessage("line one\nline two\x00" + strings.Repeat("x", 300))
	assert.NotContains(t, message, "\n")
	assert.NotContains(t, message, "\x00")
	assert.LessOrEqual(t, len(message), maxMessageBytes)
}

func TestObserverAssignsEventIdentityBeforeDelivery(t *testing.T) {
	observer := NewObserver(io.Discard)
	t.Cleanup(observer.Close)
	observer.Report(Event{Level: LevelWarn, Component: ComponentFilter, Kind: KindFilterRejected, Message: "one\ntwo"})
	events := observer.Hub().Snapshot(1)
	require.Len(t, events, 1)
	assert.Equal(t, uint64(1), events[0].ID)
	assert.False(t, events[0].Time.IsZero())
	assert.Equal(t, "one two", events[0].Message)
}

func TestSlogReporterWritesStructuredJSON(t *testing.T) {
	var output bytes.Buffer
	reporter := NewSlogReporter(&output)
	reporter.Report(Event{ID: 7, Level: LevelError, Component: ComponentProxy, Kind: KindProxyUpstreamUnavailable, Message: "upstream unavailable", Proxy: "web"})
	reporter.Close()
	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	assert.Equal(t, "ERROR", record["level"])
	assert.Equal(t, "proxy_upstream_unavailable", record["kind"])
	assert.Equal(t, "web", record["proxy"])
	assert.NotContains(t, record, "filter")
}

func TestSlogReporterDropsWhenBlockedWithoutBlockingCaller(t *testing.T) {
	writer := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	reporter := NewSlogReporter(writer)
	reporter.Report(Event{Level: LevelError, Component: ComponentProxy, Kind: KindProxyUpstreamUnavailable, Message: "upstream unavailable"})
	<-writer.entered
	start := time.Now()
	for i := 0; i <= stderrQueueCapacity; i++ {
		reporter.Report(Event{Level: LevelError, Component: ComponentProxy, Kind: KindProxyUpstreamUnavailable, Message: "upstream unavailable"})
	}
	assert.Less(t, time.Since(start), time.Second)
	assert.Greater(t, reporter.Dropped(), uint64(0))
	close(writer.release)
	reporter.Close()
}

type panicReporter struct{}

func (panicReporter) Report(Event) { panic("observer failed") }

// ReporterFunc adapts a function for concise observer tests and callers.
type ReporterFunc func(Event)

func (f ReporterFunc) Report(event Event) { f(event) }

type blockingWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *blockingWriter) Write(data []byte) (int, error) {
	select {
	case <-w.entered:
	default:
		close(w.entered)
	}
	<-w.release
	return len(data), nil
}
