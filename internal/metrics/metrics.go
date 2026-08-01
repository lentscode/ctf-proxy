// Package metrics provides bounded, in-memory traffic aggregates.
package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Schedule defines competition-aligned retention. Round zero begins exactly at
// CompetitionStart; traffic before that point is intentionally not recorded.
type Schedule struct {
	CompetitionStart time.Time
	RoundDuration    time.Duration
	RetentionRounds  int
}

// Values are safe aggregate payload counters. No traffic metadata is retained.
// RejectionDenominator and RejectionRatio are derived when results are read,
// rather than incremented by recorders.
type Values struct {
	Requests              uint64  `json:"requests,omitempty"`
	Responses             uint64  `json:"responses,omitempty"`
	ConnectionsAccepted   uint64  `json:"connections_accepted,omitempty"`
	ConnectionsActive     uint64  `json:"connections_active,omitempty"`
	ClientChunks          uint64  `json:"client_chunks,omitempty"`
	ServerChunks          uint64  `json:"server_chunks,omitempty"`
	ClientToUpstreamBytes uint64  `json:"client_to_upstream_bytes"`
	UpstreamToClientBytes uint64  `json:"upstream_to_client_bytes"`
	RejectionsTotal       uint64  `json:"rejections_total"`
	FilterRejections      uint64  `json:"filter_rejections"`
	CapacityRejections    uint64  `json:"capacity_rejections"`
	UpstreamFailures      uint64  `json:"upstream_failures"`
	RejectionDenominator  uint64  `json:"rejection_denominator"`
	RejectionRatio        float64 `json:"rejection_ratio"`
}

