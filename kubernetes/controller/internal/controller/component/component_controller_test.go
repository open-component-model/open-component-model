package component

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

const (
	ComponentObj = "test-component"
	Version1     = "1.0.0"
	Version2     = "1.0.1"
)

var _ = Describe("Component Controller", func() {
	var ctfpath string

	BeforeEach(func() {
		ctfpath = GinkgoT().TempDir()
	})

	Context("component controller", func() {
		var repositoryObj *v1alpha1.Repository
		var namespace *corev1.Namespace
		var componentName, repositoryName string

		BeforeEach(func(ctx SpecContext) {
			repositoryName = "ocm-repository-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)

			namespaceName := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		})

		AfterEach(func(ctx SpecContext) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(repositoryObj), repositoryObj)
			if !errors.IsNotFound(err) {

				By("deleting the repository")
				Expect(k8sClient.Delete(ctx, repositoryObj)).To(Succeed())
				Eventually(func(ctx context.Context) error {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(repositoryObj), repositoryObj)
					if errors.IsNotFound(err) {
						return nil
					}

					if err != nil {
						return err
					}

					return fmt.Errorf("expect not-found error for ocm repository %s, but got no error", repositoryObj.GetName())
				}, "15s").WithContext(ctx).Should(Succeed())
			}

			components := &v1alpha1.ComponentList{}

			Expect(k8sClient.List(ctx, components, client.InNamespace(namespace.GetName()))).To(Succeed())
			Expect(components.Items).To(HaveLen(0))
		})

		It("reconciles a component", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    "1.0.0",
					Interval:  metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": Version1,
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("does not reconcile when the repository is not ready", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("marking the repository as not ready")
			conditions.MarkFalse(repositoryObj, "Ready", "notReady", "reason")
			Expect(k8sClient.Status().Update(ctx, repositoryObj)).To(Succeed())

			By("creating a component object")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    "1.0.0",
					Interval:  metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, component, v1alpha1.GetResourceFailedReason)

			By("deleting the resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("does reconcile when an unready ocm repository gets ready", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("marking the repository as not ready")
			conditions.MarkFalse(repositoryObj, "Ready", "notReady", "reason")
			Expect(k8sClient.Status().Update(ctx, repositoryObj)).To(Succeed())

			By("creating a component object")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    "1.0.0",
					Interval:  metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, component, v1alpha1.GetResourceFailedReason)

			By("marking the repository as ready")
			conditions.MarkTrue(repositoryObj, "Ready", "ready", "message")
			Expect(k8sClient.Status().Update(ctx, repositoryObj)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": Version1,
			})

			By("deleting the resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("grabs the new version when it becomes available", func(ctx SpecContext) {
			By("creating a component version")
			repo, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    ">=1.0.0",
					Interval:  metav1.Duration{Duration: time.Second},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": Version1,
			})

			By("increasing the component version")
			desc2 := &descruntime.Descriptor{
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    componentName,
							Version: Version2,
						},
					},
					Provider: descruntime.Provider{Name: "ocm.software"},
				},
			}
			Expect(repo.AddComponentVersion(ctx, desc2)).To(Succeed())

			By("checking that the increased version has been discovered successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": Version2,
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("grabs lower version if downgrade is allowed", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: "0.0.3",
								Labels: []descruntime.Label{
									{
										Name:  v1alpha1.OCMLabelDowngradable,
										Value: json.RawMessage(`"0.0.2"`),
									},
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: "0.0.2",
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component:       componentName,
					DowngradePolicy: v1alpha1.DowngradePolicyAllow,
					Semver:          "<1.0.0",
					Interval:        metav1.Duration{Duration: time.Second},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "0.0.3",
			})

			By("decreasing the component version")
			component.Spec.Semver = "0.0.2"
			Expect(k8sClient.Update(ctx, component)).To(Succeed())

			By("checking that the decreased version has been discovered successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "0.0.2",
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("does not grab lower version if downgrade is denied", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: "0.0.3",
								Labels: []descruntime.Label{
									{
										Name:  v1alpha1.OCMLabelDowngradable,
										Value: json.RawMessage(`"0.0.2"`),
									},
								},
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: "0.0.2",
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component:       componentName,
					DowngradePolicy: v1alpha1.DowngradePolicyDeny,
					Semver:          "0.0.3",
					Interval:        metav1.Duration{Duration: time.Second},
				},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "0.0.3",
			})

			By("trying to decrease component version")
			component.Spec.Semver = "0.0.2"
			Expect(k8sClient.Update(ctx, component)).To(Succeed())

			By("checking that downgrade was not allowed")
			Eventually(func(ctx context.Context) error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: component.Name, Namespace: component.Namespace}, component)
				if err != nil {
					return err
				}

				cond := conditions.Get(component, meta.ReadyCondition)
				expectedMessage := "terminal error: component version cannot be downgraded from version 0.0.3 to version 0.0.2"
				if cond.Message != expectedMessage {
					return fmt.Errorf("expected ready-condition message to be '%s', but got '%s'", expectedMessage, cond.Message)
				}

				return nil
			}, "15s").WithContext(ctx).Should(Succeed())
			Expect(component.Status.Component.Version).To(Equal("0.0.3"))

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("can force downgrade even if not allowed by the component", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: "0.0.3",
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: "0.0.2",
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component:       componentName,
					DowngradePolicy: v1alpha1.DowngradePolicyEnforce,
					Semver:          "<1.0.0",
					Interval:        metav1.Duration{Duration: time.Second},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "0.0.3",
			})

			By("decreasing the component version")
			component.Spec.Semver = "0.0.2"
			Expect(k8sClient.Update(ctx, component)).To(Succeed())

			By("checking that the decreased version has been discovered successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "0.0.2",
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("normalizes a component version with a plus", func(ctx SpecContext) {
			componentObjName := ComponentObj + "-with-plus"
			componentVersionPlus := Version1 + "+componentversionsuffix"

			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: componentVersionPlus,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component resource")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      componentObjName,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    componentVersionPlus,
					Interval:  metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": componentVersionPlus,
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("blocks deletion of a component when a resource is referencing it", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    "1.0.0",
					Interval:  metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "1.0.0",
			})

			By("creating a resource that references the component")
			resource := test.MockResource(ctx, "test-resource", component.GetNamespace(), &test.MockResourceOptions{
				ComponentRef: corev1.LocalObjectReference{
					Name: component.GetName(),
				},
				Clnt:     k8sClient,
				Recorder: recorder,
			})

			By("Deleting the component and checking for ready condition")
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())
			Eventually(func(ctx context.Context) error {
				comp := &v1alpha1.Component{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(component), comp)
				if err != nil {
					return err
				}

				reason := conditions.GetReason(comp, "Ready")
				if reason != v1alpha1.DeletionFailedReason {
					return fmt.Errorf(
						"expected component ready-condition reason to be %s, but it was %s",
						v1alpha1.DeletionFailedReason,
						reason,
					)
				}

				return nil
			}, "15s").WithContext(ctx).Should(Succeed())

			By("delete resources manually")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			test.DeleteObject(ctx, k8sClient, resource)
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("returns an error when specified semver is not found", func(ctx SpecContext) {
			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version2,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    Version1,
					Interval:  metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has not been reconciled successfully")
			test.WaitForNotReadyObject(ctx, k8sClient, component, v1alpha1.CheckVersionFailedReason)

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("verifies the signing of a component version", func(ctx SpecContext) {
			By("creating a component version")
			repo, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("signing the component version")
			signatureName := "test-signature"

			desc, err := repo.GetComponentVersion(ctx, componentName, Version1)
			Expect(err).ToNot(HaveOccurred())

			normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
			Expect(err).ToNot(HaveOccurred())
			signature, pubKey := test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)

			desc.Signatures = append(desc.Signatures, signature)
			Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    Version1,
					Interval:  metav1.Duration{Duration: time.Minute * 10},
					Verify: []v1alpha1.Verification{
						{
							Signature: signatureName,
							Value:     base64.StdEncoding.EncodeToString([]byte(pubKey)),
						},
					},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": Version1,
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("verifies the signing of a component version by secret reference", func(ctx SpecContext) {
			By("creating a component version")
			repo, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("signing the component version")
			signatureName := "test-signature"

			desc, err := repo.GetComponentVersion(ctx, componentName, Version1)
			Expect(err).ToNot(HaveOccurred())

			normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
			Expect(err).ToNot(HaveOccurred())
			signature, pubKey := test.SignComponent(ctx, signatureName, signingv1alpha1.AlgorithmRSASSAPSS, normalised, pm)

			desc.Signatures = append(desc.Signatures, signature)
			Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a secret with the public key")
			secretName := "signature-public-key"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      secretName,
				},
				Data: map[string][]byte{
					signatureName: []byte(pubKey),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    Version1,
					Interval:  metav1.Duration{Duration: time.Minute * 10},
					Verify: []v1alpha1.Verification{
						{
							Signature: signatureName,
							SecretRef: corev1.LocalObjectReference{Name: secretName},
						},
					},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": Version1,
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("verifies the signing of a component version using more than one verification", func(ctx SpecContext) {
			By("creating a component version")
			repo, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			By("signing the component version for a secret")
			signatureNameSecret := "test-signature-secret"

			descSecret, err := repo.GetComponentVersion(ctx, componentName, Version1)
			Expect(err).ToNot(HaveOccurred())

			normalisedSecret, err := normalisation.Normalise(descSecret, v4alpha1.Algorithm)
			Expect(err).ToNot(HaveOccurred())
			signatureSecret, pubKeySecret := test.SignComponent(ctx, signatureNameSecret, signingv1alpha1.AlgorithmRSASSAPSS, normalisedSecret, pm)

			descSecret.Signatures = append(descSecret.Signatures, signatureSecret)
			Expect(repo.AddComponentVersion(ctx, descSecret)).To(Succeed())

			By("creating a secret with the public key")
			secretNameSecret := "signature-public-key"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      secretNameSecret,
				},
				Data: map[string][]byte{
					signatureNameSecret: []byte(pubKeySecret),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("signing the component version for a value")
			signatureNameValue := "test-signature-value"

			descValue, err := repo.GetComponentVersion(ctx, componentName, Version1)
			Expect(err).ToNot(HaveOccurred())

			normalisedValue, err := normalisation.Normalise(descValue, v4alpha1.Algorithm)
			Expect(err).ToNot(HaveOccurred())
			signatureValue, pubKeyValue := test.SignComponent(ctx, signatureNameValue, signingv1alpha1.AlgorithmRSASSAPKCS1V15, normalisedValue, pm)

			descValue.Signatures = append(descValue.Signatures, signatureValue)
			Expect(repo.AddComponentVersion(ctx, descValue)).To(Succeed())

			By("mocking an ocm repository")
			repositoryObj = test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), repositoryName, specData)

			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    Version1,
					Interval:  metav1.Duration{Duration: time.Minute * 10},
					Verify: []v1alpha1.Verification{
						{
							Signature: signatureNameSecret,
							SecretRef: corev1.LocalObjectReference{Name: secretNameSecret},
						},
						{
							Signature: signatureNameValue,
							Value:     base64.StdEncoding.EncodeToString([]byte(pubKeyValue)),
						},
					},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": Version1,
			})

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})
	})

	Context("ocm config handling", func() {
		var (
			configs       []*corev1.ConfigMap
			secrets       []*corev1.Secret
			namespace     *corev1.Namespace
			repositoryObj *v1alpha1.Repository
			componentName string
		)

		BeforeEach(func(ctx SpecContext) {
			componentName = "ocm.software/test-component-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)

			By("creating a component version")
			_, specData := test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    componentName,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})

			namespaceName := test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			configs, secrets = createTestConfigsAndSecrets(ctx, namespace.GetName())

			By("mocking an ocm repository")
			repositoryName := "repository"
			repositoryObj = &v1alpha1.Repository{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      repositoryName,
				},
				Spec: v1alpha1.RepositorySpec{
					RepositorySpec: &apiextensionsv1.JSON{
						Raw: specData,
					},
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       secrets[0].Name,
								Namespace:  secrets[0].Namespace,
							},
						},
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       secrets[1].Name,
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								Kind: "Secret",
								Name: secrets[2].Name,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "ConfigMap",
								Name:       configs[0].Name,
								Namespace:  configs[1].Namespace,
							},
						},
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "ConfigMap",
								Name:       configs[1].Name,
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								Kind: "ConfigMap",
								Name: configs[2].Name,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
					},
					Interval: metav1.Duration{Duration: time.Minute * 10},
				},
			}

			Expect(k8sClient.Create(ctx, repositoryObj)).To(Succeed())

			repositoryObj.Status = v1alpha1.RepositoryStatus{
				EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[0].Name,
							Namespace:  secrets[0].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[1].Name,
							Namespace:  secrets[1].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[2].Name,
							Namespace:  secrets[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[0].Name,
							Namespace:  configs[1].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[1].Name,
							Namespace:  secrets[1].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[2].Name,
							Namespace:  configs[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
				},
			}

			conditions.MarkTrue(repositoryObj, "Ready", "ready", "message")
			Expect(k8sClient.Status().Update(ctx, repositoryObj)).To(Succeed())
		})

		AfterEach(func(ctx SpecContext) {
			By("make sure the repo is still ready")
			conditions.MarkTrue(repositoryObj, "Ready", "ready", "message")
			Expect(k8sClient.Status().Update(ctx, repositoryObj)).To(Succeed())
			cleanupTestConfigsAndSecrets(ctx, configs, secrets)

			By("delete repository")
			Expect(k8sClient.Delete(ctx, repositoryObj)).To(Succeed())
			Eventually(func(ctx context.Context) error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(repositoryObj), repositoryObj)
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}

				return fmt.Errorf("expected not-found error, but got none")
			}, "15s").WithContext(ctx).Should(Succeed())

			By("ensuring no components are left")
			Eventually(func(g Gomega, ctx SpecContext) {
				components := &v1alpha1.ComponentList{}
				g.Expect(k8sClient.List(ctx, components, client.InNamespace(namespace.GetName()))).To(Succeed())
				g.Expect(components.Items).To(HaveLen(0))
			}, "15s").WithContext(ctx).Should(Succeed())
		})

		It("component resolves and propagates config from repository", func(ctx SpecContext) {
			By("creating a component")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    "1.0.0",
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: v1alpha1.GroupVersion.String(),
								Kind:       v1alpha1.KindRepository,
								Namespace:  namespace.GetName(),
								Name:       repositoryObj.GetName(),
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
					},
					Interval: metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "1.0.0",
			})

			By("checking component's effective OCM config")
			Eventually(komega.Object(component), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[0].Name,
							Namespace:  secrets[0].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[2].Name,
							Namespace:  secrets[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[0].Name,
							Namespace:  configs[0].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[2].Name,
							Namespace:  configs[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
				)),
			)

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("component without ocmConfig inherits propagate entries from repository", func(ctx SpecContext) {
			By("creating a component without ocmConfig")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    "1.0.0",
					Interval:  metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "1.0.0",
			})

			By("checking component inherited only propagate entries from repository")
			Eventually(komega.Object(component), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[0].Name,
							Namespace:  secrets[0].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[2].Name,
							Namespace:  secrets[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[0].Name,
							Namespace:  configs[1].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
							Name:       configs[2].Name,
							Namespace:  configs[2].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyPropagate,
					},
				)),
			)

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})

		It("component with explicit ocmConfig ignores parent repository config", func(ctx SpecContext) {
			By("creating a component with its own ocmConfig pointing directly at a secret")
			component := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.GetName(),
					Name:      ComponentObj,
				},
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repositoryObj.GetName(),
					},
					Component: componentName,
					Semver:    "1.0.0",
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       secrets[1].Name,
								Namespace:  secrets[1].Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
					},
					Interval: metav1.Duration{Duration: time.Minute * 10},
				},
				Status: v1alpha1.ComponentStatus{},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			By("checking that the component has been reconciled successfully")
			test.WaitForReadyObject(ctx, k8sClient, component, map[string]any{
				"Status.Component.Version": "1.0.0",
			})

			By("checking component uses only its own config, not the parent's")
			Eventually(komega.Object(component), "15s").Should(
				HaveField("Status.EffectiveOCMConfig", ConsistOf(
					v1alpha1.OCMConfiguration{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "Secret",
							Name:       secrets[1].Name,
							Namespace:  secrets[1].Namespace,
						},
						Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
					},
				)),
			)

			By("delete resources manually")
			test.DeleteObject(ctx, k8sClient, component)
		})
	})
})

