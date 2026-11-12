package main

import (
	"context"
	"fmt"
	"time"
)

// measurement is a measured phase, with the raw data behind its cost.
type measurement struct {
	phaseResult
	raw raw
}

// measure prepares the phase, then opens its window: it makes the change, watches until
// every pipeline is done and the controller queues drained, and meters what that cost.
func (r *run) measure(ctx context.Context, p phase) (*measurement, error) {
	prepare, err := r.prepare(ctx, p)
	if err != nil {
		return nil, err
	}

	tr, err := startTracker(ctx, r.cluster, r.workload, p.watch, p.until, r.workload.objects)
	if err != nil {
		return nil, err
	}
	defer tr.stop()
	meter, err := startMeter(ctx, r.cluster, r.controllerPod, r.opts.sampleInterval)
	if err != nil {
		return nil, err
	}
	defer meter.stop()

	r.log.Info("measuring", "phase", p.name, "watch", p.watch.Kind, "objects", r.workload.objects)
	start := tr.markStart()
	if err := p.change(ctx); err != nil {
		return nil, err
	}
	change := time.Since(start)

	done := tr.wait(ctx, r.opts.timeout)
	elapsed, drained := r.windowEnd(ctx, meter, start, done)
	c, data, errs, err := meter.finish(ctx, elapsed)
	if err != nil {
		return nil, err
	}

	sum := tr.summary()
	out := newOutcome(sum, r.workload.objects, done, drained, r.cluster.restartedSince(ctx, r.controllerPod))
	if out.TimedOut {
		if out.NotReady, err = r.notReady(ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	return &measurement{
		phaseResult: phaseResult{
			Name:      p.name,
			Outcome:   out,
			Durations: newDurations(sum, prepare, change, elapsed, done, drained),
			Cost:      c,
			Errors:    errs,
		},
		raw: data,
	}, nil
}

// prepare runs the phase's preparation, if it has one, and times it.
func (r *run) prepare(ctx context.Context, p phase) (time.Duration, error) {
	if p.prepare == nil {
		return 0, nil
	}
	r.log.Info("preparing", "phase", p.name)
	start := time.Now()
	if err := p.prepare(ctx); err != nil {
		return 0, fmt.Errorf("preparing: %w", err)
	}
	return time.Since(start), nil
}

// windowEnd is how long the window lasted, from start: up to the timeout when not every
// pipeline was done, else until the controller queues drained as well. Done pipelines
// still leave reconciles queued, and the cost has to cover those too.
func (r *run) windowEnd(ctx context.Context, m *meter, start time.Time, done bool) (elapsed time.Duration, drained bool) {
	elapsed = time.Since(start)
	if !done {
		return elapsed, false
	}
	r.log.Info("waiting for controller queues to drain")
	settled, ok := m.waitSettled(ctx, elapsed, r.opts.timeout)
	if !ok {
		return time.Since(start), false
	}
	return max(elapsed, settled), true
}

func newOutcome(sum trackerSummary, expected int, done, drained, restarted bool) outcome {
	return outcome{
		Expected:            expected,
		Ready:               sum.ready,
		Failed:              sum.failed,
		Pending:             sum.pending,
		TimedOut:            !done,
		FailureReasons:      sum.failureReasons,
		Unsettled:           done && !drained,
		ControllerRestarted: restarted,
	}
}

// newDurations: the total is when the last pipeline was done, or the whole window when
// not every one was. Settled is only known when the queues drained.
func newDurations(sum trackerSummary, prepare, change, elapsed time.Duration, done, drained bool) durations {
	d := durations{
		Prepare: prepare.Seconds(),
		Change:  change.Seconds(),
		Total:   elapsed.Seconds(),
		P50:     percentile(sum.latencies, 50).Seconds(),
		P90:     percentile(sum.latencies, 90).Seconds(),
		P99:     percentile(sum.latencies, 99).Seconds(),
	}
	if len(sum.latencies) > 0 {
		d.FirstReady = sum.latencies[0].Seconds()
		if done {
			d.Total = sum.latencies[len(sum.latencies)-1].Seconds()
		}
	}
	if drained {
		d.Settled = elapsed.Seconds()
	}
	return d
}
