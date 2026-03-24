package deployer

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

var _ = Describe("Deployer Controller with YAML stream (ConfigMap + Secret)", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	Context("deployer controller (yaml stream)", func() {
		var resourceObj *v1alpha1.Resource
		var namespace *corev1.Namespace
		var componentName, componentObjName, resourceName, deployerObjName string
		var componentVersion string

		BeforeEach(func(ctx SpecContext) {
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentObjName = test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-yamlstream-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			deployerObjName = "test-deployer-yaml-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentVersion = "v1.0.0"

			namespaceName := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		})

		AfterEach(func(ctx SpecContext) {
			By("deleting the deployer resource object")
			if resourceObj != nil {
				Expect(k8sClient.Delete(ctx, resourceObj)).To(Succeed())
				Eventually(func(ctx context.Context) error {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("resource %s still exists", resourceObj.GetName())
				}).WithContext(ctx).Should(Succeed())
			}

			// Best-effort cleanup of applied workload objects (in case GC/owner-refs aren't set)
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-cm",
				Namespace: namespace.GetName(),
			}})
			_ = k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-secret",
				Namespace: namespace.GetName(),
			}})
		})

		It("reconciles a deployer that applies a YAML stream", func(ctx SpecContext) {
			By("creating a CTF with the YAML stream blob")
			resourceVersion := "1.0.0"

			// Multi-doc YAML stream: ConfigMap + Secret
			yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: sample-cm
  namespace: %[1]s
data:
  hello: world
---
apiVersion: v1
kind: Secret
metadata:
  name: sample-secret
  namespace: %[1]s
type: Opaque
stringData:
  password: s3cr3t
`, namespace.GetName()))

			ctfPath := tempDir
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
				Type:     "plainText",
				Relation: descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/x-yaml",
				},
			}

			blobContent := inmemory.New(bytes.NewReader(yamlStream))
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

			By("mocking a component")
			componentObj := test.MockComponent(
				ctx,
				componentObjName,
				namespace.GetName(),
				&test.MockComponentOptions{
					Client:   k8sClient,
					Recorder: recorder,
					Info: v1alpha1.ComponentInfo{
						Component:      componentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("mocking a Resource that references the YAML stream")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
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
						Type:    "plainText",
						Version: resourceVersion,
						Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
						Digest: &v2.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "genericBlobDigest/v1",
							Value:                  "test-digest-value",
						},
					},
				},
			)

			By("creating a Deployer that references the Resource")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: deployerObjName,
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespace.GetName(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, deployerObj)
			})

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("verifying the ConfigMap has been applied")
			gotCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "sample-cm",
			}, gotCM)).To(Succeed())
			Expect(gotCM.Data).To(HaveKeyWithValue("hello", "world"))

			By("verifying the Secret has been applied")
			gotSec := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "sample-secret",
			}, gotSec)).To(Succeed())
			// stringData is converted by API server into data (base64); compare the decoded value.
			Expect(string(gotSec.Data["password"])).To(Equal("s3cr3t"))
		})

		It("reconciles a deployer when resource has no digest", func(ctx SpecContext) {
			By("creating a CTF with the YAML stream blob")
			resourceVersion := "1.0.0"

			// Multi-doc YAML stream: ConfigMap + Secret
			yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: sample-cm
  namespace: %[1]s
data:
  hello: world
---
apiVersion: v1
kind: Secret
metadata:
  name: sample-secret
  namespace: %[1]s
type: Opaque
stringData:
  password: s3cr3t
`, namespace.GetName()))

			ctfPath := tempDir
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
				Type:     "plainText",
				Relation: descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobAccessType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/x-yaml",
				},
			}

			blobContent := inmemory.New(bytes.NewReader(yamlStream))
			newRes, err := repo.AddLocalResource(ctx, componentName, componentVersion, resource, blobContent)
			Expect(err).NotTo(HaveOccurred())
			// Clear the digest that AddLocalResource computed so the component descriptor
			// resource truly has no digest, exercising the no-digest cache key path.
			newRes.Digest = nil

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

			By("mocking a component")
			componentObj := test.MockComponent(
				ctx,
				componentObjName,
				namespace.GetName(),
				&test.MockComponentOptions{
					Client:   k8sClient,
					Recorder: recorder,
					Info: v1alpha1.ComponentInfo{
						Component:      componentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("mocking a Resource with no digest")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
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
						Type:    "plainText",
						Version: resourceVersion,
						Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
						// Digest intentionally nil to exercise the no-digest cache key path
					},
				},
			)

			By("creating a Deployer that references the Resource")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: deployerObjName,
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespace.GetName(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, deployerObj)
			})

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("verifying the ConfigMap has been applied")
			gotCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "sample-cm",
			}, gotCM)).To(Succeed())
			Expect(gotCM.Data).To(HaveKeyWithValue("hello", "world"))

			By("verifying the Secret has been applied")
			gotSec := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "sample-secret",
			}, gotSec)).To(Succeed())
			Expect(string(gotSec.Data["password"])).To(Equal("s3cr3t"))

			By("verifying the download cache contains the expected composite key")
			expectedCacheKey := desc.Component.Name + ":" + desc.Component.Version + "/" + resourceObj.Spec.Resource.ByReference.Resource.String()
			_, err = downloadCache.Load(expectedCacheKey, func() ([]*unstructured.Unstructured, error) {
				return nil, fmt.Errorf("cache miss for key %q: expected a hit", expectedCacheKey)
			})
			Expect(err).NotTo(HaveOccurred(), "expected download cache to contain composite key for nil-digest resource")
		})
	})

	Context("ocm config propagation from resource to deployer", func() {
		var (
			resourceObj                                                     *v1alpha1.Resource
			namespace                                                       *corev1.Namespace
			componentName, componentObjName, resourceName, componentVersion string
			specData                                                        []byte
			credentialSecret                                                *corev1.Secret
		)

		BeforeEach(func(ctx SpecContext) {
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentObjName = test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-yamlstream-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentVersion = "v1.0.0"

			namespaceName := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			credentialSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      "cred-secret",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
  credentials:
  - type: Credentials
    properties:
      username: testuser
      password: testpassword
`),
				},
			}
			Expect(k8sClient.Create(ctx, credentialSecret)).To(Succeed())

			By("creating a CTF with a YAML stream blob")
			resourceVersion := "1.0.0"
			yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: deployed-cm
  namespace: %s
data:
  key: value
`, namespace.GetName()))

			ctfPath := GinkgoT().TempDir()
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())

			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(ctfPath))
			Expect(err).NotTo(HaveOccurred())

			resource := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{
					ObjectMeta: descruntime.ObjectMeta{
						Name:    resourceName,
						Version: resourceVersion,
					},
				},
				Type:     "plainText",
				Relation: descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/x-yaml",
				},
			}

			blobContent := inmemory.New(bytes.NewReader(yamlStream))
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
			specData, err = json.Marshal(repoSpec)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func(ctx SpecContext) {
			if resourceObj != nil {
				Expect(k8sClient.Delete(ctx, resourceObj)).To(Succeed())
				Eventually(func(ctx context.Context) error {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("resource %s still exists", resourceObj.GetName())
				}).WithContext(ctx).Should(Succeed())
			}
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:      "deployed-cm",
				Namespace: namespace.GetName(),
			}})
			_ = k8sClient.Delete(ctx, credentialSecret)
		})

		It("deployer without ocmConfig inherits propagate entries from resource", func(ctx SpecContext) {
			deployerObjName := "test-deployer-inherit-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceVersion := "1.0.0"

			By("mocking a component")
			componentObj := test.MockComponent(
				ctx,
				componentObjName,
				namespace.GetName(),
				&test.MockComponentOptions{
					Client:   k8sClient,
					Recorder: recorder,
					Info: v1alpha1.ComponentInfo{
						Component:      componentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("mocking a Resource with EffectiveOCMConfig containing propagate entries")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
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
						Type:    "plainText",
						Version: resourceVersion,
						Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
						Digest: &v2.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "genericBlobDigest/v1",
							Value:                  "test-digest-inherit",
						},
					},
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       credentialSecret.Name,
								Namespace:  credentialSecret.Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
					},
				},
			)

			By("creating a Deployer without ocmConfig")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: deployerObjName,
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespace.GetName(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, deployerObj)
			})

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("verifying the deployed ConfigMap exists")
			gotCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "deployed-cm",
			}, gotCM)).To(Succeed())
			Expect(gotCM.Data).To(HaveKeyWithValue("key", "value"))

			By("checking deployer's effective OCM config inherited from resource")
			Eventually(komega.Object(deployerObj), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       credentialSecret.Name,
							Namespace:  credentialSecret.Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
				)),
			)
		})

		It("deployer with explicit ocmConfig ignores parent resource config", func(ctx SpecContext) {
			deployerObjName := "test-deployer-explicit-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceVersion := "1.0.0"

			By("creating a second secret for the deployer's own config")
			deployerSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      "deployer-own-secret",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: other.example.com
  credentials:
  - type: Credentials
    properties:
      username: deployer-user
      password: deployer-pass
`),
				},
			}
			Expect(k8sClient.Create(ctx, deployerSecret)).To(Succeed())

			By("mocking a component")
			componentObj := test.MockComponent(
				ctx,
				componentObjName,
				namespace.GetName(),
				&test.MockComponentOptions{
					Client:   k8sClient,
					Recorder: recorder,
					Info: v1alpha1.ComponentInfo{
						Component:      componentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("mocking a Resource with EffectiveOCMConfig containing propagate entries")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
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
						Type:    "plainText",
						Version: resourceVersion,
						Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
						Digest: &v2.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "genericBlobDigest/v1",
							Value:                  "test-digest-explicit",
						},
					},
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       credentialSecret.Name,
								Namespace:  credentialSecret.Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
					},
				},
			)

			By("creating a Deployer with its own ocmConfig")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: deployerObjName,
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespace.GetName(),
					},
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       deployerSecret.Name,
								Namespace:  deployerSecret.Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				_ = k8sClient.Delete(ctx, deployerSecret)
				test.DeleteObject(ctx, k8sClient, deployerObj)
			})

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("checking deployer's effective OCM config uses only its own config")
			Eventually(komega.Object(deployerObj), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       deployerSecret.Name,
							Namespace:  deployerSecret.Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
				)),
			)
		})
	})

	Context("verified component cache behavior", func() {
		It("uses separate cache entries for verified and unverified component versions", Serial, func(ctx SpecContext) {
			componentName := "ocm.software/deployer-verified-cache-test"
			componentObjName := "deployer-verified-cache-test"
			resourceName := "verified-yaml-resource"
			componentVersion := "v1.0.0"
			resourceVersion := "1.0.0"

			namespaceName := "deployer-verified-cache-ns"
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: verified-cache-cm
  namespace: %s
data:
  verified: "true"
`, namespaceName))

			ctfPath := GinkgoT().TempDir()
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())

			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(ctfPath))
			Expect(err).NotTo(HaveOccurred())

			resource := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{
					ObjectMeta: descruntime.ObjectMeta{
						Name:    resourceName,
						Version: resourceVersion,
					},
				},
				Type:     "plainText",
				Relation: descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/x-yaml",
				},
			}

			blobContent := inmemory.New(bytes.NewReader(yamlStream))
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

			By("signing the component version")
			signatureName := "deployer-test-sig"
			normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
			Expect(err).ToNot(HaveOccurred())
			signature, pubKey := test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)
			desc.Signatures = append(desc.Signatures, signature)

			Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

			repoSpec := &ctfv1.Repository{
				Type:       runtime.Type{Name: "ctf", Version: "v1"},
				FilePath:   ctfPath,
				AccessMode: ctfv1.AccessModeReadOnly,
			}
			specData, err := json.Marshal(repoSpec)
			Expect(err).NotTo(HaveOccurred())

			By("mocking a verified component")
			componentObj := test.MockComponent(
				ctx,
				componentObjName,
				namespaceName,
				&test.MockComponentOptions{
					Client:   k8sClient,
					Recorder: recorder,
					Info: v1alpha1.ComponentInfo{
						Component:      componentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
					Verify: []v1alpha1.Verification{
						{
							Signature: signatureName,
							Value:     base64.StdEncoding.EncodeToString([]byte(pubKey)),
						},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("mocking a resource")
			resourceObj := test.MockResource(
				ctx,
				resourceName,
				namespaceName,
				&test.MockResourceOptions{
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
						Type:    "plainText",
						Version: resourceVersion,
						Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
						Digest: &v2.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "genericBlobDigest/v1",
							Value:                  "verified-cache-test-digest",
						},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("creating a Deployer that references the Resource")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployer-verified-cache-test",
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespaceName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, deployerObj)
			})

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("verifying the ConfigMap has been applied")
			gotCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespaceName,
				Name:      "verified-cache-cm",
			}, gotCM)).To(Succeed())
			Expect(gotCM.Data).To(HaveKeyWithValue("verified", "true"))

			By("checking cache metrics for verified component")
			verifiedMiss, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(componentName, componentVersion, "verified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(verifiedMiss)).To(Equal(float64(1)),
				"expected at least 1 cache miss for the verified component on first resolution")

			unverifiedMiss, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(componentName, componentVersion, "unverified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(unverifiedMiss)).To(Equal(float64(0)),
				"expected 0 cache misses for unverified state — verifications should always be included in cache key")

			verifiedHit, err := workerpool.CacheHitCounterTotal.GetMetricWithLabelValues(componentName, componentVersion, "verified")
			Expect(err).ToNot(HaveOccurred())
			// The exact hit count is non-deterministic: after the resource is applied, the reconciler
			// registers a dynamic resource watch for the deployed object and retries until the watch has
			// synced. Each retry reconciliation calls getEffectiveComponentDescriptor (which hits the
			// component version cache) before reaching the download cache. How many retries occur depends
			// on the timing between the informer sync and the controller's requeue.
			Expect(testutil.ToFloat64(verifiedHit)).To(BeNumerically(">=", float64(1)),
				"expected at least 1 cache hit for the verified component on subsequent reconciliation")
		})

		It("maintains integrity chain for referenced component via reference path", Serial, func(ctx SpecContext) {
			parentComponentName := "ocm.software/deployer-ref-chain-parent"
			childComponentName := "ocm.software/deployer-ref-chain-child"
			childRefName := "child-ref"
			componentObjName := "deployer-ref-chain-component"
			childResourceName := "child-yaml-resource"
			componentVersion := "v1.0.0"
			resourceVersion := "1.0.0"

			namespaceName := "deployer-ref-chain-ns"
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: ref-chain-cm
  namespace: %s
data:
  chain: "valid"
`, namespaceName))

			ctfPath := GinkgoT().TempDir()
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())

			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(ctfPath))
			Expect(err).NotTo(HaveOccurred())

			By("creating the child component with a local resource")
			resource := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{
					ObjectMeta: descruntime.ObjectMeta{
						Name:    childResourceName,
						Version: resourceVersion,
					},
				},
				Type:     "plainText",
				Relation: descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/x-yaml",
				},
			}

			blobContent := inmemory.New(bytes.NewReader(yamlStream))
			newRes, err := repo.AddLocalResource(ctx, childComponentName, componentVersion, resource, blobContent)
			Expect(err).NotTo(HaveOccurred())

			childDesc := &descruntime.Descriptor{
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    childComponentName,
							Version: componentVersion,
						},
					},
					Provider:  descruntime.Provider{Name: "ocm.software"},
					Resources: []descruntime.Resource{*newRes},
				},
			}
			Expect(repo.AddComponentVersion(ctx, childDesc)).To(Succeed())

			By("computing the child component digest for the parent's reference")
			childDigest, err := signing.GenerateDigest(ctx, childDesc, slog.New(logr.ToSlogHandler(log.FromContext(ctx))), signing.LegacyNormalisationAlgo, crypto.SHA256.String())
			Expect(err).ToNot(HaveOccurred())

			By("creating the parent component with a reference to the child")
			parentDesc := &descruntime.Descriptor{
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    parentComponentName,
							Version: componentVersion,
						},
					},
					References: []descruntime.Reference{
						{
							ElementMeta: descruntime.ElementMeta{
								ObjectMeta: descruntime.ObjectMeta{
									Name:    childRefName,
									Version: componentVersion,
								},
							},
							Component: childComponentName,
							Digest: descruntime.Digest{
								HashAlgorithm:          childDigest.HashAlgorithm,
								Value:                  childDigest.Value,
								NormalisationAlgorithm: childDigest.NormalisationAlgorithm,
							},
						},
					},
					Provider: descruntime.Provider{Name: "ocm.software"},
				},
			}

			By("signing the parent component")
			signatureName := "ref-chain-sig"
			normalised, err := normalisation.Normalise(parentDesc, v4alpha1.Algorithm)
			Expect(err).ToNot(HaveOccurred())
			signature, pubKey := test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)
			parentDesc.Signatures = append(parentDesc.Signatures, signature)
			Expect(repo.AddComponentVersion(ctx, parentDesc)).To(Succeed())

			repoSpec := &ctfv1.Repository{
				Type:       runtime.Type{Name: "ctf", Version: "v1"},
				FilePath:   ctfPath,
				AccessMode: ctfv1.AccessModeReadOnly,
			}
			specData, err := json.Marshal(repoSpec)
			Expect(err).NotTo(HaveOccurred())

			By("mocking a verified parent component")
			componentObj := test.MockComponent(
				ctx,
				componentObjName,
				namespaceName,
				&test.MockComponentOptions{
					Client:   k8sClient,
					Recorder: recorder,
					Info: v1alpha1.ComponentInfo{
						Component:      parentComponentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
					Verify: []v1alpha1.Verification{
						{
							Signature: signatureName,
							Value:     base64.StdEncoding.EncodeToString([]byte(pubKey)),
						},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("mocking a resource that references the child component via reference path")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deployer-ref-chain-resource",
					Namespace: namespaceName,
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObjName,
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": childResourceName},
							ReferencePath: []runtime.Identity{
								{"name": childRefName},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			old := resourceObj.DeepCopy()
			resourceObj.Status.Component = &v1alpha1.ComponentInfo{
				Component:      childComponentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			}
			resourceObj.Status.Resource = &v1alpha1.ResourceInfo{
				Name:    childResourceName,
				Type:    "plainText",
				Version: resourceVersion,
				Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
				Digest: &v2.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "genericBlobDigest/v1",
					Value:                  "ref-chain-resource-digest",
				},
			}
			Eventually(func(ctx context.Context) error {
				status.MarkReady(recorder, resourceObj, "applied mock resource")
				resourceObj.SetObservedGeneration(resourceObj.GetGeneration())

				return k8sClient.Status().Patch(ctx, resourceObj, client.MergeFrom(old))
			}).WithContext(ctx).Should(Succeed())

			By("creating a Deployer that references the Resource")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployer-ref-chain-test",
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespaceName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, deployerObj)
			})

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("verifying the ConfigMap has been applied")
			gotCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespaceName,
				Name:      "ref-chain-cm",
			}, gotCM)).To(Succeed())
			Expect(gotCM.Data).To(HaveKeyWithValue("chain", "valid"))

			By("checking cache metrics for verified parent component")
			parentMissVerified, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(parentComponentName, componentVersion, "verified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(parentMissVerified)).To(Equal(float64(1)),
				"expected 1 cache miss for the verified parent component on first resolution")
			parentHitVerified, err := workerpool.CacheHitCounterTotal.GetMetricWithLabelValues(parentComponentName, componentVersion, "verified")
			Expect(err).ToNot(HaveOccurred())
			// Hit 1 from first resolution (only deployer controller is running)
			// Hit 2 from the child component resolution via reference path
			// Hit count is non-deterministic for the same reason as above: resource watch sync retries
			// cause additional reconciliations that each hit the component version cache.
			Expect(testutil.ToFloat64(parentHitVerified)).To(BeNumerically(">=", float64(2)),
				"expected at least 2 cache hits for the verified parent component on first resolution")

			parentMissUnverified, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(parentComponentName, componentVersion, "unverified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(parentMissUnverified)).To(Equal(float64(0)),
				"expected 0 unverified cache misses for the parent — it should always be resolved with verifications")

			By("checking cache metrics for child component resolved via integrity chain")
			referencedMissVerified, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(childComponentName, componentVersion, "verified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(referencedMissVerified)).To(Equal(float64(1)),
				"expected 1 cache miss for the child component resolved via digest from parent reference")
			referencedHitVerified, err := workerpool.CacheHitCounterTotal.GetMetricWithLabelValues(childComponentName, componentVersion, "verified")
			Expect(err).ToNot(HaveOccurred())
			// Hit count is non-deterministic for the same reason as above: resource watch sync retries
			// cause additional reconciliations that each hit the component version cache.
			Expect(testutil.ToFloat64(referencedHitVerified)).To(BeNumerically(">=", float64(1)),
				"expected at least 1 cache hit for the child component resolved via digest from parent reference")

			referencedMissUnverified, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(childComponentName, componentVersion, "unverified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(referencedMissUnverified)).To(Equal(float64(0)),
				"expected 0 unverified cache misses for the child — it should be resolved via integrity chain digest")
		})

		It("does not deploy when verification fails with wrong public key", Serial, func(ctx SpecContext) {
			workerpool.CacheMissCounterTotal.Reset()
			workerpool.CacheHitCounterTotal.Reset()

			parentComponentName := "ocm.software/deployer-bad-verify-parent"
			childComponentName := "ocm.software/deployer-bad-verify-child"
			childRefName := "bad-verify-child-ref"
			componentObjName := "deployer-bad-verify-component"
			childResourceName := "bad-verify-resource"
			componentVersion := "v1.0.0"
			resourceVersion := "1.0.0"

			namespaceName := "deployer-bad-verify-ns"
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: bad-verify-cm
  namespace: %s
data:
  should: "not-exist"
`, namespaceName))

			ctfPath := GinkgoT().TempDir()
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())

			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(ctfPath))
			Expect(err).NotTo(HaveOccurred())

			By("creating the child component with a local resource")
			resource := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{
					ObjectMeta: descruntime.ObjectMeta{
						Name:    childResourceName,
						Version: resourceVersion,
					},
				},
				Type:     "plainText",
				Relation: descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/x-yaml",
				},
			}

			blobContent := inmemory.New(bytes.NewReader(yamlStream))
			newRes, err := repo.AddLocalResource(ctx, childComponentName, componentVersion, resource, blobContent)
			Expect(err).NotTo(HaveOccurred())

			childDesc := &descruntime.Descriptor{
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    childComponentName,
							Version: componentVersion,
						},
					},
					Provider:  descruntime.Provider{Name: "ocm.software"},
					Resources: []descruntime.Resource{*newRes},
				},
			}
			Expect(repo.AddComponentVersion(ctx, childDesc)).To(Succeed())

			By("computing the child component digest for the parent's reference")
			childDigest, err := signing.GenerateDigest(ctx, childDesc, slog.New(logr.ToSlogHandler(log.FromContext(ctx))), signing.LegacyNormalisationAlgo, crypto.SHA256.String())
			Expect(err).ToNot(HaveOccurred())

			By("creating the parent component with a reference to the child")
			parentDesc := &descruntime.Descriptor{
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    parentComponentName,
							Version: componentVersion,
						},
					},
					References: []descruntime.Reference{
						{
							ElementMeta: descruntime.ElementMeta{
								ObjectMeta: descruntime.ObjectMeta{
									Name:    childRefName,
									Version: componentVersion,
								},
							},
							Component: childComponentName,
							Digest: descruntime.Digest{
								HashAlgorithm:          childDigest.HashAlgorithm,
								Value:                  childDigest.Value,
								NormalisationAlgorithm: childDigest.NormalisationAlgorithm,
							},
						},
					},
					Provider: descruntime.Provider{Name: "ocm.software"},
				},
			}

			By("signing the parent component with the real key")
			signatureName := "bad-verify-sig"
			normalised, err := normalisation.Normalise(parentDesc, v4alpha1.Algorithm)
			Expect(err).ToNot(HaveOccurred())
			signature, _ := test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)
			parentDesc.Signatures = append(parentDesc.Signatures, signature)
			Expect(repo.AddComponentVersion(ctx, parentDesc)).To(Succeed())

			By("generating a different RSA key to use as the wrong public key")
			wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).ToNot(HaveOccurred())
			n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
			Expect(err).ToNot(HaveOccurred())
			tmpl := &x509.Certificate{
				SerialNumber:          n,
				Subject:               pkix.Name{CommonName: "wrong-signer"},
				NotBefore:             time.Now().Add(-time.Hour),
				NotAfter:              time.Now().Add(24 * time.Hour),
				KeyUsage:              x509.KeyUsageDigitalSignature,
				BasicConstraintsValid: true,
				IsCA:                  true,
			}
			der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &wrongKey.PublicKey, wrongKey)
			Expect(err).ToNot(HaveOccurred())
			wrongPubKey := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

			repoSpec := &ctfv1.Repository{
				Type:       runtime.Type{Name: "ctf", Version: "v1"},
				FilePath:   ctfPath,
				AccessMode: ctfv1.AccessModeReadOnly,
			}
			specData, err := json.Marshal(repoSpec)
			Expect(err).NotTo(HaveOccurred())

			By("mocking a component with the WRONG public key for verification")
			componentObj := test.MockComponent(
				ctx,
				componentObjName,
				namespaceName,
				&test.MockComponentOptions{
					Client:   k8sClient,
					Recorder: recorder,
					Info: v1alpha1.ComponentInfo{
						Component:      parentComponentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
					},
					Verify: []v1alpha1.Verification{
						{
							Signature: signatureName,
							Value:     base64.StdEncoding.EncodeToString([]byte(wrongPubKey)),
						},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("mocking a resource that references the child component via reference path")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deployer-bad-verify-resource",
					Namespace: namespaceName,
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObjName,
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": childResourceName},
							ReferencePath: []runtime.Identity{
								{"name": childRefName},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			old2 := resourceObj.DeepCopy()
			resourceObj.Status.Component = &v1alpha1.ComponentInfo{
				Component:      childComponentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			}
			resourceObj.Status.Resource = &v1alpha1.ResourceInfo{
				Name:    childResourceName,
				Type:    "plainText",
				Version: resourceVersion,
				Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
				Digest: &v2.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "genericBlobDigest/v1",
					Value:                  "bad-verify-resource-digest",
				},
			}
			Eventually(func(ctx context.Context) error {
				status.MarkReady(recorder, resourceObj, "applied mock resource")
				resourceObj.SetObservedGeneration(resourceObj.GetGeneration())

				return k8sClient.Status().Patch(ctx, resourceObj, client.MergeFrom(old2))
			}).WithContext(ctx).Should(Succeed())

			By("creating a Deployer that references the Resource")
			deployerObj := &v1alpha1.Deployer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployer-bad-verify-test",
				},
				Spec: v1alpha1.DeployerSpec{
					ResourceRef: v1alpha1.ObjectKey{
						Name:      resourceObj.GetName(),
						Namespace: namespaceName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())

			By("expecting the Deployer to NOT become ready due to signature verification failure")
			test.WaitForNotReadyObject(ctx, k8sClient, deployerObj, v1alpha1.GetComponentVersionFailedReason)

			By("verifying the ConfigMap was NOT deployed")
			gotCM := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespaceName,
				Name:      "bad-verify-cm",
			}, gotCM)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "ConfigMap should not exist when verification fails")

			By("deleting the Deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)
		})
	})
})

