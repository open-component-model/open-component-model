package discovery

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

// publish writes the mutated status to the API server with an
// optimistic-lock merge patch.
func (r *Reconciler) publish(ctx context.Context, old, discovery *v1alpha1.Discovery) error {
	if equality.Semantic.DeepEqual(old.Status, discovery.Status) {
		return nil
	}
	return r.GetClient().Status().Patch(ctx, discovery,
		client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{}))
}
