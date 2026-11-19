package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewOutcomeAndDurations(t *testing.T) {
	latencies := []time.Duration{2 * time.Second, 4 * time.Second, 9 * time.Second}
	const prepare, change, elapsed = 70 * time.Second, 3 * time.Second, 12 * time.Second

	tests := []struct {
		name                     string
		sum                      trackerSummary
		done, drained, restarted bool
		wantComplete             bool
		wantOutcome              func(*require.Assertions, outcome)
		wantTotal, wantSettled   float64
	}{
		{
			name: "done and drained", sum: trackerSummary{ready: 3, latencies: latencies},
			done: true, drained: true, wantComplete: true,
			wantTotal: 9, wantSettled: 12,
			wantOutcome: func(r *require.Assertions, o outcome) {
				r.Equal(3, o.Ready)
				r.False(o.TimedOut)
				r.False(o.Unsettled)
			},
		},
		{
			name: "timed out",
			sum: trackerSummary{
				ready: 1, failed: 1, pending: 1, latencies: latencies[:1],
				failureReasons: map[string]int{"Broken": 1},
			},
			wantTotal: 12, wantSettled: 0,
			wantOutcome: func(r *require.Assertions, o outcome) {
				r.True(o.TimedOut)
				r.False(o.Unsettled, "the queues are only waited for once every pipeline is done")
				r.Equal(1, o.Pending)
				r.Equal(o.Expected-o.Ready-o.Failed, o.Pending, "the tracker counts the unseen as pending")
				r.Equal(map[string]int{"Broken": 1}, o.FailureReasons)
			},
		},
		{
			name: "done but not drained", sum: trackerSummary{ready: 3, latencies: latencies},
			done:      true,
			wantTotal: 9, wantSettled: 0,
			wantOutcome: func(r *require.Assertions, o outcome) {
				r.True(o.Unsettled)
				r.False(o.TimedOut)
			},
		},
		{
			name: "controller restarted", sum: trackerSummary{ready: 3, latencies: latencies},
			done: true, drained: true, restarted: true,
			wantTotal: 9, wantSettled: 12,
			wantOutcome: func(r *require.Assertions, o outcome) { r.True(o.ControllerRestarted) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			o := newOutcome(tt.sum, 3, tt.done, tt.drained, tt.restarted)
			r.Equal(3, o.Expected)
			r.Equal(tt.wantComplete, o.complete())
			tt.wantOutcome(r, o)

			d := newDurations(tt.sum, prepare, change, elapsed, tt.done, tt.drained)
			r.InDelta(70, d.Prepare, 0)
			r.InDelta(3, d.Change, 0)
			r.InDelta(tt.wantTotal, d.Total, 0)
			r.InDelta(tt.wantSettled, d.Settled, 0)
			r.InDelta(2, d.FirstReady, 0)
			r.InDelta(percentile(tt.sum.latencies, 50).Seconds(), d.P50, 0)
			r.InDelta(percentile(tt.sum.latencies, 90).Seconds(), d.P90, 0)
			r.InDelta(percentile(tt.sum.latencies, 99).Seconds(), d.P99, 0)
		})
	}
}

func TestNewDurationsWithoutLatencies(t *testing.T) {
	r := require.New(t)
	d := newDurations(trackerSummary{}, 0, time.Second, 5*time.Second, false, false)
	r.InDelta(5, d.Total, 0, "nothing done: the total is the whole window")
	r.Zero(d.FirstReady)
	r.Zero(d.P50)
}
