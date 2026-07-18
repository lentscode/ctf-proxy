package filter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileYAMLEvaluatesAllSupportedMatchers(t *testing.T) {
	filters, err := CompileYAML([]byte(`
version: 1
filters:
  - name: tcp-exact
    protocol: tcp
    direction: request
    action: reject
    match:
      all:
        - field: tcp.body
          operator: exact
          value: ping
  - name: http-contains
    protocol: http
    direction: request
    action: reject
    match:
      all:
        - field: http.body
          operator: contains
          value: password=
        - field: http.header
          header: X-Mode
          operator: suffix
          value: debug
        - field: http.path
          operator: prefix
          value: /admin
        - field: http.header
          header: User-Agent
          operator: regex
          value: '^curl/[0-9]+$'
`))
	require.NoError(t, err)
	require.Len(t, filters, 2)

	chain, err := NewChain(filters...)
	require.NoError(t, err)
	assert.True(t, chain.NeedsHTTPBody(DirectionRequest))

	assert.Equal(t, ActionReject, chain.Evaluate(context.Background(), Message{
		Protocol: ProtocolTCP, Direction: DirectionRequest, TCP: &TCPMessage{Data: []byte("ping")},
	}).Action)
	assert.Equal(t, ActionReject, chain.Evaluate(context.Background(), Message{
		Protocol: ProtocolHTTP, Direction: DirectionRequest,
		HTTP: &HTTPMessage{
			Path: "/admin/users", Body: []byte("email=a@b&password=secret"),
			Header: map[string][]string{"X-Mode": {"production-debug"}, "User-Agent": {"curl/8"}},
		},
	}).Action)
}

func TestCompileYAMLAllowsWhenBodyWasSkipped(t *testing.T) {
	filters, err := CompileYAML([]byte(`
version: 1
filters:
  - name: body-rule
    protocol: http
    direction: response
    action: reject
    match:
      all:
        - field: http.body
          operator: contains
          value: flag
`))
	require.NoError(t, err)

	decision, err := filters[0].Evaluate(context.Background(), Message{
		Protocol: ProtocolHTTP, Direction: DirectionResponse,
		HTTP: &HTTPMessage{BodySkipped: true},
	})
	require.NoError(t, err)
	assert.Equal(t, ActionAllow, decision.Action)
}

func TestCompileYAMLRejectsInvalidRules(t *testing.T) {
	testCases := []string{
		"version: 2\nfilters: []\n",
		"version: 1\nfilters:\n  - name: no-action\n    protocol: tcp\n    direction: request\n    match: {all: [{field: tcp.body, operator: exact, value: x}]}\n",
		"version: 1\nfilters:\n  - name: invalid-field\n    protocol: tcp\n    direction: request\n    action: reject\n    match: {all: [{field: http.path, operator: exact, value: x}]}\n",
		"version: 1\nfilters:\n  - name: response-path\n    protocol: http\n    direction: response\n    action: reject\n    match: {all: [{field: http.path, operator: exact, value: x}]}\n",
		"version: 1\nfilters:\n  - name: invalid-regex\n    protocol: http\n    direction: request\n    action: reject\n    match: {all: [{field: http.path, operator: regex, value: '['}]}\n",
		"version: 1\nunknown: true\nfilters: []\n",
	}

	for _, input := range testCases {
		_, err := CompileYAML([]byte(input))
		assert.Error(t, err, input)
	}
}

func TestLoadYAMLFilesPreservesOrder(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.yaml")
	second := filepath.Join(directory, "second.yaml")
	require.NoError(t, os.WriteFile(first, []byte(yamlRuleDocument("first")), 0o600))
	require.NoError(t, os.WriteFile(second, []byte(yamlRuleDocument("second")), 0o600))

	filters, err := LoadYAMLFiles([]string{first, second})

	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second"}, []string{filters[0].Name(), filters[1].Name()})
}

func yamlRuleDocument(name string) string {
	return "version: 1\nfilters:\n  - name: " + name + "\n    protocol: tcp\n    direction: request\n    action: reject\n    match:\n      all:\n        - field: tcp.body\n          operator: exact\n          value: ping\n"
}
