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
	// Phases are the measured phases in execution order. A phase that did not
	// complete is the last one: the phases after it are not measured.
	Phases []phaseResult `json:"phases"`
}

// phaseResult holds the numbers of one measured phase of a run.
type phaseResult struct {
	Name      string    `json:"name"`
	Outcome   outcome   `json:"outcome"`
	Durations durations `json:"durations"`
	Cost      cost      `json:"cost"`
	Errors    []string  `json:"errors,omitempty"`
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

// durations are in seconds. All but Prepare are measured from the moment the phase's change starts.
type durations struct {
	// Prepare is how long the phase took before its window, publishing its component versions for example.
	Prepare float64 `json:"prepare"`
	// Change is how long applying the change took.
	Change     float64 `json:"change"`
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
	// ControllerRestarted invalidates the counter deltas: a restart resets them.
	ControllerRestarted bool `json:"controllerRestarted"`
	// NotReady counts the run's controller objects that were not Ready when the phase
	// timed out, keyed by kind and reason.
	NotReady map[string]int `json:"notReady,omitempty"`
}

// complete reports whether every pipeline was done, the queues drained and the
// controller kept running. Only then does the next phase start from a known state.
func (o outcome) complete() bool {
	return !o.TimedOut && !o.Unsettled && !o.ControllerRestarted
}

// cost is what the controller and the registry spent in the window of a phase.
type cost struct {
	// RegistryRequests is nil when the registry metrics could not be read.
	RegistryRequests    *float64 `json:"registryRequests"`
	CPUSeconds          float64  `json:"cpuSeconds"`
	AvgCores            float64  `json:"avgCores"`
	PeakCores           float64  `json:"peakCores"`
	PeakWorkingSetBytes float64  `json:"peakWorkingSetBytes"`
	PeakRSSBytes        float64  `json:"peakRSSBytes"`
	// ControllerCounters are end-minus-start deltas over the window.
	ControllerCounters map[string]float64 `json:"controllerCounters"`
	// ControllerGaugePeaks are the maxima of the sampled gauges.
	ControllerGaugePeaks map[string]float64 `json:"controllerGaugePeaks"`
	RegistryCounters     map[string]float64 `json:"registryCounters"`
}

// notes lists what makes a run unusable as a benchmark number, each note led by its phase.
func (r *result) notes() []string {
	var notes []string
	for _, p := range r.Phases {
		notes = append(notes, p.notes()...)
	}
	return notes
}

// notes lists what makes a phase unusable as a benchmark number.
func (p *phaseResult) notes() []string {
	var notes []string
	if p.Outcome.TimedOut {
		notes = append(notes, "timed out")
	}
	if p.Outcome.Unsettled {
		notes = append(notes, "queues not drained")
	}
	if p.Outcome.ControllerRestarted {
		notes = append(notes, "controller restarted")
	}
	if len(p.Errors) > 0 {
		notes = append(notes, "metric errors")
	}
	for i, n := range notes {
		notes[i] = p.Name + " " + n
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

// writeSummary renders one table row per phase of every run, for a quick read of
// an invocation. result.json files remain the source of truth.
func writeSummary(dir string, results []*result) error {
	var b strings.Builder
	b.WriteString("| Phase | Objects | Repeat | Image | Signed | Ready | Failed | Pending | Total (s) | Settled (s) | p50 (s) | p90 (s) | CPU (s) | Avg cores | Peak cores | Peak WS (MiB) | Registry reqs | Notes |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		for i := range r.Phases {
			writeSummaryRow(&b, r, &r.Phases[i])
		}
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(b.String()), 0o600)
}

func writeSummaryRow(b *strings.Builder, r *result, p *phaseResult) {
	requests := "n/a"
	if p.Cost.RegistryRequests != nil {
		requests = fmt.Sprintf("%.0f", *p.Cost.RegistryRequests)
	}
	signed := "no"
	if r.Signed {
		signed = "yes"
	}
	fmt.Fprintf(b, "| %s | %d | %d | %s | %s | %d | %d | %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.2f | %.2f | %.0f | %s | %s |\n",
		p.Name, r.Objects, r.Repeat, shortImageID(r.Environment.ControllerImageID), signed, p.Outcome.Ready, p.Outcome.Failed, p.Outcome.Pending,
		p.Durations.Total, p.Durations.Settled, p.Durations.P50, p.Durations.P90, p.Cost.CPUSeconds, p.Cost.AvgCores, p.Cost.PeakCores,
		p.Cost.PeakWorkingSetBytes/(1<<20), requests, strings.Join(p.notes(), ", "))
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
