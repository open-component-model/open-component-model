package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func TestWorkloadComponentVersions(t *testing.T) {
	tests := []struct {
		scenario      scenario
		wantVersions  int
		wantUniqueCVs int
	}{
		{scenario: scenarioCold, wantVersions: 5, wantUniqueCVs: 5},
		{scenario: scenarioShared, wantVersions: 1, wantUniqueCVs: 1},
		{scenario: scenarioPipeline, wantVersions: 5, wantUniqueCVs: 5},
		{scenario: scenarioUpdate, wantVersions: 10, wantUniqueCVs: 5},
	}
	for _, tt := range tests {
		t.Run(string(tt.scenario), func(t *testing.T) {
			r := require.New(t)
			w := newWorkload(tt.scenario, 5, "run", "http://registry:5000")

			cvs, err := w.componentVersions()
			r.NoError(err)
			r.Len(cvs, tt.wantVersions)

			names := map[string]bool{}
			for _, cv := range cvs {
				names[cv.Name] = true
			}
			r.Len(names, tt.wantUniqueCVs)
		})
	}
}

func TestWorkloadObjectsToCreate(t *testing.T) {
	tests := []struct {
		scenario scenario
		want     int
	}{
		{scenario: scenarioCold, want: 1 + 3},
		{scenario: scenarioShared, want: 1 + 3},
		{scenario: scenarioPipeline, want: 1 + 3*3},
		{scenario: scenarioUpdate, want: 1 + 3*3},
	}
	for _, tt := range tests {
		t.Run(string(tt.scenario), func(t *testing.T) {
			r := require.New(t)
			w := newWorkload(tt.scenario, 3, "run", "http://registry:5000")

			objs, err := w.objectsToCreate()
			r.NoError(err)
			r.Len(objs, tt.want)
			r.IsType(&v1alpha1.Repository{}, objs[0], "the Repository must be created first")
			for _, o := range objs {
				r.Equal("run", o.GetLabels()[runLabel])
				if _, ok := o.(*v1alpha1.Deployer); ok {
					r.Empty(o.GetNamespace(), "Deployer is cluster-scoped")
					r.Contains(o.GetName(), "run", "cluster-scoped names must be unique per run")
					continue
				}
				r.Equal(w.namespace, o.GetNamespace())
			}
		})
	}
}

func TestWorkloadSharedComponentName(t *testing.T) {
	r := require.New(t)
	w := newWorkload(scenarioShared, 3, "run", "http://registry:5000")
	r.Equal(w.component(0).Spec.Component, w.component(2).Spec.Component)

	w = newWorkload(scenarioCold, 3, "run", "http://registry:5000")
	r.NotEqual(w.component(0).Spec.Component, w.component(2).Spec.Component)
}

func TestWorkloadManifest(t *testing.T) {
	r := require.New(t)
	w := newWorkload(scenarioPipeline, 3, "run", "http://registry:5000")

	raw, err := w.manifest(2, updatedVersion)
	r.NoError(err)

	var u unstructured.Unstructured
	r.NoError(yaml.Unmarshal(raw, &u.Object))
	r.Equal("ConfigMap", u.GetKind())
	r.Equal("cm-0002", u.GetName())
	r.Equal(w.namespace, u.GetNamespace())
	r.True(configMapVersionState(updatedVersion)(&u).ready)
	r.False(configMapVersionState(initialVersion)(&u).ready)
}

func TestReadyState(t *testing.T) {
	tests := []struct {
		name       string
		generation int64
		observed   int64
		conditions []any
		want       objectState
	}{
		{
			name:       "ready and observed",
			generation: 2, observed: 2,
			conditions: []any{map[string]any{"type": "Ready", "status": "True", "reason": "Succeeded"}},
			want:       objectState{ready: true, reason: "Succeeded"},
		},
		{
			name:       "ready for an older generation",
			generation: 3, observed: 2,
			conditions: []any{map[string]any{"type": "Ready", "status": "True", "reason": "Succeeded"}},
			want:       objectState{reason: "Succeeded"},
		},
		{
			name:       "not ready",
			generation: 1, observed: 1,
			conditions: []any{map[string]any{"type": "Ready", "status": "False", "reason": "GetComponentVersionFailed"}},
			want:       objectState{failed: true, reason: "GetComponentVersionFailed"},
		},
		{
			name:       "no ready condition",
			generation: 1, observed: 0,
			conditions: []any{map[string]any{"type": "Other", "status": "True"}},
			want:       objectState{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			u := &unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"generation": tt.generation},
				"status":   map[string]any{"observedGeneration": tt.observed, "conditions": tt.conditions},
			}}
			r.Equal(tt.want, readyState(u))
		})
	}
}

func TestTrackerSummary(t *testing.T) {
	r := require.New(t)
	tr := &tracker{
		expected: 4,
		state:    readyState,
		readyAt:  map[string]time.Time{},
		last:     map[string]objectState{},
		done:     make(chan struct{}),
	}
	tr.markStart()

	obj := func(name, status, reason string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": name},
			"status": map[string]any{"conditions": []any{
				map[string]any{"type": "Ready", "status": status, "reason": reason},
			}},
		}}
	}
	tr.observe(obj("a", "False", "Progressing"))
	tr.observe(obj("a", "True", "Succeeded"))
	tr.observe(obj("b", "True", "Succeeded"))
	// Once ready, a later failure does not undo the recorded time.
	tr.observe(obj("b", "False", "Broken"))
	tr.observe(obj("c", "False", "GetComponentVersionFailed"))

	sum := tr.summary()
	r.Equal(2, sum.ready)
	r.Equal(1, sum.failed)
	r.Equal(1, sum.pending, "d was never observed")
	r.Equal(map[string]int{"GetComponentVersionFailed": 1}, sum.failureReasons)
	r.Len(sum.latencies, 2)
	r.LessOrEqual(sum.latencies[0], sum.latencies[1])

	select {
	case <-tr.done:
		r.Fail("tracker must not finish before all objects are ready")
	default:
	}
	tr.observe(obj("c", "True", "Succeeded"))
	tr.observe(obj("d", "True", "Succeeded"))
	r.True(tr.wait(t.Context(), time.Second))
}
