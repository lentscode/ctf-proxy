package filter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"

	"go.yaml.in/yaml/v4"
)

// MatchField identifies a message value that a YAML rule can inspect.
type MatchField uint8

const (
	MatchFieldUnknown MatchField = iota
	MatchFieldTCPBody
	MatchFieldHTTPBody
	MatchFieldHTTPHeader
	MatchFieldHTTPPath
)

// MatchOperator identifies how a YAML condition compares its field and value.
type MatchOperator uint8

const (
	MatchOperatorUnknown MatchOperator = iota
	MatchOperatorExact
	MatchOperatorContains
	MatchOperatorNotContains
	MatchOperatorPrefix
	MatchOperatorSuffix
	MatchOperatorRegex
)

const yamlVersion1 = 1

// yamlConfiguration is the versioned top-level YAML filter document.
type yamlConfiguration struct {
	Version uint8      `yaml:"version"`
	Filters []yamlRule `yaml:"filters"`
}

// yamlRule is one reject-only filter declaration from a YAML document.
type yamlRule struct {
	Name      string    `yaml:"name"`
	Protocol  string    `yaml:"protocol"`
	Direction string    `yaml:"direction"`
	Action    string    `yaml:"action"`
	Match     yamlMatch `yaml:"match"`
	Active    bool      `yaml:"active"`
}

// yamlMatch groups the conditions that must all match for a rule to reject.
type yamlMatch struct {
	All []yamlCondition `yaml:"all"`
}

// yamlCondition describes one field/operator/value comparison.
type yamlCondition struct {
	Field    string `yaml:"field"`
	Header   string `yaml:"header"`
	Operator string `yaml:"operator"`
	Value    string `yaml:"value"`
}

// CompileYAML parses one versioned YAML document and returns its validated,
// compiled filters in declaration order.
func CompileYAML(data []byte) ([]Filter, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var configuration yamlConfiguration
	if err := decoder.Decode(&configuration); err != nil {
		return nil, fmt.Errorf("decode YAML filters: %w", err)
	}
	if err := ensureSingleYAMLDocument(decoder); err != nil {
		return nil, err
	}
	if configuration.Version != yamlVersion1 {
		return nil, fmt.Errorf("unsupported YAML filter version %d", configuration.Version)
	}

	filters := make([]Filter, 0, len(configuration.Filters))
	seenNames := make(map[string]struct{}, len(configuration.Filters))
	for index, rule := range configuration.Filters {
		compiled, err := compileYAMLRule(rule)
		if err != nil {
			return nil, fmt.Errorf("compile YAML filter at index %d: %w", index, err)
		}
		if _, exists := seenNames[compiled.name]; exists {
			return nil, fmt.Errorf("duplicate YAML filter name %q", compiled.name)
		}
		seenNames[compiled.name] = struct{}{}
		filters = append(filters, compiled)
	}

	return filters, nil
}

// LoadYAMLFiles compiles YAML filter files in paths' order.
func LoadYAMLFiles(paths []string) ([]Filter, error) {
	filters := make([]Filter, 0)
	seenNames := make(map[string]struct{})

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read YAML filter file %q: %w", path, err)
		}

		compiled, err := CompileYAML(data)
		if err != nil {
			return nil, fmt.Errorf("load YAML filter file %q: %w", path, err)
		}
		for _, current := range compiled {
			if _, exists := seenNames[current.Name()]; exists {
				return nil, fmt.Errorf("duplicate YAML filter name %q", current.Name())
			}
			seenNames[current.Name()] = struct{}{}
			filters = append(filters, current)
		}
	}

	return filters, nil
}

// ensureSingleYAMLDocument rejects trailing YAML documents after the first one.
func ensureSingleYAMLDocument(decoder *yaml.Decoder) error {
	var extra yaml.Node
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode YAML filters: %w", err)
	}
	return fmt.Errorf("YAML filter file must contain exactly one document")
}

// compiledYAMLRule is an immutable, executable YAML filter.
type compiledYAMLRule struct {
	name         string
	requirements Requirements
	conditions   []compiledYAMLCondition
	active       bool
}

