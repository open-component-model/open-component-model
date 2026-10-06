package main

import (
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

type scenario string

const (
	// scenarioCold resolves one unique component version per Component.
	scenarioCold scenario = "cold"
	// scenarioShared points every Component at the same component version.
	scenarioShared scenario = "shared"
	// scenarioPipeline runs Repository -> Component -> Resource -> Deployer per object.
	scenarioPipeline scenario = "pipeline"
	// scenarioUpdate moves every pipeline from initialVersion to updatedVersion.
	scenarioUpdate scenario = "update"
)

var allScenarios = []scenario{scenarioCold, scenarioShared, scenarioPipeline, scenarioUpdate}

const (
	initialVersion = "1.0.0"
	updatedVersion = "1.1.0"
	repositoryName = "registry"
	runLabel       = "perf.ocm.software/run"
	// The benchmark measures one burst of reconciles; a long interval keeps
	// periodic requeues out of the measurement window.
	reconcileInterval = 30 * time.Minute
)

var (
	componentGVK = v1alpha1.GroupVersion.WithKind("Component")
	deployerGVK  = v1alpha1.GroupVersion.WithKind("Deployer")
	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
)

// workload describes the objects and component versions of one run.
type workload struct {
	scenario    scenario
	objects     int
	runID       string
	namespace   string
	registryURL string
}

func newWorkload(s scenario, objects int, runID, registryURL string) workload {
	return workload{
		scenario:    s,
		objects:     objects,
		runID:       runID,
		namespace:   "ocm-perf-" + runID,
		registryURL: registryURL,
	}
}

func (w workload) componentName(i int) string {
	if w.scenario == scenarioShared {
		i = 0
	}
	return fmt.Sprintf("ocm.software/perf/%s/c%04d", w.runID, i)
}

func (w workload) componentVersions() ([]componentVersion, error) {
	unique := w.objects
	if w.scenario == scenarioShared {
		unique = 1
	}
	versions := []string{initialVersion}
	if w.scenario == scenarioUpdate {
		versions = append(versions, updatedVersion)
	}

	cvs := make([]componentVersion, 0, unique*len(versions))
	for i := range unique {
		for _, v := range versions {
			m, err := w.manifest(i, v)
			if err != nil {
				return nil, err
			}
			cvs = append(cvs, componentVersion{Name: w.componentName(i), Version: v, Manifest: m})
		}
	}
	return cvs, nil
}

// manifest is the payload a Deployer applies. Its data carries the version so
// the update scenario can observe the rollout in the cluster itself.
func (w workload) manifest(i int, version string) ([]byte, error) {
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cm-%04d", i),
			Namespace: w.namespace,
			Labels:    map[string]string{runLabel: w.runID},
		},
		Data: map[string]string{"version": version},
	}
	return yaml.Marshal(cm)
}

func (w workload) repository() (*v1alpha1.Repository, error) {
	spec, err := json.Marshal(map[string]string{"type": "OCIRegistry", "baseUrl": w.registryURL})
	if err != nil {
		return nil, err
	}
	return &v1alpha1.Repository{
		ObjectMeta: w.meta(repositoryName),
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{Raw: spec},
			Interval:       metav1.Duration{Duration: reconcileInterval},
		},
	}, nil
}

func (w workload) component(i int) *v1alpha1.Component {
	return &v1alpha1.Component{
		ObjectMeta: w.meta(fmt.Sprintf("c-%04d", i)),
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: repositoryName},
			Component:     w.componentName(i),
			Semver:        initialVersion,
			Interval:      metav1.Duration{Duration: reconcileInterval},
		},
	}
}

func (w workload) resource(i int) *v1alpha1.Resource {
	return &v1alpha1.Resource{
		ObjectMeta: w.meta(fmt.Sprintf("r-%04d", i)),
		Spec: v1alpha1.ResourceSpec{
			ComponentRef: corev1.LocalObjectReference{Name: fmt.Sprintf("c-%04d", i)},
			Resource: v1alpha1.ResourceID{
				ByReference: v1alpha1.ResourceReference{Resource: ocmruntime.Identity{"name": resourceName}},
			},
		},
	}
}

// deployer is cluster-scoped, so its name carries the run ID and namespace
// deletion does not remove it; see cluster.deleteDeployers.
func (w workload) deployer(i int) *v1alpha1.Deployer {
	meta := w.meta(fmt.Sprintf("%s-d-%04d", w.runID, i))
	meta.Namespace = ""
	return &v1alpha1.Deployer{
		ObjectMeta: meta,
		Spec: v1alpha1.DeployerSpec{
			ResourceRef: v1alpha1.ObjectKey{Namespace: w.namespace, Name: fmt.Sprintf("r-%04d", i)},
		},
	}
}

// objectsToCreate returns everything the run applies, Repository first.
func (w workload) objectsToCreate() ([]client.Object, error) {
	repo, err := w.repository()
	if err != nil {
		return nil, err
	}
	objs := []client.Object{repo}
	for i := range w.objects {
		objs = append(objs, w.component(i))
		if w.scenario == scenarioPipeline || w.scenario == scenarioUpdate {
			objs = append(objs, w.resource(i), w.deployer(i))
		}
	}
	return objs, nil
}

func (w workload) meta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: w.namespace, Labels: map[string]string{runLabel: w.runID}}
}

// target is the kind whose readiness ends the measurement, and the predicate
// that decides readiness.
func (w workload) target() (schema.GroupVersionKind, func(*unstructured.Unstructured) objectState) {
	switch w.scenario {
	case scenarioPipeline:
		return deployerGVK, readyState
	case scenarioUpdate:
		return configMapGVK, configMapVersionState(updatedVersion)
	default:
		return componentGVK, readyState
	}
}

// objectState classifies a tracked object. failed means Ready=False at the
// time of observation; the controller may still recover it.
type objectState struct {
	ready  bool
	failed bool
	reason string
}

func readyState(u *unstructured.Unstructured) objectState {
	conditions, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	observed, _, _ := unstructured.NestedInt64(u.Object, "status", "observedGeneration")
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok || cond["type"] != v1alpha1.ReadyCondition {
			continue
		}
		reason, _ := cond["reason"].(string)
		switch cond["status"] {
		case string(metav1.ConditionTrue):
			return objectState{ready: observed >= u.GetGeneration(), reason: reason}
		case string(metav1.ConditionFalse):
			return objectState{failed: true, reason: reason}
		}
	}
	return objectState{}
}

func configMapVersionState(version string) func(*unstructured.Unstructured) objectState {
	return func(u *unstructured.Unstructured) objectState {
		v, _, _ := unstructured.NestedString(u.Object, "data", "version")
		return objectState{ready: v == version}
	}
}
