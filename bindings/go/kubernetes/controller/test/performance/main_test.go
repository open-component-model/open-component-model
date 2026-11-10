package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    func(*require.Assertions, options)
		wantErr string
	}{
		{
			name: "defaults run the full matrix",
			want: func(r *require.Assertions, o options) {
				r.Equal([]int{100, 500, 1000}, o.sizes)
				r.Equal(3, o.repeats)
				r.Equal("kind-ocm-perf", o.kubeContext)
				r.True(o.restartController)
				r.Equal(defaultVersions, o.versions)
				r.Equal(defaultDepth, o.depth)
				r.Equal(defaultResources, o.resources)
				r.Equal(defaultRepositories, o.repositories)
				r.True(o.sign)
			},
		},
		{
			name: "subset",
			args: []string{"--objects=10, 20", "--repeats=1", "--timeout=1m"},
			want: func(r *require.Assertions, o options) {
				r.Equal([]int{10, 20}, o.sizes)
				r.Equal(1, o.repeats)
				r.Equal(time.Minute, o.timeout)
			},
		},
		{name: "scenarios are gone", args: []string{"--scenarios=complex"}, wantErr: "flag provided but not defined: -scenarios"},
		{name: "invalid size", args: []string{"--objects=100,0"}, wantErr: `invalid object count "0"`},
		{name: "invalid repeats", args: []string{"--repeats=0"}, wantErr: "repeats must be at least 1"},
		{name: "depth 0 is allowed", args: []string{"--depth=0"}, want: func(r *require.Assertions, o options) { r.Zero(o.depth) }},
		{name: "invalid depth", args: []string{"--depth=-1"}, wantErr: "depth must not be negative"},
		{name: "invalid resources", args: []string{"--resources=0"}, wantErr: "versions, resources and repositories must be at least 1"},
		{name: "invalid repositories", args: []string{"--repositories=0"}, wantErr: "versions, resources and repositories must be at least 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			o, err := parseFlags(tt.args)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			tt.want(r, o)
		})
	}
}

func TestWriteSummary(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	requests, driftRequests := 42.0, 7.0
	results := []*result{
		{
			Signed:      true,
			Environment: environment{ControllerImageID: "docker.io/library/import-2026-10-26@sha256:18bb5d7f6bba0123456789"},
			Phases: []phaseResult{
				{Name: "rollout", Cost: cost{RegistryRequests: &requests}},
				{Name: "drift", Outcome: outcome{Ready: 3}, Cost: cost{RegistryRequests: &driftRequests}, Errors: []string{"boom"}},
			},
		},
		{Phases: []phaseResult{{Name: "rollout", Outcome: outcome{TimedOut: true, ControllerRestarted: true}}}},
	}

	r.NoError(writeSummary(dir, results))

	raw, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	r.NoError(err)
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	r.Len(rows, 2+3, "one row per phase; a run whose rollout did not complete has one")
	r.Contains(rows[0], "| Phase |")
	r.Contains(rows[2], "| rollout | 0 | 0 | 18bb5d7f6bba | yes |")
	r.Contains(rows[2], "| 42 |  |")
	r.Contains(rows[3], "| drift | 0 | 0 | 18bb5d7f6bba | yes | 3 |")
	r.Contains(rows[3], "| 7 | drift metric errors |")
	r.Contains(rows[4], "| rollout | 0 | 0 | n/a | no |")
	r.Contains(rows[4], "| n/a | rollout timed out, rollout controller restarted |")
}

