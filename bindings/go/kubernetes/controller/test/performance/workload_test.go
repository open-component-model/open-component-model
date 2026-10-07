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
		{scenario: scenarioVersions, wantVersions: defaultVersions, wantUniqueCVs: 1},
		{scenario: scenarioNested, wantVersions: 5 * (defaultDepth + 1), wantUniqueCVs: 5 * (defaultDepth + 1)},
		{scenario: scenarioRepositories, wantVersions: 5, wantUniqueCVs: 5},
		{scenario: scenarioComplex, wantVersions: 5 * (complexVersions + defaultDepth), wantUniqueCVs: 5 * (defaultDepth + 1)},
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
		{scenario: scenarioVersions, want: 1 + 3},
		{scenario: scenarioNested, want: 1 + 3*2},
		{scenario: scenarioRepositories, want: 3 + 3},
		{scenario: scenarioComplex, want: 3 + 3*3},
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

func TestWorkloadComplex(t *testing.T) {
	r := require.New(t)
	w := newWorkload(scenarioComplex, complexRepositories+2, "run", "http://registry:5000")
	w.depth, w.resources = 2, 7

	cvs, err := w.componentVersions()
	r.NoError(err)
	byName := map[string]componentVersion{}
	for _, cv := range cvs {
		byName[cv.Name] = cv
	}
	pool := map[string]bool{}
	for _, img := range w.imagePool() {
		pool[img.Reference] = true
	}

	objs, err := w.objectsToCreate()
	r.NoError(err)
	r.IsType(&v1alpha1.Repository{}, objs[complexRepositories-1])
	r.IsType(&v1alpha1.Component{}, objs[complexRepositories], "objects share a fixed number of Repositories")

	// The last object wraps around onto the second Repository.
	i := complexRepositories + 1
	r.Equal(w.repositoryName(1), w.component(i).Spec.RepositoryRef.Name)
	r.Equal(versionConstraint, w.component(i).Spec.Semver)

	root := byName[w.component(i).Spec.Component]
	r.Equal(w.subPath(1), root.SubPath)
	r.Len(root.Resources, 7)
	for _, res := range root.Resources {
		r.True(pool[res.ImageReference], "every image reference must point into the pushed pool")
	}

	// The reference path leads from the root to the leaf, all in one repository.
	res := w.resource(i)
	r.Len(res.Spec.Resource.ByReference.ReferencePath, 2)
	cv := root
	for range res.Spec.Resource.ByReference.ReferencePath {
		r.Len(cv.References, 1)
		cv = byName[cv.References[0].Component]
		r.Equal(root.SubPath, cv.SubPath)
	}
	r.Equal(res.Spec.Resource.ByReference.Resource["name"], cv.Resources[0].Name)
}

func TestWorkloadNested(t *testing.T) {
	r := require.New(t)
	w := newWorkload(scenarioNested, 2, "run", "http://registry:5000")
	w.depth = 3

	cvs, err := w.componentVersions()
	r.NoError(err)
	byName := map[string]componentVersion{}
	for _, cv := range cvs {
		byName[cv.Name] = cv
	}

	// Walking the Resource's reference path from the root must end at the leaf.
	res := w.resource(1)
	r.Len(res.Spec.Resource.ByReference.ReferencePath, 3)
	cv := byName[w.component(1).Spec.Component]
	for _, step := range res.Spec.Resource.ByReference.ReferencePath {
		r.Empty(cv.Resources)
		r.Len(cv.References, 1)
		r.Equal(step["name"], cv.References[0].Name)
		cv = byName[cv.References[0].Component]
	}
	r.Len(cv.Resources, 1)
	r.Equal(res.Spec.Resource.ByReference.Resource["name"], cv.Resources[0].Name)
}

func TestWorkloadVersions(t *testing.T) {
	r := require.New(t)
	w := newWorkload(scenarioVersions, 3, "run", "http://registry:5000")
	w.versions = 4

	cvs, err := w.componentVersions()
	r.NoError(err)
	versions := make([]string, len(cvs))
	for i, cv := range cvs {
		versions[i] = cv.Version
	}
	r.Equal([]string{"1.0.0", "1.0.1", "1.0.2", "1.0.3"}, versions)
	r.Equal(versionConstraint, w.component(2).Spec.Semver)
	r.Equal(initialVersion, newWorkload(scenarioCold, 3, "run", "").component(2).Spec.Semver)
}

func TestWorkloadRepositories(t *testing.T) {
	r := require.New(t)
	w := newWorkload(scenarioRepositories, 2, "run", "http://registry:5000")

	cvs, err := w.componentVersions()
	r.NoError(err)
	for i, cv := range cvs {
		repo, err := w.repository(i)
		r.NoError(err)
		r.Equal(repo.Name, w.component(i).Spec.RepositoryRef.Name)
		// The Repository must point at the sub-path its component version was published to.
		r.JSONEq(`{"type":"OCIRegistry","baseUrl":"http://registry:5000","subPath":"`+cv.SubPath+`"}`,
			string(repo.Spec.RepositorySpec.Raw))
	}
	r.NotEqual(cvs[0].SubPath, cvs[1].SubPath)

	single, err := newWorkload(scenarioCold, 2, "run", "http://registry:5000").repository(0)
	r.NoError(err)
	r.NotContains(string(single.Spec.RepositorySpec.Raw), "subPath")
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
