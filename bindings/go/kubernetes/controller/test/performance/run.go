package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

func (r *runner) runOnce(ctx context.Context, objects, repeat int) (*result, error) {
	runID := fmt.Sprintf("n%d-r%d-%s", objects, repeat, randomSuffix())
	w := newWorkload(objects, runID, r.opts.clusterRegistryURL)
	w.versions, w.depth, w.resources = r.opts.versions, r.opts.depth, r.opts.resources
	w.repositoryCount, w.sign = r.opts.repositories, r.opts.sign
	dir := filepath.Join(r.outDir, runID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	log := slog.With("run", runID)
	res := &result{
		Objects: objects, Repeat: repeat, RunID: runID, StartedAt: time.Now().UTC(),
		Versions: w.versions, Depth: w.depth, Resources: w.resources, Repositories: w.repositories(), Signed: w.sign,
	}

	var sgn *signer
	if w.sign {
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

	if err := r.c.createNamespace(ctx, w.namespace, runID); err != nil {
		return nil, fmt.Errorf("creating namespace: %w", err)
	}
	if !r.opts.keepNamespaces {
		defer func() {
			log.Info("cleaning up")
			start := time.Now()
			if err := r.c.deleteNamespace(context.WithoutCancel(ctx), w.namespace, r.opts.timeout); err != nil {
				log.Error("deleting namespace", "error", err)
			}
			log.Info("cleaned up", "duration", time.Since(start))
		}()
	}

	objs, err := w.objectsToCreate()
	if err != nil {
		return nil, err
	}
	rollout, err := r.measure(ctx, pod, w, phase{
		name: "rollout", gvk: deployerGVK, state: readyState, expected: w.objects,
		trigger: func(ctx context.Context) error { return r.create(ctx, objs) },
	})
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
	log.Info("run finished", "ready", res.Outcome.Ready, "expected", w.objects, "total", res.Durations.Total)
	return res, nil
}

// drift measures the drift phase into res.Drift: every deployed ConfigMap is
// changed, until each Deployer has reverted its own. It needs a finished rollout
// with drained queues: a Deployer only watches an object once it applied it.
func (r *runner) drift(ctx context.Context, log *slog.Logger, pod *corev1.Pod, w workload, rollout *measurement, res *result, dir string) error {
	if !rollout.complete() {
		log.Warn("skipping the drift phase, the rollout did not finish")
		return nil
	}
	keys := w.configMaps()
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
