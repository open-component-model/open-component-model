package resource

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ = Describe("Resource Controller Deletion", func() {
	DescribeTable("blocks deletion while a deployer references the resource",
		func(ctx SpecContext, newDeployer func(namespace, resource string) client.Object, expectedName func(namespace string) string) {
			namespace := test.NamespaceForTest(ctx)
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			resource := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "referenced-resource",
					Namespace:  namespace.GetName(),
					Finalizers: []string{v1alpha1.ResourceFinalizer},
				},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{Name: "test-component"},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{
							Resource: runtime.Identity{"name": "referenced-resource"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			deployer := newDeployer(namespace.GetName(), resource.GetName())
			Expect(k8sClient.Create(ctx, deployer)).To(Succeed())

			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			// Wait for the message too, a transient error can carry the same reason.
			Eventually(func(g Gomega, ctx context.Context) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), resource)).To(Succeed())
				cond := apimeta.FindStatusCondition(resource.Status.Conditions, v1alpha1.ReadyCondition)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Reason).To(Equal(v1alpha1.DeletionFailedReason))
				g.Expect(cond.Message).To(ContainSubstring(expectedName(namespace.GetName())))
			}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(Succeed())

			By("removing the deployer releases the resource")
			test.DeleteObject(ctx, k8sClient, deployer)
			Eventually(func(ctx SpecContext) error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), resource)
			}).WithTimeout(test.DefaultKubernetesOperationTimeout).WithContext(ctx).Should(MatchError(ContainSubstring("not found")))
		},
		Entry("Deployer",
			func(namespace, resource string) client.Object {
				return &v1alpha1.Deployer{
					ObjectMeta: metav1.ObjectMeta{Name: "blocking-deployer"},
					Spec: v1alpha1.DeployerSpec{
						ResourceRef: v1alpha1.ObjectKey{Name: resource, Namespace: namespace},
					},
				}
			},
			func(string) string { return "blocking-deployer" },
		),
		Entry("NamespacedDeployer",
			func(namespace, resource string) client.Object {
				return &v1alpha1.NamespacedDeployer{
					ObjectMeta: metav1.ObjectMeta{Name: "blocking-namespaced-deployer", Namespace: namespace},
					Spec: v1alpha1.NamespacedDeployerSpec{
						ResourceRef:        corev1.LocalObjectReference{Name: resource},
						ServiceAccountName: "deployer",
					},
				}
			},
			func(namespace string) string { return namespace + "/blocking-namespaced-deployer" },
		),
	)
})
