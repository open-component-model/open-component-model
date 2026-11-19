package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func TestWorkloadReleases(t *testing.T) {
	tests := []struct {
		rel          release
		version      string
		rootVersions []string
		constraint   string
	}{
		{rel: rolloutRelease, version: "1.0.0", rootVersions: []string{"1.0.0", "1.0.1", "1.0.2"}, constraint: ">=1.0.0 <1.1.0"},
		{rel: updateRelease, version: "1.1.0", rootVersions: []string{"1.1.0", "1.1.1", "1.1.2"}, constraint: ">=1.1.0 <1.2.0"},
	}
	for _, depth := range []int{0, defaultDepth} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s depth %d", tt.version, depth), func(t *testing.T) {
				r := require.New(t)
				w := newWorkload(4, "run", "http://registry:5000")
				w.depth, w.versions, w.resources = depth, 3, 7
				r.Equal(tt.constraint, tt.rel.constraint())

				cvs, err := w.componentVersions(tt.rel)
				r.NoError(err)
				r.Len(cvs, 4*(3+depth))

				byKey := map[string]componentVersion{}
				for _, cv := range cvs {
					byKey[cv.Name+":"+cv.Version] = cv
				}
				for i := range 4 {
					var roots []string
					for _, cv := range cvs {
						if cv.Name == w.componentName(i) {
							roots = append(roots, cv.Version)
							r.Len(imageResources(cv), 7)
						} else if strings.HasPrefix(cv.Name, w.componentName(i)+"/") {
							r.Equal(tt.version, cv.Version, "the chain and the leaf are at the release version")
						}
					}
					r.Equal(tt.rootVersions, roots)

					// Follow the chain from the newest root to the manifest; every reference resolves inside the set.
					cv := byKey[w.componentName(i)+":"+tt.rootVersions[2]]
					for range depth {
						r.Len(cv.References, 1)
						next, ok := byKey[cv.References[0].Component+":"+cv.References[0].Version]
						r.True(ok, "%s references %s:%s outside the release", cv.Name, cv.References[0].Component, cv.References[0].Version)
						cv = next
					}
					var cm corev1.ConfigMap
					r.NoError(yaml.Unmarshal(manifestOf(cv), &cm))
					r.Equal(configMapName(i), cm.Name)
					r.Equal(w.namespace, cm.Namespace)
					r.Equal(tt.version, cm.Data["version"])
				}
			})
		}
	}
}

func TestWorkloadReleasesAreDisjoint(t *testing.T) {
	r := require.New(t)
	w := newWorkload(2, "run", "http://registry:5000")
	rollout, err := w.componentVersions(rolloutRelease)
	r.NoError(err)
	update, err := w.componentVersions(updateRelease)
	r.NoError(err)

	published := map[string]bool{}
	for _, cv := range rollout {
		published[cv.Name+":"+cv.Version] = true
	}
	for _, cv := range update {
		r.False(published[cv.Name+":"+cv.Version], "%s:%s is in both releases", cv.Name, cv.Version)
	}
}

// A Component that polls while the update is prepared must not move to the update release before the update changes it.
func TestWorkloadConstraintsStayInTheirRelease(t *testing.T) {
	r := require.New(t)
	w := newWorkload(1, "run", "http://registry:5000")
	for _, rel := range []release{rolloutRelease, updateRelease} {
		c, err := semver.NewConstraint(rel.constraint())
		r.NoError(err)
		for _, other := range []release{rolloutRelease, updateRelease} {
			cvs, err := w.componentVersions(other)
			r.NoError(err)
			for _, cv := range cvs {
				if cv.Name != w.componentName(0) {
					continue
				}
				r.Equal(rel == other, c.Check(semver.MustParse(cv.Version)), "%s selects %s", rel.constraint(), cv.Version)
			}
		}
	}
}

func imageResources(cv componentVersion) []resource {
	var out []resource
	for _, res := range cv.Resources {
		if res.ImageReference != "" {
			out = append(out, res)
		}
	}
	return out
}

func manifestOf(cv componentVersion) []byte {
	for _, res := range cv.Resources {
		if res.Name == resourceName {
			return res.Blob
		}
	}
	return nil
}

func TestWorkloadObjectsToCreate(t *testing.T) {
	r := require.New(t)
	w := newWorkload(3, "run", "http://registry:5000")

	objs, err := w.objectsToCreate()
	r.NoError(err)
	// The plain HTTP ConfigMap, the signing Secret, the deployer RBAC, one Repository per object (fewer than the cap),
	// and a pipeline each.
	r.Len(objs, 1+1+3+3+3*3)
	for _, o := range objs {
		if o.GetName() != plainHTTPConfigName {
			r.Equal("run", o.GetLabels()[runLabel])
		}
		r.Equal(w.namespace, o.GetNamespace(), "namespace deletion must clean up every object")
		if d, ok := o.(*v1alpha1.NamespacedDeployer); ok {
			r.Equal(deployerAccount, d.Spec.ServiceAccountName)
		}
		if c, ok := o.(*v1alpha1.Component); ok {
			r.Equal(rolloutRelease.constraint(), c.Spec.Semver, "the update phase changes the release, not the setup")
		}
	}
}

func TestWorkloadReferenceChain(t *testing.T) {
	r := require.New(t)
	w := newWorkload(defaultRepositories+2, "run", "http://registry:5000")
	w.depth, w.resources, w.versions = 2, 7, 3

	cvs, err := w.componentVersions(rolloutRelease)
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
	r.IsType(&corev1.ConfigMap{}, objs[0], "the plain HTTP ConfigMap must exist before the Repositories that reference it")
	r.IsType(&corev1.Secret{}, objs[1], "the signing Secret must exist before the Components that verify with it")
	r.IsType(&corev1.ServiceAccount{}, objs[2], "the deployer account must exist before the Deployers impersonate it")
	r.IsType(&v1alpha1.Repository{}, objs[4+defaultRepositories])
	r.IsType(&v1alpha1.Component{}, objs[4+defaultRepositories+1], "objects share a capped number of Repositories")

	// The last object wraps around onto the second Repository.
	i := defaultRepositories + 1
	r.Equal(w.repositoryName(1), w.component(i).Spec.RepositoryRef.Name)
	r.Equal(">=1.0.0 <1.1.0", w.component(i).Spec.Semver, "the rollout selects its release by constraint")
	r.Equal(signingSecretName, w.component(i).Spec.OCMConfig[0].Name)
	r.Equal(v1alpha1.ConfigurationPolicyPropagate, w.component(i).Spec.OCMConfig[0].Policy,
		"the Resource and Deployer inherit the verification config")

	repo, err := w.repository(i)
	r.NoError(err)
	r.JSONEq(`{"type":"OCIRegistry","baseUrl":"http://registry:5000","subPath":"`+w.subPath(1)+`"}`,
		string(repo.Spec.RepositorySpec.Raw), "the Repository must point at the sub-path its component versions were published to")
	r.Equal(plainHTTPConfigName, repo.Spec.OCMConfig[0].Name, "an http:// baseUrl needs plainHTTP in the HTTP client configuration")
	r.Equal(plainHTTPConfigName, w.component(i).Spec.OCMConfig[1].Name, "an own config replaces the Repository's, so it must list the plain HTTP config too")

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

func TestWorkloadWithoutChain(t *testing.T) {
	r := require.New(t)
	w := newWorkload(2, "run", "http://registry:5000")
	w.depth, w.resources, w.versions = 0, 3, 4

	cvs, err := w.componentVersions(rolloutRelease)
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

			cvs, err := w.componentVersions(rolloutRelease)
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