// compiledYAMLCondition stores parsed matching data for allocation-free evaluation.
type compiledYAMLCondition struct {
	field    MatchField
	header   string
	operator MatchOperator
	value    []byte
	regex    *regexp.Regexp
}

// compileYAMLRule validates one declaration and derives its filter requirements.
func compileYAMLRule(rule yamlRule) (*compiledYAMLRule, error) {
	if rule.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	protocol, err := parseYAMLProtocol(rule.Protocol)
	if err != nil {
		return nil, err
	}
	direction, err := parseYAMLDirection(rule.Direction)
	if err != nil {
		return nil, err
	}
	if rule.Action != "reject" {
		return nil, fmt.Errorf("action must be %q, got %q", "reject", rule.Action)
	}
	if len(rule.Match.All) == 0 {
		return nil, fmt.Errorf("match.all must contain at least one condition")
	}

	compiled := &compiledYAMLRule{
		name: rule.Name,
		requirements: Requirements{
			Protocols:  []Protocol{protocol},
			Directions: []Direction{direction},
		},
		conditions: make([]compiledYAMLCondition, 0, len(rule.Match.All)),
		active:     rule.Active,
	}

	for index, condition := range rule.Match.All {
		parsed, err := compileYAMLCondition(protocol, direction, condition)
		if err != nil {
			return nil, fmt.Errorf("condition at index %d: %w", index, err)
		}
		if parsed.field == MatchFieldHTTPBody {
			compiled.requirements.NeedsHTTPBody = true
		}
		compiled.conditions = append(compiled.conditions, parsed)
	}

	return compiled, nil
}

// compileYAMLCondition validates field compatibility and compiles regex operators.
func compileYAMLCondition(protocol Protocol, direction Direction, condition yamlCondition) (compiledYAMLCondition, error) {
	field, err := parseMatchField(condition.Field)
	if err != nil {
		return compiledYAMLCondition{}, err
	}
	if !isFieldSupportedByProtocol(field, protocol) {
		return compiledYAMLCondition{}, fmt.Errorf("field %q is not valid for protocol", condition.Field)
	}
	if !isFieldSupportedByDirection(field, direction) {
		return compiledYAMLCondition{}, fmt.Errorf("field %q is not valid for direction", condition.Field)
	}
	operator, err := parseMatchOperator(condition.Operator)
	if err != nil {
		return compiledYAMLCondition{}, err
	}
	if field == MatchFieldHTTPHeader && condition.Header == "" {
		return compiledYAMLCondition{}, fmt.Errorf("header is required for field %q", condition.Field)
	}
	if field != MatchFieldHTTPHeader && condition.Header != "" {
		return compiledYAMLCondition{}, fmt.Errorf("header is only valid for field %q", "http.header")
	}

	compiled := compiledYAMLCondition{
		field:    field,
		header:   condition.Header,
		operator: operator,
		value:    []byte(condition.Value),
	}
	if operator == MatchOperatorRegex {
		compiled.regex, err = regexp.Compile(condition.Value)
		if err != nil {
			return compiledYAMLCondition{}, fmt.Errorf("compile regex: %w", err)
		}
	}

	return compiled, nil
}

// parseYAMLProtocol converts a YAML protocol name to its contract value.
func parseYAMLProtocol(value string) (Protocol, error) {
	switch value {
	case "tcp":
		return ProtocolTCP, nil
	case "http":
		return ProtocolHTTP, nil
	default:
		return ProtocolUnknown, fmt.Errorf("unsupported protocol %q", value)
	}
}

// parseYAMLDirection converts a YAML direction name to its contract value.
func parseYAMLDirection(value string) (Direction, error) {
	switch value {
	case "request":
		return DirectionRequest, nil
	case "response":
		return DirectionResponse, nil
	default:
		return DirectionUnknown, fmt.Errorf("unsupported direction %q", value)
	}
}

// parseMatchField converts a YAML field name to its matcher value.
func parseMatchField(value string) (MatchField, error) {
	switch value {
	case "tcp.body":
		return MatchFieldTCPBody, nil
	case "http.body":
		return MatchFieldHTTPBody, nil
	case "http.header":
		return MatchFieldHTTPHeader, nil
	case "http.path":
		return MatchFieldHTTPPath, nil
	default:
		return MatchFieldUnknown, fmt.Errorf("unsupported match field %q", value)
	}
}