func TestResultJSON(t *testing.T) {
	r := require.New(t)
	requests := 5.0
	res := &result{Phases: []phaseResult{
		{
			Name:      "rollout",
			Outcome:   outcome{ControllerRestarted: true},
			Durations: durations{Prepare: 70, Change: 3, Total: 10},
			Cost:      cost{ControllerCounters: map[string]float64{"a": 1}},
		},
		{
			Name:      "drift",
			Outcome:   outcome{NotReady: map[string]int{"NamespacedDeployer/unknown": 1}},
			Durations: durations{Total: 2},
			Cost:      cost{RegistryRequests: &requests, RegistryCounters: map[string]float64{"b": 4}},
		},
	}}

	raw, err := json.Marshal(res)
	r.NoError(err)
	var got map[string]any
	r.NoError(json.Unmarshal(raw, &got))
	for _, key := range []string{"durations", "outcome", "usage", "controllerCounters", "scenario", "drift"} {
		r.NotContains(got, key, "every phase lives in phases")
	}
	for _, key := range []string{"versions", "depth", "resources", "repositories", "signed"} {
		r.Contains(got, key, "the workload knobs are always written, 0 and false included")
	}

	phases := got["phases"].([]any)
	r.Len(phases, 2)
	rollout, drift := phases[0].(map[string]any), phases[1].(map[string]any)
	r.Equal("rollout", rollout["name"])
	r.Equal("drift", drift["name"], "in execution order")
	r.ElementsMatch([]string{"name", "outcome", "durations", "cost"}, keys(rollout))

	r.Equal(true, rollout["outcome"].(map[string]any)["controllerRestarted"])
	r.NotContains(rollout["outcome"], "notReady", "only written when a phase timed out")
	r.Equal(map[string]any{"NamespacedDeployer/unknown": 1.0}, drift["outcome"].(map[string]any)["notReady"])
	r.InDelta(70, rollout["durations"].(map[string]any)["prepare"], 0)
	r.InDelta(3, rollout["durations"].(map[string]any)["change"], 0)
	r.InDelta(0, drift["durations"].(map[string]any)["prepare"], 0, "written for every phase")
	r.InDelta(5, drift["cost"].(map[string]any)["registryRequests"], 0)
	r.Contains(drift["cost"], "registryCounters")
	r.Contains(rollout["cost"], "controllerCounters")
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPercentile(t *testing.T) {
	sorted := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	tests := []struct {
		p    float64
		want time.Duration
	}{
		{p: 0, want: 1},
		{p: 50, want: 5},
		{p: 90, want: 9},
		{p: 99, want: 10},
		{p: 100, want: 10},
	}
	for _, tt := range tests {
		r := require.New(t)
		r.Equal(tt.want, percentile(sorted, tt.p), "p%v", tt.p)
	}
	require.Zero(t, percentile(nil, 50))
}

func TestSettledAt(t *testing.T) {
	busy := sample{Gauges: map[string]float64{`workqueue_depth{controller="deployer"}`: 3}}
	working := sample{Gauges: map[string]float64{`controller_runtime_active_workers{controller="deployer"}`: 1}}
	idle := func(at float64) sample {
		return sample{At: at, Gauges: map[string]float64{`workqueue_depth{controller="deployer"}`: 0, "go_goroutines": 250}}
	}
	tests := []struct {
		name    string
		samples []sample
		after   float64
		at      float64
		settled bool
	}{
		{name: "too few samples", samples: []sample{idle(0), idle(2)}},
		{name: "queue not empty", samples: []sample{idle(0), idle(2), busy}},
		{name: "worker active", samples: []sample{idle(0), working, idle(4)}},
		{name: "failed scrape is not idle", samples: []sample{idle(0), {At: 2}, idle(4)}},
		{name: "idle before the objects were ready", samples: []sample{idle(0), idle(2), idle(4)}, after: 1},
		{name: "settled", samples: []sample{busy, idle(2), idle(4), idle(6)}, at: 2, settled: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			at, settled := settledAt(tc.samples, tc.after)
			r.Equal(tc.settled, settled)
			r.InDelta(tc.at, at, 0)
		})
	}
}

func TestResultNotesNameTheirPhase(t *testing.T) {
	r := require.New(t)
	res := &result{Phases: []phaseResult{{Name: "rollout"}, {Name: "drift", Outcome: outcome{Unsettled: true}}}}
	r.Equal([]string{"drift queues not drained"}, res.notes(), "a phase that did not complete makes the run unusable")
}
