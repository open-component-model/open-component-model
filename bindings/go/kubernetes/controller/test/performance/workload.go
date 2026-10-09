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
	// scenarioVersions makes every Component pick the latest of many versions
	// through a semver constraint.
	scenarioVersions scenario = "versions"
	// scenarioNested resolves each Resource through a chain of component references.
	scenarioNested scenario = "nested"
	// scenarioRepositories gives every Component its own Repository, each a
	// different sub-path of the one registry.
	scenarioRepositories scenario = "repositories"
	// scenarioComplex combines the others: pipelines over a few Repositories,
	// with large descriptors, several versions and a reference chain each.
	scenarioComplex scenario = "complex"
)

var allScenarios = []scenario{
	scenarioCold, scenarioShared, scenarioPipeline, scenarioUpdate,
	scenarioVersions, scenarioNested, scenarioRepositories, scenarioComplex,
}

const (
	initialVersion = "1.0.0"
	updatedVersion = "1.1.0"
	// A constraint, unlike an exact version, makes the controller list versions.
	versionConstraint = ">=" + initialVersion
	defaultVersions   = 100
	defaultDepth      = 5
	defaultResources  = 100
	// Fixed shape of scenarioComplex; only its descriptor size and chain length are flags.
	complexVersions     = 5
	complexRepositories = 10
	imagePoolSize       = 10
	repositoryName      = "registry"
	referenceName       = "child"
	runLabel            = "perf.ocm.software/run"
	// The benchmark measures one burst of reconciles; a long interval keeps
	// periodic requeues out of the measurement window.
	reconcileInterval = 30 * time.Minute
)

var (
	componentGVK = v1alpha1.GroupVersion.WithKind("Component")
	resourceGVK  = v1alpha1.GroupVersion.WithKind("Resource")
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
	// versions is the number of versions per component in scenarioVersions.
	versions int
	// depth is the length of the reference chain in scenarioNested and scenarioComplex.
	depth int
	// resources is the number of resources per root component in scenarioComplex.
	resources int
}

func newWorkload(s scenario, objects int, runID, registryURL string) workload {
	return workload{
		scenario:    s,
		objects:     objects,
		runID:       runID,
		namespace:   "ocm-perf-" + runID,
		registryURL: registryURL,
		versions:    defaultVersions,
		depth:       defaultDepth,
		resources:   defaultResources,
	}
}

// sharesComponent reports whether every object resolves the same component.
func (w workload) sharesComponent() bool {
	return w.scenario == scenarioShared || w.scenario == scenarioVersions
}

func (w workload) hasResources() bool {
	return w.hasDeployers() || w.scenario == scenarioNested
}

func (w workload) hasDeployers() bool {
	return w.scenario == scenarioPipeline || w.scenario == scenarioUpdate || w.scenario == scenarioComplex
}

// hasReferenceChain reports whether the resource sits depth references below
// the component a Component object points at.
func (w workload) hasReferenceChain() bool {
	return w.scenario == scenarioNested || w.scenario == scenarioComplex
}

// usesConstraint reports whether Components select their version by constraint.
func (w workload) usesConstraint() bool {
	return w.scenario == scenarioVersions || w.scenario == scenarioComplex
}

// repositories is the number of Repository objects, one registry sub-path each.
func (w workload) repositories() int {
	switch w.scenario {
	case scenarioRepositories:
		return w.objects
	case scenarioComplex:
		return min(w.objects, complexRepositories)
	default:
		return 1
	}
}

func (w workload) componentName(i int) string {
	if w.sharesComponent() {
		i = 0
	}
	return fmt.Sprintf("ocm.software/perf/%s/c%04d", w.runID, i)
}

// nestedComponentName is the component at level of object i's reference
// chain; level 0 is the component the Component object points at.
func (w workload) nestedComponentName(i, level int) string {
	if level == 0 {
		return w.componentName(i)
	}
	return fmt.Sprintf("%s/n%02d", w.componentName(i), level)
}

// subPath separates the OCM repositories inside the registry when a scenario
// uses more than one; object i lives in repository i modulo their number.
func (w workload) subPath(i int) string {
	if w.repositories() == 1 {
		return ""
	}
	return fmt.Sprintf("perf-%s/r%04d", w.runID, i%w.repositories())
}

func (w workload) repositoryName(i int) string {
	if w.repositories() == 1 {
		return repositoryName
	}
	return fmt.Sprintf("%s-%04d", repositoryName, i%w.repositories())
}

// imagePool lists the images the complex scenario descriptors reference. A small
// shared pool keeps publishing cheap while every reference stays resolvable.
func (w workload) imagePool() []image {
	if w.scenario != scenarioComplex {
		return nil
	}
	pool := make([]image, imagePoolSize)
	for i := range pool {
		repo := fmt.Sprintf("perf/%s/image-%02d", w.runID, i)
		pool[i] = image{
			Repository: repo,
			Tag:        initialVersion,
			Reference:  fmt.Sprintf("%s/%s:%s", w.registryURL, repo, initialVersion),
		}
	}
	return pool
}

