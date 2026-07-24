package filter

import (
	"context"
	"fmt"
)

// Chain evaluates an ordered, immutable set of filters.
type Chain struct {
	filters []Filter
	events  EventSink
}

// NewChain validates filters and returns an immutable evaluation chain.
func NewChain(filters ...Filter) (*Chain, error) {
	return NewChainWithEventSink(nil, filters...)
}

// NewChainWithEventSink validates filters and returns an immutable evaluation
// chain that reports sanitized events to sink.
func NewChainWithEventSink(sink EventSink, filters ...Filter) (*Chain, error) {
	if sink == nil {
		sink = discardEventSink{}
	}

	chain := &Chain{filters: make([]Filter, len(filters)), events: sink}
	seenNames := make(map[string]struct{}, len(filters))

	for index, current := range filters {
		if current == nil {
			return nil, fmt.Errorf("filter at index %d is nil", index)
		}

		name := current.Name()
		if name == "" {
			return nil, fmt.Errorf("filter at index %d has an empty name", index)
		}
		if _, exists := seenNames[name]; exists {
			return nil, fmt.Errorf("duplicate filter name %q", name)
		}
		seenNames[name] = struct{}{}

		if err := validateRequirements(name, current.Requirements()); err != nil {
			return nil, err
		}

		chain.filters[index] = current
	}

	return chain, nil
}

// Evaluate runs every eligible filter in order. The first rejection is
// returned. Filter errors, invalid decisions, and panics fail open so one
// filter cannot interrupt unrelated traffic.
func (c *Chain) Evaluate(ctx context.Context, message Message) Decision {
	if c == nil {
		return Decision{Action: ActionAllow}
	}

	for _, current := range c.filters {
		if !matches(current.Requirements(), message) {
			continue
		}

		decision, failure := evaluateSafely(ctx, current, message)
		if failure != EventKindUnknown {
			c.report(Event{
				Kind:      failure,
				Filter:    current.Name(),
				Protocol:  message.Protocol,
				Direction: message.Direction,
			})
			continue
		}
		if decision.Action != ActionReject {
			continue
		}
		if decision.Filter == "" {
			decision.Filter = current.Name()
		}
		c.report(Event{
			Kind:      EventKindRejected,
			Filter:    decision.Filter,
			Protocol:  message.Protocol,
			Direction: message.Direction,
			Action:    decision.Action,
		})
		return decision
	}

	return Decision{Action: ActionAllow}
}

// NeedsHTTPBody reports whether an HTTP filter eligible for direction needs a
// buffered body.
func (c *Chain) NeedsHTTPBody(direction Direction) bool {
	if c == nil {
		return false
	}

	for _, current := range c.filters {
		requirements := current.Requirements()
		if requirements.NeedsHTTPBody && matchesRequirements(requirements, ProtocolHTTP, direction) {
			return true
		}
	}

	return false
}

// report forwards an event without allowing a faulty sink to affect filtering.
func (c *Chain) report(event Event) {
	defer func() { _ = recover() }()
	c.events.TryReport(event)
}

// evaluateSafely isolates filter errors, invalid decisions, and panics.
func evaluateSafely(ctx context.Context, current Filter, message Message) (decision Decision, failure EventKind) {
	defer func() {
		if recover() != nil {
			failure = EventKindPanic
		}
	}()

	decision, err := current.Evaluate(ctx, message)
	if err != nil {
		return Decision{}, EventKindEvaluationError
	}
	if decision.Action != ActionAllow && decision.Action != ActionReject {
		return Decision{}, EventKindInvalidDecision
	}

	return decision, EventKindUnknown
}

// matches checks whether a filter is eligible for the message's protocol and direction.
func matches(requirements Requirements, message Message) bool {
	return matchesRequirements(requirements, message.Protocol, message.Direction)
}

// matchesRequirements applies the zero-list wildcard semantics for requirements.
func matchesRequirements(requirements Requirements, protocol Protocol, direction Direction) bool {
	return containsProtocol(requirements.Protocols, protocol) && containsDirection(requirements.Directions, direction)
}

// containsProtocol reports whether wanted is allowed by protocols.
func containsProtocol(protocols []Protocol, wanted Protocol) bool {
	if len(protocols) == 0 {
		return true
	}
	for _, protocol := range protocols {
		if protocol == wanted {
			return true
		}
	}
	return false
}

// containsDirection reports whether wanted is allowed by directions.
func containsDirection(directions []Direction, wanted Direction) bool {
	if len(directions) == 0 {
		return true
	}
	for _, direction := range directions {
		if direction == wanted {
			return true
		}
	}
	return false
}

// validateRequirements rejects protocol and direction values outside the contract.
func validateRequirements(filterName string, requirements Requirements) error {
	for _, protocol := range requirements.Protocols {
		if protocol != ProtocolTCP && protocol != ProtocolHTTP {
			return fmt.Errorf("filter %q has invalid protocol %d", filterName, protocol)
		}
	}
	for _, direction := range requirements.Directions {
		if direction != DirectionRequest && direction != DirectionResponse {
			return fmt.Errorf("filter %q has invalid direction %d", filterName, direction)
		}
	}
	return nil
}
