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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// phase is one measured change of a run: the rollout, or the drift after it.
type phase struct {
	name string
	// gvk and state decide when an object of the phase is done; expected is how many there are.
	gvk      schema.GroupVersionKind
	state    func(*unstructured.Unstructured) objectState
	expected int
	// trigger makes the change the phase measures.
	trigger func(context.Context) error
}

// measurement is a measured phase, with the raw data behind its window.
type measurement struct {
	window
	before, after scrapes
	samples       []sample
}

// complete reports whether every object of the phase was done, the queues drained
// and the controller kept running.
func (m *measurement) complete() bool {
	return !m.Outcome.TimedOut && !m.Outcome.Unsettled && !m.ControllerRestarted
}

// measure scrapes, triggers the phase, waits until its objects are done and the
// controller queues drained, and scrapes again.
func (r *runner) measure(ctx context.Context, pod *corev1.Pod, w workload, p phase) (*measurement, error) {
	tr, err := startTracker(ctx, r.c, w, p.gvk, p.state, p.expected)
	if err != nil {
		return nil, err
	}
	defer tr.stop()

	m := &measurement{}
	if m.before, err = r.scrapeAll(ctx, pod); err != nil {
		return nil, err
	}

	smpCtx, stopSampling := context.WithCancel(ctx)
	smp := &sampler{c: r.c, pod: pod.Name, node: pod.Spec.NodeName, interval: r.opts.sampleInterval}
	var wg sync.WaitGroup
	wg.Go(func() { smp.run(smpCtx) })

	slog.Info("measuring", "run", w.runID, "phase", p.name, "target", p.gvk.Kind, "targets", p.expected)
	start := tr.markStart()
	if err := p.trigger(ctx); err != nil {
		stopSampling()
		wg.Wait()
		return nil, err
	}
	m.Durations.Create = time.Since(start).Seconds()

	allReady := tr.wait(ctx, r.opts.timeout)
	elapsed := time.Since(start)
	// Ready objects still leave reconciles queued; usage has to cover those too.
	if allReady {
		slog.Info("waiting for controller queues to drain", "run", w.runID, "phase", p.name)
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

	if m.after, err = r.scrapeAll(ctx, pod); err != nil {
		return nil, err
	}

	sum := tr.summary()
	m.Outcome = outcome{
		Expected:       p.expected,
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
	if err := r.summarize(&m.window, m.before, m.after, m.samples, elapsed); err != nil {
		m.Errors = append(m.Errors, err.Error())
	}
	m.ControllerRestarted = r.restartedSince(ctx, pod)
	return m, nil
}

// scrapeAll fails only on the controller scrape. Registry numbers are
// secondary, so a failed registry scrape is logged and the run goes on.
func (r *runner) scrapeAll(ctx context.Context, pod *corev1.Pod) (scrapes, error) {
	var s scrapes
	var err error
	if s.controller, err = r.c.scrapeController(ctx, pod.Name); err != nil {
		return s, fmt.Errorf("scraping controller metrics: %w", err)
	}
	if s.registry, err = r.c.scrapeRegistry(ctx); err != nil {
		slog.Warn("scraping registry metrics", "error", err)
		s.registry, s.registryErr = nil, err
	}
	return s, nil
}

func (r *runner) summarize(res *window, before, after scrapes, samples []sample, elapsed time.Duration) error {
	var errs []error

	ctlBefore, err1 := parseScrape(before.controller)
	ctlAfter, err2 := parseScrape(after.controller)
	if err := errors.Join(err1, err2); err != nil {
		errs = append(errs, fmt.Errorf("controller metrics: %w", err))
	} else {
		res.ControllerCounters = counterDeltas(ctlBefore, ctlAfter, controllerCounterPrefixes)
		// The process counter is read at scrape time; kubelet's cAdvisor value
		// lags by its housekeeping interval, which dominates short runs.
		res.Usage.CPUSeconds = res.ControllerCounters[cpuCounter]
		res.Usage.AvgCores = res.Usage.CPUSeconds / elapsed.Seconds()
	}

	if err := errors.Join(before.registryErr, after.registryErr); err != nil {
		errs = append(errs, fmt.Errorf("registry metrics unavailable: %w", err))
	} else {
		rBefore, err1 := parseScrape(before.registry)
		rAfter, err2 := parseScrape(after.registry)
		if err := errors.Join(err1, err2); err != nil {
			errs = append(errs, fmt.Errorf("registry metrics: %w", err))
		} else {
			res.RegistryCounters = counterDeltas(rBefore, rAfter, registryCounterPrefixes)
			requests := sumSeries(res.RegistryCounters, "registry_http_requests_total")
			res.Usage.RegistryRequests = &requests
		}
	}

	res.Usage.PeakWorkingSetBytes, res.ControllerGaugePeaks = peaks(samples)
	res.Usage.PeakRSSBytes = res.ControllerGaugePeaks["process_resident_memory_bytes"]
	res.Usage.PeakCores = peakCores(samples)
	return errors.Join(errs...)
}

// writeRaw writes the scrapes and samples behind a window, each file name
// starting with prefix.
func (m *measurement) writeRaw(dir, prefix string) error {
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
		if err := os.WriteFile(filepath.Join(dir, prefix+name), data, 0o600); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(dir, prefix+"samples.json"), m.samples)
}
