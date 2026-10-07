package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const createWorkers = 32

var (
	controllerCounterPrefixes = []string{
		"controller_runtime_",
		"workqueue_",
		"ocm_system_",
		"rest_client_requests_total",
		"process_cpu_seconds_total",
	}
	registryCounterPrefixes = []string{"registry_http_"}
)

type runner struct {
	c      *cluster
	opts   options
	outDir string
}

// scrapes holds one point-in-time snapshot of every metrics source.
type scrapes struct {
	controller, registry []byte
	// registryErr is set when the registry scrape failed; registry is nil then.
	registryErr error
}

func (r *runner) runOnce(ctx context.Context, s scenario, objects, repeat int) (*result, error) {
	runID := fmt.Sprintf("%s-%d-r%d-%s", s, objects, repeat, randomSuffix())
	w := newWorkload(s, objects, runID, r.opts.clusterRegistryURL)
	w.versions, w.depth, w.resources = r.opts.versions, r.opts.depth, r.opts.resources
	dir := filepath.Join(r.outDir, runID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	log := slog.With("run", runID)
	res := &result{Scenario: s, Objects: objects, Repeat: repeat, RunID: runID, StartedAt: time.Now().UTC()}
	switch s {
	case scenarioVersions:
		res.Versions = w.versions
	case scenarioNested:
		res.Depth = w.depth
	case scenarioComplex:
		res.Versions, res.Depth, res.Resources = complexVersions, w.depth, w.resources
	default:
	}

	if r.opts.restartController {
		log.Info("restarting controller")
		if err := r.c.restartController(ctx, r.opts.timeout); err != nil {
			return nil, fmt.Errorf("restarting controller: %w", err)
		}
	}
	pod, err := r.c.controllerPod(ctx)
	if err != nil {
		return nil, err
	}
	res.Environment = r.environment(pod)

	cvs, err := w.componentVersions()
	if err != nil {
		return nil, err
	}
	log.Info("publishing component versions", "count", len(cvs))
	publishStart := time.Now()
	if err := r.publish(ctx, w.imagePool(), cvs); err != nil {
		return nil, err
	}
	res.Durations.Publish = time.Since(publishStart).Seconds()

	if err := r.c.createNamespace(ctx, w.namespace, runID); err != nil {
		return nil, fmt.Errorf("creating namespace: %w", err)
	}
	if !r.opts.keepNamespaces {
		defer func() {
			log.Info("cleaning up")
			cctx := context.WithoutCancel(ctx)
			// Resources refuse deletion while a Deployer references them, so
			// the cluster-scoped Deployers have to go first.
			if err := r.c.deleteDeployers(cctx, runID, r.opts.timeout); err != nil {
				log.Error("deleting deployers", "error", err)
			}
			if err := r.c.deleteNamespace(cctx, w.namespace, r.opts.timeout); err != nil {
				log.Error("deleting namespace", "error", err)
			}
		}()
	}

	objs, err := w.objectsToCreate()
	if err != nil {
		return nil, err
	}
	if s == scenarioUpdate {
		log.Info("preparing pipelines at initial version")
		if err := r.prepareUpdate(ctx, w, objs); err != nil {
			return nil, err
		}
	}

	gvk, state := w.target()
	tr, err := startTracker(ctx, r.c, w, gvk, state, objects)
	if err != nil {
		return nil, err
	}
	defer tr.stop()

	before, err := r.scrapeAll(ctx, pod)
	if err != nil {
		return nil, err
	}

	smpCtx, stopSampling := context.WithCancel(ctx)
	smp := &sampler{c: r.c, pod: pod.Name, node: pod.Spec.NodeName, interval: r.opts.sampleInterval}
	var wg sync.WaitGroup
	wg.Go(func() { smp.run(smpCtx) })

	log.Info("measuring", "target", gvk.Kind, "objects", objects)
	start := tr.markStart()
	if s == scenarioUpdate {
		err = r.updateComponents(ctx, w)
	} else {
		err = r.create(ctx, objs)
	}
	if err != nil {
		stopSampling()
		wg.Wait()
		return nil, err
	}
	res.Durations.Create = time.Since(start).Seconds()

	allReady := tr.wait(ctx, r.opts.timeout)
	elapsed := time.Since(start)
	stopSampling()
	wg.Wait()

	after, err := r.scrapeAll(ctx, pod)
	if err != nil {
		return nil, err
	}

	sum := tr.summary()
	res.Outcome = outcome{
		Expected:       objects,
		Ready:          sum.ready,
		Failed:         sum.failed,
		Pending:        sum.pending,
		TimedOut:       !allReady,
		FailureReasons: sum.failureReasons,
	}
	res.Durations.Total = elapsed.Seconds()
	if allReady {
		res.Durations.Total = sum.latencies[len(sum.latencies)-1].Seconds()
	}
	if len(sum.latencies) > 0 {
		res.Durations.FirstReady = sum.latencies[0].Seconds()
	}
	res.Durations.P50 = percentile(sum.latencies, 50).Seconds()
	res.Durations.P90 = percentile(sum.latencies, 90).Seconds()
	res.Durations.P99 = percentile(sum.latencies, 99).Seconds()

	samples, sampleErrs := smp.result()
	res.Errors = append(res.Errors, sampleErrs...)
	if err := r.summarize(res, before, after, samples, elapsed); err != nil {
		res.Errors = append(res.Errors, err.Error())
	}
	res.ControllerRestarted = r.restartedSince(ctx, pod)

	if err := writeRaw(dir, before, after, samples); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(dir, "result.json"), res); err != nil {
		return nil, err
	}
	log.Info("run finished", "ready", sum.ready, "expected", objects, "total", res.Durations.Total)
	return res, nil
}

