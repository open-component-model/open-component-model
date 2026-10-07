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
		{name: "invalid depth", args: []string{"--depth=0"}, wantErr: "versions, depth and resources must be at least 1"},
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
		{Scenario: scenarioCold, Usage: usage{RegistryRequests: &requests}},
		{Scenario: scenarioCold, Outcome: outcome{TimedOut: true}, ControllerRestarted: true},
	}

	r.NoError(writeSummary(dir, results))

	raw, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	r.NoError(err)
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	r.Len(rows, 4)
	r.Contains(rows[2], "| 42 |  |")
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
