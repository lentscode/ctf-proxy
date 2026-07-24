package filter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChainEvaluatesEligibleFiltersInOrder protects ordered short-circuit evaluation.
func TestChainEvaluatesEligibleFiltersInOrder(t *testing.T) {
	var calls []string
	chain, err := NewChain(
		testFilter{name: "tcp-only", requirements: Requirements{Protocols: []Protocol{ProtocolTCP}}, evaluate: func(Message) (Decision, error) {
			calls = append(calls, "tcp-only")
			return Decision{Action: ActionReject}, nil
		}},
		testFilter{name: "allow", evaluate: func(Message) (Decision, error) {
			calls = append(calls, "allow")
			return Decision{Action: ActionAllow}, nil
		}},
		testFilter{name: "reject", evaluate: func(Message) (Decision, error) {
			calls = append(calls, "reject")
			return Decision{Action: ActionReject, Reason: "matched"}, nil
		}},
		testFilter{name: "after-reject", evaluate: func(Message) (Decision, error) {
			calls = append(calls, "after-reject")
			return Decision{Action: ActionAllow}, nil
		}},
	)
	require.NoError(t, err)

	decision := chain.Evaluate(context.Background(), Message{Protocol: ProtocolHTTP, Direction: DirectionRequest})

	assert.Equal(t, Decision{Action: ActionReject, Filter: "reject", Reason: "matched"}, decision)
	assert.Equal(t, []string{"allow", "reject"}, calls)
}

// TestChainFilterFailuresFailOpen covers errors, invalid decisions, and panics.
func TestChainFilterFailuresFailOpen(t *testing.T) {
	events := &recordingEventSink{}
	chain, err := NewChainWithEventSink(events,
		testFilter{name: "error", evaluate: func(Message) (Decision, error) { return Decision{}, errors.New("failed") }},
		testFilter{name: "panic", evaluate: func(Message) (Decision, error) { panic("failed") }},
		testFilter{name: "invalid", evaluate: func(Message) (Decision, error) { return Decision{Action: ActionUnknown}, nil }},
	)
	require.NoError(t, err)

	message := Message{Protocol: ProtocolHTTP, Direction: DirectionResponse}
	assert.Equal(t, Decision{Action: ActionAllow}, chain.Evaluate(context.Background(), message))
	assert.Equal(t, []Event{
		{Kind: EventKindEvaluationError, Filter: "error", Protocol: ProtocolHTTP, Direction: DirectionResponse},
		{Kind: EventKindPanic, Filter: "panic", Protocol: ProtocolHTTP, Direction: DirectionResponse},
		{Kind: EventKindInvalidDecision, Filter: "invalid", Protocol: ProtocolHTTP, Direction: DirectionResponse},
	}, events.events)
}

// TestChainReportsRejection verifies rejection metadata reaches the event sink.
func TestChainReportsRejection(t *testing.T) {
	events := &recordingEventSink{}
	chain, err := NewChainWithEventSink(events, testFilter{name: "reject", evaluate: func(Message) (Decision, error) {
		return Decision{Action: ActionReject}, nil
	}})
	require.NoError(t, err)

	decision := chain.Evaluate(context.Background(), Message{Protocol: ProtocolTCP, Direction: DirectionRequest})

	assert.Equal(t, ActionReject, decision.Action)
	assert.Equal(t, []Event{{
		Kind: EventKindRejected, Filter: "reject", Protocol: ProtocolTCP, Direction: DirectionRequest, Action: ActionReject,
	}}, events.events)
}

// TestChainNeedsHTTPBodyOnlyForEligibleDirection protects body-buffering eligibility.
func TestChainNeedsHTTPBodyOnlyForEligibleDirection(t *testing.T) {
	chain, err := NewChain(testFilter{name: "body", requirements: Requirements{
		Protocols:     []Protocol{ProtocolHTTP},
		Directions:    []Direction{DirectionResponse},
		NeedsHTTPBody: true,
	}})
	require.NoError(t, err)

	assert.False(t, chain.NeedsHTTPBody(DirectionRequest))
	assert.True(t, chain.NeedsHTTPBody(DirectionResponse))
}

// TestNewChainRejectsInvalidFilters covers chain contract validation.
func TestNewChainRejectsInvalidFilters(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		filters []Filter
	}{
		{name: "unknown protocol", filters: []Filter{testFilter{name: "invalid", requirements: Requirements{Protocols: []Protocol{ProtocolUnknown}}}}},
		{name: "duplicate names", filters: []Filter{testFilter{name: "same"}, testFilter{name: "same"}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewChain(testCase.filters...)
			require.Error(t, err)
		})
	}
}

// testFilter is a configurable filter double for chain tests.
type testFilter struct {
	name         string
	requirements Requirements
	evaluate     func(Message) (Decision, error)
}

// recordingEventSink captures non-blocking chain events for assertions.
type recordingEventSink struct {
	events []Event
}

// TryReport records one event in the test sink.
func (s *recordingEventSink) TryReport(event Event) {
	s.events = append(s.events, event)
}

// Name returns the configured test filter name.
func (f testFilter) Name() string { return f.name }

// Requirements returns the configured test filter requirements.
func (f testFilter) Requirements() Requirements { return f.requirements }

// Evaluate returns the configured decision and optionally records the message.
func (f testFilter) Evaluate(_ context.Context, message Message) (Decision, error) {
	if f.evaluate == nil {
		return Decision{Action: ActionAllow}, nil
	}
	return f.evaluate(message)
}
