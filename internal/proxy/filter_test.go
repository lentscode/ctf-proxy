package proxy

import (
	"context"
	"sync"

	"github.com/lentscode/ctf-proxy/internal/filter"
)

type proxyTestFilter struct {
	name         string
	requirements filter.Requirements
	evaluate     func(filter.Message) (filter.Decision, error)
}

func (f proxyTestFilter) Name() string { return f.name }

func (f proxyTestFilter) Requirements() filter.Requirements { return f.requirements }

func (f proxyTestFilter) Evaluate(_ context.Context, message filter.Message) (filter.Decision, error) {
	if f.evaluate == nil {
		return filter.Decision{Action: filter.ActionAllow}, nil
	}
	return f.evaluate(message)
}

type messageRecorder struct {
	mu       sync.Mutex
	messages []filter.Message
}

func (r *messageRecorder) add(message filter.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, message)
}

func (r *messageRecorder) all() []filter.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]filter.Message(nil), r.messages...)
}
