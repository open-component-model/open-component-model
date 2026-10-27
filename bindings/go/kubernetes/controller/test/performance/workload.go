package main

import (
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	// scenarioUpdate moves every Repository -> Component -> Resource -> Deployer
	// pipeline from initialVersion to updatedVersion.
	scenarioUpdate scenario = "update"
	// scenarioComplex runs a signed pipeline per object over a few Repositories,
	// with large descriptors, several versions and a reference chain each.
	scenarioComplex scenario = "complex"
	// scenarioFanout points many Resources at each Component, one per entry
	// of a large descriptor, and stops at the Resources.
	scenarioFanout scenario = "fanout"
)

var allScenarios = []scenario{scenarioCold, scenarioShared, scenarioUpdate, scenarioComplex, scenarioFanout}

const (
	initialVersion = "1.0.0"
	updatedVersion = "1.1.0"
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
	return w.scenario == scenarioUpdate || w.scenario == scenarioComplex
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
	c := &v1alpha1.Component{
		ObjectMeta: w.meta(fmt.Sprintf("c-%04d", i)),
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: w.repositoryName(i)},
			Component:     w.componentName(i),
			Semver:        semver,
			Interval:      metav1.Duration{Duration: reconcileInterval},
		},
	}
	if w.signed() {
		// Propagated, so the Resource and Deployer verify the root as well.
		c.Spec.OCMConfig = []v1alpha1.OCMConfiguration{{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
				APIVersion: "v1", Kind: "Secret", Name: signingSecretName,
			},
			Policy: v1alpha1.ConfigurationPolicyPropagate,
		}}
	}
	return c
}

// signingSecret holds the OCM config that makes the controller verify the
// signatures named signatureName with the run's public key.
func (w workload) signingSecret() (*corev1.Secret, error) {
	cfg, err := verificationConfig(w.publicKeyPEM)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: w.meta(signingSecretName),
		StringData: map[string]string{v1alpha1.OCMConfigKey: cfg},
	}, nil
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

// fanoutResource is the j-th Resource of object i. It wraps around when
// there are more Resources than descriptor entries.
func (w workload) fanoutResource(i, j int) *v1alpha1.Resource {
	return &v1alpha1.Resource{
		ObjectMeta: w.meta(fmt.Sprintf("r-%04d-%03d", i, j)),
		Spec: v1alpha1.ResourceSpec{
			ComponentRef: corev1.LocalObjectReference{Name: fmt.Sprintf("c-%04d", i)},
			Resource: v1alpha1.ResourceID{ByReference: v1alpha1.ResourceReference{
				Resource: ocmruntime.Identity{"name": imageResourceName(j % w.resources)},
			}},
		},
	}
}

func (w workload) deployer(i int) *v1alpha1.NamespacedDeployer {
	return &v1alpha1.NamespacedDeployer{
		ObjectMeta: w.meta(fmt.Sprintf("d-%04d", i)),
		Spec: v1alpha1.NamespacedDeployerSpec{
			ResourceRef:        corev1.LocalObjectReference{Name: fmt.Sprintf("r-%04d", i)},
			ServiceAccountName: deployerAccount,
		},
	}
}

// deployerRBAC is the service account the NamespacedDeployers apply the ConfigMaps as.
func (w workload) deployerRBAC() []client.Object {
	return []client.Object{
		&corev1.ServiceAccount{ObjectMeta: w.meta(deployerAccount)},
		&rbacv1.Role{
			ObjectMeta: w.meta(deployerAccount),
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: w.meta(deployerAccount),
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: deployerAccount},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: deployerAccount, Namespace: w.namespace}},
		},
	}
}

// objectsToCreate returns everything the run applies, the signing Secret, deployer RBAC and Repositories first.
func (w workload) objectsToCreate() ([]client.Object, error) {
	objs := make([]client.Object, 0, 4+w.repositories()+(2+w.resourcesPerComponent())*w.objects)
	if w.signed() {
		secret, err := w.signingSecret()
		if err != nil {
			return nil, err
		}
		objs = append(objs, secret)
	}
	if w.hasDeployers() {
		objs = append(objs, w.deployerRBAC()...)
	}
	for i := range w.repositories() {
		repo, err := w.repository(i)
		if err != nil {
			return nil, err
		}
		objs = append(objs, repo)
	}
	for i := range w.objects {
		objs = append(objs, w.component(i))
		switch {
		case w.scenario == scenarioFanout:
			for j := range w.fanout {
				objs = append(objs, w.fanoutResource(i, j))
			}
		case w.hasResources():
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
	case scenarioComplex:
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
