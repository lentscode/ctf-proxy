package proxy

import (
	"context"
	"sync"

	"github.com/lentscode/ctf-proxy/internal/filter"
)

// proxyTestFilter is a filter double that records messages and returns a decision.
type proxyTestFilter struct {
	name         string
	requirements filter.Requirements
	evaluate     func(filter.Message) (filter.Decision, error)
}

// Name returns the test filter's configured name.
func (f proxyTestFilter) Name() string { return f.name }

// Requirements returns the test filter's protocol and direction scope.
func (f proxyTestFilter) Requirements() filter.Requirements { return f.requirements }

// Evaluate records the message and returns the configured test result.
func (f proxyTestFilter) Evaluate(_ context.Context, message filter.Message) (filter.Decision, error) {
	if f.evaluate == nil {
		return filter.Decision{Action: filter.ActionAllow}, nil
	}
	return f.evaluate(message)
}

// messageRecorder stores filter messages in arrival order for assertions.
type messageRecorder struct {
	mu       sync.Mutex
	messages []filter.Message
}

// add appends one message to the recorder.
func (r *messageRecorder) add(message filter.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, message)
}

// all returns a snapshot of the recorded messages.
func (r *messageRecorder) all() []filter.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]filter.Message(nil), r.messages...)
}
