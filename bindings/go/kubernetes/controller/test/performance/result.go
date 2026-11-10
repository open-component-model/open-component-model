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
	Objects      int         `json:"objects"`
	Repeat       int         `json:"repeat"`
	Versions     int         `json:"versions"`
	Depth        int         `json:"depth"`
	Resources    int         `json:"resources"`
	Repositories int         `json:"repositories"`
	Signed       bool        `json:"signed"`
	RunID        string      `json:"runID"`
	StartedAt    time.Time   `json:"startedAt"`
	Environment  environment `json:"environment"`
	// window is the measured rollout, inlined at the top level.
	window
	// Drift is the window of the drift phase: every deployed ConfigMap changed,
	// until every Deployer reverted its own. It is nil when the rollout did not finish.
	Drift *window `json:"drift,omitempty"`
}

// window holds the numbers of one measured phase of a run.
type window struct {
	Durations durations `json:"durations"`
	Outcome   outcome   `json:"outcome"`
	Usage     usage     `json:"usage"`
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

// durations are in seconds, measured from the moment the window's change starts:
// object creation, or the patches of the drift.
type durations struct {
	// Publish is only set for the rollout.
	Publish float64 `json:"publish,omitempty"`
	// Create is how long applying the change took.
	Create     float64 `json:"create"`
	Total      float64 `json:"total"`
	FirstReady float64 `json:"firstReady"`
	P50        float64 `json:"p50"`
	P90        float64 `json:"p90"`
	P99        float64 `json:"p99"`
	// Settled is when the controller had no queued or running reconciles left.
	Settled float64 `json:"settled"`
}

type outcome struct {
	Expected       int            `json:"expected"`
	Ready          int            `json:"ready"`
	Failed         int            `json:"failed"`
	Pending        int            `json:"pending"`
	TimedOut       bool           `json:"timedOut"`
	FailureReasons map[string]int `json:"failureReasons,omitempty"`
	// Unsettled is set when the controller queues did not drain within the timeout.
	Unsettled bool `json:"unsettled"`
}

type usage struct {
	CPUSeconds          float64 `json:"cpuSeconds"`
	AvgCores            float64 `json:"avgCores"`
	PeakCores           float64 `json:"peakCores"`
	PeakWorkingSetBytes float64 `json:"peakWorkingSetBytes"`
	PeakRSSBytes        float64 `json:"peakRSSBytes"`
	// RegistryRequests is nil when the registry metrics could not be read.
	RegistryRequests *float64 `json:"registryRequests"`
}

// notes lists what makes a run unusable as a benchmark number, its drift phase included.
func (r *result) notes() []string {
	notes := r.window.notes()
	if r.Drift != nil {
		for _, n := range r.Drift.notes() {
			notes = append(notes, "drift "+n)
		}
	}
	return notes
}

// notes lists what makes a window unusable as a benchmark number.
func (w *window) notes() []string {
	var notes []string
	if w.Outcome.TimedOut {
		notes = append(notes, "timed out")
	}
	if w.Outcome.Unsettled {
		notes = append(notes, "queues not drained")
	}
	if w.ControllerRestarted {
		notes = append(notes, "controller restarted")
	}
	if len(w.Errors) > 0 {
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
	b.WriteString("| Phase | Objects | Repeat | Image | Signed | Ready | Failed | Pending | Total (s) | Settled (s) | p50 (s) | p90 (s) | CPU (s) | Avg cores | Peak cores | Peak WS (MiB) | Registry reqs | Notes |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		writeSummaryRow(&b, r, "rollout", &r.window)
		if r.Drift != nil {
			writeSummaryRow(&b, r, "drift", r.Drift)
		}
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(b.String()), 0o600)
}

func writeSummaryRow(b *strings.Builder, r *result, name string, w *window) {
	requests := "n/a"
	if w.Usage.RegistryRequests != nil {
		requests = fmt.Sprintf("%.0f", *w.Usage.RegistryRequests)
	}
	signed := "no"
	if r.Signed {
		signed = "yes"
	}
	fmt.Fprintf(b, "| %s | %d | %d | %s | %s | %d | %d | %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.2f | %.2f | %.0f | %s | %s |\n",
		name, r.Objects, r.Repeat, shortImageID(r.Environment.ControllerImageID), signed, w.Outcome.Ready, w.Outcome.Failed, w.Outcome.Pending,
		w.Durations.Total, w.Durations.Settled, w.Durations.P50, w.Durations.P90, w.Usage.CPUSeconds, w.Usage.AvgCores, w.Usage.PeakCores,
		w.Usage.PeakWorkingSetBytes/(1<<20), requests, strings.Join(w.notes(), ", "))
}

// shortImageID keeps the first 12 hex digits of the digest, as docker does.
// Runs are only comparable on the same image, and the tag alone does not tell.
func shortImageID(id string) string {
	if i := strings.LastIndex(id, "sha256:"); i >= 0 {
		id = id[i+len("sha256:"):]
	}
	if len(id) > 12 {
		id = id[:12]
	}
	if id == "" {
		return "n/a"
	}
	return id
}
