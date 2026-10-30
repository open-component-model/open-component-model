package resource

import (
	"bytes"
	"encoding/json"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencontainers/go-digest"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ = Describe("Resource Controller verification", func() {
	// A local blob is checked against the digest the repository stores it under, so a
	// descriptor recording another digest fails without the blob being downloaded.
	DescribeTable("verifies a local blob against its descriptor digest",
		func(ctx SpecContext, recordOtherDigest bool) {
			name := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName := "ocm.software/local-verification-" + name
			namespace := test.NamespaceForTest(ctx)
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			By("storing a component version with a local blob in a CTF")
			ctfPath := GinkgoT().TempDir()
			fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
			Expect(err).NotTo(HaveOccurred())
			repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(GinkgoT().TempDir()))
			Expect(err).NotTo(HaveOccurred())

			res := &descruntime.Resource{
				ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "data", Version: "v1.0.0"}},
				Type:        "plainText",
				Relation:    descruntime.LocalRelation,
				Access: &v2.LocalBlob{
					Type:      runtime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
					MediaType: "text/plain",
				},
			}
			stored, err := repo.AddLocalResource(ctx, componentName, "v1.0.0", res, inmemory.New(bytes.NewReader([]byte("content"))))
			Expect(err).NotTo(HaveOccurred())
			if recordOtherDigest {
				stored.Digest.Value = digest.FromString("other").Encoded()
			}
			Expect(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
				Meta: descruntime.Meta{Version: "v2"},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
					Provider:      descruntime.Provider{Name: "ocm.software"},
					Resources:     []descruntime.Resource{*stored},
				},
			})).To(Succeed())
			specData, err := json.Marshal(&ctfv1.Repository{
				Type:       runtime.Type{Name: "ctf", Version: "v1"},
				FilePath:   ctfPath,
				AccessMode: ctfv1.AccessModeReadOnly,
			})
			Expect(err).NotTo(HaveOccurred())

			componentObj := test.MockComponent(ctx, "component-"+name, namespace.GetName(), &test.MockComponentOptions{
				Client:   k8sClient,
				Recorder: recorder,
				Info: v1alpha1.ComponentInfo{
					Component:      componentName,
					Version:        "v1.0.0",
					RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
				},
				Repository: "ocm.software/test-repository",
			})
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{Name: "resource-" + name, Namespace: namespace.GetName()},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "data"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

			if recordOtherDigest {
				test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason, "digest mismatch")
				return
			}
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{"Status.Resource.Name": "data"})
		},
		Entry("the recorded digest", false),
		Entry("another recorded digest", true),
	)

	// Without a digest processor for its access type (here a type no plugin provides) a
	// resource cannot be verified, so it fails unless verification is skipped.
	It("fails a resource that cannot be verified", func(ctx SpecContext) {
		componentName := "ocm.software/unverifiable"
		namespace := test.NamespaceForTest(ctx)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		_, specData := test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), []*descruntime.Descriptor{{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName, Version: "v1.0.0"}},
				Provider:      descruntime.Provider{Name: "ocm.software"},
				Resources: []descruntime.Resource{{
					ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "source", Version: "v1.0.0"}},
					Type:        "blob",
					Relation:    descruntime.ExternalRelation,
					Access: &runtime.Raw{
						Type: runtime.Type{Name: "unknownAccess", Version: "v1"},
						Data: []byte(`{"type":"unknownAccess/v1"}`),
					},
				}},
			},
		}})

		componentObj := test.MockComponent(ctx, "unverifiable", namespace.GetName(), &test.MockComponentOptions{
			Client:   k8sClient,
			Recorder: recorder,
			Info: v1alpha1.ComponentInfo{
				Component:      componentName,
				Version:        "v1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: specData},
			},
			Repository: "ocm.software/test-repository",
		})
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, componentObj) })

		resourceObj := &v1alpha1.Resource{
			ObjectMeta: metav1.ObjectMeta{Name: "unverifiable", Namespace: namespace.GetName()},
			Spec: v1alpha1.ResourceSpec{
				ComponentRef: corev1.LocalObjectReference{Name: componentObj.GetName()},
				Resource: v1alpha1.ResourceID{
					ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": "source"}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { test.DeleteObject(ctx, k8sClient, resourceObj) })

		test.WaitForNotReadyObjectWithMessage(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason, "cannot be verified")
	})
})
