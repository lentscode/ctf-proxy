package filter

import (
	"fmt"
	"sync"
)

// Factory creates a new instance of a compiled Go filter.
type Factory func() (Filter, error)

// Registry maps stable configuration names to compiled Go filter factories.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register makes factory available under name. Names are unique and cannot be
// changed after registration.
func (r *Registry) Register(name string, factory Factory) error {
	if r == nil {
		return fmt.Errorf("register filter %q: nil registry", name)
	}
	if name == "" {
		return fmt.Errorf("register filter: empty name")
	}
	if factory == nil {
		return fmt.Errorf("register filter %q: nil factory", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("register filter %q: already registered", name)
	}
	r.factories[name] = factory
	return nil
}

// Build creates filters in names' order. Factories may return an immutable
// shared filter or a fresh instance, but every returned filter must be safe for
// concurrent use.
func (r *Registry) Build(names []string) ([]Filter, error) {
	if r == nil {
		return nil, fmt.Errorf("build filters: nil registry")
	}

	filters := make([]Filter, 0, len(names))
	for _, name := range names {
		r.mu.RLock()
		factory, exists := r.factories[name]
		r.mu.RUnlock()
		if !exists {
			return nil, fmt.Errorf("build filter %q: not registered", name)
		}

		current, err := factory()
		if err != nil {
			return nil, fmt.Errorf("build filter %q: %w", name, err)
		}
		if current == nil {
			return nil, fmt.Errorf("build filter %q: factory returned nil", name)
		}
		if current.Name() != name {
			return nil, fmt.Errorf("build filter %q: factory returned filter named %q", name, current.Name())
		}

		filters = append(filters, current)
	}

	return filters, nil
}
