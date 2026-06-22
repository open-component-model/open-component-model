package replication

import (
	"bytes"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

var _ = Describe("Replication Controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	Context("when the referenced Component is not ready", func() {
		It("adds the finalizer and marks the Replication not ready", func(ctx SpecContext) {
			replication := &v1alpha1.Replication{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replicate-podinfo",
					Namespace: "default",
				},
				Spec: v1alpha1.ReplicationSpec{
					ComponentRef:        corev1.LocalObjectReference{Name: "missing-component"},
					TargetRepositoryRef: corev1.LocalObjectReference{Name: "missing-target-repository"},
				},
			}
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				Expect(k8sClient.Delete(ctx, replication)).To(Succeed())
			})

			Eventually(komega.Object(replication), timeout, interval).Should(
				HaveField("Finalizers", ContainElement(v1alpha1.ReplicationFinalizer)),
			)

			test.WaitForNotReadyObject(ctx, k8sClient, replication, "ResourceIsNotAvailable")
		})
	})

	Context("when source and target are ready", func() {
		var (
			namespace     *corev1.Namespace
			recorder      *record.FakeRecorder
			componentName string
			childName     string
		)

		BeforeEach(func(ctx SpecContext) {
			namespace = test.NamespaceForTest(ctx)
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			recorder = record.NewFakeRecorder(32)
			componentName = "ocm.software/replication-parent-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
			childName = "ocm.software/replication-child-" + test.SanitizeNameForK8s(ctx.SpecReport().LeafNodeText)
		})

		AfterEach(func(ctx SpecContext) {
			By("verifying every replication in the namespace drains its finalizer and is deleted")
			replications := &v1alpha1.ReplicationList{}
			Expect(k8sClient.List(ctx, replications, client.InNamespace(namespace.GetName()))).To(Succeed())

			for i := range replications.Items {
				replication := &replications.Items[i]
				Expect(k8sClient.Delete(ctx, replication)).To(Or(Succeed(), MatchError(errors.IsNotFound, "replication should already be gone")))

				Eventually(func(g Gomega) {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(replication), &v1alpha1.Replication{})
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "replication %s still exists, finalizer was not removed", replication.GetName())
				}, timeout, interval).Should(Succeed())
			}

			Expect(k8sClient.List(ctx, replications, client.InNamespace(namespace.GetName()))).To(Succeed())
			Expect(replications.Items).To(BeEmpty())
		})

		newDescriptor := func(name, version string, refs ...descruntime.Reference) *descruntime.Descriptor {
			return &descruntime.Descriptor{
				Meta: descruntime.Meta{Version: "v2"},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    name,
							Version: version,
						},
					},
					Provider:   descruntime.Provider{Name: "ocm.software"},
					References: refs,
				},
			}
		}

		setupSourceAndTarget := func(ctx SpecContext, descs []*descruntime.Descriptor) (*v1alpha1.Component, *v1alpha1.Repository, string) {
			targetPath := GinkgoT().TempDir()
			sourceRepo, sourceSpecData := test.SetupCTFComponentVersionRepository(ctx, GinkgoT().TempDir(), nil)
			_, targetSpecData := test.SetupCTFComponentVersionRepository(ctx, targetPath, nil)

			// Also check if the blobs were actually transferred correctly.
			for _, desc := range descs {
				res := descruntime.Resource{
					ElementMeta: descruntime.ElementMeta{
						ObjectMeta: descruntime.ObjectMeta{Name: "payload", Version: "1.0.0"},
					},
					Type:     "plainText",
					Relation: descruntime.LocalRelation,
					Access: &v2.LocalBlob{
						Type:      ocmruntime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						MediaType: "text/plain",
					},
				}
				updated, err := sourceRepo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, &res,
					inmemory.New(bytes.NewReader([]byte("payload of "+desc.Component.Name))))
				Expect(err).NotTo(HaveOccurred())
				desc.Component.Resources = append(desc.Component.Resources, *updated)
				Expect(sourceRepo.AddComponentVersion(ctx, desc)).To(Succeed())
			}

			component := test.MockComponent(ctx, "source-component", namespace.GetName(), &test.MockComponentOptions{
				Client:     k8sClient,
				Recorder:   recorder,
				Repository: "source-repository",
				Info: v1alpha1.ComponentInfo{
					RepositorySpec: &apiextensionsv1.JSON{Raw: sourceSpecData},
					Component:      componentName,
					Version:        "1.0.0",
					Digest: &v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
						Value:                  "deadbeef",
					},
				},
			})

			targetRepository := test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), "target-repository", targetSpecData)

			return component, targetRepository, targetPath
		}

		setupTransferConfig := func(ctx SpecContext) string {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "transfer-config",
					Namespace: namespace.GetName(),
				},
				Data: map[string]string{
					v1alpha1.OCMConfigKey: `{
						"type": "generic.config.ocm.software/v1",
						"configurations": [
							{"type": "transfer.config.ocm.software/v1alpha1", "recursive": -1, "copyMode": "localBlob"}
						]
					}`,
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			})

			return configMap.GetName()
		}

		newReplication := func(component *v1alpha1.Component, targetRepository *v1alpha1.Repository, transferConfig []v1alpha1.OCMConfiguration) *v1alpha1.Replication {
			return &v1alpha1.Replication{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replication",
					Namespace: namespace.GetName(),
				},
				Spec: v1alpha1.ReplicationSpec{
					ComponentRef:        corev1.LocalObjectReference{Name: component.GetName()},
					TargetRepositoryRef: corev1.LocalObjectReference{Name: targetRepository.GetName()},
					OCMConfig:           transferConfig,
				},
			}
		}

		It("transfers the recursive component graph to the target repository", func(ctx SpecContext) {
			component, targetRepository, targetPath := setupSourceAndTarget(ctx, []*descruntime.Descriptor{
				newDescriptor(componentName, "1.0.0", descruntime.Reference{
					ElementMeta: descruntime.ElementMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    "child",
							Version: "0.1.0",
						},
					},
					Component: childName,
				}),
				newDescriptor(childName, "0.1.0"),
			})

			replication := newReplication(component, targetRepository, []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Name: setupTransferConfig(ctx), Kind: "ConfigMap"},
			}})
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(komega.Get(replication)()).To(Succeed())
				ready := apimeta.FindStatusCondition(replication.Status.Conditions, v1alpha1.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))

				inProgress := apimeta.FindStatusCondition(replication.Status.Conditions, v1alpha1.TransferInProgressCondition)
				g.Expect(inProgress).NotTo(BeNil())
				g.Expect(inProgress.Status).To(Equal(metav1.ConditionFalse))
			}, timeout, interval).Should(Succeed())

			Expect(replication.Status.Component).NotTo(BeNil())
			Expect(replication.Status.Component.Version).To(Equal("1.0.0"))
			Expect(replication.Status.LastTransferredVersion).To(Equal("1.0.0"))
			Expect(replication.Status.LastTransferredDigest).To(Equal("deadbeef"))

			// Open the target ctf to check all components are where they are supposed to be.
			targetRepo, _ := test.SetupCTFComponentVersionRepository(ctx, targetPath, nil)
			parentDesc, err := targetRepo.GetComponentVersion(ctx, componentName, "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(parentDesc.Component.Name).To(Equal(componentName))
			Expect(parentDesc.Component.Resources).To(HaveLen(1))

			childDesc, err := targetRepo.GetComponentVersion(ctx, childName, "0.1.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(childDesc.Component.Name).To(Equal(childName))

			blob, _, err := targetRepo.GetLocalResource(ctx, componentName, "1.0.0", ocmruntime.Identity{
				"name":    "payload",
				"version": "1.0.0",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(blob).NotTo(BeNil())
		})

		It("fails discovery when a referenced component version does not exist", func(ctx SpecContext) {
			component, targetRepository, _ := setupSourceAndTarget(ctx, []*descruntime.Descriptor{
				newDescriptor(componentName, "1.0.0", descruntime.Reference{
					ElementMeta: descruntime.ElementMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    "child",
							Version: "0.1.0",
						},
					},
					Component: childName,
				}),
			})

			replication := newReplication(component, targetRepository, []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Name: setupTransferConfig(ctx), Kind: "ConfigMap"},
			}})
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(komega.Get(replication)()).To(Succeed())
				ready := apimeta.FindStatusCondition(replication.Status.Conditions, v1alpha1.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(ready.Reason).To(Equal(v1alpha1.ReplicationFailedReason))
				g.Expect(ready.Message).To(ContainSubstring(childName))

				// A failed attempt must not leave a stale in-progress condition behind.
				inProgress := apimeta.FindStatusCondition(replication.Status.Conditions, v1alpha1.TransferInProgressCondition)
				g.Expect(inProgress).NotTo(BeNil())
				g.Expect(inProgress.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(inProgress.Reason).To(Equal(v1alpha1.ReplicationFailedReason))
				g.Expect(inProgress.Message).To(ContainSubstring(childName))
			}, timeout, interval).Should(Succeed())
		})

		It("records the failed transfer events when the transfer cannot write to the target", func(ctx SpecContext) {
			component, _, _ := setupSourceAndTarget(ctx, []*descruntime.Descriptor{
				newDescriptor(componentName, "1.0.0"),
			})

			// Create the CTF so it exists, then reference it read-only. Discovery and
			// the graph build only read, so they succeed; the transfer write phase fails.
			readonlyPath := GinkgoT().TempDir()
			test.SetupCTFComponentVersionRepository(ctx, readonlyPath, nil)
			readonlySpec, err := json.Marshal(&ctf.Repository{
				Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
				FilePath:   readonlyPath,
				AccessMode: ctf.AccessModeReadOnly,
			})
			Expect(err).NotTo(HaveOccurred())
			targetRepository := test.SetupRepositoryWithSpecData(ctx, k8sClient, namespace.GetName(), "readonly-target-repository", readonlySpec)

			replication := newReplication(component, targetRepository, []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Name: setupTransferConfig(ctx), Kind: "ConfigMap"},
			}})
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(komega.Get(replication)()).To(Succeed())
				ready := apimeta.FindStatusCondition(replication.Status.Conditions, v1alpha1.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(ready.Reason).To(Equal(v1alpha1.ReplicationFailedReason))

				g.Expect(replication.Status.LastFailedTransferEvents).NotTo(BeEmpty())
			}, timeout, interval).Should(Succeed())

			last := replication.Status.LastFailedTransferEvents[0]
			Expect(last.ID).NotTo(BeEmpty())
			Expect(last.Name).NotTo(BeEmpty())
			Expect(last.Error).NotTo(BeEmpty())
		})
	})

	Context("when suspended", func() {
		It("does not add the finalizer", func(ctx SpecContext) {
			replication := &v1alpha1.Replication{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "suspended-replication",
					Namespace: "default",
				},
				Spec: v1alpha1.ReplicationSpec{
					ComponentRef:        corev1.LocalObjectReference{Name: "missing-component"},
					TargetRepositoryRef: corev1.LocalObjectReference{Name: "missing-target-repository"},
					Suspend:             true,
				},
			}
			Expect(k8sClient.Create(ctx, replication)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				Expect(k8sClient.Delete(ctx, replication)).To(Succeed())
			})

			Consistently(func(g Gomega) {
				g.Expect(komega.Get(replication)()).To(Succeed())
				g.Expect(controllerutil.ContainsFinalizer(replication, v1alpha1.ReplicationFinalizer)).To(BeFalse())
			}, 2*time.Second, interval).Should(Succeed())
		})
	})
})
