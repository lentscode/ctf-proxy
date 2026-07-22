package observe

// Reporter receives sanitized operational events. Report implementations must
// return promptly and must never be allowed to interrupt proxied traffic.
type Reporter interface {
	Report(Event)
}

// NopReporter discards every event.
type NopReporter struct{}

func (NopReporter) Report(Event) {}

// FanoutReporter sends an event to each child. A broken observer is isolated
// from the remaining observers and the data plane.
type FanoutReporter struct {
	reporters []Reporter
}

// NewFanoutReporter constructs a reporter that invokes every non-nil child.
func NewFanoutReporter(reporters ...Reporter) *FanoutReporter {
	filtered := make([]Reporter, 0, len(reporters))
	for _, reporter := range reporters {
		if reporter != nil {
			filtered = append(filtered, reporter)
		}
	}
	return &FanoutReporter{reporters: filtered}
}

func (r *FanoutReporter) Report(event Event) {
	if r == nil {
		return
	}
	for _, reporter := range r.reporters {
		func() {
			defer func() { _ = recover() }()
			reporter.Report(event)
		}()
	}
}

// WithProxy adds proxyName to events that do not already identify a proxy.
func WithProxy(reporter Reporter, proxyName string) Reporter {
	if reporter == nil {
		reporter = NopReporter{}
	}
	return proxyReporter{reporter: reporter, proxy: proxyName}
}

type proxyReporter struct {
	reporter Reporter
	proxy    string
}

func (r proxyReporter) Report(event Event) {
	if event.Proxy == "" {
		event.Proxy = r.proxy
	}
	r.reporter.Report(event)
}
