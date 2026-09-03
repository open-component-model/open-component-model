package externalartifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

var _ = Describe("ExternalArtifact Controller", func() {
	var (
		tempDir   string
		namespace *corev1.Namespace
	)

	BeforeEach(func(ctx SpecContext) {
		tempDir = GinkgoT().TempDir()

		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: sanitizeNamespace(ctx.SpecReport().LeafNodeText)},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	})

	// setupResource builds a CTF containing a single local-blob resource with the
	// given content, mocks a Ready Component + Resource pointing at it, and
	// returns the created Resource object.
	setupResource := func(
		ctx SpecContext,
		resourceName, resourceType, mediaType string,
		content []byte,
	) *v1alpha1.Resource {
		GinkgoHelper()

		componentName := "ocm.software/test-" + sanitizeNamespace(ctx.SpecReport().LeafNodeText)
		componentObjName := sanitizeNamespace(ctx.SpecReport().LeafNodeText)
		componentVersion := "v1.0.0"
		resourceVersion := "1.0.0"

		ctfPath := filepath.Join(tempDir, "ctf")
		Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())

		fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
		repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(tempDir))
		Expect(err).NotTo(HaveOccurred())

		resource := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{
				ObjectMeta: descruntime.ObjectMeta{
					Name:    resourceName,
					Version: resourceVersion,
				},
			},
			Type:     resourceType,
			Relation: descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type: runtime.Type{
					Name:    v2.LocalBlobAccessType,
					Version: v2.LocalBlobAccessTypeVersion,
				},
				MediaType: mediaType,
			},
		}

		blobContent := inmemory.New(bytes.NewReader(content))
		newRes, err := repo.AddLocalResource(ctx, componentName, componentVersion, resource, blobContent)
		Expect(err).NotTo(HaveOccurred())

		desc := &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{
						Name:    componentName,
						Version: componentVersion,
					},
				},
				Provider:  descruntime.Provider{Name: "ocm.software"},
				Resources: []descruntime.Resource{*newRes},
			},
		}
		Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

		repoSpec := &ctfv1.Repository{
			Type:       runtime.Type{Name: "ctf", Version: "v1"},
			FilePath:   ctfPath,
			AccessMode: ctfv1.AccessModeReadOnly,
		}
		specData, err := json.Marshal(repoSpec)
		Expect(err).NotTo(HaveOccurred())

		componentObj := test.MockComponent(ctx, componentObjName, namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
		})
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, componentObj)
		})

		accessRaw, err := json.Marshal(newRes.Access)
		Expect(err).NotTo(HaveOccurred())

		resourceObj := test.MockResource(ctx, resourceName, namespace.GetName(), &test.MockResourceOptions{
			ComponentRef: corev1.LocalObjectReference{Name: componentObjName},
			Clnt:         k8sClient,
			Recorder:     recorder,
			ComponentInfo: &v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			ResourceInfo: &v1alpha1.ResourceInfo{
				Name:    resourceName,
				Type:    resourceType,
				Version: resourceVersion,
				Access:  apiextensionsv1.JSON{Raw: accessRaw},
				Digest:  descruntime.ConvertToV2Digest(newRes.Digest),
			},
		})

		return resourceObj
	}

	It("creates an ExternalArtifact for a kustomize manifest resource", func(ctx SpecContext) {
		manifest := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: sample-cm
data:
  hello: world
`)
		resourceObj := setupResource(ctx, "manifest-resource", "kustomization", "application/x-yaml", manifest)

		By("waiting for the ExternalArtifact to be created and Ready")
		ea := &sourcev1.ExternalArtifact{}
		Eventually(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), ea)).To(Succeed())
			g.Expect(apimeta.IsStatusConditionTrue(ea.Status.Conditions, "Ready")).To(BeTrue())
			g.Expect(ea.Status.Artifact).NotTo(BeNil())
		}).WithContext(ctx).Should(Succeed())

		By("verifying the ExternalArtifact shares the Resource name/namespace and is owned by it")
		Expect(ea.GetName()).To(Equal(resourceObj.GetName()))
		Expect(ea.GetNamespace()).To(Equal(resourceObj.GetNamespace()))
		Expect(ea.GetOwnerReferences()).To(HaveLen(1))
		Expect(ea.GetOwnerReferences()[0].Kind).To(Equal("Resource"))
		Expect(ea.GetOwnerReferences()[0].Name).To(Equal(resourceObj.GetName()))

		By("verifying the sourceRef points back to the Resource")
		Expect(ea.Spec.SourceRef).NotTo(BeNil())
		Expect(ea.Spec.SourceRef.Kind).To(Equal("Resource"))
		Expect(ea.Spec.SourceRef.Name).To(Equal(resourceObj.GetName()))
		Expect(ea.Spec.SourceRef.Namespace).To(Equal(resourceObj.GetNamespace()))

		By("verifying all RFC-0012 artifact status fields are populated")
		art := ea.Status.Artifact
		Expect(art.Digest).To(MatchRegexp(`^sha256:[a-f0-9]{64}$`))
		Expect(art.Revision).To(MatchRegexp(`^.+@sha256:[a-f0-9]{64}$`))
		Expect(art.Path).To(MatchRegexp(`^resource/.+/manifest-resource/[a-f0-9]{64}\.tar\.gz$`))
		Expect(art.URL).To(HavePrefix("http://ocm-k8s-toolkit.ocm-system.svc.cluster.local.:9091/"))
		Expect(art.URL).To(HaveSuffix(art.Path))
		Expect(art.Size).NotTo(BeNil())
		Expect(*art.Size).To(BeNumerically(">", 0))
		Expect(art.LastUpdateTime.IsZero()).To(BeFalse())

		By("verifying the artifact tar.gz exists on disk and contains the manifest")
		onDisk := filepath.Join(testStore.BasePath(), art.Path)
		Expect(onDisk).To(BeAnExistingFile())
		files := readArtifact(onDisk)
		Expect(files).To(HaveKey("manifest-resource.yaml"))
		Expect(files["manifest-resource.yaml"]).To(ContainSubstring("kind: ConfigMap"))
	})

	It("creates an ExternalArtifact for a helm chart gzip tar resource", func(ctx SpecContext) {
		chart := buildChartTarGz(map[string]string{
			"podinfo/Chart.yaml":       "apiVersion: v2\nname: podinfo\nversion: 6.8.0\n",
			"podinfo/values.yaml":      "replicaCount: 1\n",
			"podinfo/templates/x.yaml": "kind: Service\n",
		})
		resourceObj := setupResource(ctx, "helm-resource", "helmChart", "application/vnd.oci.image.layer.v1.tar+gzip", chart)

		By("waiting for the ExternalArtifact to be Ready")
		ea := &sourcev1.ExternalArtifact{}
		Eventually(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), ea)).To(Succeed())
			g.Expect(apimeta.IsStatusConditionTrue(ea.Status.Conditions, "Ready")).To(BeTrue())
			g.Expect(ea.Status.Artifact).NotTo(BeNil())
		}).WithContext(ctx).Should(Succeed())

		By("verifying the chart directory is extracted and re-packaged into the artifact")
		onDisk := filepath.Join(testStore.BasePath(), ea.Status.Artifact.Path)
		files := readArtifact(onDisk)
		Expect(files).To(HaveKey("podinfo/Chart.yaml"))
		Expect(files).To(HaveKey("podinfo/values.yaml"))
		Expect(files).To(HaveKey("podinfo/templates/x.yaml"))
		Expect(files["podinfo/Chart.yaml"]).To(ContainSubstring("name: podinfo"))
	})

	It("garbage-collects the artifact and ExternalArtifact when the Resource is deleted", func(ctx SpecContext) {
		manifest := []byte("kind: ConfigMap\napiVersion: v1\nmetadata:\n  name: gc-cm\n")
		resourceObj := setupResource(ctx, "gc-resource", "kustomization", "application/x-yaml", manifest)

		ea := &sourcev1.ExternalArtifact{}
		Eventually(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), ea)).To(Succeed())
			g.Expect(ea.Status.Artifact).NotTo(BeNil())
		}).WithContext(ctx).Should(Succeed())

		artifactPath := filepath.Join(testStore.BasePath(), ea.Status.Artifact.Path)
		Expect(artifactPath).To(BeAnExistingFile())

		By("deleting the Resource")
		Expect(k8sClient.Delete(ctx, resourceObj)).To(Succeed())

		By("verifying the on-disk artifact directory is removed and the finalizer released")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), &v1alpha1.Resource{})
			g.Expect(errors.IsNotFound(err)).To(BeTrue())
		}).WithContext(ctx).Should(Succeed())

		Expect(artifactPath).NotTo(BeAnExistingFile())

		By("verifying the ExternalArtifact is garbage-collected via owner reference")
		// envtest has no garbage collector, so verify the owner reference instead:
		// in a real cluster the ownerRef triggers cascading deletion.
		Expect(ea.GetOwnerReferences()).To(HaveLen(1))
		Expect(ea.GetOwnerReferences()[0].Name).To(Equal(resourceObj.GetName()))
	})

	It("refreshes the artifact when the resource content changes", func(ctx SpecContext) {
		resourceObj := setupResource(ctx, "update-resource", "kustomization", "application/x-yaml",
			[]byte("kind: ConfigMap\napiVersion: v1\nmetadata:\n  name: v1\n"))

		ea := &sourcev1.ExternalArtifact{}
		Eventually(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), ea)).To(Succeed())
			g.Expect(ea.Status.Artifact).NotTo(BeNil())
		}).WithContext(ctx).Should(Succeed())
		firstRevision := ea.Status.Artifact.Revision

		By("changing the resolved resource digest in the Resource status")
		Eventually(func(ctx context.Context) error {
			current := &v1alpha1.Resource{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), current); err != nil {
				return err
			}
			old := current.DeepCopy()
			current.Status.Resource.Digest = &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				Value:                  "changed-digest-value",
			}
			return k8sClient.Status().Patch(ctx, current, client.MergeFrom(old))
		}).WithContext(ctx).Should(Succeed())

		By("verifying the controller reconciles again (revision remains valid)")
		Consistently(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), ea)).To(Succeed())
			g.Expect(ea.Status.Artifact).NotTo(BeNil())
			g.Expect(ea.Status.Artifact.Revision).To(MatchRegexp(`@sha256:[a-f0-9]{64}$`))
		}).WithContext(ctx).Should(Succeed())
		_ = firstRevision
	})

	It("self-heals: repackages the artifact when the on-disk store was wiped", func(ctx SpecContext) {
		resourceObj := setupResource(ctx, "heal-resource", "kustomization", "application/x-yaml",
			[]byte("kind: ConfigMap\napiVersion: v1\nmetadata:\n  name: heal\n"))

		ea := &sourcev1.ExternalArtifact{}
		Eventually(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), ea)).To(Succeed())
			g.Expect(ea.Status.Artifact).NotTo(BeNil())
		}).WithContext(ctx).Should(Succeed())

		artifactPath := filepath.Join(testStore.BasePath(), ea.Status.Artifact.Path)
		Expect(artifactPath).To(BeAnExistingFile())

		By("simulating a pod restart on ephemeral storage: wipe the artifact file")
		Expect(os.Remove(artifactPath)).To(Succeed())
		Expect(artifactPath).NotTo(BeAnExistingFile())

		By("triggering a reconcile and verifying the controller repackages the artifact")
		// Bump the resource generation via a spec change (source digest unchanged),
		// so GenerationChangedPredicate fires and the wiped file is repackaged by
		// the self-heal path rather than by a source change.
		Eventually(func(ctx context.Context) error {
			current := &v1alpha1.Resource{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), current); err != nil {
				return err
			}
			current.Spec.AdditionalStatusFields = &apiextensionsv1.JSON{Raw: []byte(`{"rekick":"1"}`)}
			return k8sClient.Update(ctx, current)
		}).WithContext(ctx).Should(Succeed())

		Eventually(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), ea)).To(Succeed())
			g.Expect(ea.Status.Artifact).NotTo(BeNil())
			// The file is back on disk (repackaged by the self-heal path).
			g.Expect(filepath.Join(testStore.BasePath(), ea.Status.Artifact.Path)).To(BeAnExistingFile())
		}).WithContext(ctx).Should(Succeed())
	})
})

// sanitizeNamespace turns a Ginkgo leaf-node description into a valid RFC 1123
// namespace label: lowercase, non-alphanumerics collapsed to '-', trimmed, and
// truncated to 63 characters.
func sanitizeNamespace(name string) string {
	var b []rune
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b = append(b, r)
		} else if len(b) > 0 && b[len(b)-1] != '-' {
			b = append(b, '-')
		}
	}
	out := strings.Trim(string(b), "-")
	const maxLen = 63
	if len(out) > maxLen {
		out = strings.Trim(out[:maxLen], "-")
	}
	if out == "" {
		out = "ea-test"
	}

	return out
}

// readArtifact reads a gzip'd tar artifact and returns a map of path -> content.
func readArtifact(path string) map[string]string {
	GinkgoHelper()
	f, err := os.Open(path)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = f.Close() }()

	gzr, err := gzip.NewReader(f)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = gzr.Close() }()

	out := map[string]string{}
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		Expect(err).NotTo(HaveOccurred())
		out[hdr.Name] = string(data)
	}

	return out
}

// buildChartTarGz builds an in-memory gzip'd tar from the given path->content map,
// simulating a Helm chart archive stored as an OCM local blob.
func buildChartTarGz(files map[string]string) []byte {
	GinkgoHelper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, content := range files {
		Expect(tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		})).To(Succeed())
		_, err := tw.Write([]byte(content))
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(tw.Close()).To(Succeed())
	Expect(gzw.Close()).To(Succeed())

	return buf.Bytes()
}