// Round is one chronological aggregate.
type Round struct {
	Number   int64     `json:"number"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Metrics  Values    `json:"metrics"`
}

// ProxySummary combines a currently configured proxy's identity with metrics
// from the requested round. A proxy is included even when it has no traffic.
type ProxySummary struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Configured bool   `json:"configured"`
	Metrics    Values `json:"metrics"`
}

type bucket struct {
	number int64
	values map[string]Values
}
type identity struct {
	name, protocol string
	active         atomic.Uint64
}

// Registry is intentionally synchronous and bounded by retention and known
// proxies. It uses a fixed-size ring: a new round overwrites only the bucket
// that has aged past RetentionRounds.
type Registry struct {
	mu             sync.Mutex
	schedule       Schedule
	collectedSince time.Time
	buckets        []bucket
	identities     map[string]*identity
}

// New creates an empty registry for a validated competition schedule.
func New(schedule Schedule) *Registry {
	return &Registry{schedule: schedule, collectedSince: time.Now().UTC(), buckets: make([]bucket, schedule.RetentionRounds), identities: make(map[string]*identity)}
}

// Schedule returns the immutable schedule used to place traffic into rounds.
func (r *Registry) Schedule() Schedule { return r.schedule }

// CollectedSince returns the time at which this in-memory registry was created.
func (r *Registry) CollectedSince() time.Time { return r.collectedSince }

// Register creates or refreshes the identity used by a proxy-bound Recorder.
// Existing recorders remain attached to that identity across a protocol update.
func (r *Registry) Register(name, protocol string) Recorder {
	r.mu.Lock()
	id := r.identities[name]
	if id == nil {
		id = &identity{name: name, protocol: protocol}
		r.identities[name] = id
	} else {
		id.protocol = protocol
	}
	r.mu.Unlock()
	return Recorder{r: r, identity: id}
}

// Has reports whether a metric series currently exists for name.
func (r *Registry) Has(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.identities[name] != nil
}

// Rename moves one proxy's bounded metric series to a new name. Recorders
// obtained before the rename continue to write to the moved series.
func (r *Registry) Rename(oldName, newName, protocol string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.identities[oldName]
	if id == nil {
		return
	}
	if oldName != newName {
		if existing := r.identities[newName]; existing != nil && existing != id {
			return
		}
		delete(r.identities, oldName)
		r.identities[newName] = id
		for index := range r.buckets {
			values := r.buckets[index].values
			if values == nil {
				continue
			}
			if value, exists := values[oldName]; exists {
				delete(values, oldName)
				values[newName] = value
			}
		}
		id.name = newName
	}
	id.protocol = protocol
}

// Remove discards one proxy's bounded metric series. Existing recorders for
// the removed series become no-ops and cannot contaminate a reused name.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.identities, name)
	for index := range r.buckets {
		delete(r.buckets[index].values, name)
	}
}

// current converts a timestamp to its competition round. The boolean prevents
// pre-competition traffic from entering the ring.
func (r *Registry) current(now time.Time) (int64, bool) {
	if now.Before(r.schedule.CompetitionStart) {
		return 0, false
	}
	return int64(now.Sub(r.schedule.CompetitionStart) / r.schedule.RoundDuration), true
}

// update applies one recorder mutation to the current bucket. Identity is
// checked while locked so a stale recorder cannot recreate a removed series.
func (r *Registry) update(id *identity, fn func(*Values)) {
	if id == nil {
		return
	}
	n, ok := r.current(time.Now().UTC())
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	name := id.name
	if r.identities[name] != id {
		return
	}
	index := int(n % int64(len(r.buckets)))
	b := &r.buckets[index]
	if b.number != n || b.values == nil {
		b.number = n
		b.values = make(map[string]Values)
	}
	v := b.values[name]
	fn(&v)
	b.values[name] = v
}

// Current returns the active round and summaries for every configured proxy.
// It returns false before the configured competition start.
func (r *Registry) Current() (Round, []ProxySummary, bool) {
	n, ok := r.current(time.Now().UTC())
	if !ok {
		return Round{}, []ProxySummary{}, false
	}
	return r.round(n), r.summaries(n), true
}
func (r *Registry) summaries(n int64) []ProxySummary {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.bucketLocked(n)
	out := make([]ProxySummary, 0, len(r.identities))
	for name, id := range r.identities {
		v := Values{}
		if b != nil {
			v = b.values[name]
		}
		v.ConnectionsActive = id.active.Load()
		finalize(&v, id.protocol)
		out = append(out, ProxySummary{Name: name, Protocol: id.protocol, Configured: true, Metrics: v})
	}
	return out
}
func (r *Registry) bucketLocked(n int64) *bucket {
	b := &r.buckets[int(n%int64(len(r.buckets)))]
	if b.number != n {
		return nil
	}
	return b
}
func (r *Registry) round(n int64) Round {
	s := r.schedule.CompetitionStart.Add(time.Duration(n) * r.schedule.RoundDuration)
	return Round{Number: n, StartsAt: s, EndsAt: s.Add(r.schedule.RoundDuration)}
}

// Rounds returns up to limit chronological rounds for name, including empty
// rounds inside the retained window. The boolean is false when name is unknown.
func (r *Registry) Rounds(name string, limit int) ([]Round, bool) {
	n, ok := r.current(time.Now().UTC())
	if limit > r.schedule.RetentionRounds {
		limit = r.schedule.RetentionRounds
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.identities[name]
	if id == nil {
		return nil, false
	}
	if !ok {
		return []Round{}, true
	}
	start := n - int64(limit) + 1
	if start < 0 {
		start = 0
	}
	out := make([]Round, 0, n-start+1)
	for i := start; i <= n; i++ {
		item := r.round(i)
		if b := r.bucketLocked(i); b != nil {
			item.Metrics = b.values[name]
		}
		item.Metrics.ConnectionsActive = id.active.Load()
		finalize(&item.Metrics, id.protocol)
		out = append(out, item)
	}
	return out, true
}

// finalize derives the rejection rate using the unit the protocol admits:
// TCP connections (including capacity rejections) or HTTP requests.
func finalize(v *Values, protocol string) {
	if protocol == "tcp" {
		v.RejectionDenominator = v.ConnectionsAccepted + v.CapacityRejections
	} else {
		v.RejectionDenominator = v.Requests
	}
	if v.RejectionDenominator > 0 {
		v.RejectionRatio = float64(v.RejectionsTotal) / float64(v.RejectionDenominator)
	}
}

// Recorder is a proxy-bound metrics sink.
type Recorder struct {
	r        *Registry
	identity *identity
}

func (x Recorder) add(fn func(*Values)) {
	if x.r != nil {
		x.r.update(x.identity, fn)
	}
}

// Request records an HTTP request admitted to its proxy handler.
func (x Recorder) Request() { x.add(func(v *Values) { v.Requests++ }) }

// Response records an HTTP response emitted by its proxy handler.
func (x Recorder) Response() { x.add(func(v *Values) { v.Responses++ }) }

// AcceptedConnection records a TCP connection and tracks it as active.
func (x Recorder) AcceptedConnection() {
	x.add(func(v *Values) { v.ConnectionsAccepted++ })
	if x.identity != nil {
		x.identity.active.Add(1)
	}
}

// ClosedConnection removes one TCP connection from the active total.
func (x Recorder) ClosedConnection() {
	if x.identity != nil {
		x.identity.active.Add(^uint64(0))
	}
}

// Chunk records one successfully read TCP chunk in the requested direction.
func (x Recorder) Chunk(server bool) {
	x.add(func(v *Values) {
		if server {
			v.ServerChunks++
		} else {
			v.ClientChunks++
		}
	})
}

// Bytes records forwarded traffic, ignoring non-positive write counts.
func (x Recorder) Bytes(server bool, n int) {
	if n <= 0 {
		return
	}
	x.add(func(v *Values) {
		if server {
			v.UpstreamToClientBytes += uint64(n)
		} else {
			v.ClientToUpstreamBytes += uint64(n)
		}
	})
}

// Reject records a capacity or filter rejection for the current round.
func (x Recorder) Reject(capacity bool) {
	x.add(func(v *Values) {
		v.RejectionsTotal++
		if capacity {
			v.CapacityRejections++
		} else {
			v.FilterRejections++
		}
	})
}

// UpstreamFailure records a failed attempt to reach or use the upstream.
func (x Recorder) UpstreamFailure() { x.add(func(v *Values) { v.UpstreamFailures++ }) }
