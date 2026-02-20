package deployer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/apis/meta"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"

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

var _ = Describe("Deployer Controller with YAML stream (ConfigMap + Secret)", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	Context("deployer controller (yaml stream)", func() {
		var resourceObj *v1alpha1.Resource
		var namespace *corev1.Namespace
		var componentName, resourceName, deployerObjName string
		var componentVersion string

		BeforeEach(func(ctx SpecContext) {
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
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

			By("mocking a Resource that references the YAML stream")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{Name: componentName},
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
			resourceObj                                   *v1alpha1.Resource
			namespace                                     *corev1.Namespace
			componentName, resourceName, componentVersion string
			specData                                      []byte
			credentialSecret                              *corev1.Secret
		)

		BeforeEach(func(ctx SpecContext) {
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
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

			By("mocking a Resource with EffectiveOCMConfig containing propagate entries")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{Name: componentName},
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

			By("mocking a Resource with EffectiveOCMConfig containing propagate entries")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{Name: componentName},
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
})
