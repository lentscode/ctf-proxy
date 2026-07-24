package observe

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const stderrQueueCapacity = 256

// SlogReporter writes JSON records asynchronously so stderr congestion cannot
// block the data plane.
type SlogReporter struct {
	logger  *slog.Logger
	queue   chan Event
	mu      sync.Mutex
	closed  bool
	done    chan struct{}
	dropped atomic.Uint64
}

// NewSlogReporter creates a JSON stderr-style reporter targeting writer.
func NewSlogReporter(writer io.Writer) *SlogReporter {
	r := &SlogReporter{
		logger: slog.New(slog.NewJSONHandler(writer, nil)),
		queue:  make(chan Event, stderrQueueCapacity),
		done:   make(chan struct{}),
	}
	go r.run()
	return r
}

// run drains the asynchronous stderr queue until it is closed.
func (r *SlogReporter) run() {
	defer close(r.done)
	for event := range r.queue {
		level := slog.LevelWarn
		if event.Level == LevelError {
			level = slog.LevelError
		}
		attrs := []any{"id", event.ID, "component", event.Component, "kind", event.Kind, "message", event.Message}
		if event.Proxy != "" {
			attrs = append(attrs, "proxy", event.Proxy)
		}
		if event.Filter != "" {
			attrs = append(attrs, "filter", event.Filter)
		}
		if event.Protocol != "" {
			attrs = append(attrs, "protocol", event.Protocol)
		}
		if event.Direction != "" {
			attrs = append(attrs, "direction", event.Direction)
		}
		r.logger.Log(nil, level, "ctf-proxy event", attrs...)
	}
}

// Report queues one already-sanitized event, dropping it if stderr is congested.
func (r *SlogReporter) Report(event Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.dropped.Add(1)
		return
	}
	select {
	case r.queue <- event:
	default:
		r.dropped.Add(1)
	}
}

// Close drains events already queued and stops the worker.
func (r *SlogReporter) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	<-r.done
}

// Dropped reports records dropped because the stderr queue was full or closed.
func (r *SlogReporter) Dropped() uint64 { return r.dropped.Load() }

// Observer enriches event intents and delivers each complete event to bounded
// in-memory history and asynchronous stderr logging.
type Observer struct {
	nextID atomic.Uint64
	hub    *Hub
	logs   *SlogReporter
}

// NewObserver creates the default bounded observability coordinator.
func NewObserver(stderr io.Writer) *Observer {
	return &Observer{hub: NewHub(), logs: NewSlogReporter(stderr)}
}

// Hub returns the observer's shared history and live-subscription source.
func (o *Observer) Hub() *Hub {
	if o == nil {
		return nil
	}
	return o.hub
}

// Report assigns identity and time, sanitizes the message, then delivers the
// same complete event to the hub and stderr logger.
func (o *Observer) Report(event Event) {
	if o == nil || o.hub == nil {
		return
	}
	event.ID = o.nextID.Add(1)
	event.Time = time.Now().UTC()
	event.Message = SanitizeMessage(event.Message)
	if o.hub.appendEvent(event) && o.logs != nil {
		o.logs.Report(event)
	}
}

// Close stops live observation and drains pending stderr records.
func (o *Observer) Close() {
	if o == nil {
		return
	}
	if o.hub != nil {
		o.hub.Close()
	}
	if o.logs != nil {
		o.logs.Close()
	}
}
