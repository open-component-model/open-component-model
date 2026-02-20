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

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
						Name:    v2.LocalBlobAccessType,
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

			By("deleting the Deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)
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
						Name:    v2.LocalBlobAccessType,
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
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
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
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       credentialSecret.Name,
							Namespace:  credentialSecret.Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
				)),
			)

			By("deleting the Deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)
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
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
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
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
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

			By("waiting until the Deployer is Ready")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("checking deployer's effective OCM config uses only its own config")
			Eventually(komega.Object(deployerObj), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       deployerSecret.Name,
							Namespace:  deployerSecret.Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
				)),
			)

			By("deleting the Deployer")
			_ = k8sClient.Delete(ctx, deployerSecret)
			test.DeleteObject(ctx, k8sClient, deployerObj)
		})
	})

	Context("verified component cache behavior", func() {
		It("uses separate cache entries for verified and unverified component versions", Serial, func(ctx SpecContext) {
			workerpool.CacheMissCounterTotal.Reset()
			workerpool.CacheHitCounterTotal.Reset()

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
						Name:    v2.LocalBlobAccessType,
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
			Expect(testutil.ToFloat64(verifiedHit)).To(Equal(float64(1)),
				"expected at least 1 cache hit for the verified component on subsequent reconciliation")

			By("deleting the Deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)
		})

		It("maintains integrity chain for referenced component via reference path", Serial, func(ctx SpecContext) {
			workerpool.CacheMissCounterTotal.Reset()
			workerpool.CacheHitCounterTotal.Reset()

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
						Name:    v2.LocalBlobAccessType,
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

			patchHelper := patch.NewSerialPatcher(resourceObj, k8sClient)
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

				return status.UpdateStatus(ctx, patchHelper, resourceObj, recorder, time.Hour, nil)
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

			parentMissUnverified, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(parentComponentName, componentVersion, "unverified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(parentMissUnverified)).To(Equal(float64(0)),
				"expected 0 unverified cache misses for the parent — it should always be resolved with verifications")

			By("checking cache metrics for child component resolved via integrity chain")
			childMissVerified, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(childComponentName, componentVersion, "verified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(childMissVerified)).To(Equal(float64(1)),
				"expected 1 cache miss for the child component resolved via digest from parent reference")

			childMissUnverified, err := workerpool.CacheMissCounterTotal.GetMetricWithLabelValues(childComponentName, componentVersion, "unverified")
			Expect(err).ToNot(HaveOccurred())
			Expect(testutil.ToFloat64(childMissUnverified)).To(Equal(float64(0)),
				"expected 0 unverified cache misses for the child — it should be resolved via integrity chain digest")

			By("deleting the Deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)
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
						Name:    v2.LocalBlobAccessType,
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

			patchHelper := patch.NewSerialPatcher(resourceObj, k8sClient)
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

				return status.UpdateStatus(ctx, patchHelper, resourceObj, recorder, time.Hour, nil)
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