func createTestConfigsAndSecrets(ctx context.Context, namespace string) (configs []*corev1.ConfigMap, secrets []*corev1.Secret) {
	const (
		Config1 = "config1"
		Config2 = "config2"
		Config3 = "config3"

		Secret1 = "secret1"
		Secret2 = "secret2"
		Secret3 = "secret3"
	)

	By("setup configs")
	config1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      Config1,
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set1:
    description: set1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
		},
	}
	configs = append(configs, config1)
	Expect(k8sClient.Create(ctx, config1)).To(Succeed())

	config2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      Config2,
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set2:
    description: set2
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
		},
	}
	configs = append(configs, config2)
	Expect(k8sClient.Create(ctx, config2)).To(Succeed())

	config3 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      Config3,
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set3:
    description: set3
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
		},
	}
	configs = append(configs, config3)
	Expect(k8sClient.Create(ctx, config3)).To(Succeed())

	By("setup secrets")
	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      Secret1,
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path1
  credentials:
  - type: Credentials
    properties:
      username: testuser1
      password: testpassword1
`),
		},
	}
	secrets = append(secrets, secret1)
	Expect(k8sClient.Create(ctx, secret1)).To(Succeed())

	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      Secret2,
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path2
  credentials:
  - type: Credentials
    properties:
      username: testuser2
      password: testpassword2
`),
		},
	}
	secrets = append(secrets, secret2)
	Expect(k8sClient.Create(ctx, secret2)).To(Succeed())

	secret3 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      Secret3,
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path3
  credentials:
  - type: Credentials
    properties:
      username: testuser3
      password: testpassword3
`),
		},
	}
	secrets = append(secrets, &secret3)
	Expect(k8sClient.Create(ctx, &secret3)).To(Succeed())

	return configs, secrets
}

func cleanupTestConfigsAndSecrets(ctx context.Context, configs []*corev1.ConfigMap, secrets []*corev1.Secret) {
	for _, config := range configs {
		Expect(k8sClient.Delete(ctx, config)).To(Succeed())
	}
	for _, secret := range secrets {
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
	}
}
