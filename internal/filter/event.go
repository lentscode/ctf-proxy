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

type discardEventSink struct{}

func (discardEventSink) TryReport(Event) {}
