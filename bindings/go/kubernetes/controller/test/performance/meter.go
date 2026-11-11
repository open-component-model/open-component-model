package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const cpuCounter = "process_cpu_seconds_total"

var (
	controllerCounterPrefixes = []string{
		"controller_runtime_",
		"workqueue_",
		"ocm_system_",
		"rest_client_requests_total",
		cpuCounter,
	}
	registryCounterPrefixes = []string{"registry_http_"}
)

// meter measures what a phase's window costs the controller and the registry: a scrape
// at either end for the counters, and the samples in between for the peaks. It does not
// know what the phase changes; the tracker does.
type meter struct {
	c      *cluster
	pod    *corev1.Pod
	before scrapes
	smp    *sampler

	stopSampling context.CancelFunc
	wg           sync.WaitGroup
}

// scrapes holds one point-in-time snapshot of every metrics source.
type scrapes struct {
	controller, registry []byte
	// registryErr is set when the registry scrape failed; registry is nil then.
	registryErr error
}

// raw is the data behind the cost of a phase, written next to its result.
type raw struct {
	before, after scrapes
	samples       []sample
}

// startMeter scrapes, then starts sampling.
func startMeter(ctx context.Context, c *cluster, pod *corev1.Pod, interval time.Duration) (*meter, error) {
	m := &meter{c: c, pod: pod, smp: &sampler{c: c, pod: pod.Name, node: pod.Spec.NodeName, interval: interval}}
	var err error
	if m.before, err = scrapeAll(ctx, c, pod); err != nil {
		return nil, err
	}
	smpCtx, stop := context.WithCancel(ctx)
	m.stopSampling = stop
	m.wg.Go(func() { m.smp.run(smpCtx) })
	return m, nil
}

// waitSettled blocks until the controller has worked off everything it queued, at
// the earliest after offset after the sampler started. It returns that point's offset.
func (m *meter) waitSettled(ctx context.Context, after, timeout time.Duration) (time.Duration, bool) {
	settled, ok := m.smp.waitSettled(ctx, after.Seconds(), timeout)
	return time.Duration(settled * float64(time.Second)), ok
}

// stop ends sampling. It is safe to call more than once.
func (m *meter) stop() {
	m.stopSampling()
	m.wg.Wait()
}

// finish stops sampling, scrapes again and computes the cost of a window that lasted
// elapsed. Errors that only make the cost incomplete are returned as messages; only
// a failed controller scrape fails the phase.
func (m *meter) finish(ctx context.Context, elapsed time.Duration) (cost, raw, []string, error) {
	m.stop()
	after, err := scrapeAll(ctx, m.c, m.pod)
	if err != nil {
		return cost{}, raw{}, nil, err
	}
	samples, errs := m.smp.result()
	data := raw{before: m.before, after: after, samples: samples}
	c, err := costOf(data, elapsed)
	if err != nil {
		errs = append(errs, err.Error())
	}
	return c, data, errs, nil
}

// scrapeAll fails only on the controller scrape. Registry numbers are
// secondary, so a failed registry scrape is logged and the run goes on.
func scrapeAll(ctx context.Context, c *cluster, pod *corev1.Pod) (scrapes, error) {
	var s scrapes
	var err error
	if s.controller, err = c.scrapeController(ctx, pod.Name); err != nil {
		return s, fmt.Errorf("scraping controller metrics: %w", err)
	}
	if s.registry, err = c.scrapeRegistry(ctx); err != nil {
		slog.Warn("scraping registry metrics", "error", err)
		s.registry, s.registryErr = nil, err
	}
	return s, nil
}

// costOf computes the counter deltas and peaks of a window. It fills in what it can
// and reports the rest.
func costOf(data raw, elapsed time.Duration) (cost, error) {
	var c cost
	var errs []error

	ctlBefore, err1 := parseScrape(data.before.controller)
	ctlAfter, err2 := parseScrape(data.after.controller)
	if err := errors.Join(err1, err2); err != nil {
		errs = append(errs, fmt.Errorf("controller metrics: %w", err))
	} else {
		c.ControllerCounters = counterDeltas(ctlBefore, ctlAfter, controllerCounterPrefixes)
		// The process counter is read at scrape time; kubelet's cAdvisor value
		// lags by its housekeeping interval, which dominates short runs.
		c.CPUSeconds = c.ControllerCounters[cpuCounter]
		c.AvgCores = c.CPUSeconds / elapsed.Seconds()
	}

	if err := errors.Join(data.before.registryErr, data.after.registryErr); err != nil {
		errs = append(errs, fmt.Errorf("registry metrics unavailable: %w", err))
	} else {
		rBefore, err1 := parseScrape(data.before.registry)
		rAfter, err2 := parseScrape(data.after.registry)
		if err := errors.Join(err1, err2); err != nil {
			errs = append(errs, fmt.Errorf("registry metrics: %w", err))
		} else {
			c.RegistryCounters = counterDeltas(rBefore, rAfter, registryCounterPrefixes)
			requests := sumSeries(c.RegistryCounters, "registry_http_requests_total")
			c.RegistryRequests = &requests
		}
	}

	c.PeakWorkingSetBytes, c.ControllerGaugePeaks = peaks(data.samples)
	c.PeakRSSBytes = c.ControllerGaugePeaks["process_resident_memory_bytes"]
	c.PeakCores = peakCores(data.samples)
	return c, errors.Join(errs...)
}

// write writes the scrapes and samples into dir.
func (d raw) write(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	files := map[string][]byte{
		"controller-start.prom": d.before.controller,
		"controller-end.prom":   d.after.controller,
		"registry-start.prom":   d.before.registry,
		"registry-end.prom":     d.after.registry,
	}
	for name, data := range files {
		if data == nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(dir, "samples.json"), d.samples)
}
