package main

import (
	"errors"
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
				r.Equal(allScenarios, o.scenarios)
				r.Equal([]int{100, 500, 1000}, o.sizes)
				r.Equal(3, o.repeats)
				r.Equal("kind-ocm-perf", o.kubeContext)
				r.True(o.restartController)
				r.Equal(defaultVersions, o.versions)
				r.Equal(defaultDepth, o.depth)
				r.Equal(defaultResources, o.resources)
				r.Equal(defaultFanout, o.fanout)
				r.Equal(defaultRepositories, o.repositories)
				r.True(o.sign)
			},
		},
		{
			name: "subset",
			args: []string{"--scenarios=cold, update", "--objects=10", "--repeats=1", "--timeout=1m"},
			want: func(r *require.Assertions, o options) {
				r.Equal([]scenario{scenarioCold, scenarioUpdate}, o.scenarios)
				r.Equal([]int{10}, o.sizes)
				r.Equal(1, o.repeats)
				r.Equal(time.Minute, o.timeout)
			},
		},
		{name: "unknown scenario", args: []string{"--scenarios=warm"}, wantErr: `unknown scenario "warm"`},
		{name: "invalid size", args: []string{"--objects=100,0"}, wantErr: `invalid object count "0"`},
		{name: "invalid repeats", args: []string{"--repeats=0"}, wantErr: "repeats must be at least 1"},
		{name: "depth 0 is allowed", args: []string{"--depth=0"}, want: func(r *require.Assertions, o options) { r.Zero(o.depth) }},
		{name: "invalid depth", args: []string{"--depth=-1"}, wantErr: "depth must not be negative"},
		{name: "invalid fanout", args: []string{"--fanout=0"}, wantErr: "versions, resources, fanout and repositories must be at least 1"},
		{name: "invalid repositories", args: []string{"--repositories=0"}, wantErr: "versions, resources, fanout and repositories must be at least 1"},
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

func TestSummarizeRegistryUnavailable(t *testing.T) {
	r := require.New(t)
	res := &result{}
	after := scrapes{registryErr: errors.New("connection refused")}

	err := (&runner{}).summarize(res, scrapes{}, after, nil, time.Second)

	r.ErrorContains(err, "registry metrics unavailable: connection refused")
	r.Nil(res.Usage.RegistryRequests)
}

func TestWriteSummary(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	requests := 42.0
	results := []*result{
		{
			Scenario: scenarioComplex, Signed: true, Usage: usage{RegistryRequests: &requests},
			Environment: environment{ControllerImageID: "docker.io/library/import-2026-10-26@sha256:18bb5d7f6bba0123456789"},
		},
		{Scenario: scenarioCold, Outcome: outcome{TimedOut: true}, ControllerRestarted: true},
	}

	r.NoError(writeSummary(dir, results))

	raw, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	r.NoError(err)
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	r.Len(rows, 4)
	r.Contains(rows[2], "| complex | 0 | 0 | 18bb5d7f6bba | yes |")
	r.Contains(rows[2], "| 42 |  |")
	r.Contains(rows[3], "| cold | 0 | 0 | n/a | no |")
	r.Contains(rows[3], "| n/a | timed out, controller restarted |")
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
