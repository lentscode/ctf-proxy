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
	MatchOperatorPrefix
	MatchOperatorSuffix
	MatchOperatorRegex
)

const yamlVersion1 = 1

type yamlConfiguration struct {
	Version uint8      `yaml:"version"`
	Filters []yamlRule `yaml:"filters"`
}

type yamlRule struct {
	Name      string    `yaml:"name"`
	Protocol  string    `yaml:"protocol"`
	Direction string    `yaml:"direction"`
	Action    string    `yaml:"action"`
	Match     yamlMatch `yaml:"match"`
}

type yamlMatch struct {
	All []yamlCondition `yaml:"all"`
}

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

type compiledYAMLRule struct {
	name         string
	requirements Requirements
	conditions   []compiledYAMLCondition
}

type compiledYAMLCondition struct {
	field    MatchField
	header   string
	operator MatchOperator
	value    []byte
	regex    *regexp.Regexp
}

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

func parseMatchOperator(value string) (MatchOperator, error) {
	switch value {
	case "exact":
		return MatchOperatorExact, nil
	case "contains":
		return MatchOperatorContains, nil
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

func isFieldSupportedByDirection(field MatchField, direction Direction) bool {
	return field != MatchFieldHTTPPath || direction == DirectionRequest
}

func (r *compiledYAMLRule) Name() string {
	return r.name
}

func (r *compiledYAMLRule) Requirements() Requirements {
	return r.requirements
}

func (r *compiledYAMLRule) Evaluate(_ context.Context, message Message) (Decision, error) {
	for _, condition := range r.conditions {
		if !condition.matches(message) {
			return Decision{Action: ActionAllow}, nil
		}
	}

	return Decision{Action: ActionReject, Filter: r.name, Reason: "YAML rule matched"}, nil
}

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
		for _, value := range message.HTTP.Header.Values(c.header) {
			if c.matchesValue([]byte(value)) {
				return true
			}
		}
	}

	return false
}

func (c compiledYAMLCondition) matchesValue(value []byte) bool {
	switch c.operator {
	case MatchOperatorExact:
		return bytes.Equal(value, c.value)
	case MatchOperatorContains:
		return bytes.Contains(value, c.value)
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
