package transformer

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
)

// weakEdgeFailurePolicyConfigurable is implemented by resource repositories
// that can derive a copy with a different weak edge failure policy.
type weakEdgeFailurePolicyConfigurable interface {
	WithWeakEdgeFailurePolicy(oci.WeakEdgeFailurePolicy) ocistream.ResourceRepository
}

func parseWeakEdgeFailurePolicy(policy v1alpha1.WeakEdgeFailurePolicy) (oci.WeakEdgeFailurePolicy, error) {
	switch policy {
	case "", v1alpha1.WeakEdgeFailurePolicyAbort:
		return oci.WeakEdgeFailurePolicyAbort, nil
	case v1alpha1.WeakEdgeFailurePolicySkip:
		return oci.WeakEdgeFailurePolicySkip, nil
	default:
		return oci.WeakEdgeFailurePolicyAbort, fmt.Errorf("unsupported weakEdgeFailurePolicy %q (must be %q or %q)",
			policy, v1alpha1.WeakEdgeFailurePolicyAbort, v1alpha1.WeakEdgeFailurePolicySkip)
	}
}
