package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// gaugePrefixes select the controller gauges sampled over a run.
var gaugePrefixes = []string{
	"workqueue_depth",
	"controller_runtime_active_workers",
	"ocm_system_",
	"go_goroutines",
	"go_memstats_heap_inuse_bytes",
	"process_resident_memory_bytes",
}

type sample struct {
	// At is the offset from the start of the measurement, in seconds.
	At              float64            `json:"at"`
	WorkingSetBytes float64            `json:"workingSetBytes,omitempty"`
	Gauges          map[string]float64 `json:"gauges,omitempty"`
	// CPUSeconds is the controller's cumulative process CPU time.
	CPUSeconds float64 `json:"cpuSeconds,omitempty"`
}

// sampler polls controller gauges and kubelet container memory. Counters only
// need the start and end scrape, but peaks need samples in between.
type sampler struct {
	c        *cluster
	pod      string
	node     string
	interval time.Duration

	mu      sync.Mutex
	start   time.Time
	samples []sample
	errs    []string
}

func (s *sampler) run(ctx context.Context) {
	s.start = time.Now()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		s.take(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *sampler) take(ctx context.Context) {
	smp := sample{At: time.Since(s.start).Seconds()}
	var errs []string

	if raw, err := s.c.scrapeController(ctx, s.pod); err != nil {
		errs = append(errs, fmt.Sprintf("controller: %v", err))
	} else if sc, err := parseScrape(raw); err != nil {
		errs = append(errs, err.Error())
	} else {
		smp.Gauges = gaugeValues(sc, gaugePrefixes)
		smp.CPUSeconds = cumulativeValues(sc, []string{cpuCounter})[cpuCounter]
	}

	if raw, err := s.c.scrapeKubelet(ctx, s.node); err != nil {
		errs = append(errs, fmt.Sprintf("kubelet: %v", err))
	} else if sc, err := parseScrape(raw); err != nil {
		errs = append(errs, err.Error())
	} else if v, ok := containerValue(sc, "container_memory_working_set_bytes", s.pod, managerContainer); ok {
		smp.WorkingSetBytes = v
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// A cancelled context during shutdown is not a sampling failure.
	if ctx.Err() == nil {
		s.errs = append(s.errs, errs...)
	}
	s.samples = append(s.samples, smp)
}

func (s *sampler) result() ([]sample, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.samples, s.errs
}

// peakCores returns the highest CPU rate between two consecutive samples.
// It averages over one sample interval, so shorter bursts read lower than they were.
func peakCores(samples []sample) float64 {
	var peak float64
	var prev *sample
	for i := range samples {
		cur := &samples[i]
		// A failed scrape leaves the counter at zero; skip it instead of reading a drop.
		if cur.CPUSeconds == 0 {
			continue
		}
		if prev != nil && cur.At > prev.At {
			peak = max(peak, (cur.CPUSeconds-prev.CPUSeconds)/(cur.At-prev.At))
		}
		prev = cur
	}
	return peak
}

// settleSamples is the number of consecutive idle samples that count as settled.
// One idle sample is not enough: rate-limited requeues leave the queue empty in between.
const settleSamples = 3

// idle reports whether every workqueue was empty and no worker was active.
func (s sample) idle() bool {
	if s.Gauges == nil {
		return false
	}
	for k, v := range s.Gauges {
		if v != 0 && (strings.HasPrefix(k, "workqueue_depth") || strings.HasPrefix(k, "controller_runtime_active_workers")) {
			return false
		}
	}
	return true
}

// settledAt returns the offset at which the trailing idle samples began.
// Samples before after do not count: the controller is idle before the run starts, too.
func settledAt(samples []sample, after float64) (float64, bool) {
	if len(samples) < settleSamples {
		return 0, false
	}
	tail := samples[len(samples)-settleSamples:]
	for _, smp := range tail {
		if smp.At < after || !smp.idle() {
			return 0, false
		}
	}
	return tail[0].At, true
}

// waitSettled blocks until the controller has worked off everything it queued.
func (s *sampler) waitSettled(ctx context.Context, after float64, timeout time.Duration) (float64, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		samples, _ := s.result()
		if at, ok := settledAt(samples, after); ok {
			return at, true
		}
		select {
		case <-ctx.Done():
			return 0, false
		case <-ticker.C:
		}
	}
}

// peaks returns the maximum working set and the per-series gauge maxima.
func peaks(samples []sample) (float64, map[string]float64) {
	var ws float64
	gauges := map[string]float64{}
	for _, smp := range samples {
		ws = max(ws, smp.WorkingSetBytes)
		for k, v := range smp.Gauges {
			if cur, ok := gauges[k]; !ok || v > cur {
				gauges[k] = v
			}
		}
	}
	return ws, gauges
}
