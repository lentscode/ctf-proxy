// Package observe provides bounded, sanitized operational observability.
package observe

import (
	"strings"
	"time"
	"unicode"
)

// Level is an event severity that is safe to expose to the local dashboard.
type Level string

const (
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Component identifies the subsystem that emitted an Event.
type Component string

const (
	ComponentFilter  Component = "filter"
	ComponentProxy   Component = "proxy"
	ComponentControl Component = "control"
)

// Kind is a stable machine-readable event category.
type Kind string

const (
	KindFilterRejected                        Kind = "filter_rejected"
	KindFilterEvaluationError                 Kind = "filter_evaluation_error"
	KindFilterPanic                           Kind = "filter_panic"
	KindFilterInvalidDecision                 Kind = "filter_invalid_decision"
	KindProxyUpstreamUnavailable              Kind = "proxy_upstream_unavailable"
	KindProxyListenerFailed                   Kind = "proxy_listener_failed"
	KindProxyStoppedUnexpectedly              Kind = "proxy_stopped_unexpectedly"
	KindProxyRestoreFailed                    Kind = "proxy_restore_failed"
	KindControlConfigurationRejected          Kind = "control_configuration_rejected"
	KindControlConfigurationPersistenceFailed Kind = "control_configuration_persistence_failed"
)

// Event is sanitized operational metadata. It deliberately contains no
// traffic data, HTTP headers, URLs, or peer addresses.
type Event struct {
	ID        uint64    `json:"id"`
	Time      time.Time `json:"time"`
	Level     Level     `json:"level"`
	Component Component `json:"component"`
	Kind      Kind      `json:"kind"`
	Proxy     string    `json:"proxy,omitempty"`
	Filter    string    `json:"filter,omitempty"`
	Protocol  string    `json:"protocol,omitempty"`
	Direction string    `json:"direction,omitempty"`
	Message   string    `json:"message"`
}

const maxMessageBytes = 256

// SanitizeMessage returns bounded, single-line text suitable for both stderr
// and the dashboard. Callers must still never pass traffic or secrets here.
func SanitizeMessage(message string) string {
	message = strings.ToValidUTF8(message, "?")
	var builder strings.Builder
	for _, r := range message {
		if unicode.IsControl(r) {
			r = ' '
		}
		if builder.Len()+len(string(r)) > maxMessageBytes {
			break
		}
		builder.WriteRune(r)
	}
	return builder.String()
}
