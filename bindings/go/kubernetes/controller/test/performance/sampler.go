package main

import (
	"context"
	"fmt"
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
	// At is the offset from the start of the measurement.
	At              time.Duration      `json:"at"`
	WorkingSetBytes float64            `json:"workingSetBytes,omitempty"`
	Gauges          map[string]float64 `json:"gauges,omitempty"`
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
	smp := sample{At: time.Since(s.start)}
	var errs []string

	if raw, err := s.c.scrapeController(ctx, s.pod); err != nil {
		errs = append(errs, fmt.Sprintf("controller: %v", err))
	} else if sc, err := parseScrape(raw); err != nil {
		errs = append(errs, err.Error())
	} else {
		smp.Gauges = gaugeValues(sc, gaugePrefixes)
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
