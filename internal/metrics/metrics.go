// Package metrics provides bounded, in-memory traffic aggregates.
package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Schedule defines competition-aligned retention.
type Schedule struct {
	CompetitionStart time.Time
	RoundDuration    time.Duration
	RetentionRounds  int
}

// Values are safe aggregate payload counters. No traffic metadata is retained.
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

// Registry is intentionally synchronous and bounded by retention and known proxies.
type Registry struct {
	mu             sync.Mutex
	schedule       Schedule
	collectedSince time.Time
	buckets        []bucket
	identities     map[string]*identity
}

func New(schedule Schedule) *Registry {
	return &Registry{schedule: schedule, collectedSince: time.Now().UTC(), buckets: make([]bucket, schedule.RetentionRounds), identities: make(map[string]*identity)}
}
func (r *Registry) Schedule() Schedule        { return r.schedule }
func (r *Registry) CollectedSince() time.Time { return r.collectedSince }
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
func (r *Registry) current(now time.Time) (int64, bool) {
	if now.Before(r.schedule.CompetitionStart) {
		return 0, false
	}
	return int64(now.Sub(r.schedule.CompetitionStart) / r.schedule.RoundDuration), true
}
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
func (x Recorder) Request()  { x.add(func(v *Values) { v.Requests++ }) }
func (x Recorder) Response() { x.add(func(v *Values) { v.Responses++ }) }
func (x Recorder) AcceptedConnection() {
	x.add(func(v *Values) { v.ConnectionsAccepted++ })
	if x.identity != nil {
		x.identity.active.Add(1)
	}
}
func (x Recorder) ClosedConnection() {
	if x.identity != nil {
		x.identity.active.Add(^uint64(0))
	}
}
func (x Recorder) Chunk(server bool) {
	x.add(func(v *Values) {
		if server {
			v.ServerChunks++
		} else {
			v.ClientChunks++
		}
	})
}
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
func (x Recorder) UpstreamFailure() { x.add(func(v *Values) { v.UpstreamFailures++ }) }
