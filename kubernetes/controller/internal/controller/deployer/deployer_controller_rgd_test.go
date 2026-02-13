package deployer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

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
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

// setupCTFWithResource creates a CTF with a component version containing a local blob resource.
func setupCTFWithResource(
	ctx context.Context,
	tempDir string,
	componentName, componentVersion string,
	resourceName, resourceVersion string,
	blobContent []byte,
) (string, []byte, error) {
	ctfPath := tempDir
	if err := os.MkdirAll(ctfPath, 0o777); err != nil {
		return "", nil, err
	}

	fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
	if err != nil {
		return "", nil, err
	}
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(tempDir))
	if err != nil {
		return "", nil, err
	}

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
			MediaType: "text/plain",
		},
	}

	blob := inmemory.New(bytes.NewReader(blobContent))
	newRes, err := repo.AddLocalResource(ctx, componentName, componentVersion, resource, blob)
	if err != nil {
		return "", nil, err
	}

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

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return "", nil, err
	}

	repoSpec := &ctfv1.Repository{
		Type:       runtime.Type{Name: "ctf", Version: "v1"},
		FilePath:   ctfPath,
		AccessMode: ctfv1.AccessModeReadOnly,
	}
	specData, err := json.Marshal(repoSpec)
	if err != nil {
		return "", nil, err
	}

	return ctfPath, specData, nil
}