func (w workload) componentVersions() ([]componentVersion, error) {
	switch w.scenario {
	case scenarioNested:
		return w.nestedComponentVersions()
	case scenarioComplex:
		return w.complexComponentVersions()
	default:
	}

	unique := w.objects
	if w.sharesComponent() {
		unique = 1
	}
	versions := []string{initialVersion}
	switch w.scenario {
	case scenarioUpdate:
		versions = append(versions, updatedVersion)
	case scenarioVersions:
		versions = make([]string, w.versions)
		for patch := range versions {
			versions[patch] = fmt.Sprintf("1.0.%d", patch)
		}
	default:
	}

	cvs := make([]componentVersion, 0, unique*len(versions))
	for i := range unique {
		for _, v := range versions {
			m, err := w.manifest(i, v)
			if err != nil {
				return nil, err
			}
			cvs = append(cvs, componentVersion{
				Name:      w.componentName(i),
				Version:   v,
				SubPath:   w.subPath(i),
				Resources: []resource{{Name: resourceName, Blob: m}},
			})
		}
	}
	return cvs, nil
}

// nestedComponentVersions builds one chain per object: depth components that
// only reference the next one, and a leaf that carries the resource.
func (w workload) nestedComponentVersions() ([]componentVersion, error) {
	cvs := make([]componentVersion, 0, w.objects*(w.depth+1))
	for i := range w.objects {
		chain, err := w.chain(i, 0)
		if err != nil {
			return nil, err
		}
		cvs = append(cvs, chain...)
	}
	return cvs, nil
}

// complexComponentVersions gives every object a root component with several
// versions and a large descriptor, on top of the reference chain to the leaf.
func (w workload) complexComponentVersions() ([]componentVersion, error) {
	pool := w.imagePool()
	cvs := make([]componentVersion, 0, w.objects*(complexVersions+w.depth))
	for i := range w.objects {
		resources := make([]resource, w.resources)
		for j := range resources {
			resources[j] = resource{
				Name:           fmt.Sprintf("image-%04d", j),
				ImageReference: pool[(i+j)%len(pool)].Reference,
			}
		}
		for patch := range complexVersions {
			cvs = append(cvs, componentVersion{
				Name:       w.nestedComponentName(i, 0),
				Version:    fmt.Sprintf("1.0.%d", patch),
				SubPath:    w.subPath(i),
				Resources:  resources,
				References: []reference{w.childReference(i, 0)},
			})
		}
		chain, err := w.chain(i, 1)
		if err != nil {
			return nil, err
		}
		cvs = append(cvs, chain...)
	}
	return cvs, nil
}

// chain returns object i's reference chain from level down to the leaf.
func (w workload) chain(i, level int) ([]componentVersion, error) {
	var cvs []componentVersion
	for ; level < w.depth; level++ {
		cvs = append(cvs, componentVersion{
			Name:       w.nestedComponentName(i, level),
			Version:    initialVersion,
			SubPath:    w.subPath(i),
			References: []reference{w.childReference(i, level)},
		})
	}
	m, err := w.manifest(i, initialVersion)
	if err != nil {
		return nil, err
	}
	return append(cvs, componentVersion{
		Name:      w.nestedComponentName(i, w.depth),
		Version:   initialVersion,
		SubPath:   w.subPath(i),
		Resources: []resource{{Name: resourceName, Blob: m}},
	}), nil
}

func (w workload) childReference(i, level int) reference {
	return reference{Name: referenceName, Component: w.nestedComponentName(i, level+1), Version: initialVersion}
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

func (w workload) repository(i int) (*v1alpha1.Repository, error) {
	fields := map[string]string{"type": "OCIRegistry", "baseUrl": w.registryURL}
	if subPath := w.subPath(i); subPath != "" {
		fields["subPath"] = subPath
	}
	spec, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.Repository{
		ObjectMeta: w.meta(w.repositoryName(i)),
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{Raw: spec},
			Interval:       metav1.Duration{Duration: reconcileInterval},
		},
	}, nil
}

func (w workload) component(i int) *v1alpha1.Component {
	semver := initialVersion
	if w.usesConstraint() {
		semver = versionConstraint
	}
	return &v1alpha1.Component{
		ObjectMeta: w.meta(fmt.Sprintf("c-%04d", i)),
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: w.repositoryName(i)},
			Component:     w.componentName(i),
			Semver:        semver,
			Interval:      metav1.Duration{Duration: reconcileInterval},
		},
	}
}

func (w workload) resource(i int) *v1alpha1.Resource {
	ref := v1alpha1.ResourceReference{Resource: ocmruntime.Identity{"name": resourceName}}
	if w.hasReferenceChain() {
		for range w.depth {
			ref.ReferencePath = append(ref.ReferencePath, ocmruntime.Identity{"name": referenceName})
		}
	}
	return &v1alpha1.Resource{
		ObjectMeta: w.meta(fmt.Sprintf("r-%04d", i)),
		Spec: v1alpha1.ResourceSpec{
			ComponentRef: corev1.LocalObjectReference{Name: fmt.Sprintf("c-%04d", i)},
			Resource:     v1alpha1.ResourceID{ByReference: ref},
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

// objectsToCreate returns everything the run applies, Repositories first.
func (w workload) objectsToCreate() ([]client.Object, error) {
	objs := make([]client.Object, 0, w.repositories()+3*w.objects)
	for i := range w.repositories() {
		repo, err := w.repository(i)
		if err != nil {
			return nil, err
		}
		objs = append(objs, repo)
	}
	for i := range w.objects {
		objs = append(objs, w.component(i))
		if w.hasResources() {
			objs = append(objs, w.resource(i))
		}
		if w.hasDeployers() {
			objs = append(objs, w.deployer(i))
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
	case scenarioPipeline, scenarioComplex:
		return deployerGVK, readyState
	case scenarioUpdate:
		return configMapGVK, configMapVersionState(updatedVersion)
	case scenarioNested:
		return resourceGVK, readyState
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