// parseMatchOperator converts a YAML operator name to its matcher value.
func parseMatchOperator(value string) (MatchOperator, error) {
	switch value {
	case "exact":
		return MatchOperatorExact, nil
	case "contains":
		return MatchOperatorContains, nil
	case "not_contains":
		return MatchOperatorNotContains, nil
	case "prefix":
		return MatchOperatorPrefix, nil
	case "suffix":
		return MatchOperatorSuffix, nil
	case "regex":
		return MatchOperatorRegex, nil
	default:
		return MatchOperatorUnknown, fmt.Errorf("unsupported match operator %q", value)
	}
}

// isFieldSupportedByProtocol reports whether field belongs to protocol.
func isFieldSupportedByProtocol(field MatchField, protocol Protocol) bool {
	switch protocol {
	case ProtocolTCP:
		return field == MatchFieldTCPBody
	case ProtocolHTTP:
		return field == MatchFieldHTTPBody || field == MatchFieldHTTPHeader || field == MatchFieldHTTPPath
	default:
		return false
	}
}

// isFieldSupportedByDirection reports whether field is valid for direction.
func isFieldSupportedByDirection(field MatchField, direction Direction) bool {
	return field != MatchFieldHTTPPath || direction == DirectionRequest
}

// Name returns the stable name of the compiled YAML rule.
func (r *compiledYAMLRule) Name() string {
	return r.name
}

// Active reports whether the YAML declaration enables this rule.
func (r *compiledYAMLRule) Active() bool {
	return r.active
}

// DeclaredRequirements returns the rule's protocol and direction metadata,
// regardless of whether it is currently active.
func (r *compiledYAMLRule) DeclaredRequirements() Requirements {
	return r.requirements
}

// Requirements returns the protocol, direction, and body-buffering needs of an
// active rule. Inactive rules are skipped entirely, including body buffering.
func (r *compiledYAMLRule) Requirements() Requirements {
	if !r.active {
		return Requirements{}
	}
	return r.requirements
}

// Evaluate rejects when this active rule's compiled conditions all match.
func (r *compiledYAMLRule) Evaluate(_ context.Context, message Message) (Decision, error) {
	if !r.active {
		return Decision{Action: ActionAllow}, nil
	}

	for _, condition := range r.conditions {
		if !condition.matches(message) {
			return Decision{Action: ActionAllow}, nil
		}
	}

	return Decision{Action: ActionReject, Filter: r.name, Reason: "YAML rule matched"}, nil
}

// matches evaluates a condition against the field selected from message.
func (c compiledYAMLCondition) matches(message Message) bool {
	switch c.field {
	case MatchFieldTCPBody:
		return message.TCP != nil && c.matchesValue(message.TCP.Data)
	case MatchFieldHTTPBody:
		return message.HTTP != nil && !message.HTTP.BodySkipped && c.matchesValue(message.HTTP.Body)
	case MatchFieldHTTPPath:
		return message.HTTP != nil && c.matchesValue([]byte(message.HTTP.Path))
	case MatchFieldHTTPHeader:
		if message.HTTP == nil {
			return false
		}
		values := message.HTTP.Header.Values(c.header)
		if len(values) == 0 {
			return false
		}
		if c.operator == MatchOperatorNotContains {
			for _, value := range values {
				if bytes.Contains([]byte(value), c.value) {
					return false
				}
			}
			return true
		}
		for _, value := range values {
			if c.matchesValue([]byte(value)) {
				return true
			}
		}
	}

	return false
}

// matchesValue applies the condition's exact, substring, boundary, or regex operator.
func (c compiledYAMLCondition) matchesValue(value []byte) bool {
	switch c.operator {
	case MatchOperatorExact:
		return bytes.Equal(value, c.value)
	case MatchOperatorContains:
		return bytes.Contains(value, c.value)
	case MatchOperatorNotContains:
		return !bytes.Contains(value, c.value)
	case MatchOperatorPrefix:
		return bytes.HasPrefix(value, c.value)
	case MatchOperatorSuffix:
		return bytes.HasSuffix(value, c.value)
	case MatchOperatorRegex:
		return c.regex.Match(value)
	default:
		return false
	}
}
