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

const (
	initialVersion = "1.0.0"
	// driftedVersion is what the drift phase writes into the deployed ConfigMaps.
	driftedVersion = "drifted"
	// A constraint, unlike an exact version, makes the controller list versions.
	versionConstraint   = ">=" + initialVersion
	defaultVersions     = 5
	defaultDepth        = 5
	defaultResources    = 100
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
	deployerGVK  = v1alpha1.GroupVersion.WithKind(v1alpha1.KindNamespacedDeployer)
	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
)

// workload describes the objects and component versions of one run: a signed pipeline per
// object over a few Repositories, with large descriptors, several versions and a reference
// chain each, in one namespace.
type workload struct {
	objects     int
	runID       string
	namespace   string
	registryURL string
	// versions is the number of versions per root component.
	versions int
	// depth is the length of the reference chain; 0 puts the manifest into the root.
	depth int
	// resources is the number of image resources per root component.
	resources int
	// repositoryCount caps the Repository objects.
	repositoryCount int
	// sign makes the run sign every component version and verify the roots.
	sign bool
	// publicKeyPEM is the key the controller verifies signatures with; set when sign is.
	publicKeyPEM string
}

func newWorkload(objects int, runID, registryURL string) workload {
	return workload{
		objects:         objects,
		runID:           runID,
		namespace:       "ocm-perf-" + runID,
		registryURL:     registryURL,
		versions:        defaultVersions,
		depth:           defaultDepth,
		resources:       defaultResources,
		repositoryCount: defaultRepositories,
		sign:            true,
	}
}

// configMaps lists the ConfigMaps the Deployers deploy, one per pipeline.
func (w workload) configMaps() []client.ObjectKey {
	keys := make([]client.ObjectKey, w.objects)
	for i := range keys {
		keys[i] = client.ObjectKey{Namespace: w.namespace, Name: configMapName(i)}
	}
	return keys
}

func configMapName(i int) string {
	return fmt.Sprintf("cm-%04d", i)
}

// repositories is the number of Repository objects, one registry sub-path each.
func (w workload) repositories() int {
	return min(w.objects, w.repositoryCount)
}

func (w workload) componentName(i int) string {
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

// subPath separates the OCM repositories inside the registry when there is more
// than one; object i lives in repository i modulo their number.
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

// imagePool lists the images the descriptors reference. A small shared pool
// keeps publishing cheap while every reference stays resolvable.
func (w workload) imagePool() []image {
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

// componentVersions gives every object a root component with several versions and
// a large descriptor, on top of the reference chain to the leaf. With a depth of 0
// the root carries the manifest itself.
func (w workload) componentVersions() ([]componentVersion, error) {
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
// the drift phase can observe the rollout in the cluster itself.
func (w workload) manifest(i int, version string) ([]byte, error) {
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName(i),
			Namespace: w.namespace,
			Labels:    map[string]string{runLabel: w.runID},
		},
		Data: map[string]string{"version": version},
	}
	return yaml.Marshal(cm)
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
