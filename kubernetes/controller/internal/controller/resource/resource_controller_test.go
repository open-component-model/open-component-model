package resource

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/apis/meta"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

var _ = Describe("Resource Controller", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	Context("resource controller", func() {
		var componentName, componentObjName, resourceName string
		var componentVersion string
		repositoryName := "ocm.software/test-repository"

		BeforeEach(func(ctx SpecContext) {
			componentObjName = test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-resource-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentVersion = "v1.0.0"

			namespace := test.NamespaceForTest(ctx)

			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			DeferCleanup(func(ctx SpecContext) {
				resources := &v1alpha1.ResourceList{}
				Expect(k8sClient.List(ctx, resources, client.InNamespace(namespace.GetName()))).To(Succeed())
				Expect(resources.Items).To(BeEmpty(), "make sure all resources are deleted and there are no leftovers from the test")
			})
		})

		type testCase struct {
			Registry      string
			Repository    string
			Reference     string
			HELMChart     string
			GithubRepoURL string
			GitRepository string
		}

		DescribeTable("reconciles a created resource",
			func(ctx SpecContext, createDescriptors func() ([]*descruntime.Descriptor, string), tc *testCase) {
				By("creating a CTF")
				descs, ctfPath := createDescriptors()
				Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
				_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, descs)

				By("mocking a component")
				namespace := test.NamespaceForTest(ctx)
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
						Repository: repositoryName,
					},
				)
				DeferCleanup(func(ctx SpecContext) {
					test.DeleteObject(ctx, k8sClient, componentObj)
				})

				additionalStatusFields := map[string]string{}
				if tc != nil {
					if tc.Registry != "" {
						additionalStatusFields["registry"] = "resource.access.toOCI().registry"
					}
					if tc.Repository != "" {
						additionalStatusFields["repository"] = "resource.access.toOCI().repository"
					}
					if tc.Reference != "" {
						additionalStatusFields["reference"] = "resource.access.toOCI().reference"
					}
					if tc.HELMChart != "" {
						additionalStatusFields["helmChart"] = "resource.access.helmChart"
					}
					if tc.GithubRepoURL != "" {
						additionalStatusFields["gitRepoURL"] = "resource.access.repoUrl"
					}
					if tc.GitRepository != "" {
						additionalStatusFields["gitRepository"] = "resource.access.repository"
					}
				}

				By("creating a resource")
				resourceObj := &v1alpha1.Resource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: namespace.GetName(),
					},
					Spec: v1alpha1.ResourceSpec{
						ComponentRef: corev1.LocalObjectReference{
							Name: componentObj.GetName(),
						},
						Resource: v1alpha1.ResourceID{
							ByReference: v1alpha1.ResourceReference{
								Resource: runtime.Identity{"name": resourceName},
							},
						},
						AdditionalStatusFields: additionalStatusFields,
					},
				}
				Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
				DeferCleanup(func(ctx SpecContext) {
					test.DeleteObject(ctx, k8sClient, resourceObj)
				})

				By("checking that the resource has been reconciled successfully")

				fields := map[string]any{
					"Status.Component.Component": descs[0].Component.Name,
					"Status.Component.Version":   descs[0].Component.Version,
					"Status.Resource.Name":       descs[0].Component.Resources[0].Name,
					"Status.Resource.Type":       descs[0].Component.Resources[0].Type,
					"Status.Resource.Version":    descs[0].Component.Resources[0].Version,
				}

				if tc != nil {
					m := map[string]apiextensionsv1.JSON{}
					if tc.Registry != "" {
						m["registry"] = mustToJSON(tc.Registry)
					}
					if tc.Repository != "" {
						m["repository"] = mustToJSON(tc.Repository)
					}
					if tc.Reference != "" {
						m["reference"] = mustToJSON(tc.Reference)
					}
					if tc.HELMChart != "" {
						m["helmChart"] = mustToJSON(tc.HELMChart)
					}
					if tc.GithubRepoURL != "" {
						m["gitRepoURL"] = mustToJSON(tc.GithubRepoURL)
					}
					if tc.GitRepository != "" {
						m["gitRepository"] = mustToJSON(tc.GitRepository)
					}
					fields["Status.Additional"] = m
				}

				test.WaitForReadyObject(ctx, k8sClient, resourceObj, fields)
			},

			Entry("plain text", func() ([]*descruntime.Descriptor, string) {
				ctfName := "plainText"
				ctfPath := filepath.Join(tempDir, ctfName)
				return []*descruntime.Descriptor{
					{
						Component: descruntime.Component{
							ComponentMeta: descruntime.ComponentMeta{
								ObjectMeta: descruntime.ObjectMeta{
									Name:    componentName,
									Version: componentVersion,
								},
							},
							Resources: []descruntime.Resource{
								{
									ElementMeta: descruntime.ElementMeta{
										ObjectMeta: descruntime.ObjectMeta{
											Name:    resourceName,
											Version: "1.0.0",
										},
									},
									Type:     "plainText",
									Relation: descruntime.LocalRelation,
									Access: &v2.LocalBlob{
										Type: runtime.Type{
											Name:    v2.LocalBlobAccessType,
											Version: v2.LocalBlobAccessTypeVersion,
										},
										LocalReference: "sha256:1234567890",
										MediaType:      "text/plain",
									},
								},
							},
							Provider: descruntime.Provider{Name: "ocm.software"},
						},
					},
				}, ctfPath
			},
				nil),
			Entry("OCI artifact access", func() ([]*descruntime.Descriptor, string) {
				ctfName := "ociArtifactAccess"
				ctfPath := filepath.Join(tempDir, ctfName)
				access := ocispec.OCIImage{
					Type: runtime.Type{
						Name:    "ociArtifact",
						Version: "v1",
					},
					ImageReference: "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0",
				}

				rawAccess := &runtime.Raw{}
				Expect(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&access, rawAccess)).To(Succeed())

				return []*descruntime.Descriptor{
					{
						Component: descruntime.Component{
							ComponentMeta: descruntime.ComponentMeta{
								ObjectMeta: descruntime.ObjectMeta{
									Name:    componentName,
									Version: componentVersion,
								},
							},
							Resources: []descruntime.Resource{
								{
									ElementMeta: descruntime.ElementMeta{
										ObjectMeta: descruntime.ObjectMeta{
											Name:    resourceName,
											Version: "1.0.0",
										},
									},
									Type:     "ociArtifact",
									Relation: descruntime.ExternalRelation,
									Access:   rawAccess,
								},
							},
							Provider: descruntime.Provider{Name: "ocm.software"},
						},
					},
				}, ctfPath
			},
				&testCase{
					Registry:   "ghcr.io",
					Repository: "open-component-model/ocm/ocm.software/ocmcli/ocmcli-image",
					Reference:  "0.24.0@sha256:7a91508d9177f43552b60cfc0182d7c30a84e95bed03854855b3ab29b6a85db2",
				},
			),
			Entry("Helm access", func() ([]*descruntime.Descriptor, string) {
				ctfName := "helmAccess"
				ctfPath := filepath.Join(tempDir, ctfName)
				return []*descruntime.Descriptor{
					{
						Component: descruntime.Component{
							ComponentMeta: descruntime.ComponentMeta{
								ObjectMeta: descruntime.ObjectMeta{
									Name:    componentName,
									Version: componentVersion,
								},
							},
							Resources: []descruntime.Resource{
								{
									ElementMeta: descruntime.ElementMeta{
										ObjectMeta: descruntime.ObjectMeta{
											Name:    resourceName,
											Version: "1.0.0",
										},
									},
									Type:     "helmChart",
									Relation: descruntime.ExternalRelation,
									Access: &runtime.Raw{
										Type: runtime.Type{
											Name:    "helmChart",
											Version: "v1",
										},
										Data: mustMarshalJSON(map[string]any{
											"helmChart":      "podinfo:6.9.1",
											"helmRepository": "oci://ghcr.io/stefanprodan/charts",
										}),
									},
								},
							},
							Provider: descruntime.Provider{Name: "ocm.software"},
						},
					},
				}, ctfPath
			},
				&testCase{
					HELMChart: "podinfo:6.9.1",
				},
			),
			Entry("GitHub access", func() ([]*descruntime.Descriptor, string) {
				ctfName := "gitHubAccess"
				ctfPath := filepath.Join(tempDir, ctfName)
				return []*descruntime.Descriptor{
					{
						Component: descruntime.Component{
							ComponentMeta: descruntime.ComponentMeta{
								ObjectMeta: descruntime.ObjectMeta{
									Name:    componentName,
									Version: componentVersion,
								},
							},
							Resources: []descruntime.Resource{
								{
									ElementMeta: descruntime.ElementMeta{
										ObjectMeta: descruntime.ObjectMeta{
											Name:    resourceName,
											Version: "1.0.0",
										},
									},
									Type:     "directoryTree",
									Relation: descruntime.ExternalRelation,
									Access: &runtime.Raw{
										Type: runtime.Type{
											Name:    "github",
											Version: "v1",
										},
										Data: mustMarshalJSON(map[string]any{
											"repoUrl": "https://github.com/open-component-model/ocm-k8s-toolkit",
											"apiUrl":  "/repos/open-component-model/ocm-k8s-toolkit",
											"commit":  "8f7e04f4b58d2a9e22f88e79dddfc36183688f28",
										}),
									},
								},
							},
							Provider: descruntime.Provider{Name: "ocm.software"},
						},
					},
				}, ctfPath
			},
				&testCase{
					GithubRepoURL: "https://github.com/open-component-model/ocm-k8s-toolkit",
				},
			),
			Entry("git access", func() ([]*descruntime.Descriptor, string) {
				ctfName := "gitAccess"
				ctfPath := filepath.Join(tempDir, ctfName)
				return []*descruntime.Descriptor{
					{
						Component: descruntime.Component{
							ComponentMeta: descruntime.ComponentMeta{
								ObjectMeta: descruntime.ObjectMeta{
									Name:    componentName,
									Version: componentVersion,
								},
							},
							Resources: []descruntime.Resource{
								{
									ElementMeta: descruntime.ElementMeta{
										ObjectMeta: descruntime.ObjectMeta{
											Name:    resourceName,
											Version: "1.0.0",
										},
									},
									Type:     "directoryTree",
									Relation: descruntime.ExternalRelation,
									Access: &runtime.Raw{
										Type: runtime.Type{
											Name:    "git",
											Version: "v1",
										},
										Data: mustMarshalJSON(map[string]any{
											"repository": "https://github.com/open-component-model/ocm-k8s-toolkit",
											"ref":        "refs/heads/main",
										}),
									},
								},
							},
							Provider: descruntime.Provider{Name: "ocm.software"},
						},
					},
				}, ctfPath
			},
				&testCase{
					GitRepository: "https://github.com/open-component-model/ocm-k8s-toolkit",
				},
			),
		)

		It("should reconcile when the resource has extra identities", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "resource-with-extra-identities"
			ctfPath := filepath.Join(tempDir, ctfName)
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			extraIdentity := runtime.Identity{
				"key1": "value1",
				"key2": "value2",
			}
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
									ExtraIdentity: extraIdentity,
								},
								Type:     "plainText",
								Relation: descruntime.LocalRelation,
								Access: &v2.LocalBlob{
									Type: runtime.Type{
										Name:    v2.LocalBlobAccessType,
										Version: v2.LocalBlobAccessTypeVersion,
									},
									LocalReference: "sha256:1234567890",
									MediaType:      "text/plain",
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking a component")
			namespace := test.NamespaceForTest(ctx)
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
					Repository: repositoryName,
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("creating a resource")
			identity := extraIdentity.Clone()
			identity["name"] = resourceName
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: identity,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has been reconciled successfully")
			var expExtraIdentity map[string]string
			expExtraIdentity = extraIdentity.DeepCopy()
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Component.Component":    componentName,
				"Status.Component.Version":      componentVersion,
				"Status.Resource.Name":          resourceName,
				"Status.Resource.ExtraIdentity": expExtraIdentity,
			})
		})

		It("should not reconcile when the resource has non-matching extra identities", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "resource-without-matching-extra-identities"
			ctfPath := filepath.Join(tempDir, ctfName)
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			extraIdentity := runtime.Identity{
				"key1": "value1",
				"key2": "value2",
			}
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
									ExtraIdentity: extraIdentity,
								},
								Type:     "plainText",
								Relation: descruntime.LocalRelation,
								Access: &v2.LocalBlob{
									Type: runtime.Type{
										Name:    v2.LocalBlobAccessType,
										Version: v2.LocalBlobAccessTypeVersion,
									},
									LocalReference: "sha256:1234567890",
									MediaType:      "text/plain",
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking a component")
			namespace := test.NamespaceForTest(ctx)
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
					Repository: repositoryName,
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("creating a resource")
			identity := extraIdentity.Clone()
			identity["name"] = resourceName
			// Mismatched extra identity
			identity["extra"] = "non-matching-value"
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: identity,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason)

		})

		It("should not reconcile when the component is not ready", func(ctx SpecContext) {
			By("mocking a component")
			namespace := test.NamespaceForTest(ctx)
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
						RepositorySpec: &apiextensionsv1.JSON{Raw: []byte("{}")},
					},
					Repository: repositoryName,
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("marking the mocked component as not ready")
			componentObjNotReady := &v1alpha1.Component{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObjNotReady)).To(Succeed())

			status.MarkNotReady(recorder, componentObjNotReady, v1alpha1.ResourceIsNotAvailable, "mock component is not ready")
			Expect(k8sClient.Status().Update(ctx, componentObjNotReady)).To(Succeed())

			By("creating a resource")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": resourceName},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, resourceObj, v1alpha1.ResourceIsNotAvailable)
		})

		// This test is checking that the resource is reconciled again when the status of the component changes.
		It("reconciles when the component is updated to ready status", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "component-ready"
			ctfPath := filepath.Join(tempDir, ctfName)
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
								},
								Type:     "ociArtifact",
								Relation: descruntime.ExternalRelation,
								Access: &runtime.Raw{
									Type: runtime.Type{
										Name:    "ociArtifact",
										Version: "v1",
									},
									Data: mustMarshalJSON(map[string]any{
										"imageReference": "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0",
									}),
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking a component")
			namespace := test.NamespaceForTest(ctx)
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
					Repository: repositoryName,
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("marking the mocked component as not ready")
			componentObjNotReady := &v1alpha1.Component{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObjNotReady)).To(Succeed())

			status.MarkNotReady(recorder, componentObjNotReady, v1alpha1.ResourceIsNotAvailable, "mock component is not ready")
			Expect(k8sClient.Status().Update(ctx, componentObjNotReady)).To(Succeed())

			By("creating a resource")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": resourceName},
						},
					},
					AdditionalStatusFields: map[string]string{
						"registry":   "resource.access.toOCI().registry",
						"repository": "resource.access.toOCI().repository",
						"reference":  "resource.access.toOCI().reference",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, resourceObj, v1alpha1.ResourceIsNotAvailable)

			By("updating the component to ready")
			componentObjReady := &v1alpha1.Component{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObjReady)).To(Succeed())

			status.MarkReady(recorder, componentObjReady, "mock component is ready")
			Expect(k8sClient.Status().Update(ctx, componentObjReady)).To(Succeed())

			By("checking that the resource has updated its additional status to the new version")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Component.Component": componentName,
				"Status.Component.Version":   componentVersion,
				"Status.Resource.Name":       resourceName,
				"Status.Resource.Type":       "ociArtifact",
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"registry":   mustToJSON("ghcr.io"),
					"repository": mustToJSON("open-component-model/ocm/ocm.software/ocmcli/ocmcli-image"),
					"reference":  mustToJSON("0.24.0"),
				},
			})
		})

		// This test checks if the resource is reconciled again, when the resource spec is updated.
		It("reconciles again when the resource changes", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "resource-change"
			resourceVersionUpdated := "1.0.1"
			ctfPath := filepath.Join(tempDir, ctfName)
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
								},
								Type:     "ociArtifact",
								Relation: descruntime.ExternalRelation,
								Access: &runtime.Raw{
									Type: runtime.Type{
										Name:    "ociArtifact",
										Version: "v1",
									},
									Data: mustMarshalJSON(map[string]any{
										"imageReference": "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.23.0",
									}),
								},
							},
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: resourceVersionUpdated,
									},
								},
								Type:     "ociArtifact",
								Relation: descruntime.ExternalRelation,
								Access: &runtime.Raw{
									Type: runtime.Type{
										Name:    "ociArtifact",
										Version: "v1",
									},
									Data: mustMarshalJSON(map[string]any{
										"imageReference": "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0",
									}),
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking a component")
			namespace := test.NamespaceForTest(ctx)
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
					Repository: repositoryName,
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("creating a resource")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": resourceName, "version": "1.0.0"},
						},
					},
					AdditionalStatusFields: map[string]string{
						"reference": "resource.access.toOCI().reference",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"reference": mustToJSON("0.23.0"),
				},
			})

			By("updating resource spec")
			resourceObjUpdate := &v1alpha1.Resource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObjUpdate)).To(Succeed())

			resourceObjUpdate.Spec.Resource = v1alpha1.ResourceID{
				ByReference: v1alpha1.ResourceReference{
					Resource: runtime.Identity{"name": resourceName, "version": resourceVersionUpdated},
				},
			}
			Expect(k8sClient.Update(ctx, resourceObjUpdate)).To(Succeed())

			By("checking that the updated resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Resource.Version": resourceVersionUpdated,
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"reference": mustToJSON("0.24.0"),
				},
			})
		})

		// In this test the component version is updated with a new resource. This should trigger the control-loop of
		// the resource and we expect an updated source reference.
		It("reconciles again when the component and resource changes", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "component-change"
			ctfPath := filepath.Join(tempDir, ctfName)
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
								},
								Type:     "ociArtifact",
								Relation: descruntime.ExternalRelation,
								Access: &runtime.Raw{
									Type: runtime.Type{
										Name:    "ociArtifact",
										Version: "v1",
									},
									Data: mustMarshalJSON(map[string]any{
										"imageReference": "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.23.0",
									}),
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking a component")
			namespace := test.NamespaceForTest(ctx)
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
					Repository: repositoryName,
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("creating a resource")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": resourceName},
						},
					},
					AdditionalStatusFields: map[string]string{
						"reference": "resource.access.toOCI().reference",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has been reconciled successfully")
			expected := &testCase{
				Registry:   "ghcr.io",
				Repository: "open-component-model/ocm/ocm.software/ocmcli/ocmcli-image",
				Reference:  "0.23.0",
			}
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"reference": mustToJSON(expected.Reference),
				},
			})

			By("updating the component version with a new resource")
			componentVersionUpdated := "v1.0.1"
			test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersionUpdated,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.1",
									},
								},
								Type:     "ociArtifact",
								Relation: descruntime.ExternalRelation,
								Access: &runtime.Raw{
									Type: runtime.Type{
										Name:    "ociArtifact",
										Version: "v1",
									},
									Data: mustMarshalJSON(map[string]any{
										"imageReference": "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0",
									}),
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("updating mock component status")
			componentObjUpdate := &v1alpha1.Component{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObjUpdate)).To(Succeed())

			componentObjUpdate.Status.Component.Version = componentVersionUpdated
			Expect(k8sClient.Status().Update(ctx, componentObjUpdate)).To(Succeed())

			By("updating mock component spec")
			componentObjUpdate = &v1alpha1.Component{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObjUpdate)).To(Succeed())

			componentObjUpdate.Spec.Semver = componentVersionUpdated
			Expect(k8sClient.Update(ctx, componentObjUpdate)).To(Succeed())

			// component spec update should trigger resource reconciliation
			By("checking that the resource was reconciled again")
			expected = &testCase{
				Registry:   "ghcr.io",
				Repository: "open-component-model/ocm/ocm.software/ocmcli/ocmcli-image",
				Reference:  "0.24.0",
			}
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Component.Version": componentVersionUpdated,
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"reference": mustToJSON(expected.Reference),
				},
			})

		})

		It("reconcile a nested component by reference path", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "nested-component"
			nestedComponentName := "ocm.software/nested-component"
			nestedComponentReference := "some-reference"
			ctfPath := filepath.Join(tempDir, ctfName)
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						References: []descruntime.Reference{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    nestedComponentReference,
										Version: componentVersion,
									},
								},
								Component: nestedComponentName,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    nestedComponentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
								},
								Type:     "ociArtifact",
								Relation: descruntime.ExternalRelation,
								Access: &runtime.Raw{
									Type: runtime.Type{
										Name:    "ociArtifact",
										Version: "v1",
									},
									Data: mustMarshalJSON(map[string]any{
										"imageReference": "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.23.0",
									}),
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking a component")
			namespace := test.NamespaceForTest(ctx)
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
					Repository: repositoryName,
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("creating a resource")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource:      runtime.Identity{"name": resourceName},
							ReferencePath: []runtime.Identity{{"name": nestedComponentReference}},
						},
					},
					AdditionalStatusFields: map[string]string{
						"reference": "resource.access.toOCI().reference",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"reference": mustToJSON("0.23.0"),
				},
				"Status.Component.Component": nestedComponentName,
				"Status.Component.Version":   componentVersion,
			})
		})

	})

	Context("ocm config propagation from component to resource", func() {
		var componentName, componentObjName, resourceName, componentVersion string
		repositoryName := "ocm.software/test-repository"

		BeforeEach(func(ctx SpecContext) {
			componentObjName = test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-resource-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentVersion = "v1.0.0"

			namespace := test.NamespaceForTest(ctx)
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			DeferCleanup(func(ctx SpecContext) {
				resources := &v1alpha1.ResourceList{}
				Expect(k8sClient.List(ctx, resources, client.InNamespace(namespace.GetName()))).To(Succeed())
				Expect(resources.Items).To(BeEmpty())
			})
		})

		It("resource without ocmConfig inherits propagate entries from component", func(ctx SpecContext) {
			By("creating a CTF")
			ctfPath := filepath.Join(tempDir, "res-inherit-config")
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
								},
								Type:     "plainText",
								Relation: descruntime.LocalRelation,
								Access: &v2.LocalBlob{
									Type: runtime.Type{
										Name:    v2.LocalBlobAccessType,
										Version: v2.LocalBlobAccessTypeVersion,
									},
									LocalReference: "sha256:1234567890",
									MediaType:      "text/plain",
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			namespace := test.NamespaceForTest(ctx)

			By("creating a credential secret")
			credSecret := &corev1.Secret{
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
			Expect(k8sClient.Create(ctx, credSecret)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				_ = k8sClient.Delete(ctx, credSecret)
			})

			By("mocking a component with EffectiveOCMConfig")
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
					Repository: repositoryName,
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       credSecret.Name,
								Namespace:  credSecret.Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       "do-not-propagate-secret",
								Namespace:  namespace.GetName(),
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("creating a resource without ocmConfig")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": resourceName},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Component.Component": componentName,
			})

			By("checking resource inherited only propagate entries from component")
			Eventually(komega.Object(resourceObj), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       credSecret.Name,
							Namespace:  credSecret.Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
				)),
			)
		})

		It("resource with explicit ocmConfig ignores parent component config", func(ctx SpecContext) {
			By("creating a CTF")
			ctfPath := filepath.Join(tempDir, "res-explicit-config")
			Expect(os.MkdirAll(ctfPath, 0o777)).To(Succeed())
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfPath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersion,
							},
						},
						Resources: []descruntime.Resource{
							{
								ElementMeta: descruntime.ElementMeta{
									ObjectMeta: descruntime.ObjectMeta{
										Name:    resourceName,
										Version: "1.0.0",
									},
								},
								Type:     "plainText",
								Relation: descruntime.LocalRelation,
								Access: &v2.LocalBlob{
									Type: runtime.Type{
										Name:    v2.LocalBlobAccessType,
										Version: v2.LocalBlobAccessTypeVersion,
									},
									LocalReference: "sha256:1234567890",
									MediaType:      "text/plain",
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			namespace := test.NamespaceForTest(ctx)

			By("creating secrets")
			parentSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      "parent-cred-secret",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: parent.example.com
  credentials:
  - type: Credentials
    properties:
      username: parentuser
      password: parentpass
`),
				},
			}
			Expect(k8sClient.Create(ctx, parentSecret)).To(Succeed())

			resourceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      "resource-own-secret",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: resource.example.com
  credentials:
  - type: Credentials
    properties:
      username: resourceuser
      password: resourcepass
`),
				},
			}
			Expect(k8sClient.Create(ctx, resourceSecret)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				_ = k8sClient.Delete(ctx, parentSecret)
				_ = k8sClient.Delete(ctx, resourceSecret)
			})

			By("mocking a component with EffectiveOCMConfig")
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
					Repository: repositoryName,
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       parentSecret.Name,
								Namespace:  parentSecret.Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
					},
				},
			)
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, componentObj)
			})

			By("creating a resource with its own ocmConfig")
			resourceObj := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{
						Name: componentObj.GetName(),
					},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": resourceName},
						},
					},
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       resourceSecret.Name,
								Namespace:  resourceSecret.Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				test.DeleteObject(ctx, k8sClient, resourceObj)
			})

			By("checking that the resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Component.Component": componentName,
			})

			By("checking resource uses only its own config, not the parent's")
			Eventually(komega.Object(resourceObj), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       resourceSecret.Name,
							Namespace:  resourceSecret.Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
				)),
			)
		})
	})
})

func mustToJSON(v string) apiextensionsv1.JSON {
	raw, err := json.Marshal(v)
	Expect(err).ToNot(HaveOccurred())
	return apiextensionsv1.JSON{Raw: raw}
}

func mustMarshalJSON(v any) []byte {
	raw, err := json.Marshal(v)
	Expect(err).ToNot(HaveOccurred())
	return raw
}
