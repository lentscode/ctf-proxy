package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegistryBuildsFreshFiltersInRequestedOrder protects registry order and isolation.
func TestRegistryBuildsFreshFiltersInRequestedOrder(t *testing.T) {
	registry := NewRegistry()
	created := 0
	require.NoError(t, registry.Register("first", func() (Filter, error) {
		created++
		return testFilter{name: "first"}, nil
	}))
	require.NoError(t, registry.Register("second", func() (Filter, error) {
		created++
		return testFilter{name: "second"}, nil
	}))

	filters, err := registry.Build([]string{"second", "first", "second"})

	require.NoError(t, err)
	assert.Len(t, filters, 3)
	assert.Equal(t, []string{"second", "first", "second"}, []string{filters[0].Name(), filters[1].Name(), filters[2].Name()})
	assert.Equal(t, 3, created)
}

// TestRegistryRejectsInvalidRegistrationAndBuild covers registry input failures.
func TestRegistryRejectsInvalidRegistrationAndBuild(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*Registry) error
	}{
		{name: "empty registration name", run: func(registry *Registry) error {
			return registry.Register("", func() (Filter, error) { return testFilter{}, nil })
		}},
		{name: "nil factory", run: func(registry *Registry) error { return registry.Register("filter", nil) }},
		{name: "missing requested filter", run: func(registry *Registry) error { _, err := registry.Build([]string{"missing"}); return err }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Error(t, testCase.run(NewRegistry()))
		})
	}

	registry := NewRegistry()
	require.NoError(t, registry.Register("filter", func() (Filter, error) { return testFilter{name: "filter"}, nil }))
	require.Error(t, registry.Register("filter", func() (Filter, error) { return testFilter{name: "filter"}, nil }))
}
