package observe

import (
	"sync"
	"sync/atomic"
)

const (
	// HistoryCapacity is the maximum number of shared dashboard events kept in memory.
	HistoryCapacity         = 512
	maxSubscribers          = 16
	subscriberQueueCapacity = 16
)

// Subscription is one live event-stream consumer.
type Subscription struct {
	events chan Event
}

// Events returns the read-only event stream. It closes when the subscriber is
// slow, explicitly removed, or the hub is closed.
func (s *Subscription) Events() <-chan Event { return s.events }

// Hub retains recent events and distributes new events to bounded subscribers.
type Hub struct {
	mu           sync.Mutex
	events       [HistoryCapacity]Event
	start, count int
	closed       bool
	subscribers  map[*Subscription]struct{}
	dropped      atomic.Uint64
}

// NewHub returns an empty hub with the fixed retention and subscriber limits.
func NewHub() *Hub { return &Hub{subscribers: make(map[*Subscription]struct{})} }

// appendEvent retains a complete event and notifies consumers. Event
// enrichment, including ID allocation and message sanitization, is owned by
// Observer. It returns false after Close.
func (h *Hub) appendEvent(event Event) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		h.dropped.Add(1)
		return false
	}
	if h.count < HistoryCapacity {
		h.events[(h.start+h.count)%HistoryCapacity] = event
		h.count++
	} else {
		h.events[h.start] = event
		h.start = (h.start + 1) % HistoryCapacity
	}
	for subscriber := range h.subscribers {
		select {
		case subscriber.events <- event:
		default:
			delete(h.subscribers, subscriber)
			close(subscriber.events)
			h.dropped.Add(1)
		}
	}
	return true
}

// Snapshot returns at most limit events, ordered from oldest to newest.
func (h *Hub) Snapshot(limit int) []Event {
	if h == nil || limit <= 0 {
		return []Event{}
	}
	if limit > HistoryCapacity {
		limit = HistoryCapacity
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit > h.count {
		limit = h.count
	}
	start := (h.start + h.count - limit) % HistoryCapacity
	result := make([]Event, limit)
	for i := range result {
		result[i] = h.events[(start+i)%HistoryCapacity]
	}
	return result
}

// Subscribe adds a bounded live consumer. It returns false at capacity or
// after the hub has closed.
func (h *Hub) Subscribe() (*Subscription, bool) {
	if h == nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subscribers) >= maxSubscribers {
		return nil, false
	}
	subscription := &Subscription{events: make(chan Event, subscriberQueueCapacity)}
	h.subscribers[subscription] = struct{}{}
	return subscription, true
}

// Unsubscribe removes a subscriber. It is safe to call more than once.
func (h *Hub) Unsubscribe(subscription *Subscription) {
	if h == nil || subscription == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subscribers[subscription]; ok {
		delete(h.subscribers, subscription)
		close(subscription.events)
	}
}

// Close disconnects live consumers. Retained history remains available only to
// in-process users; no new subscriptions or events are accepted.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for subscriber := range h.subscribers {
		close(subscriber.events)
	}
	clear(h.subscribers)
}

// Dropped reports events dropped because delivery could not be bounded.
func (h *Hub) Dropped() uint64 { return h.dropped.Load() }