// prepareUpdate brings every pipeline to initialVersion before the update is
// measured, so the measurement only covers the version change.
func (r *runner) prepareUpdate(ctx context.Context, w workload, objs []client.Object) error {
	tr, err := startTracker(ctx, r.c, w, configMapGVK, configMapVersionState(initialVersion), w.objects)
	if err != nil {
		return err
	}
	defer tr.stop()
	tr.markStart()
	if err := r.create(ctx, objs); err != nil {
		return err
	}
	if !tr.wait(ctx, r.opts.timeout) {
		sum := tr.summary()
		return fmt.Errorf("update preparation: %d of %d pipelines reached %s", sum.ready, w.objects, initialVersion)
	}
	return nil
}

// create applies the Repositories first, then everything else in parallel,
// the way a GitOps tool would apply a directory.
func (r *runner) create(ctx context.Context, objs []client.Object) error {
	var repositories, rest []client.Object
	for _, obj := range objs {
		if _, ok := obj.(*v1alpha1.Repository); ok {
			repositories = append(repositories, obj)
		} else {
			rest = append(rest, obj)
		}
	}
	for _, batch := range [][]client.Object{repositories, rest} {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(createWorkers)
		for _, obj := range batch {
			g.Go(func() error {
				if err := r.c.client.Create(gctx, obj); err != nil {
					return fmt.Errorf("creating %s: %w", obj.GetName(), err)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) updateComponents(ctx context.Context, w workload) error {
	patch := client.RawPatch(types.MergePatchType, fmt.Appendf(nil, `{"spec":{"semver":%q}}`, updatedVersion))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(createWorkers)
	for i := range w.objects {
		g.Go(func() error {
			c := w.component(i)
			if err := r.c.client.Patch(ctx, c, patch); err != nil {
				return fmt.Errorf("updating %s: %w", c.Name, err)
			}
			return nil
		})
	}
	return g.Wait()
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

func (r *runner) summarize(res *result, before, after scrapes, samples []sample, elapsed time.Duration) error {
	var errs []error

	ctlBefore, err1 := parseScrape(before.controller)
	ctlAfter, err2 := parseScrape(after.controller)
	if err := errors.Join(err1, err2); err != nil {
		errs = append(errs, fmt.Errorf("controller metrics: %w", err))
	} else {
		res.ControllerCounters = counterDeltas(ctlBefore, ctlAfter, controllerCounterPrefixes)
		// The process counter is read at scrape time; kubelet's cAdvisor value
		// lags by its housekeeping interval, which dominates short runs.
		res.Usage.CPUSeconds = res.ControllerCounters["process_cpu_seconds_total"]
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
	return errors.Join(errs...)
}

func (r *runner) restartedSince(ctx context.Context, before *corev1.Pod) bool {
	now, err := r.c.controllerPod(ctx)
	if err != nil || now.Name != before.Name {
		return true
	}
	return restartCount(now) != restartCount(before)
}

func (r *runner) environment(pod *corev1.Pod) environment {
	env := environment{Node: pod.Spec.NodeName, KubernetesVersion: r.c.serverVersion(), ControllerPod: pod.Name}
	for _, c := range pod.Spec.Containers {
		if c.Name == managerContainer {
			env.ControllerImage = c.Image
			env.ControllerArgs = c.Args
			env.ControllerResources = c.Resources
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == managerContainer {
			env.ControllerImageID = cs.ImageID
		}
	}
	return env
}

func (r *runner) publish(ctx context.Context, images []image, cvs []componentVersion) error {
	tmp, err := os.MkdirTemp("", "ocm-perf-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	p, err := newPublisher(r.opts.registryURL, tmp)
	if err != nil {
		return err
	}
	if err := p.publishImages(ctx, images); err != nil {
		return err
	}
	return p.publish(ctx, cvs)
}

func restartCount(pod *corev1.Pod) int32 {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == managerContainer {
			return cs.RestartCount
		}
	}
	return 0
}

func writeRaw(dir string, before, after scrapes, samples []sample) error {
	files := map[string][]byte{
		"controller-start.prom": before.controller,
		"controller-end.prom":   after.controller,
		"registry-start.prom":   before.registry,
		"registry-end.prom":     after.registry,
	}
	for name, data := range files {
		if data == nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(dir, "samples.json"), samples)
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
