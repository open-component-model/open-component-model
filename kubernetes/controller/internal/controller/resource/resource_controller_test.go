package resource

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/mandelsoft/vfs/pkg/osfs"
	"github.com/mandelsoft/vfs/pkg/projectionfs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	. "ocm.software/ocm/api/helper/builder"
	ocmmetav1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"ocm.software/ocm/api/ocm/extensions/accessmethods/git"
	"ocm.software/ocm/api/ocm/extensions/accessmethods/github"
	"ocm.software/ocm/api/ocm/extensions/accessmethods/helm"
	ocmociartifact "ocm.software/ocm/api/ocm/extensions/accessmethods/ociartifact"
	"ocm.software/ocm/api/ocm/extensions/artifacttypes"
	"ocm.software/ocm/api/ocm/extensions/repositories/ctf"
	"ocm.software/ocm/api/utils/accessio"
	"ocm.software/ocm/api/utils/mime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	environment "ocm.software/ocm/api/helper/env"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

var _ = Describe("Resource Controller", func() {
	var (
		env     *Builder
		tempDir string
	)

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
		fs, err := projectionfs.New(osfs.OsFs, tempDir)
		Expect(err).NotTo(HaveOccurred())
		env = NewBuilder(environment.FileSystem(fs))
	})
	AfterEach(func() {
		Expect(env.Cleanup()).To(Succeed())
	})

	Context("resource controller", func() {
		var componentObj *v1alpha1.Component
		var namespace *corev1.Namespace
		var componentName, componentObjName, resourceName string
		var componentVersion string
		repositoryName := "ocm.software/test-repository"

		BeforeEach(func(ctx SpecContext) {
			componentObjName = test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			resourceName = "test-resource-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
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
			By("deleting the component")
			Expect(k8sClient.Delete(ctx, componentObj)).To(Succeed())
			Eventually(func(ctx context.Context) error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObj)
				if err != nil {
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				}

				return fmt.Errorf("resource %s still exists", componentObj.Name)
			}).WithContext(ctx).Should(Succeed())

			resources := &v1alpha1.ResourceList{}
			Expect(k8sClient.List(ctx, resources, client.InNamespace(namespace.GetName()))).To(Succeed())
			Expect(resources.Items).To(HaveLen(0))
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
			func(createCTF func() string, tc *testCase) {
				By("creating a CTF")
				ctfPath := createCTF()

				spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, ctfPath)
				Expect(err).NotTo(HaveOccurred())
				specData, err := spec.MarshalJSON()
				Expect(err).NotTo(HaveOccurred())

				By("mocking a component")
				componentObj = test.MockComponent(
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
								Resource: ocmmetav1.NewIdentity(resourceName),
							},
						},
						AdditionalStatusFields: additionalStatusFields,
					},
				}
				Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())

				By("checking that the resource has been reconciled successfully")

				fields := map[string]any{}

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

				By("deleting the resource")
				test.DeleteObject(ctx, k8sClient, resourceObj)
			},

			Entry("plain text", func() string {
				ctfName := "plainText"
				env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
					env.Component(componentName, func() {
						env.Version(componentVersion, func() {
							env.Resource(resourceName, "1.0.0", artifacttypes.PLAIN_TEXT, ocmmetav1.LocalRelation, func() {
								env.BlobData(mime.MIME_TEXT, []byte("Hello World!"))
							})
						})
					})
				})
				return filepath.Join(tempDir, ctfName)
			},
				nil),
			Entry("OCI artifact access", func() string {
				ctfName := "ociArtifactAccess"
				env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
					env.Component(componentName, func() {
						env.Version(componentVersion, func() {
							env.Resource(resourceName, "1.0.0", artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
								env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0"))
							})
						})
					})
				})
				return filepath.Join(tempDir, ctfName)
			},
				&testCase{
					Registry:   "ghcr.io",
					Repository: "open-component-model/ocm/ocm.software/ocmcli/ocmcli-image",
					Reference:  "0.24.0",
				},
			),
			Entry("Helm access", func() string {
				ctfName := "helmAccess"
				env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
					env.Component(componentName, func() {
						env.Version(componentVersion, func() {
							env.Resource(resourceName, "1.0.0", artifacttypes.HELM_CHART, ocmmetav1.ExternalRelation, func() {
								env.Access(helm.New("podinfo:6.9.1", "oci://ghcr.io/stefanprodan/charts"))
							})
						})
					})
				})
				return filepath.Join(tempDir, ctfName)
			},
				&testCase{
					HELMChart: "podinfo:6.9.1",
				},
			),
			Entry("GitHub access", func() string {
				ctfName := "gitHubAccess"
				env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
					env.Component(componentName, func() {
						env.Version(componentVersion, func() {
							env.Resource(resourceName, "1.0.0", artifacttypes.DIRECTORY_TREE, ocmmetav1.ExternalRelation, func() {
								env.Access(github.New(
									"https://github.com/open-component-model/ocm-k8s-toolkit",
									"/repos/open-component-model/ocm-k8s-toolkit",
									"8f7e04f4b58d2a9e22f88e79dddfc36183688f28",
								))
							})
						})
					})
				})
				return filepath.Join(tempDir, ctfName)
			},
				&testCase{
					GithubRepoURL: "https://github.com/open-component-model/ocm-k8s-toolkit",
				},
			),
			Entry("git access", func() string {
				ctfName := "gitAccess"
				env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
					env.Component(componentName, func() {
						env.Version(componentVersion, func() {
							env.Resource(resourceName, "1.0.0", artifacttypes.DIRECTORY_TREE, ocmmetav1.ExternalRelation, func() {
								env.Access(git.New(
									"https://github.com/open-component-model/ocm-k8s-toolkit",
									git.WithRef("refs/heads/main"),
								))
							})
						})
					})
				})
				return filepath.Join(tempDir, ctfName)
			},
				&testCase{
					GitRepository: "https://github.com/open-component-model/ocm-k8s-toolkit",
				},
			),
		)

		It("should not reconcile when the component is not ready", func(ctx SpecContext) {
			By("mocking a component")
			componentObj = test.MockComponent(
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
							Resource: ocmmetav1.NewIdentity(resourceName),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())

			By("checking that the resource has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, resourceObj, v1alpha1.ResourceIsNotAvailable)

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, resourceObj)
		})

		It("returns an appropriate error when the resource cannot be fetched", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "resource-not-found"
			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion, func() {
						env.Resource(resourceName, "1.0.0", artifacttypes.PLAIN_TEXT, ocmmetav1.LocalRelation, func() {
							env.BlobData(mime.MIME_TEXT, []byte("Hello World!"))
						})
					})
				})
			})

			ctfPath := filepath.Join(tempDir, ctfName)
			spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, ctfPath)
			Expect(err).NotTo(HaveOccurred())
			specData, err := spec.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			By("mocking a component")
			componentObj = test.MockComponent(
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
							Resource: ocmmetav1.NewIdentity("resource-not-found"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())

			By("checking that the resource has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, resourceObj, v1alpha1.GetOCMResourceFailedReason)

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, resourceObj)
		})

		// This test is checking that the resource is reconciled again when the status of the component changes.
		It("reconciles when the component is updated to ready status", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "component-ready"
			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion, func() {
						env.Resource(resourceName, "1.0.0", artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
							env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0"))
						})
					})
				})
			})

			ctfPath := filepath.Join(tempDir, ctfName)
			spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, ctfPath)
			Expect(err).NotTo(HaveOccurred())
			specData, err := spec.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			By("mocking a component")
			componentObj = test.MockComponent(
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
							Resource: ocmmetav1.NewIdentity(resourceName),
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

			By("checking that the resource has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, resourceObj, v1alpha1.ResourceIsNotAvailable)

			By("updating the component to ready")
			componentObjReady := &v1alpha1.Component{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObjReady)).To(Succeed())

			status.MarkReady(recorder, componentObjReady, "mock component is ready")
			Expect(k8sClient.Status().Update(ctx, componentObjReady)).To(Succeed())

			By("checking that the resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"registry":   mustToJSON("ghcr.io"),
					"repository": mustToJSON("open-component-model/ocm/ocm.software/ocmcli/ocmcli-image"),
					"reference":  mustToJSON("0.24.0"),
				},
			})

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, resourceObj)
		})

		// This test checks if the resource is reconciled again, when the resource spec is updated.
		It("reconciles again when the resource changes", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "resource-change"
			resourceVersionUpdated := "1.0.1"
			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion, func() {
						env.Resource(resourceName, "1.0.0", artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
							env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.23.0"))
						})
						env.Resource("resource-update", resourceVersionUpdated, artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
							env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0"))
						})
					})
				})
			})

			ctfPath := filepath.Join(tempDir, ctfName)
			spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, ctfPath)
			Expect(err).NotTo(HaveOccurred())
			specData, err := spec.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			By("mocking a component")
			componentObj = test.MockComponent(
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
							Resource: ocmmetav1.NewIdentity(resourceName),
						},
					},
					AdditionalStatusFields: map[string]string{
						"reference": "resource.access.toOCI().reference",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())

			By("checking that the resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"reference": mustToJSON("0.23.0"),
				},
			})

			By("updating resource spec")
			resourceObjUpdate := &v1alpha1.Resource{}
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(resourceObj), resourceObjUpdate)
			Expect(err).ToNot(HaveOccurred())

			resourceObjUpdate.Spec.Resource = v1alpha1.ResourceID{
				ByReference: v1alpha1.ResourceReference{
					Resource: ocmmetav1.NewIdentity("resource-update"),
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

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, resourceObj)
		})

		// In this test the component version is updated with a new resource. This should trigger the control-loop of
		// the resource and we expect an updated source reference.
		It("reconciles again when the component and resource changes", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "component-change"
			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion, func() {
						env.Resource(resourceName, "1.0.0", artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
							env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.23.0"))
						})
					})
				})
			})

			ctfPath := filepath.Join(tempDir, ctfName)
			spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, ctfPath)
			Expect(err).NotTo(HaveOccurred())
			specData, err := spec.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			By("mocking a component")
			componentObj = test.MockComponent(
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
							Resource: ocmmetav1.NewIdentity(resourceName),
						},
					},
					AdditionalStatusFields: map[string]string{
						"reference": "resource.access.toOCI().reference",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())

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
			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion, func() {
						env.Resource(resourceName, "1.0.0", artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
							env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.23.0"))
						})
					})
				})
				env.Component(componentName, func() {
					env.Version(componentVersionUpdated, func() {
						env.Resource(resourceName, "1.0.1", artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
							env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0"))
						})
					})
				})
			})

			By("updating mock component status")
			componentObj = &v1alpha1.Component{
				ObjectMeta: k8smetav1.ObjectMeta{
					Namespace: componentObj.GetNamespace(),
					Name:      componentObj.GetName(),
				},
			}
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObj)
			Expect(err).ToNot(HaveOccurred())

			componentObj.Status.Component.Version = componentVersionUpdated
			Expect(k8sClient.Status().Update(ctx, componentObj)).To(Succeed())

			By("updating mock component spec")
			componentObj = &v1alpha1.Component{
				ObjectMeta: k8smetav1.ObjectMeta{
					Namespace: componentObj.GetNamespace(),
					Name:      componentObj.GetName(),
				},
			}
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(componentObj), componentObj)
			Expect(err).ToNot(HaveOccurred())

			componentObj.Spec.Semver = componentVersionUpdated

			By("clearing OCM context cache before triggering controller reconcile")
			ocmContextCache.Clear()

			Expect(k8sClient.Update(ctx, componentObj)).To(Succeed())

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

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, resourceObj)
		})

		It("reconcile a nested component by reference path", func(ctx SpecContext) {
			By("creating a CTF")
			ctfName := "nested-component"
			nestedComponentName := "ocm.software/nested-component"
			nestedComponentReference := "some-reference"
			env.OCMCommonTransport(ctfName, accessio.FormatDirectory, func() {
				env.Component(componentName, func() {
					env.Version(componentVersion, func() {
						env.Reference(nestedComponentReference, nestedComponentName, componentVersion, func() {})
					})
				})
				env.Component(nestedComponentName, func() {
					env.Version(componentVersion, func() {
						env.Resource(resourceName, "1.0.0", artifacttypes.OCI_ARTIFACT, ocmmetav1.ExternalRelation, func() {
							env.Access(ocmociartifact.New("ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.23.0"))
						})
					})
				})
			})

			ctfPath := filepath.Join(tempDir, ctfName)
			spec, err := ctf.NewRepositorySpec(ctf.ACC_READONLY, ctfPath)
			Expect(err).NotTo(HaveOccurred())
			specData, err := spec.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			By("mocking a component")
			componentObj = test.MockComponent(
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
							Resource:      ocmmetav1.NewIdentity(resourceName),
							ReferencePath: []ocmmetav1.Identity{ocmmetav1.NewIdentity(nestedComponentReference)},
						},
					},
					AdditionalStatusFields: map[string]string{
						"reference": "resource.access.toOCI().reference",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resourceObj)).To(Succeed())

			By("checking that the resource has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, resourceObj, map[string]any{
				"Status.Additional": map[string]apiextensionsv1.JSON{
					"reference": mustToJSON("0.23.0"),
				},
				"Status.Component.Component": nestedComponentName,
				"Status.Component.Version":   componentVersion,
			})

			By("deleting the resource")
			test.DeleteObject(ctx, k8sClient, resourceObj)
		})

	})
})

func mustToJSON(v string) apiextensionsv1.JSON {
	raw, err := json.Marshal(v)
	Expect(err).ToNot(HaveOccurred())
	return apiextensionsv1.JSON{Raw: raw}
}
