package main

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

type scenario string

const (
	// scenarioCold resolves one unique component version per Component.
	scenarioCold scenario = "cold"
	// scenarioShared deploys one component version once per tenant: every tenant
	// namespace has a pipeline that resolves it and deploys its one resource.
	scenarioShared scenario = "shared"
	// scenarioUpdate moves every Repository -> Component -> Resource -> Deployer
	// pipeline from initialVersion to updatedVersion.
	scenarioUpdate scenario = "update"
	// scenarioComplex runs a signed pipeline per object over a few Repositories,
	// with large descriptors, several versions and a reference chain each. After
	// the rollout, its drift phase changes every deployed ConfigMap.
	scenarioComplex scenario = "complex"
	// scenarioFanout points many Resources at each Component, one per entry
	// of a large descriptor, and stops at the Resources.
	scenarioFanout scenario = "fanout"
)

var allScenarios = []scenario{scenarioCold, scenarioShared, scenarioUpdate, scenarioComplex, scenarioFanout}

const (
	initialVersion = "1.0.0"
	updatedVersion = "1.1.0"
	// driftedVersion is what the drift phase writes into the deployed ConfigMaps.
	driftedVersion = "drifted"
	// A constraint, unlike an exact version, makes the controller list versions.
	versionConstraint   = ">=" + initialVersion
	defaultVersions     = 5
	defaultDepth        = 5
	defaultResources    = 100
	defaultFanout       = 20
	defaultRepositories = 10
	imagePoolSize       = 10
	repositoryName      = "registry"
	referenceName       = "child"
	runLabel            = "perf.ocm.software/run"
	signingSecretName   = "perf-signing"
	deployerAccount     = "perf-deployer"
	// The benchmark measures one burst of reconciles; a long interval keeps
	// periodic requeues out of the measurement window.
	reconcileInterval = 30 * time.Minute
)

var (
	componentGVK = v1alpha1.GroupVersion.WithKind("Component")
	resourceGVK  = v1alpha1.GroupVersion.WithKind("Resource")
	deployerGVK  = v1alpha1.GroupVersion.WithKind(v1alpha1.KindNamespacedDeployer)
	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
)

// workload describes the objects and component versions of one run.
type workload struct {
	scenario    scenario
	objects     int
	runID       string
	namespace   string
	registryURL string
	// versions is the number of versions per root component in scenarioComplex.
	versions int
	// depth is the length of the reference chain in scenarioComplex; 0 puts the manifest into the root.
	depth int
	// resources is the number of resources per root component in scenarioComplex and scenarioFanout.
	resources int
	// fanout is the number of Resource objects per Component in scenarioFanout.
	fanout int
	// repositoryCount caps the Repository objects in scenarioComplex.
	repositoryCount int
	// sign makes scenarioComplex sign every component version and verify the roots.
	sign bool
	// publicKeyPEM is the key the controller verifies signatures with; set when sign is.
	publicKeyPEM string
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
		fanout:      defaultFanout,
		// The repository count and signing only shape scenarioComplex.
		repositoryCount: defaultRepositories,
		sign:            true,
	}
}

// sharesComponent reports whether every object resolves the same component.
func (w workload) sharesComponent() bool {
	return w.scenario == scenarioShared
}

// tenants reports whether every object lives in a namespace of its own. Deployers
// that deploy one digest apply the same object, which only works in separate namespaces.
func (w workload) tenants() bool {
	return w.scenario == scenarioShared
}

// namespaces lists every namespace of the run.
func (w workload) namespaces() []string {
	if !w.tenants() {
		return []string{w.namespace}
	}
	namespaces := make([]string, w.objects)
	for i := range namespaces {
		namespaces[i] = w.namespaceOf(i)
	}
	return namespaces
}

// namespaceOf is the namespace of object i.
func (w workload) namespaceOf(i int) string {
	if w.tenants() {
		return fmt.Sprintf("%s-t%04d", w.namespace, i)
	}
	return w.namespace
}

func (w workload) hasResources() bool {
	return w.hasDeployers() || w.scenario == scenarioFanout
}

// hasImages reports whether descriptors list OCI images from the image pool.
func (w workload) hasImages() bool {
	return w.scenario == scenarioComplex || w.scenario == scenarioFanout
}

// resourcesPerComponent is the number of Resource objects that select from one Component.
func (w workload) resourcesPerComponent() int {
	if w.scenario == scenarioFanout {
		return w.fanout
	}
	return 1
}

// targets is the number of objects whose readiness ends the measurement.
func (w workload) targets() int {
	if w.scenario == scenarioFanout {
		return w.objects * w.fanout
	}
	return w.objects
}

