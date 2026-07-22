package observe

import "github.com/lentscode/ctf-proxy/internal/filter"

// FilterSink adapts protocol-neutral filter-chain events to operational events.
func FilterSink(reporter Reporter, proxyName string) filter.EventSink {
	if reporter == nil {
		reporter = NopReporter{}
	}
	return filterSink{reporter: reporter, proxy: proxyName}
}

type filterSink struct {
	reporter Reporter
	proxy    string
}

func (s filterSink) TryReport(event filter.Event) {
	result := Event{Component: ComponentFilter, Proxy: s.proxy, Filter: event.Filter, Protocol: protocol(event.Protocol), Direction: direction(event.Direction)}
	switch event.Kind {
	case filter.EventKindRejected:
		result.Level, result.Kind, result.Message = LevelWarn, KindFilterRejected, "traffic rejected by filter"
	case filter.EventKindEvaluationError:
		result.Level, result.Kind, result.Message = LevelError, KindFilterEvaluationError, "filter evaluation failed; traffic allowed"
	case filter.EventKindPanic:
		result.Level, result.Kind, result.Message = LevelError, KindFilterPanic, "filter panicked; traffic allowed"
	case filter.EventKindInvalidDecision:
		result.Level, result.Kind, result.Message = LevelError, KindFilterInvalidDecision, "filter returned invalid decision; traffic allowed"
	default:
		return
	}
	s.reporter.Report(result)
}

func protocol(value filter.Protocol) string {
	if value == filter.ProtocolTCP {
		return "tcp"
	}
	if value == filter.ProtocolHTTP {
		return "http"
	}
	return ""
}

func direction(value filter.Direction) string {
	if value == filter.DirectionRequest {
		return "request"
	}
	if value == filter.DirectionResponse {
		return "response"
	}
	return ""
}
