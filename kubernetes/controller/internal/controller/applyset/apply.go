package applyset

import (
	"context"

	slogcontext "github.com/veqryn/slog-context"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// appliedObject represents an appliedObject that was applied.
type appliedObject struct {
	Object *unstructured.Unstructured
	Error  error
}

func (r *Result) recordApplied(obj *unstructured.Unstructured, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Applied = append(r.Applied, appliedObject{Object: obj, Error: err})
	if err != nil {
		r.Errors = append(r.Errors, err)
	}
}

// AppliedUIDs returns the set of UIDs for successfully applied objects.
func (r *Result) AppliedUIDs() sets.Set[types.UID] {
	uids := sets.New[types.UID]()
	for _, applied := range r.Applied {
		if applied.Error == nil && applied.Object != nil {
			uids.Insert(applied.Object.GetUID())
		}
	}
	return uids
}

func (a *applySet) apply(ctx context.Context, result *Result, dryRun bool) error {
	concurrency := a.concurrency
	if concurrency <= 0 {
		concurrency = len(a.desiredObjects)
	}

	if concurrency > maxConcurrency {
		concurrency = maxConcurrency
	}

	eg, egctx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrency)

	for _, obj := range a.desiredObjects {
		eg.Go(func() error {
			logger := slogcontext.FromCtx(ctx).With("name", obj.GetName(),
				"namespace", obj.GetNamespace(),
				"gvk", obj.GetObjectKind().GroupVersionKind().String(),
			)

			applied, err := a.applyObject(egctx, obj, dryRun)
			if err != nil {
				logger.Error("error applying object", "error", err)
				return err
			}
			result.recordApplied(applied, err)
			return nil
		})
	}

	return eg.Wait()
}

func (a *applySet) applyObject(
	ctx context.Context,
	obj *unstructured.Unstructured,
	dryRun bool,
) (*unstructured.Unstructured, error) {
	logger := slogcontext.FromCtx(ctx).With(
		"name", obj.GetName(),
		"namespace", obj.GetNamespace(),
		"gvk", obj.GetObjectKind().GroupVersionKind().String(),
	)

	// Use controller-runtime Patch with Apply patch type for server-side apply
	patchOptions := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(a.fieldManager),
	}

	if dryRun {
		patchOptions = append(patchOptions, client.DryRunAll)
	}

	err := a.k8sClient.Patch(ctx, obj, client.Apply, patchOptions...)
	if err != nil {
		logger.Error("failed to apply object", "error", err)
		return nil, err
	}

	logger.Info("applied object", "resourceVersion", obj.GetResourceVersion())
	return obj, nil
}