func (w workload) hasDeployers() bool {
	return w.scenario == scenarioUpdate || w.scenario == scenarioComplex || w.scenario == scenarioShared
}

// driftedConfigMaps lists the deployed ConfigMaps the drift phase changes. Only
// scenarioComplex has a drift phase.
func (w workload) driftedConfigMaps() []client.ObjectKey {
	if w.scenario != scenarioComplex {
		return nil
	}
	keys := make([]client.ObjectKey, w.objects)
	for i := range keys {
		keys[i] = client.ObjectKey{Namespace: w.namespace, Name: configMapName(i)}
	}
	return keys
}

func configMapName(i int) string {
	return fmt.Sprintf("cm-%04d", i)
}

// hasReferenceChain reports whether the resource sits depth references below
// the component a Component object points at.
func (w workload) hasReferenceChain() bool {
	return w.scenario == scenarioComplex
}

// usesConstraint reports whether Components select their version by constraint.
func (w workload) usesConstraint() bool {
	return w.scenario == scenarioComplex
}

// signed reports whether component versions carry signatures the controller verifies.
func (w workload) signed() bool {
	return w.scenario == scenarioComplex && w.sign
}

// repositories is the number of Repository objects, one registry sub-path each.
func (w workload) repositories() int {
	if w.scenario == scenarioComplex {
		return min(w.objects, w.repositoryCount)
	}
	return 1
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

// imagePool lists the images the complex and fanout descriptors reference. A small
// shared pool keeps publishing cheap while every reference stays resolvable.
func (w workload) imagePool() []image {
	if !w.hasImages() {
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
	case scenarioComplex:
		return w.complexComponentVersions()
	case scenarioFanout:
		return w.fanoutComponentVersions(), nil
	default:
	}

	unique := w.objects
	if w.sharesComponent() {
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

// complexComponentVersions gives every object a root component with several
// versions and a large descriptor, on top of the reference chain to the leaf.
// With a depth of 0 the root carries the manifest itself.
func (w workload) complexComponentVersions() ([]componentVersion, error) {
	pool := w.imagePool()
	cvs := make([]componentVersion, 0, w.objects*(w.versions+w.depth))
	for i := range w.objects {
		resources := w.imageResources(i, pool)
		var references []reference
		var chain []componentVersion
		if w.depth == 0 {
			m, err := w.manifest(i, initialVersion)
			if err != nil {
				return nil, err
			}
			resources = append(resources, resource{Name: resourceName, Blob: m})
		} else {
			references = []reference{w.childReference(i, 0)}
			var err error
			if chain, err = w.chain(i, 1); err != nil {
				return nil, err
			}
		}
		for patch := range w.versions {
			cvs = append(cvs, componentVersion{
				Name:       w.nestedComponentName(i, 0),
				Version:    fmt.Sprintf("1.0.%d", patch),
				SubPath:    w.subPath(i),
				Resources:  resources,
				References: references,
			})
		}
		cvs = append(cvs, chain...)
	}
	return cvs, nil
}

// fanoutComponentVersions gives every object one component version whose
// descriptor is large and holds nothing but the images the Resources select.
func (w workload) fanoutComponentVersions() []componentVersion {
	pool := w.imagePool()
	cvs := make([]componentVersion, w.objects)
	for i := range cvs {
		cvs[i] = componentVersion{
			Name:      w.componentName(i),
			Version:   initialVersion,
			SubPath:   w.subPath(i),
			Resources: w.imageResources(i, pool),
		}
	}
	return cvs
}

func (w workload) imageResources(i int, pool []image) []resource {
	resources := make([]resource, w.resources)
	for j := range resources {
		resources[j] = resource{
			Name:           imageResourceName(j),
			ImageReference: pool[(i+j)%len(pool)].Reference,
		}
	}
	return resources
}

func imageResourceName(j int) string {
	return fmt.Sprintf("image-%04d", j)
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
// the update scenario and the drift phase can observe the rollout in the cluster itself.
func (w workload) manifest(i int, version string) ([]byte, error) {
	name, namespace := configMapName(i), w.namespace
	if w.tenants() {
		// One digest for all tenants needs one content, so each Deployer defaults the namespace.
		name, namespace = "cm-shared", ""
	}
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{runLabel: w.runID},
		},
		Data: map[string]string{"version": version},
	}
	return yaml.Marshal(cm)
}

// target is the kind whose readiness ends the measurement, and the predicate
// that decides readiness.
func (w workload) target() (schema.GroupVersionKind, func(*unstructured.Unstructured) objectState) {
	switch w.scenario {
	case scenarioComplex, scenarioShared:
		return deployerGVK, readyState
	case scenarioUpdate:
		return configMapGVK, configMapVersionState(updatedVersion)
	case scenarioFanout:
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
