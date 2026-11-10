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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
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
	// pipelineGVKs are the controller kinds of a pipeline, listed when a phase times out.
	pipelineGVKs = []schema.GroupVersionKind{
		v1alpha1.GroupVersion.WithKind("Component"),
		v1alpha1.GroupVersion.WithKind("Resource"),
		deployerGVK,
	}
)

// runner runs the benchmark invocation: every object count, every repeat.
type runner struct {
	c      *cluster
	opts   options
	outDir string
}

// run is one benchmark run: one workload, measured phase by phase.
type run struct {
	*runner
	w workload
	// pub is one per run: the image digests pushed for the rollout are needed again by later phases.
	pub  *publisher
	pod  *corev1.Pod
	objs []client.Object
	log  *slog.Logger
	dir  string
}

// runOnce sets up a run, measures its phases in order until one does not complete, and cleans up.
func (r *runner) runOnce(ctx context.Context, objects, repeat int) (*result, error) {
	ru, err := r.newRun(ctx, objects, repeat)
	if err != nil {
		return nil, err
	}
	defer ru.close()
	res := ru.result(repeat)

	if err := r.c.createNamespace(ctx, ru.w.namespace, ru.w.runID); err != nil {
		return nil, fmt.Errorf("creating namespace: %w", err)
	}
	if !r.opts.keepNamespaces {
		defer ru.cleanup(context.WithoutCancel(ctx))
	}

	for _, p := range ru.phases() {
		m, err := ru.measure(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.name, err)
		}
		res.Phases = append(res.Phases, m.phaseResult)
		// Written after every phase, so a later failure keeps what was measured.
		if err := errors.Join(m.writeRaw(filepath.Join(ru.dir, p.name)), writeJSON(filepath.Join(ru.dir, "result.json"), res)); err != nil {
			return nil, err
		}
		if !m.Outcome.complete() {
			ru.log.Warn("skipping the remaining phases", "after", p.name)
			break
		}
	}
	ru.log.Info("run finished", "phases", len(res.Phases))
	return res, nil
}

// newRun prepares everything a run's phases share: the signing key, the publisher, a
// controller with empty caches, and the objects to create. Nothing in it is measured.
func (r *runner) newRun(ctx context.Context, objects, repeat int) (_ *run, err error) {
	runID := fmt.Sprintf("n%d-r%d-%s", objects, repeat, randomSuffix())
	w := newWorkload(objects, runID, r.opts.clusterRegistryURL)
	w.versions, w.depth, w.resources = r.opts.versions, r.opts.depth, r.opts.resources
	w.repositoryCount, w.sign = r.opts.repositories, r.opts.sign
	ru := &run{runner: r, w: w, log: slog.With("run", runID), dir: filepath.Join(r.outDir, runID)}
	if err := os.MkdirAll(ru.dir, 0o750); err != nil {
		return nil, err
	}

	var sgn *signer
	if w.sign {
		if sgn, err = newSigner(); err != nil {
			return nil, err
		}
		ru.w.publicKeyPEM = sgn.publicKeyPEM
	}
	tmp, err := os.MkdirTemp("", "ocm-perf-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp)
		}
	}()
	if ru.pub, err = newPublisher(r.opts.registryURL, tmp, sgn); err != nil {
		return nil, err
	}

	if r.opts.restartController {
		ru.log.Info("restarting controller")
		if err := r.c.restartController(ctx, r.opts.timeout); err != nil {
			return nil, fmt.Errorf("restarting controller: %w", err)
		}
	}
	if ru.pod, err = r.c.controllerPod(ctx); err != nil {
		return nil, err
	}
	if ru.objs, err = ru.w.objectsToCreate(); err != nil {
		return nil, err
	}
	return ru, nil
}

// close removes what the run kept on the local disk.
func (r *run) close() {
	_ = os.RemoveAll(r.pub.tempDir)
}

func (r *run) result(repeat int) *result {
	return &result{
		Objects: r.w.objects, Repeat: repeat, RunID: r.w.runID, StartedAt: time.Now().UTC(),
		Versions: r.w.versions, Depth: r.w.depth, Resources: r.w.resources, Repositories: r.w.repositories(), Signed: r.w.sign,
		Environment: r.environment(),
	}
}

// cleanup deletes the run's namespace and waits until it is gone, so the next run starts
// without finalizer work of this one.
func (r *run) cleanup(ctx context.Context) {
	r.log.Info("cleaning up")
	start := time.Now()
	if err := r.c.deleteNamespace(ctx, r.w.namespace, r.opts.timeout); err != nil {
		r.log.Error("deleting namespace", "error", err)
	}
	r.log.Info("cleaned up", "duration", time.Since(start))
}

// notReady counts the pipeline objects that are not Ready, by kind and reason. It
// tells why a phase timed out when the kind it watches has no reason of its own.
func (r *run) notReady(ctx context.Context) (map[string]int, error) {
	counts := map[string]int{}
	for _, gvk := range pipelineGVKs {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
		if err := r.c.client.List(ctx, list, client.InNamespace(r.w.namespace), client.MatchingLabels{runLabel: r.w.runID}); err != nil {
			return counts, fmt.Errorf("listing %s: %w", gvk.Kind, err)
		}
		for i := range list.Items {
			s := readyState(&list.Items[i])
			if s.ready {
				continue
			}
			reason := s.reason
			if reason == "" {
				reason = "unknown"
			}
			counts[gvk.Kind+"/"+reason]++
		}
	}
	return counts, nil
}

func (r *run) environment() environment {
	env := environment{Node: r.pod.Spec.NodeName, KubernetesVersion: r.c.serverVersion(), ControllerPod: r.pod.Name}
	for _, c := range r.pod.Spec.Containers {
		if c.Name == managerContainer {
			env.ControllerImage = c.Image
			env.ControllerArgs = c.Args
			env.ControllerResources = c.Resources
		}
	}
	for _, cs := range r.pod.Status.ContainerStatuses {
		if cs.Name == managerContainer {
			env.ControllerImageID = cs.ImageID
		}
	}
	return env
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
