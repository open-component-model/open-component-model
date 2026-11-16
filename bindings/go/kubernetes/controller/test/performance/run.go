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

// pipelineGVKs are the controller kinds of a pipeline, listed when a phase times out.
var pipelineGVKs = []schema.GroupVersionKind{
	v1alpha1.GroupVersion.WithKind("Component"),
	v1alpha1.GroupVersion.WithKind("Resource"),
	deployerGVK,
}

// runner runs the benchmark invocation: every object count, every repeat.
type runner struct {
	cluster *cluster
	opts    options
	outDir  string
}

// run is one benchmark run: one workload, measured phase by phase.
type run struct {
	*runner
	workload workload
	// publisher is one per run: the image digests pushed for the rollout are needed again by later phases.
	publisher *publisher
	// tempDir is where the publisher stages component versions, removed by close.
	tempDir string
	// controllerPod is the pod measured; a different one later means the controller restarted.
	controllerPod *corev1.Pod
	// objectsToCreate are the run's objects, created by the rollout.
	objectsToCreate []client.Object
	log             *slog.Logger
	// resultDir holds result.json and a directory of raw data per phase.
	resultDir string
}

// runOnce sets up a run, measures its phases in order until one does not complete, and cleans up.
func (r *runner) runOnce(ctx context.Context, objects, repeat int) (*result, error) {
	benchRun, err := r.newRun(ctx, objects, repeat)
	if err != nil {
		return nil, err
	}
	defer benchRun.close()
	res := benchRun.result(repeat)

	if err := r.cluster.createNamespace(ctx, benchRun.workload.namespace, benchRun.workload.runID); err != nil {
		return nil, fmt.Errorf("creating namespace: %w", err)
	}
	if !r.opts.keepNamespaces {
		defer benchRun.cleanup(context.WithoutCancel(ctx))
	}

	for _, p := range benchRun.phases() {
		measured, err := benchRun.measure(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.name, err)
		}
		res.Phases = append(res.Phases, measured.phaseResult)
		// Written after every phase, so a later failure keeps what was measured.
		if err := errors.Join(measured.raw.write(filepath.Join(benchRun.resultDir, p.name)), writeJSON(filepath.Join(benchRun.resultDir, "result.json"), res)); err != nil {
			return nil, err
		}
		if !measured.Outcome.complete() {
			benchRun.log.Warn("skipping the remaining phases", "after", p.name)
			break
		}
	}
	benchRun.log.Info("run finished", "phases", len(res.Phases))
	return res, nil
}

// newRun prepares everything a run's phases share: the signing key, the publisher, a
// controller with empty caches, and the objects to create. Nothing in it is measured.
func (r *runner) newRun(ctx context.Context, objects, repeat int) (_ *run, err error) {
	runID := fmt.Sprintf("n%d-r%d-%s", objects, repeat, randomSuffix())
	w := newWorkload(objects, runID, r.opts.clusterRegistryURL)
	w.versions, w.depth, w.resources = r.opts.versions, r.opts.depth, r.opts.resources
	w.repositoryCount, w.sign = r.opts.repositories, r.opts.sign
	benchRun := &run{runner: r, workload: w, log: slog.With("run", runID), resultDir: filepath.Join(r.outDir, runID)}
	if err := os.MkdirAll(benchRun.resultDir, 0o750); err != nil {
		return nil, err
	}

	var sgn *signer
	if w.sign {
		if sgn, err = newSigner(); err != nil {
			return nil, err
		}
		benchRun.workload.publicKeyPEM = sgn.publicKeyPEM
	}
	if benchRun.tempDir, err = os.MkdirTemp("", "ocm-perf-"); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			benchRun.close()
		}
	}()
	if benchRun.publisher, err = newPublisher(r.opts.registryURL, benchRun.tempDir, sgn); err != nil {
		return nil, err
	}

	if r.opts.restartController {
		benchRun.log.Info("restarting controller")
		if err := r.cluster.restartController(ctx, r.opts.timeout); err != nil {
			return nil, fmt.Errorf("restarting controller: %w", err)
		}
	}
	if benchRun.controllerPod, err = r.cluster.controllerPod(ctx); err != nil {
		return nil, err
	}
	if benchRun.objectsToCreate, err = benchRun.workload.objectsToCreate(); err != nil {
		return nil, err
	}
	return benchRun, nil
}

// close removes what the run kept on the local disk.
func (r *run) close() {
	_ = os.RemoveAll(r.tempDir)
}

func (r *run) result(repeat int) *result {
	return &result{
		Objects: r.workload.objects, Repeat: repeat, RunID: r.workload.runID, StartedAt: time.Now().UTC(),
		Versions: r.workload.versions, Depth: r.workload.depth, Resources: r.workload.resources, Repositories: r.workload.repositories(), Signed: r.workload.sign,
		Environment: r.environment(),
	}
}

// cleanup deletes the run's namespace and waits until it is gone, so the next run starts
// without finalizer work of this one.
func (r *run) cleanup(ctx context.Context) {
	r.log.Info("cleaning up")
	start := time.Now()
	if err := r.cluster.deleteNamespace(ctx, r.workload.namespace, r.opts.timeout); err != nil {
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
		if err := r.cluster.client.List(ctx, list, client.InNamespace(r.workload.namespace), client.MatchingLabels{runLabel: r.workload.runID}); err != nil {
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
	env := environment{Node: r.controllerPod.Spec.NodeName, KubernetesVersion: r.cluster.serverVersion(), ControllerPod: r.controllerPod.Name}
	for _, c := range r.controllerPod.Spec.Containers {
		if c.Name == managerContainer {
			env.ControllerImage = c.Image
			env.ControllerArgs = c.Args
			env.ControllerResources = c.Resources
		}
	}
	for _, cs := range r.controllerPod.Status.ContainerStatuses {
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