var _ = Describe("Deployer Controller ApplySet Prune", func() {
	It("prunes removed resources when the OCM resource content changes", func(ctx SpecContext) {
		// Regression test: when a YAML stream goes from 2 resources (ConfigMap+Secret)
		// to 1 resource (ConfigMap only), the Secret must be pruned. The bug was that
		// applyWithApplySet passed the batch-only metadata (from Apply) to Prune instead
		// of the projected metadata (from Project), so prune never searched for the
		// Secret's GroupKind and the orphan survived.

		sanitized := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: sanitized},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		componentName := "ocm.software/test-prune-scope-" + sanitized
		componentObjName := "prune-scope-component-" + sanitized
		resourceName := "prune-scope-resource-" + sanitized
		deployerObjName := "deployer-prune-scope-" + sanitized

		// --- v1: ConfigMap + Secret ---

		v1Version := "v1.0.0"
		v1ResourceVersion := "1.0.0"

		v1YamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: prune-cm
  namespace: %[1]s
data:
  version: v1
---
apiVersion: v1
kind: Secret
metadata:
  name: prune-secret
  namespace: %[1]s
type: Opaque
stringData:
  key: should-be-pruned
`, namespace.GetName()))

		v1CTFPath := GinkgoT().TempDir()
		Expect(os.MkdirAll(v1CTFPath, 0o777)).To(Succeed())

		v1FS, err := filesystem.NewFS(v1CTFPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		v1Store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(v1FS))
		v1Repo, err := oci.NewRepository(ocictf.WithCTF(v1Store), oci.WithTempDir(v1CTFPath))
		Expect(err).NotTo(HaveOccurred())

		v1Resource := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{
				ObjectMeta: descruntime.ObjectMeta{
					Name:    resourceName,
					Version: v1ResourceVersion,
				},
			},
			Type:     "plainText",
			Relation: descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type: runtime.Type{
					Name:    v2.LocalBlobAccessType,
					Version: v2.LocalBlobAccessTypeVersion,
				},
				MediaType: "application/x-yaml",
			},
		}
		v1Blob := inmemory.New(bytes.NewReader(v1YamlStream))
		v1NewRes, err := v1Repo.AddLocalResource(ctx, componentName, v1Version, v1Resource, v1Blob)
		Expect(err).NotTo(HaveOccurred())

		v1Desc := &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{
						Name:    componentName,
						Version: v1Version,
					},
				},
				Provider:  descruntime.Provider{Name: "ocm.software"},
				Resources: []descruntime.Resource{*v1NewRes},
			},
		}
		Expect(v1Repo.AddComponentVersion(ctx, v1Desc)).To(Succeed())

		v1RepoSpec := &ctfv1.Repository{
			Type:       runtime.Type{Name: "ctf", Version: "v1"},
			FilePath:   v1CTFPath,
			AccessMode: ctfv1.AccessModeReadOnly,
		}
		v1SpecData, err := json.Marshal(v1RepoSpec)
		Expect(err).NotTo(HaveOccurred())

		By("creating the v1 component and resource mocks")
		componentObj := test.MockComponent(
			ctx,
			componentObjName,
			namespace.GetName(),
			&test.MockComponentOptions{
				Client:   k8sClient,
				Recorder: recorder,
				Info: v1alpha1.ComponentInfo{
					Component:      componentName,
					Version:        v1Version,
					RepositorySpec: &apiextensionsv1.JSON{Raw: v1SpecData},
				},
			},
		)
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, componentObj)
		})

		resourceObj := test.MockResource(
			ctx,
			resourceName,
			namespace.GetName(),
			&test.MockResourceOptions{
				ComponentRef: corev1.LocalObjectReference{Name: componentObjName},
				Clnt:         k8sClient,
				Recorder:     recorder,
				ComponentInfo: &v1alpha1.ComponentInfo{
					Component:      componentName,
					Version:        v1Version,
					RepositorySpec: &apiextensionsv1.JSON{Raw: v1SpecData},
				},
				ResourceInfo: &v1alpha1.ResourceInfo{
					Name:    resourceName,
					Type:    "plainText",
					Version: v1ResourceVersion,
					Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
					Digest: &v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "genericBlobDigest/v1",
						Value:                  "prune-scope-digest-v1",
					},
				},
			},
		)
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, resourceObj)
		})

		By("creating the deployer")
		deployerObj := &v1alpha1.Deployer{
			ObjectMeta: metav1.ObjectMeta{
				Name: deployerObjName,
			},
			Spec: v1alpha1.DeployerSpec{
				ResourceRef: v1alpha1.ObjectKey{
					Name:      resourceObj.GetName(),
					Namespace: namespace.GetName(),
				},
			},
		}
		Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, deployerObj)
		})

		By("waiting for the deployer to be Ready with v1")
		test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

		By("verifying both ConfigMap and Secret exist after v1 deploy")
		gotCM := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Namespace: namespace.GetName(),
			Name:      "prune-cm",
		}, gotCM)).To(Succeed())
		Expect(gotCM.Data).To(HaveKeyWithValue("version", "v1"))

		gotSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Namespace: namespace.GetName(),
			Name:      "prune-secret",
		}, gotSecret)).To(Succeed())
		Expect(string(gotSecret.Data["key"])).To(Equal("should-be-pruned"))

		// --- v2: ConfigMap only (Secret removed) ---

		v2Version := "v2.0.0"
		v2ResourceVersion := "2.0.0"

		v2YamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: prune-cm
  namespace: %[1]s
data:
  version: v2
`, namespace.GetName()))

		v2CTFPath := GinkgoT().TempDir()
		Expect(os.MkdirAll(v2CTFPath, 0o777)).To(Succeed())

		v2FS, err := filesystem.NewFS(v2CTFPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		v2Store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(v2FS))
		v2Repo, err := oci.NewRepository(ocictf.WithCTF(v2Store), oci.WithTempDir(v2CTFPath))
		Expect(err).NotTo(HaveOccurred())

		v2Resource := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{
				ObjectMeta: descruntime.ObjectMeta{
					Name:    resourceName,
					Version: v2ResourceVersion,
				},
			},
			Type:     "plainText",
			Relation: descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type: runtime.Type{
					Name:    v2.LocalBlobAccessType,
					Version: v2.LocalBlobAccessTypeVersion,
				},
				MediaType: "application/x-yaml",
			},
		}
		v2Blob := inmemory.New(bytes.NewReader(v2YamlStream))
		v2NewRes, err := v2Repo.AddLocalResource(ctx, componentName, v2Version, v2Resource, v2Blob)
		Expect(err).NotTo(HaveOccurred())

		v2Desc := &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{
						Name:    componentName,
						Version: v2Version,
					},
				},
				Provider:  descruntime.Provider{Name: "ocm.software"},
				Resources: []descruntime.Resource{*v2NewRes},
			},
		}
		Expect(v2Repo.AddComponentVersion(ctx, v2Desc)).To(Succeed())

		v2RepoSpec := &ctfv1.Repository{
			Type:       runtime.Type{Name: "ctf", Version: "v1"},
			FilePath:   v2CTFPath,
			AccessMode: ctfv1.AccessModeReadOnly,
		}
		v2SpecData, err := json.Marshal(v2RepoSpec)
		Expect(err).NotTo(HaveOccurred())

		By("updating the component mock to v2")
		Eventually(func(ctx context.Context) error {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObj)).To(Succeed())
			old := componentObj.DeepCopy()
			componentObj.Status.Component = v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        v2Version,
				RepositorySpec: &apiextensionsv1.JSON{Raw: v2SpecData},
			}
			componentObj.SetObservedGeneration(componentObj.GetGeneration())
			return k8sClient.Status().Patch(ctx, componentObj, client.MergeFrom(old))
		}).WithContext(ctx).Should(Succeed())

		By("updating the resource mock to v2 (triggers deployer re-reconciliation)")
		Eventually(func(ctx context.Context) error {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)).To(Succeed())
			old := resourceObj.DeepCopy()
			resourceObj.Status.Component = &v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        v2Version,
				RepositorySpec: &apiextensionsv1.JSON{Raw: v2SpecData},
			}
			resourceObj.Status.Resource = &v1alpha1.ResourceInfo{
				Name:    resourceName,
				Type:    "plainText",
				Version: v2ResourceVersion,
				Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
				Digest: &v2.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "genericBlobDigest/v1",
					Value:                  "prune-scope-digest-v2",
				},
			}
			resourceObj.SetObservedGeneration(resourceObj.GetGeneration())
			return k8sClient.Status().Patch(ctx, resourceObj, client.MergeFrom(old))
		}).WithContext(ctx).Should(Succeed())

		By("waiting for the deployer to re-reconcile and apply v2")
		Eventually(func(ctx context.Context) string {
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "prune-cm",
			}, cm); err != nil {
				return ""
			}
			return cm.Data["version"]
		}).WithContext(ctx).WithTimeout(60 * time.Second).Should(Equal("v2"))

		By("verifying the Secret was pruned (this is the regression assertion)")
		Eventually(func(ctx context.Context) bool {
			err := k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace.GetName(),
				Name:      "prune-secret",
			}, &corev1.Secret{})
			return errors.IsNotFound(err)
		}).WithContext(ctx).WithTimeout(30 * time.Second).Should(BeTrue(),
			"Secret should have been pruned after being removed from the YAML stream, "+
				"but it still exists. This indicates the prune scope is too narrow (batch-only instead of projected).")
	})
})

