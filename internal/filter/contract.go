package filter

import (
	"context"
	"net/http"
)

// Protocol identifies the protocol represented by a Message.
type Protocol uint8

const (
	// ProtocolUnknown is the zero value and is not valid for evaluation.
	ProtocolUnknown Protocol = iota
	ProtocolTCP
	ProtocolHTTP
)

// Direction identifies which side originated a Message.
type Direction uint8

const (
	// DirectionUnknown is the zero value and is not valid for evaluation.
	DirectionUnknown Direction = iota
	DirectionRequest
	DirectionResponse
)

// Action is the outcome requested by a filter.
type Action uint8

const (
	// ActionUnknown is the zero value and is not a valid filter outcome.
	ActionUnknown Action = iota
	ActionAllow
	ActionReject
)

// Message is the protocol-neutral, read-only view evaluated by a Filter.
//
// Exactly one of TCP and HTTP is populated according to Protocol. Filter
// implementations must not mutate Message or any value reachable from it.
type Message struct {
	Protocol   Protocol
	Direction  Direction
	Connection ConnectionInfo

	TCP  *TCPMessage
	HTTP *HTTPMessage
}

// ConnectionInfo describes the connection carrying a Message.
type ConnectionInfo struct {
	LocalAddr  string
	RemoteAddr string
}

// TCPMessage contains a single chunk read from a TCP stream.
//
// TCP does not preserve packet or application-message boundaries. Data is valid
// only for the duration of Filter.Evaluate and must not be retained or mutated.
type TCPMessage struct {
	Data []byte
}

// HTTPMessage contains the HTTP fields available to a filter.
//
// Method and Path are set for request messages. StatusCode is set for response
// messages. Header and Body are read-only. Body is nil when it was not needed
// by the active filter set or could not be safely buffered; BodySkipped reports
// the latter case.
type HTTPMessage struct {
	Method      string
	Path        string
	StatusCode  int
	Header      http.Header
	Body        []byte
	BodySkipped bool
}

// Decision is the result of evaluating a Filter.
//
// Filter identifies the filter that produced the decision. Reason is intended
// for bounded, non-sensitive observability and must not contain traffic data.
type Decision struct {
	Action Action
	Filter string
	Reason string
}

// Requirements declares the traffic a Filter needs to inspect.
//
// NeedsHTTPBody permits the chain to avoid buffering HTTP bodies unless at
// least one eligible filter requires them.
type Requirements struct {
	Protocols     []Protocol
	Directions    []Direction
	NeedsHTTPBody bool
}

// Filter evaluates one message and returns an allow or reject decision.
// Implementations must be safe for concurrent use by multiple proxied
// connections.
type Filter interface {
	Name() string
	Requirements() Requirements
	Evaluate(context.Context, Message) (Decision, error)
}
