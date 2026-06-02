package replication

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
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

			Eventually(func(g Gomega) {
				g.Expect(komega.Get(replication)()).To(Succeed())
				ready := apimeta.FindStatusCondition(replication.Status.Conditions, v1alpha1.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(ready.Reason).To(Equal(v1alpha1.ReplicationFailedReason))
			}, timeout, interval).Should(Succeed())
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