var _ = Describe("Deployer Controller Finalizer Persistence", func() {
	It("adds finalizers to the deployer object during the first reconciliation", func(ctx SpecContext) {
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "deployer-finalizer-add"},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		ctfPath := GinkgoT().TempDir()
		Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())

		resourceVersion := "1.0.0"
		componentName := "ocm.software/test-component-finalizer-add"
		componentVersion := "v1.0.0"
		resourceName := "test-resource-finalizer-add"

		yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: finalizer-add-cm
  namespace: %s
data:
  key: value
`, namespace.GetName()))

		fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
		repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(ctfPath))
		Expect(err).NotTo(HaveOccurred())

		resource := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: resourceName, Version: resourceVersion},
			},
			Type:     "plainText",
			Relation: descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
				MediaType: "application/x-yaml",
			},
		}
		blobContent := inmemory.New(bytes.NewReader(yamlStream))
		newRes, err := repo.AddLocalResource(ctx, componentName, componentVersion, resource, blobContent)
		Expect(err).NotTo(HaveOccurred())

		desc := &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: componentVersion},
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

		componentObj := test.MockComponent(ctx, "component-finalizer-add", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := test.MockResource(ctx, resourceName, namespace.GetName(), &test.MockResourceOptions{
			ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
			Clnt:         k8sClient,
			Recorder:     recorder,
			ComponentInfo: &v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			ResourceInfo: &v1alpha1.ResourceInfo{
				Name:    resourceName,
				Type:    "plainText",
				Version: resourceVersion,
				Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
				Digest: &v2.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "genericBlobDigest/v1",
					Value:                  "finalizer-add-digest",
				},
			},
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		deployerObj := &v1alpha1.Deployer{
			ObjectMeta: metav1.ObjectMeta{Name: "deployer-finalizer-add"},
			Spec: v1alpha1.DeployerSpec{
				ResourceRef: v1alpha1.ObjectKey{
					Name:      resourceObj.GetName(),
					Namespace: namespace.GetName(),
				},
			},
		}
		Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, deployerObj) })

		By("verifying both finalizers are persisted after the first reconciliation")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployerObj), deployerObj)).To(Succeed())
			g.Expect(deployerObj.Finalizers).To(ContainElements(applySetPruneFinalizer, resourceWatchFinalizer))
		}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(Succeed())
	})

	It("removes finalizers so the deployer is fully deleted", func(ctx SpecContext) {
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "deployer-finalizer-remove"},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		ctfPath := GinkgoT().TempDir()
		Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())

		resourceVersion := "1.0.0"
		componentName := "ocm.software/test-component-finalizer-remove"
		componentVersion := "v1.0.0"
		resourceName := "test-resource-finalizer-remove"

		yamlStream := []byte(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: finalizer-remove-cm
  namespace: %s
data:
  key: value
`, namespace.GetName()))

		fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
		repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(ctfPath))
		Expect(err).NotTo(HaveOccurred())

		resource := &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: resourceName, Version: resourceVersion},
			},
			Type:     "plainText",
			Relation: descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
				MediaType: "application/x-yaml",
			},
		}
		blobContent := inmemory.New(bytes.NewReader(yamlStream))
		newRes, err := repo.AddLocalResource(ctx, componentName, componentVersion, resource, blobContent)
		Expect(err).NotTo(HaveOccurred())

		desc := &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{
					ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: componentVersion},
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

		componentObj := test.MockComponent(ctx, "component-finalizer-remove", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := test.MockResource(ctx, resourceName, namespace.GetName(), &test.MockResourceOptions{
			ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
			Clnt:         k8sClient,
			Recorder:     recorder,
			ComponentInfo: &v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        componentVersion,
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			ResourceInfo: &v1alpha1.ResourceInfo{
				Name:    resourceName,
				Type:    "plainText",
				Version: resourceVersion,
				Access:  apiextensionsv1.JSON{Raw: []byte(`{"type":"localBlob/v1"}`)},
				Digest: &v2.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "genericBlobDigest/v1",
					Value:                  "finalizer-remove-digest",
				},
			},
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		deployerObj := &v1alpha1.Deployer{
			ObjectMeta: metav1.ObjectMeta{Name: "deployer-finalizer-remove"},
			Spec: v1alpha1.DeployerSpec{
				ResourceRef: v1alpha1.ObjectKey{
					Name:      resourceObj.GetName(),
					Namespace: namespace.GetName(),
				},
			},
		}
		Expect(k8sClient.Create(ctx, deployerObj)).To(Succeed())

		By("waiting until the Deployer is Ready (finalizers are added)")
		test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployerObj), deployerObj)).To(Succeed())
		Expect(deployerObj.Finalizers).To(ContainElements(applySetPruneFinalizer, resourceWatchFinalizer))

		By("deleting the Deployer and verifying it is fully removed (no stuck finalizers)")
		test.DeleteObject(ctx, k8sClient, deployerObj)
	})
})

