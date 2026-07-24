package filter

// EventKind identifies a filter-chain event that is safe to observe outside
// the data plane.
type EventKind uint8

const (
	EventKindUnknown EventKind = iota
	EventKindRejected
	EventKindEvaluationError
	EventKindPanic
	EventKindInvalidDecision
)

// Event contains sanitized metadata about filter evaluation. It deliberately
// excludes traffic content, connection addresses, and error values.
type Event struct {
	Kind      EventKind
	Filter    string
	Protocol  Protocol
	Direction Direction
	Action    Action
}

// EventSink receives filter events. TryReport must not block; implementations
// should use bounded queues or drop events when they cannot accept more work.
type EventSink interface {
	TryReport(Event)
}

// discardEventSink is the default sink for chains without observability.
type discardEventSink struct{}

// TryReport intentionally drops events when no sink was configured.
func (discardEventSink) TryReport(Event) {}