var _ = Describe("Deployer Controller with KRO (RGD)", func() {
	var tempDir string

	rgd := []byte(`apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: valid-rgd
spec:
  schema:
    apiVersion: v1alpha1
    kind: SomeKind
    group: kro.run
    spec:
      testField: string
  resources:
    - id: exampleResource
      template:
        apiVersion: v1
        kind: Pod
        metadata:
          name: some-name
        spec:
          container:
            - name: some-container
              image: some-image:latest`)

	rgdObj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(rgd, rgdObj)).To(Succeed())
	gvk := rgdObj.GroupVersionKind()
	listGVK := gvk.GroupVersion().WithKind(gvk.Kind + "List")

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	Context("deployer controller", func() {
		var resourceObj *v1alpha1.Resource
		var namespace *corev1.Namespace
		var componentName, componentObjName, resourceName, deployerObjName string
		var componentVersion string

		BeforeEach(func(ctx SpecContext) {
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentObjName = test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-resource-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			deployerObjName = "test-deployer-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
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
			By("deleting the resource")
			Expect(k8sClient.Delete(ctx, resourceObj)).To(Succeed())
			Eventually(func(ctx context.Context) error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObj)
				if err != nil {
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				}

				return fmt.Errorf("resource %s still exists", resourceObj.Name)
			}).WithContext(ctx).Should(Succeed())

			deployers := &v1alpha1.DeployerList{}
			Expect(k8sClient.List(ctx, deployers)).To(Succeed())
			Expect(deployers.Items).To(HaveLen(0))

			RGDs := &unstructured.UnstructuredList{}
			RGDs.SetGroupVersionKind(listGVK)
			Expect(k8sClient.List(ctx, RGDs)).To(Succeed())
			Expect(RGDs.Items).To(HaveLen(0))
		})

		It("reconciles a deployer with a valid RGD", func(ctx SpecContext) {
			By("creating a CTF")
			resourceVersion := "1.0.0"
			_, specData, err := setupCTFWithResource(ctx, tempDir, componentName, componentVersion, resourceName, resourceVersion, rgd)
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

			By("mocking a resource")
			hashRgd := sha256.Sum256(rgd)
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObjName,
					},
					Clnt:     k8sClient,
					Recorder: recorder,
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
							Value:                  hex.EncodeToString(hashRgd[:]),
						},
					},
				},
			)

			By("creating a deployer")
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

			By("checking that the deployer has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("checking that the deployed ResourceGraphDefinition is correct")
			rgdObjApplied := &unstructured.Unstructured{}
			rgdObjApplied.SetGroupVersionKind(gvk)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rgdObj), rgdObjApplied)).To(Succeed())
			Expect(rgdObjApplied.Object["spec"]).To(Equal(rgdObj.Object["spec"]))

			By("deleting the deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)

			By("mocking the GC")
			test.DeleteObject(ctx, k8sClient, rgdObj)
		})

		It("does not reconcile a deployer with an invalid RGD", func(ctx SpecContext) {
			By("creating a CTF")
			resourceVersion := "1.0.0"
			invalidRgd := []byte("invalid-rgd")
			_, specData, err := setupCTFWithResource(ctx, tempDir, componentName, componentVersion, resourceName, resourceVersion, invalidRgd)
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

			By("mocking a resource")
			hashRgd := sha256.Sum256(invalidRgd)
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObjName,
					},
					Clnt:     k8sClient,
					Recorder: recorder,
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
							Value:                  hex.EncodeToString(hashRgd[:]),
						},
					},
				},
			)

			By("creating a deployer")
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

			By("checking that the deployer has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, deployerObj, v1alpha1.MarshalFailedReason)

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, deployerObj)
		})

		It("does not reconcile a deployer when the resource is not ready", func(ctx SpecContext) {
			By("mocking a resource")
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentName,
					},
					Clnt:     k8sClient,
					Recorder: recorder,
					ComponentInfo: &v1alpha1.ComponentInfo{
						Component:      componentName,
						Version:        componentVersion,
						RepositorySpec: &apiextensionsv1.JSON{Raw: []byte("{}")},
					},
					ResourceInfo: &v1alpha1.ResourceInfo{
						Name:    resourceName,
						Type:    "resource-not-ready-type",
						Version: "v1.0.0",
						Access:  apiextensionsv1.JSON{Raw: []byte("{}")},
						Digest: &v2.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "genericBlobDigest/v1",
							Value:                  "resource-not-ready-digest",
						},
					},
				},
			)

			By("marking the mocked resource as not ready")
			resourceObjNotReady := &v1alpha1.Resource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObjNotReady)).To(Succeed())
			status.MarkNotReady(recorder, resourceObjNotReady, v1alpha1.ResourceIsNotAvailable, "mock resource is not ready")
			Expect(k8sClient.Status().Update(ctx, resourceObjNotReady)).To(Succeed())

			By("creating a deployer")
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

			By("checking that the deployer has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, deployerObj, v1alpha1.ResourceIsNotAvailable)

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, deployerObj)
		})

		It("updates the RGD when the resource is updated with a valid change", func(ctx SpecContext) {
			By("creating a CTF")
			resourceVersion := "1.0.0"
			_, specData, err := setupCTFWithResource(ctx, tempDir, componentName, componentVersion, resourceName, resourceVersion, rgd)
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

			By("mocking a resource")
			hashRgd := sha256.Sum256(rgd)
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObjName,
					},
					Clnt:     k8sClient,
					Recorder: recorder,
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
							Value:                  hex.EncodeToString(hashRgd[:]),
						},
					},
				},
			)

			By("creating a deployer")
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

			By("checking that the deployer has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})

			By("checking that the deployed ResourceGraphDefinition is correct")
			rgdObjApplied := &unstructured.Unstructured{}
			rgdObjApplied.SetGroupVersionKind(gvk)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rgdObj), rgdObjApplied)).To(Succeed())
			Expect(rgdObjApplied.Object["spec"]).To(Equal(rgdObj.Object["spec"]))

			By("updating the mocked resource")
			componentVersionUpdated := "v1.0.1"
			resourceVersionUpdated := "1.0.1"
			// client go resets the gvk
			rgdObjApplied.SetGroupVersionKind(gvk)

			Expect(unstructured.SetNestedMap(rgdObjApplied.Object, map[string]interface{}{
				"adjustedField": "string",
			}, "spec", "schema", "spec")).To(Succeed())
			rgdObjApplied.SetManagedFields(nil)
			rgdObjApplied.SetResourceVersion("")
			rgdUpdated, err := yaml.Marshal(rgdObjApplied)
			Expect(err).NotTo(HaveOccurred())

			updatedTempDir := GinkgoT().TempDir()
			_, specDataUpdated, err := setupCTFWithResource(ctx, updatedTempDir, componentName, componentVersionUpdated, resourceName, resourceVersionUpdated, rgdUpdated)
			Expect(err).NotTo(HaveOccurred())

			By("updating the mocked resource")
			resourceObjNotReady := &v1alpha1.Resource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObjNotReady)).To(Succeed())
			status.MarkNotReady(recorder, resourceObjNotReady, v1alpha1.ResourceIsNotAvailable, "mock resource is not ready")
			Expect(k8sClient.Status().Update(ctx, resourceObjNotReady)).To(Succeed())

			resourceObjNotReady.Status.Component.Version = componentVersionUpdated
			resourceObjNotReady.Status.Component.RepositorySpec = &apiextensionsv1.JSON{Raw: specDataUpdated}
			resourceObjNotReady.Status.Resource.Version = resourceVersionUpdated
			hashRgdUpdated := sha256.Sum256(rgdUpdated)
			resourceObjNotReady.Status.Resource.Digest = &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				Value:                  hex.EncodeToString(hashRgdUpdated[:]),
			}
			status.MarkReady(recorder, resourceObjNotReady, "updated mock resource")
			Expect(k8sClient.Status().Update(ctx, resourceObjNotReady)).To(Succeed())

			By("checking that the deployer gets reconciled again")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, map[string]any{})
			rgdObjUpdated := &unstructured.Unstructured{}
			rgdObjUpdated.SetGroupVersionKind(gvk)
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rgdObj), rgdObjUpdated))
				g.Expect(rgdObjUpdated.Object["spec"]).To(Equal(rgdObjApplied.Object["spec"]))
			}, "15s").WithContext(ctx).Should(Succeed())

			By("deleting the deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)

			By("mocking the GC")
			test.DeleteObject(ctx, k8sClient, rgdObj)
		})

		It("fails when the resource is updated with an invalid change", func(ctx SpecContext) {
			By("creating a CTF")
			resourceVersion := "1.0.0"
			_, specData, err := setupCTFWithResource(ctx, tempDir, componentName, componentVersion, resourceName, resourceVersion, rgd)
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

			By("mocking a resource")
			hashRgd := sha256.Sum256(rgd)
			resourceObj = test.MockResource(
				ctx,
				resourceName,
				namespace.GetName(),
				&test.MockResourceOptions{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObjName,
					},
					Clnt:     k8sClient,
					Recorder: recorder,
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
							Value:                  hex.EncodeToString(hashRgd[:]),
						},
					},
				},
			)

			By("creating a deployer")
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

			By("checking that the deployer has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, deployerObj, nil)

			By("checking that the deployed ResourceGraphDefinition is correct")
			rgdObjApplied := &unstructured.Unstructured{}
			rgdObjApplied.SetGroupVersionKind(gvk)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rgdObj), rgdObjApplied)).To(Succeed())
			Expect(rgdObjApplied.Object["spec"]).To(Equal(rgdObj.Object["spec"]))

			By("updating the mocked resource with invalid content")
			componentVersionUpdated := "v1.0.1"
			resourceVersionUpdated := "1.0.1"
			invalidRgd := []byte("invalid-rgd")

			updatedTempDir := GinkgoT().TempDir()
			_, specDataUpdated, err := setupCTFWithResource(ctx, updatedTempDir, componentName, componentVersionUpdated, resourceName, resourceVersionUpdated, invalidRgd)
			Expect(err).NotTo(HaveOccurred())

			By("updating the mocked resource")
			resourceObjNotReady := &v1alpha1.Resource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObjNotReady)).To(Succeed())
			status.MarkNotReady(recorder, resourceObjNotReady, v1alpha1.ResourceIsNotAvailable, "mock resource is not ready")
			Expect(k8sClient.Status().Update(ctx, resourceObjNotReady)).To(Succeed())

			resourceObjNotReady.Status.Component.Version = componentVersionUpdated
			resourceObjNotReady.Status.Component.RepositorySpec = &apiextensionsv1.JSON{Raw: specDataUpdated}
			resourceObjNotReady.Status.Resource.Version = resourceVersionUpdated
			hashRgdInvalid := sha256.Sum256(invalidRgd)
			resourceObjNotReady.Status.Resource.Digest = &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				Value:                  hex.EncodeToString(hashRgdInvalid[:]),
			}
			status.MarkReady(recorder, resourceObjNotReady, "updated mock resource")
			Expect(k8sClient.Status().Update(ctx, resourceObjNotReady)).To(Succeed())

			By("checking that the deployer gets reconciled again and fails")
			test.WaitForNotReadyObject(ctx, k8sClient, deployerObj, v1alpha1.MarshalFailedReason)

			By("deleting the deployer")
			test.DeleteObject(ctx, k8sClient, deployerObj)

			By("mocking the GC")
			test.DeleteObject(ctx, k8sClient, rgdObj)
		})
	})
})
