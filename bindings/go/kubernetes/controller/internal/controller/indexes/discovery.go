package indexes

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const DiscoveryComponentRef = "spec.componentRef.name"

// RegisterDiscoveryComponentRef registers the Discovery component-reference
// index. Manager bootstrap must call it once before setting up consumers.
func RegisterDiscoveryComponentRef(ctx context.Context, indexer client.FieldIndexer) error {
	if err := indexer.IndexField(ctx, &v1alpha1.Discovery{}, DiscoveryComponentRef, func(obj client.Object) []string {
		discovery, ok := obj.(*v1alpha1.Discovery)
		if !ok {
			return nil
		}
		return []string{discovery.Spec.ComponentRef.Name}
	}); err != nil {
		return fmt.Errorf("failed setting discovery component reference index: %w", err)
	}

	return nil
}
