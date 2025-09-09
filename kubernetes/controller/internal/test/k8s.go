package test

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"ocm.software/open-component-model/kubernetes/controller/internal/util"
)

const DefaultKubernetesOperationTimeout = 60 * time.Second

func NamespaceForTest(ctx SpecContext) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: SanitizeNameForK8s(ctx.SpecReport().LeafNodeText),
		},
	}
}

func SanitizeNameForK8s(name string) string {
	replaced := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	maxLength := 63 // RFC 1123 Label Names
	if len(replaced) > maxLength {
		return replaced[:maxLength]
	}

	return replaced
}

// WaitForReadyObject waits for a Kubernetes object to reach a ready state.
//
// Parameters:
// - ctx: The context for managing request deadlines and cancellations.
// - k8sClient: The Kubernetes client used to interact with the cluster.
// - obj: The Kubernetes object to monitor, implementing the util.Getter interface.
// - waitForField: A map specifying field-value pairs to validate on the object.
//
// Behavior:
// - Periodically checks if the object exists, is not being deleted, and is in a ready state.
// - Verifies that the specified fields in waitForField match the expected values.
// - Fails the test if the object does not meet the conditions within 15 seconds.
func WaitForReadyObject(ctx context.Context, k8sClient client.Client, obj util.Getter, waitForField map[string]any) {
	GinkgoHelper()

	gvk, _ := apiutil.GVKForObject(obj, k8sClient.Scheme())

	Eventually(func(g Gomega, ctx context.Context) {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		g.Expect(err).To(Not(HaveOccurred()), "failed to get object %s (Kind: %s)", obj.GetName(), gvk)

		g.Expect(obj.GetDeletionTimestamp()).To(BeNil(), "object %s (Kind: %s) should not be marked for deletion", obj.GetName(), gvk)
		g.Expect(conditions.IsReady(obj)).To(BeTrue(), "object %s (Kind: %s) is not ready, condition: %v", obj.GetName(), gvk, conditions.GetMessage(obj, meta.ReadyCondition))

		for field, value := range waitForField {
			g.Expect(obj).Should(HaveField(field, value), "field %s of object %s (Kind: %s) does not match expected value %v", field, obj.GetName(), gvk, value)
		}
	}).WithTimeout(DefaultKubernetesOperationTimeout).WithContext(ctx).Should(Succeed())
}

func DeleteObject(ctx context.Context, k8sClient client.Client, obj client.Object) {
	GinkgoHelper()

	gvk, _ := k8sClient.GroupVersionKindFor(obj)

	By(fmt.Sprintf("deleting object %s (%s)", obj.GetName(), gvk))
	Expect(k8sClient.Delete(ctx, obj)).To(Or(Succeed(), MatchError(errors.IsNotFound, "object should already be gone")))

	Eventually(func(ctx context.Context) error {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		return fmt.Errorf("resource %s (Kind: %s) still exists", obj.GetName(), obj.GetObjectKind())
	}).WithTimeout(DefaultKubernetesOperationTimeout).WithContext(ctx).Should(Succeed())
	GinkgoLogr.Info("object deleted", "name", obj.GetName(), "namespace", obj.GetNamespace(), "gvk", gvk.String())
}

func WaitForNotReadyObject(ctx context.Context, k8sClient client.Client, obj util.Getter, expectedReason string) {
	GinkgoHelper()

	Eventually(func(ctx context.Context) error {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			return fmt.Errorf("failed to get object: %w", err)
		}

		if conditions.IsReady(obj) {
			return fmt.Errorf("object %s (Kind: %s) is ready", obj.GetName(), obj.GetObjectKind())
		}

		reason := conditions.GetReason(obj, "Ready")
		if reason != expectedReason {
			return fmt.Errorf("expected not-ready object reason %s, got %s", expectedReason, reason)
		}

		return nil
	}).WithTimeout(DefaultKubernetesOperationTimeout).WithContext(ctx).Should(Succeed())
}
