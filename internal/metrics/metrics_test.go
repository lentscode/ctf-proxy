package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegistryAggregatesByProxyAndComputesRatios(t *testing.T) {
	registry := New(Schedule{CompetitionStart: time.Now().UTC().Add(-time.Minute), RoundDuration: time.Minute, RetentionRounds: 3})
	web := registry.Register("web", "http")
	tcp := registry.Register("tcp", "tcp")
	web.Request()
	web.Response()
	web.Bytes(false, 12)
	web.Bytes(true, 24)
	web.Reject(false)
	tcp.AcceptedConnection()
	tcp.Chunk(false)
	tcp.Reject(true)

	_, summaries, current := registry.Current()
	require.True(t, current)
	values := map[string]Values{}
	for _, summary := range summaries {
		values[summary.Name] = summary.Metrics
	}
	require.Equal(t, uint64(1), values["web"].Requests)
	require.Equal(t, uint64(12), values["web"].ClientToUpstreamBytes)
	require.Equal(t, 1.0, values["web"].RejectionRatio)
	require.Equal(t, uint64(1), values["tcp"].ConnectionsAccepted)
	require.Equal(t, uint64(1), values["tcp"].CapacityRejections)
	require.Equal(t, 0.5, values["tcp"].RejectionRatio)
}

func TestRegistryDropsTrafficBeforeCompetitionStart(t *testing.T) {
	registry := New(Schedule{CompetitionStart: time.Now().UTC().Add(time.Hour), RoundDuration: time.Minute, RetentionRounds: 1})
	registry.Register("web", "http").Request()
	_, _, current := registry.Current()
	require.False(t, current)
}