var _ = Describe("Deployer Controller Error Handling", func() {
	It("should not requeue when the resource is not ready", func(ctx SpecContext) {
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "deployer-err-not-ready",
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		resource := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "not-ready-resource",
				Namespace: namespace.GetName(),
			},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef: corev1.LocalObjectReference{Name: "test-component"},
				Resource: v1alpha1.ResourceID{
					ByReference: v1alpha1.ResourceReference{
						Resource: runtime.Identity{"name": "not-ready-resource"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, resource)
		})

		deployer := &v1alpha1.Deployer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-deployer-not-ready",
			},
			Spec: v1alpha1.DeployerSpec{
				ResourceRef: v1alpha1.ObjectKey{
					Name:      "not-ready-resource",
					Namespace: namespace.GetName(),
				},
			},
		}
		Expect(k8sClient.Create(ctx, deployer)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, deployer)
		})

		test.WaitForNotReadyObject(ctx, k8sClient, deployer, v1alpha1.ResourceIsNotAvailable)
	})

	It("should not requeue when the resource is being deleted", func(ctx SpecContext) {
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "deployer-err-deleting",
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		resource := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deleting-resource",
				Namespace: namespace.GetName(),
			},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef: corev1.LocalObjectReference{Name: "test-component"},
				Resource: v1alpha1.ResourceID{
					ByReference: v1alpha1.ResourceReference{
						Resource: runtime.Identity{"name": "deleting-resource"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		deployer := &v1alpha1.Deployer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-deployer-deleting",
			},
			Spec: v1alpha1.DeployerSpec{
				ResourceRef: v1alpha1.ObjectKey{
					Name:      "deleting-resource",
					Namespace: namespace.GetName(),
				},
			},
		}
		Expect(k8sClient.Create(ctx, deployer)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			test.DeleteObject(ctx, k8sClient, deployer)
		})

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

		test.WaitForNotReadyObject(ctx, k8sClient, deployer, v1alpha1.ResourceIsNotAvailable)
	})
})
