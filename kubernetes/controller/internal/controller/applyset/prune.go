package applyset

import (
	"context"
	"sync"

	slogcontext "github.com/veqryn/slog-context"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PrunableObject represents an appliedObject that can be pruned.
type PrunableObject struct {
	Name      string
	Namespace string
	GVK       schema.GroupVersionKind
	UID       types.UID
	Mapping   *meta.RESTMapping
}

func (a *applySet) findObjectsToPrune(ctx context.Context, appliedUIDs sets.Set[types.UID]) ([]PrunableObject, error) {
	logger := slogcontext.FromCtx(ctx)

	// Get the list of GKs from current annotations to know what to look for
	gks := a.getGKsFromAnnotations()
	if len(gks) == 0 {
		logger.Info("no group-kinds found in annotations, nothing to prune")
		return nil, nil
	}

	// Get the list of namespaces to check
	namespaces := a.getNamespacesToCheck()

	logger.Info("searching for objects to prune",
		"gks", len(gks),
		"namespaces", len(namespaces))

	var pruneObjects []PrunableObject
	var mu sync.Mutex
	eg, egctx := errgroup.WithContext(ctx)

	concurrency := a.concurrency
	if concurrency <= 0 {
		concurrency = maxConcurrency
	}
	if concurrency > maxConcurrency {
		concurrency = maxConcurrency
	}

	eg.SetLimit(concurrency) // Limit concurrent list operations

	for _, gkStr := range gks {
		eg.Go(func() error {
			gk := parseGroupKind(gkStr)
			mapping, err := a.restMapper.RESTMapping(gk)
			if err != nil {
				logger.Info("could not find mapping for group-kind, skipping", "gk", gkStr, "error", err)
				return nil // Skip unknown GKs
			}

			objects, err := a.listObjectsForGK(egctx, mapping, namespaces)
			if err != nil {
				return err
			}

			mu.Lock()
			defer mu.Unlock()

			for _, obj := range objects {
				// Skip objects that were just applied
				if appliedUIDs.Has(obj.GetUID()) {
					continue
				}

				pruneObjects = append(pruneObjects, PrunableObject{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
					GVK:       obj.GetObjectKind().GroupVersionKind(),
					UID:       obj.GetUID(),
					Mapping:   mapping,
				})
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return pruneObjects, nil
}

func (a *applySet) prune(ctx context.Context, result *Result, dryRun bool) error {
	logger := slogcontext.FromCtx(ctx)

	// Find all objects that should be pruned
	pruneObjects, err := a.findObjectsToPrune(ctx, result.AppliedUIDs())
	if err != nil {
		return err
	}

	logger.Info("found objects to prune", "count", len(pruneObjects))

	var deleteOptions []client.DeleteOption
	if dryRun {
		deleteOptions = append(deleteOptions, client.DryRunAll)
	}

	for _, obj := range pruneObjects {
		// Create an unstructured appliedObject for deletion
		deleteObj := &unstructured.Unstructured{}
		deleteObj.SetGroupVersionKind(obj.GVK)
		deleteObj.SetName(obj.Name)
		deleteObj.SetNamespace(obj.Namespace)

		logger = logger.With(
			"name", obj.Name,
			"namespace", obj.Namespace,
			"gvk", obj.GVK.String(),
		)

		err := a.k8sClient.Delete(ctx, deleteObj, deleteOptions...)
		result.recordPruned(obj, err)

		if err != nil && !apierrors.IsNotFound(err) {
			logger.Error("failed to prune object",
				"error", err)
		} else {
			logger.Info("pruned object")
		}
	}

	return nil
}

// PrunedObject represents an appliedObject that was pruned.
type PrunedObject struct {
	Name      string
	Namespace string
	GVK       schema.GroupVersionKind
	UID       types.UID
	Error     error
}

func (r *Result) recordPruned(obj PrunableObject, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Pruned = append(r.Pruned, PrunedObject{
		Name:      obj.Name,
		Namespace: obj.Namespace,
		GVK:       obj.GVK,
		UID:       obj.UID,
		Error:     err,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		r.Errors = append(r.Errors, err)
	}
}
