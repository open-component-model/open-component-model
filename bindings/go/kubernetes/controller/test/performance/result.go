package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

type result struct {
	Scenario    scenario    `json:"scenario"`
	Objects     int         `json:"objects"`
	Repeat      int         `json:"repeat"`
	RunID       string      `json:"runID"`
	StartedAt   time.Time   `json:"startedAt"`
	Environment environment `json:"environment"`
	Durations   durations   `json:"durations"`
	Outcome     outcome     `json:"outcome"`
	Usage       usage       `json:"usage"`
	// ControllerCounters are end-minus-start deltas over the measurement window.
	ControllerCounters map[string]float64 `json:"controllerCounters"`
	// ControllerGaugePeaks are the maxima of the sampled gauges.
	ControllerGaugePeaks map[string]float64 `json:"controllerGaugePeaks"`
	RegistryCounters     map[string]float64 `json:"registryCounters"`
	// ControllerRestarted invalidates the counter deltas: a restart resets them.
	ControllerRestarted bool     `json:"controllerRestarted"`
	Errors              []string `json:"errors,omitempty"`
}

type environment struct {
	ControllerPod       string                      `json:"controllerPod"`
	ControllerImage     string                      `json:"controllerImage"`
	ControllerImageID   string                      `json:"controllerImageID"`
	ControllerArgs      []string                    `json:"controllerArgs"`
	ControllerResources corev1.ResourceRequirements `json:"controllerResources"`
	Node                string                      `json:"node"`
	KubernetesVersion   string                      `json:"kubernetesVersion"`
}

// durations are in seconds, measured from the moment object creation starts.
type durations struct {
	Publish    float64 `json:"publish"`
	Create     float64 `json:"create"`
	Total      float64 `json:"total"`
	FirstReady float64 `json:"firstReady"`
	P50        float64 `json:"p50"`
	P90        float64 `json:"p90"`
	P99        float64 `json:"p99"`
}

type outcome struct {
	Expected       int            `json:"expected"`
	Ready          int            `json:"ready"`
	Failed         int            `json:"failed"`
	Pending        int            `json:"pending"`
	TimedOut       bool           `json:"timedOut"`
	FailureReasons map[string]int `json:"failureReasons,omitempty"`
}

type usage struct {
	CPUSeconds          float64 `json:"cpuSeconds"`
	AvgCores            float64 `json:"avgCores"`
	PeakWorkingSetBytes float64 `json:"peakWorkingSetBytes"`
	PeakRSSBytes        float64 `json:"peakRSSBytes"`
	// RegistryRequests is nil when the registry metrics could not be read.
	RegistryRequests *float64 `json:"registryRequests"`
}

// notes lists what makes a run unusable as a benchmark number.
func (r *result) notes() []string {
	var notes []string
	if r.Outcome.TimedOut {
		notes = append(notes, "timed out")
	}
	if r.ControllerRestarted {
		notes = append(notes, "controller restarted")
	}
	if len(r.Errors) > 0 {
		notes = append(notes, "metric errors")
	}
	return notes
}

// percentile uses the nearest-rank method on sorted latencies.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[min(max(rank, 1), len(sorted))-1]
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// writeSummary renders one table row per run, for a quick read of an
// invocation. result.json files remain the source of truth.
func writeSummary(dir string, results []*result) error {
	var b strings.Builder
	b.WriteString("| Scenario | Objects | Repeat | Ready | Failed | Pending | Total (s) | p50 (s) | p90 (s) | CPU (s) | Peak WS (MiB) | Registry reqs | Notes |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		requests := "n/a"
		if r.Usage.RegistryRequests != nil {
			requests = fmt.Sprintf("%.0f", *r.Usage.RegistryRequests)
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %.1f | %.1f | %.1f | %.1f | %.0f | %s | %s |\n",
			r.Scenario, r.Objects, r.Repeat, r.Outcome.Ready, r.Outcome.Failed, r.Outcome.Pending,
			r.Durations.Total, r.Durations.P50, r.Durations.P90, r.Usage.CPUSeconds,
			r.Usage.PeakWorkingSetBytes/(1<<20), requests, strings.Join(r.notes(), ", "))
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(b.String()), 0o600)
}
