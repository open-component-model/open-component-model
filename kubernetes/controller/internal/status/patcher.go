package status

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusPatcher tracks an object's state to compute and apply merge patches
// for both the object itself and its status sub-resource. Patches are only
// sent when the computed diff is non-empty, avoiding unnecessary API calls.
type StatusPatcher struct {
	client       client.Client
	beforeObject client.Object
}

// NewStatusPatcher returns a StatusPatcher with the given object as the initial
// base object for the patching operations.
func NewStatusPatcher(obj client.Object, c client.Client) *StatusPatcher {
	return &StatusPatcher{
		client:       c,
		beforeObject: obj.DeepCopyObject().(client.Object),
	}
}

// Patch computes a merge patch from the baseline snapshot and applies it to
// both the status sub-resource and the object itself. Patches that carry no
// changes are skipped. After a successful patch the baseline is updated for
// subsequent calls.
func (sp *StatusPatcher) Patch(ctx context.Context, obj client.Object) error {
	patch := client.MergeFrom(sp.beforeObject)

	data, err := patch.Data(obj)
	if err != nil {
		return err
	}

	// An empty JSON merge patch is "{}" (2 bytes). Nothing to do.
	if len(data) > 2 {
		if err := sp.client.Status().Patch(ctx, obj, patch); err != nil {
			return err
		}
		if err := sp.client.Patch(ctx, obj, patch); err != nil {
			return err
		}
	}

	sp.beforeObject = obj.DeepCopyObject().(client.Object)
	return nil
}
