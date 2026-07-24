package filter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileYAMLEvaluatesAllSupportedMatchers covers every supported matcher kind.
func TestCompileYAMLEvaluatesAllSupportedMatchers(t *testing.T) {
	filters, err := CompileYAML([]byte(`
version: 1
filters:
  - name: tcp-exact
    active: true
    protocol: tcp
    direction: request
    action: reject
    match:
      all:
        - field: tcp.body
          operator: exact
          value: ping
  - name: http-contains
    active: true
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

// TestCompileYAMLAllowsWhenBodyWasSkipped protects fail-open behavior for unavailable bodies.
func TestCompileYAMLAllowsWhenBodyWasSkipped(t *testing.T) {
	filters, err := CompileYAML([]byte(`
version: 1
filters:
  - name: body-rule
    active: true
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

// TestCompileYAMLInactiveRulesAreSkipped verifies that a filter is inactive
// unless its YAML declaration explicitly sets active to true.
func TestCompileYAMLInactiveRulesAreSkipped(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		active string
		want   Action
	}{
		{name: "omitted", active: "", want: ActionAllow},
		{name: "false", active: "    active: false\n", want: ActionAllow},
		{name: "true", active: "    active: true\n", want: ActionReject},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			filters, err := CompileYAML([]byte("version: 1\nfilters:\n  - name: test\n" + testCase.active + "    protocol: http\n    direction: request\n    action: reject\n    match:\n      all:\n        - field: http.body\n          operator: contains\n          value: secret\n"))
			require.NoError(t, err)

			chain, err := NewChain(filters...)
			require.NoError(t, err)
			assert.Equal(t, testCase.want == ActionReject, chain.NeedsHTTPBody(DirectionRequest))
			assert.Equal(t, testCase.want, chain.Evaluate(context.Background(), Message{
				Protocol: ProtocolHTTP, Direction: DirectionRequest, HTTP: &HTTPMessage{Body: []byte("secret")},
			}).Action)
		})
	}
}

// TestCompileYAMLNotContainsRejectsOnlyWhenHeaderLacksValue covers the explicit
// RE2-safe alternative to negative-lookahead regular expressions.
func TestCompileYAMLNotContainsRejectsOnlyWhenHeaderLacksValue(t *testing.T) {
	filters, err := CompileYAML([]byte(`
version: 1
filters:
  - name: require-checker
    active: true
    protocol: http
    direction: request
    action: reject
    match:
      all:
        - field: http.header
          header: User-Agent
          operator: not_contains
          value: checker
`))
	require.NoError(t, err)

	for _, testCase := range []struct {
		name   string
		header []string
		want   Action
	}{
		{name: "missing checker", header: []string{"curl/8"}, want: ActionReject},
		{name: "includes checker", header: []string{"checker/1"}, want: ActionAllow},
		{name: "one value includes checker", header: []string{"curl/8", "checker/1"}, want: ActionAllow},
		{name: "header absent", want: ActionAllow},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			header := make(map[string][]string)
			if testCase.header != nil {
				header["User-Agent"] = testCase.header
			}
			decision, evaluateErr := filters[0].Evaluate(context.Background(), Message{
				Protocol: ProtocolHTTP, Direction: DirectionRequest, HTTP: &HTTPMessage{Header: header},
			})
			require.NoError(t, evaluateErr)
			assert.Equal(t, testCase.want, decision.Action)
		})
	}
}

// TestCompileYAMLRejectsInvalidRules covers malformed and incompatible YAML rules.
func TestCompileYAMLRejectsInvalidRules(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input string
	}{
		{name: "unsupported version", input: "version: 2\nfilters: []\n"},
		{name: "missing action", input: "version: 1\nfilters:\n  - name: no-action\n    active: true\n    protocol: tcp\n    direction: request\n    match: {all: [{field: tcp.body, operator: exact, value: x}]}\n"},
		{name: "protocol-incompatible field", input: "version: 1\nfilters:\n  - name: invalid-field\n    active: true\n    protocol: tcp\n    direction: request\n    action: reject\n    match: {all: [{field: http.path, operator: exact, value: x}]}\n"},
		{name: "response path", input: "version: 1\nfilters:\n  - name: response-path\n    active: true\n    protocol: http\n    direction: response\n    action: reject\n    match: {all: [{field: http.path, operator: exact, value: x}]}\n"},
		{name: "invalid regular expression", input: "version: 1\nfilters:\n  - name: invalid-regex\n    active: true\n    protocol: http\n    direction: request\n    action: reject\n    match: {all: [{field: http.path, operator: regex, value: '['}]}\n"},
		{name: "unknown top-level field", input: "version: 1\nunknown: true\nfilters: []\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := CompileYAML([]byte(testCase.input))
			assert.Error(t, err)
		})
	}
}

// TestLoadYAMLFilesPreservesOrder protects file and rule declaration order.
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

// yamlRuleDocument returns a minimal HTTP path rejection document.
func yamlRuleDocument(name string) string {
	return "version: 1\nfilters:\n  - name: " + name + "\n    active: true\n    protocol: tcp\n    direction: request\n    action: reject\n    match:\n      all:\n        - field: tcp.body\n          operator: exact\n          value: ping\n"
}
