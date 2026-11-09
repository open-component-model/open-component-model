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
	"reflect"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const (
	createWorkers = 32
	cpuCounter    = "process_cpu_seconds_total"
)

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
	w.versions, w.depth, w.resources, w.fanout = r.opts.versions, r.opts.depth, r.opts.resources, r.opts.fanout
	w.repositoryCount, w.sign = r.opts.repositories, r.opts.sign
	dir := filepath.Join(r.outDir, runID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	log := slog.With("run", runID)
	res := &result{Scenario: s, Objects: objects, Repeat: repeat, RunID: runID, StartedAt: time.Now().UTC()}
	switch s {
	case scenarioComplex:
		res.Versions, res.Depth, res.Resources = w.versions, &w.depth, w.resources
		res.Repositories, res.Signed = w.repositories(), w.signed()
	case scenarioFanout:
		res.Resources, res.Fanout = w.resources, w.fanout
	default:
	}

	var sgn *signer
	if w.signed() {
		var err error
		if sgn, err = newSigner(); err != nil {
			return nil, err
		}
		w.publicKeyPEM = sgn.publicKeyPEM
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
	if err := r.publish(ctx, sgn, w.imagePool(), cvs); err != nil {
		return nil, err
	}
	published := time.Since(publishStart).Seconds()

	if err := r.forEachNamespace(ctx, w, func(ctx context.Context, namespace string) error {
		return r.c.createNamespace(ctx, namespace, runID)
	}); err != nil {
		return nil, fmt.Errorf("creating namespaces: %w", err)
	}
	if !r.opts.keepNamespaces {
		defer func() {
			log.Info("cleaning up", "namespaces", len(w.namespaces()))
			cleanupStart := time.Now()
			if err := r.cleanup(context.WithoutCancel(ctx), log, w); err != nil {
				log.Error("cleaning up", "error", err)
			}
			log.Info("cleaned up", "duration", time.Since(cleanupStart))
		}()
	}

	objs, err := w.objectsToCreate()
	if err != nil {
		return nil, err
	}
	trigger := func(ctx context.Context) error { return r.create(ctx, objs) }
	if s == scenarioUpdate {
		log.Info("preparing pipelines at initial version")
		if err := r.prepareUpdate(ctx, w, objs); err != nil {
			return nil, err
		}
		trigger = func(ctx context.Context) error { return r.updateComponents(ctx, w) }
	}

	gvk, state := w.target()
	rollout, err := r.measure(ctx, pod, w, phase{name: "rollout", gvk: gvk, state: state, expected: w.targets(), trigger: trigger})
	if err != nil {
		return nil, err
	}
	res.window = rollout.window
	res.Durations.Publish = published
	if err := rollout.writeRaw(dir, ""); err != nil {
		return nil, err
	}

	if err := r.drift(ctx, log, pod, w, rollout, res, dir); err != nil {
		return nil, err
	}

	if err := writeJSON(filepath.Join(dir, "result.json"), res); err != nil {
		return nil, err
	}
	log.Info("run finished", "ready", res.Outcome.Ready, "expected", w.targets(), "total", res.Durations.Total)
	return res, nil
}

// drift measures the drift phase into res.Drift: every deployed ConfigMap is
// changed, until each Deployer has reverted its own. It needs a finished rollout
// with drained queues: a Deployer only watches an object once it applied it.
func (r *runner) drift(ctx context.Context, log *slog.Logger, pod *corev1.Pod, w workload, rollout *measurement, res *result, dir string) error {
	keys := w.driftedConfigMaps()
	if len(keys) == 0 {
		return nil
	}
	if !rollout.complete() {
		log.Warn("skipping the drift phase, the rollout did not finish")
		return nil
	}
	m, err := r.measure(ctx, pod, w, phase{
		name:     "drift",
		gvk:      configMapGVK,
		state:    revertedState(initialVersion),
		expected: len(keys),
		trigger:  func(ctx context.Context) error { return r.driftConfigMaps(ctx, keys) },
	})
	if err != nil {
		return fmt.Errorf("drift: %w", err)
	}
	res.Drift = &m.window
	return m.writeRaw(dir, "drift-")
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

// create applies the Repositories, the signing Secret and the deployer RBAC first, then
// everything else in parallel, the way a GitOps tool would apply a directory.
func (r *runner) create(ctx context.Context, objs []client.Object) error {
	var first, rest []client.Object
	for _, obj := range objs {
		switch obj.(type) {
		case *v1alpha1.Repository, *corev1.Secret, *corev1.ServiceAccount, *rbacv1.Role, *rbacv1.RoleBinding:
			first = append(first, obj)
		default:
			rest = append(rest, obj)
		}
	}
	for _, batch := range [][]client.Object{first, rest} {
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

// driftConfigMaps changes the version of every deployed ConfigMap, the way a
// person editing a deployed object would.
func (r *runner) driftConfigMaps(ctx context.Context, keys []client.ObjectKey) error {
	patch := client.RawPatch(types.MergePatchType, fmt.Appendf(nil, `{"data":{"version":%q}}`, driftedVersion))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(createWorkers)
	for _, key := range keys {
		g.Go(func() error {
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
			if err := r.c.client.Patch(ctx, cm, patch); err != nil {
				return fmt.Errorf("drifting %s: %w", key, err)
			}
			return nil
		})
	}
	return g.Wait()
}

// cleanup deletes a run the way a user would: the Deployers first, while their service
// account can still prune what they deployed, then the rest of the pipelines, then the
// namespaces. Deleting the namespaces right away deletes all of it at once; the Deployers
// then fail on their half-deleted RBAC and every kind waits for the next with backoff.
// A stage that fails or times out does not stop the next: the namespaces are always deleted.
func (r *runner) cleanup(ctx context.Context, log *slog.Logger, w workload) error {
	stages := [][]client.Object{
		{&v1alpha1.NamespacedDeployer{}},
		{&v1alpha1.Resource{}, &v1alpha1.Component{}, &v1alpha1.Repository{}},
	}
	var errs []error
	for _, kinds := range stages {
		start := time.Now()
		for _, kind := range kinds {
			if err := r.forEachNamespace(ctx, w, func(ctx context.Context, namespace string) error {
				return r.c.client.DeleteAllOf(ctx, kind, client.InNamespace(namespace), client.MatchingLabels{runLabel: w.runID})
			}); err != nil {
				errs = append(errs, fmt.Errorf("deleting %T: %w", kind, err))
			}
		}
		for _, kind := range kinds {
			if err := r.c.waitGone(ctx, kind, w.runID, r.opts.timeout); err != nil {
				errs = append(errs, err)
			}
		}
		log.Info("deleted pipeline objects", "kinds", kindNames(kinds), "duration", time.Since(start))
	}
	// Waiting once for all of them, so finalizer work from one run does not overlap the next.
	if err := r.forEachNamespace(ctx, w, func(ctx context.Context, namespace string) error {
		return r.c.deleteNamespace(ctx, namespace)
	}); err != nil {
		errs = append(errs, fmt.Errorf("deleting namespaces: %w", err))
	}
	return errors.Join(append(errs, r.c.waitGone(ctx, &corev1.Namespace{}, w.runID, r.opts.timeout))...)
}

func kindNames(objs []client.Object) []string {
	names := make([]string, len(objs))
	for i, obj := range objs {
		names[i] = reflect.TypeOf(obj).Elem().Name()
	}
	return names
}

func (r *runner) forEachNamespace(ctx context.Context, w workload, f func(context.Context, string) error) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(createWorkers)
	for _, namespace := range w.namespaces() {
		g.Go(func() error { return f(ctx, namespace) })
	}
	return g.Wait()
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

func (r *runner) publish(ctx context.Context, sgn *signer, images []image, cvs []componentVersion) error {
	tmp, err := os.MkdirTemp("", "ocm-perf-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	p, err := newPublisher(r.opts.registryURL, tmp, sgn)
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

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
