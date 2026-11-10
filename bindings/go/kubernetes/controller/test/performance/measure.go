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
)

// scrapes holds one point-in-time snapshot of every metrics source.
type scrapes struct {
	controller, registry []byte
	// registryErr is set when the registry scrape failed; registry is nil then.
	registryErr error
}

// measurement is a measured phase, with the raw data behind it.
type measurement struct {
	phaseResult
	before, after scrapes
	samples       []sample
}

// measure prepares the phase, scrapes, makes its change, waits until every pipeline
// is done and the controller queues drained, and scrapes again.
func (r *run) measure(ctx context.Context, p phase) (*measurement, error) {
	m := &measurement{phaseResult: phaseResult{Name: p.name}}
	if p.prepare != nil {
		r.log.Info("preparing", "phase", p.name)
		start := time.Now()
		if err := p.prepare(ctx); err != nil {
			return nil, fmt.Errorf("preparing: %w", err)
		}
		m.Durations.Prepare = time.Since(start).Seconds()
	}

	tr, err := startTracker(ctx, r.c, r.w, p.watch, p.until, r.w.objects)
	if err != nil {
		return nil, err
	}
	defer tr.stop()

	if m.before, err = r.scrapeAll(ctx); err != nil {
		return nil, err
	}

	smpCtx, stopSampling := context.WithCancel(ctx)
	smp := &sampler{c: r.c, pod: r.pod.Name, node: r.pod.Spec.NodeName, interval: r.opts.sampleInterval}
	var wg sync.WaitGroup
	wg.Go(func() { smp.run(smpCtx) })

	r.log.Info("measuring", "phase", p.name, "watch", p.watch.Kind, "objects", r.w.objects)
	start := tr.markStart()
	if err := p.change(ctx); err != nil {
		stopSampling()
		wg.Wait()
		return nil, err
	}
	m.Durations.Change = time.Since(start).Seconds()

	allReady := tr.wait(ctx, r.opts.timeout)
	elapsed := time.Since(start)
	// Ready objects still leave reconciles queued; the cost has to cover those too.
	if allReady {
		r.log.Info("waiting for controller queues to drain", "phase", p.name)
		if settled, ok := smp.waitSettled(ctx, elapsed.Seconds(), r.opts.timeout); ok {
			elapsed = max(elapsed, time.Duration(settled*float64(time.Second)))
			m.Durations.Settled = elapsed.Seconds()
		} else {
			m.Outcome.Unsettled = true
			elapsed = time.Since(start)
		}
	}
	stopSampling()
	wg.Wait()

	if m.after, err = r.scrapeAll(ctx); err != nil {
		return nil, err
	}

	sum := tr.summary()
	m.Outcome = outcome{
		Expected:       r.w.objects,
		Ready:          sum.ready,
		Failed:         sum.failed,
		Pending:        sum.pending,
		TimedOut:       !allReady,
		Unsettled:      m.Outcome.Unsettled,
		FailureReasons: sum.failureReasons,
	}
	m.Durations.Total = elapsed.Seconds()
	if allReady {
		m.Durations.Total = sum.latencies[len(sum.latencies)-1].Seconds()
	}
	if len(sum.latencies) > 0 {
		m.Durations.FirstReady = sum.latencies[0].Seconds()
	}
	m.Durations.P50 = percentile(sum.latencies, 50).Seconds()
	m.Durations.P90 = percentile(sum.latencies, 90).Seconds()
	m.Durations.P99 = percentile(sum.latencies, 99).Seconds()

	var sampleErrs []string
	m.samples, sampleErrs = smp.result()
	m.Errors = append(m.Errors, sampleErrs...)
	if err := summarize(&m.Cost, m.before, m.after, m.samples, elapsed); err != nil {
		m.Errors = append(m.Errors, err.Error())
	}
	m.Outcome.ControllerRestarted = r.c.restartedSince(ctx, r.pod)
	if m.Outcome.TimedOut {
		if m.Outcome.NotReady, err = r.notReady(ctx); err != nil {
			m.Errors = append(m.Errors, err.Error())
		}
	}
	return m, nil
}

// scrapeAll fails only on the controller scrape. Registry numbers are
// secondary, so a failed registry scrape is logged and the run goes on.
func (r *run) scrapeAll(ctx context.Context) (scrapes, error) {
	var s scrapes
	var err error
	if s.controller, err = r.c.scrapeController(ctx, r.pod.Name); err != nil {
		return s, fmt.Errorf("scraping controller metrics: %w", err)
	}
	if s.registry, err = r.c.scrapeRegistry(ctx); err != nil {
		slog.Warn("scraping registry metrics", "error", err)
		s.registry, s.registryErr = nil, err
	}
	return s, nil
}

func summarize(c *cost, before, after scrapes, samples []sample, elapsed time.Duration) error {
	var errs []error

	ctlBefore, err1 := parseScrape(before.controller)
	ctlAfter, err2 := parseScrape(after.controller)
	if err := errors.Join(err1, err2); err != nil {
		errs = append(errs, fmt.Errorf("controller metrics: %w", err))
	} else {
		c.ControllerCounters = counterDeltas(ctlBefore, ctlAfter, controllerCounterPrefixes)
		// The process counter is read at scrape time; kubelet's cAdvisor value
		// lags by its housekeeping interval, which dominates short runs.
		c.CPUSeconds = c.ControllerCounters[cpuCounter]
		c.AvgCores = c.CPUSeconds / elapsed.Seconds()
	}

	if err := errors.Join(before.registryErr, after.registryErr); err != nil {
		errs = append(errs, fmt.Errorf("registry metrics unavailable: %w", err))
	} else {
		rBefore, err1 := parseScrape(before.registry)
		rAfter, err2 := parseScrape(after.registry)
		if err := errors.Join(err1, err2); err != nil {
			errs = append(errs, fmt.Errorf("registry metrics: %w", err))
		} else {
			c.RegistryCounters = counterDeltas(rBefore, rAfter, registryCounterPrefixes)
			requests := sumSeries(c.RegistryCounters, "registry_http_requests_total")
			c.RegistryRequests = &requests
		}
	}

	c.PeakWorkingSetBytes, c.ControllerGaugePeaks = peaks(samples)
	c.PeakRSSBytes = c.ControllerGaugePeaks["process_resident_memory_bytes"]
	c.PeakCores = peakCores(samples)
	return errors.Join(errs...)
}

// writeRaw writes the scrapes and samples behind a phase into dir.
func (m *measurement) writeRaw(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	files := map[string][]byte{
		"controller-start.prom": m.before.controller,
		"controller-end.prom":   m.after.controller,
		"registry-start.prom":   m.before.registry,
		"registry-end.prom":     m.after.registry,
	}
	for name, data := range files {
		if data == nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(dir, "samples.json"), m.samples)
}
