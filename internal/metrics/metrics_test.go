package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegistryAggregatesByProxyAndComputesRatios(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		record func(Recorder)
		want   Values
	}{
		{name: "HTTP records requests responses bytes and filter rejections", record: func(recorder Recorder) {
			recorder.Request()
			recorder.Response()
			recorder.Bytes(false, 12)
			recorder.Bytes(true, 24)
			recorder.Reject(false)
		}, want: Values{Requests: 1, Responses: 1, ClientToUpstreamBytes: 12, UpstreamToClientBytes: 24, RejectionsTotal: 1, FilterRejections: 1, RejectionDenominator: 1, RejectionRatio: 1}},
		{name: "TCP records connections chunks and capacity rejections", record: func(recorder Recorder) {
			recorder.AcceptedConnection()
			recorder.Chunk(false)
			recorder.Chunk(true)
			recorder.Bytes(false, 3)
			recorder.Bytes(true, 5)
			recorder.Reject(true)
			recorder.ClosedConnection()
		}, want: Values{ConnectionsAccepted: 1, ClientChunks: 1, ServerChunks: 1, ClientToUpstreamBytes: 3, UpstreamToClientBytes: 5, RejectionsTotal: 1, CapacityRejections: 1, RejectionDenominator: 2, RejectionRatio: .5}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			registry := New(Schedule{CompetitionStart: time.Now().UTC().Add(-time.Minute), RoundDuration: time.Minute, RetentionRounds: 3})
			protocol := "http"
			if testCase.want.ConnectionsAccepted > 0 {
				protocol = "tcp"
			}
			recorder := registry.Register("service", protocol)
			testCase.record(recorder)
			_, summaries, current := registry.Current()
			require.True(t, current)
			require.Len(t, summaries, 1)
			actual := summaries[0].Metrics
			actual.ConnectionsActive = 0 // Lifecycle is covered separately from round aggregates.
			require.Equal(t, testCase.want, actual)
		})
	}
}

func TestRegistryDropsTrafficBeforeCompetitionStart(t *testing.T) {
	registry := New(Schedule{CompetitionStart: time.Now().UTC().Add(time.Hour), RoundDuration: time.Minute, RetentionRounds: 1})
	registry.Register("web", "http").Request()
	_, _, current := registry.Current()
	require.False(t, current)
}

func TestFinalizeUsesProtocolSpecificRejectionDenominators(t *testing.T) {
	for _, testCase := range []struct {
		name, protocol  string
		values          Values
		wantDenominator uint64
		wantRatio       float64
	}{
		{name: "HTTP uses all requests", protocol: "http", values: Values{Requests: 4, RejectionsTotal: 1}, wantDenominator: 4, wantRatio: .25},
		{name: "TCP includes capacity rejections", protocol: "tcp", values: Values{ConnectionsAccepted: 3, CapacityRejections: 2, RejectionsTotal: 2}, wantDenominator: 5, wantRatio: .4},
		{name: "zero denominator remains zero", protocol: "http", values: Values{RejectionsTotal: 1}, wantDenominator: 0, wantRatio: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			values := testCase.values
			finalize(&values, testCase.protocol)
			require.Equal(t, testCase.wantDenominator, values.RejectionDenominator)
			require.Equal(t, testCase.wantRatio, values.RejectionRatio)
		})
	}
}

func TestRegistryCurrentHandlesCompetitionBoundaries(t *testing.T) {
	start := time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC)
	registry := New(Schedule{CompetitionStart: start, RoundDuration: 2 * time.Minute, RetentionRounds: 2})
	for _, testCase := range []struct {
		name string
		now  time.Time
		want int64
		ok   bool
	}{
		{name: "before start", now: start.Add(-time.Nanosecond), ok: false},
		{name: "at start", now: start, want: 0, ok: true},
		{name: "inside first round", now: start.Add(time.Minute), want: 0, ok: true},
		{name: "at next boundary", now: start.Add(2 * time.Minute), want: 1, ok: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := registry.current(testCase.now)
			require.Equal(t, testCase.ok, ok)
			if ok {
				require.Equal(t, testCase.want, got)
			}
		})
	}
}
