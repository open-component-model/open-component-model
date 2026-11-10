package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func TestWorkloadObjectsToCreate(t *testing.T) {
	r := require.New(t)
	w := newWorkload(3, "run", "http://registry:5000")

	objs, err := w.objectsToCreate()
	r.NoError(err)
	// The signing Secret, the deployer RBAC, one Repository per object (fewer than the cap), and a pipeline each.
	r.Len(objs, 1+3+3+3*3)
	for _, o := range objs {
		r.Equal("run", o.GetLabels()[runLabel])
		r.Equal(w.namespace, o.GetNamespace(), "namespace deletion must clean up every object")
		if d, ok := o.(*v1alpha1.NamespacedDeployer); ok {
			r.Equal(deployerAccount, d.Spec.ServiceAccountName)
		}
	}
}

func TestWorkloadComplex(t *testing.T) {
	r := require.New(t)
	w := newWorkload(defaultRepositories+2, "run", "http://registry:5000")
	w.depth, w.resources, w.versions = 2, 7, 3

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
	r.IsType(&corev1.Secret{}, objs[0], "the signing Secret must exist before the Components that verify with it")
	r.IsType(&corev1.ServiceAccount{}, objs[1], "the deployer account must exist before the Deployers impersonate it")
	r.IsType(&v1alpha1.Repository{}, objs[3+defaultRepositories])
	r.IsType(&v1alpha1.Component{}, objs[3+defaultRepositories+1], "objects share a capped number of Repositories")

	// The last object wraps around onto the second Repository.
	i := defaultRepositories + 1
	r.Equal(w.repositoryName(1), w.component(i).Spec.RepositoryRef.Name)
	r.Equal(versionConstraint, w.component(i).Spec.Semver)
	r.Equal(signingSecretName, w.component(i).Spec.OCMConfig[0].Name)
	r.Equal(v1alpha1.ConfigurationPolicyPropagate, w.component(i).Spec.OCMConfig[0].Policy,
		"the Resource and Deployer inherit the verification config")

	repo, err := w.repository(i)
	r.NoError(err)
	r.JSONEq(`{"type":"OCIRegistry","baseUrl":"http://registry:5000","subPath":"`+w.subPath(1)+`"}`,
		string(repo.Spec.RepositorySpec.Raw), "the Repository must point at the sub-path its component versions were published to")

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

func TestWorkloadComplexWithoutChain(t *testing.T) {
	r := require.New(t)
	w := newWorkload(2, "run", "http://registry:5000")
	w.depth, w.resources, w.versions = 0, 3, 4

	cvs, err := w.componentVersions()
	r.NoError(err)
	r.Len(cvs, 2*4, "only the root versions, no chain")
	versions := map[string]bool{}
	for _, cv := range cvs {
		versions[cv.Version] = true
		r.Empty(cv.References)
		r.Len(cv.Resources, 3+1, "the images plus the manifest")
		r.Equal(resourceName, cv.Resources[3].Name)
	}
	r.Equal(map[string]bool{"1.0.0": true, "1.0.1": true, "1.0.2": true, "1.0.3": true}, versions)
	r.Empty(w.resource(1).Spec.Resource.ByReference.ReferencePath)
}

func TestWorkloadUnsigned(t *testing.T) {
	r := require.New(t)
	w := newWorkload(2, "run", "http://registry:5000")
	w.sign = false

	objs, err := w.objectsToCreate()
	r.NoError(err)
	for _, o := range objs {
		r.NotEqual(signingSecretName, o.GetName())
		if c, ok := o.(*v1alpha1.Component); ok {
			r.Empty(c.Spec.OCMConfig)
		}
	}

	w.repositoryCount = 1
	single, err := w.repository(0)
	r.NoError(err)
	r.NotContains(string(single.Spec.RepositorySpec.Raw), "subPath")
}

func TestWorkloadConfigMaps(t *testing.T) {
	for _, depth := range []int{0, defaultDepth} {
		t.Run(fmt.Sprintf("depth %d", depth), func(t *testing.T) {
			r := require.New(t)
			w := newWorkload(3, "run", "http://registry:5000")
			w.depth = depth

			cvs, err := w.componentVersions()
			r.NoError(err)
			deployed := map[client.ObjectKey]bool{}
			for _, cv := range cvs {
				for _, res := range cv.Resources {
					if res.Blob == nil {
						continue
					}
					var cm corev1.ConfigMap
					r.NoError(yaml.Unmarshal(res.Blob, &cm))
					deployed[client.ObjectKeyFromObject(&cm)] = true
				}
			}

			keys := w.configMaps()
			r.Len(keys, 3)
			r.Len(deployed, 3)
			for _, key := range keys {
				r.True(deployed[key], "%s is not deployed by any Deployer", key)
			}
		})
	}
}
