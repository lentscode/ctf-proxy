package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestRegistryRejectsInvalidRegistrationAndBuild(t *testing.T) {
	registry := NewRegistry()
	require.Error(t, registry.Register("", func() (Filter, error) { return testFilter{}, nil }))
	require.Error(t, registry.Register("filter", nil))
	require.NoError(t, registry.Register("filter", func() (Filter, error) { return testFilter{name: "filter"}, nil }))
	require.Error(t, registry.Register("filter", func() (Filter, error) { return testFilter{name: "filter"}, nil }))

	_, err := registry.Build([]string{"missing"})
	assert.Error(t, err)
}
