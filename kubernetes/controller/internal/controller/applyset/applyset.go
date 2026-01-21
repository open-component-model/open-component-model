package applyset

import (
	"context"
	"fmt"
	"strings"
	"sync"

	slogcontext "github.com/veqryn/slog-context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToolingID identifies the tool managing an ApplySet.
type ToolingID struct {
	Name    string
	Version string
}

func (t ToolingID) String() string {
	return fmt.Sprintf("%s/%s", t.Name, t.Version)
}

// Config contains configuration for creating an ApplySet.
type Config struct {
	// ToolingID identifies the tool managing the ApplySet.
	// This is required and used to prevent accidental modification by other tools.
	ToolingID ToolingID

	// FieldManager is the name used for server-side apply field management.
	// This is required for proper conflict detection and resolution.
	FieldManager string

	// ToolLabels are additional labels to inject into all managed resources.
	// These labels are added on top of the required ApplySet labels.
	ToolLabels map[string]string

	// Concurrency controls the maximum number of concurrent apply and prune operations.
	// If not provided or <= 0, defaults to the number of objects in the ApplySet.
	Concurrency int
}

// Set represents an ApplySet that can be applied to a Kubernetes cluster.
type Set interface {
	// Add registers a new appliedObject as part of the ApplySet.
	// Returns the current state of the appliedObject in the cluster, or nil if it doesn't exist.
	Add(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)

	// Apply applies all registered objects to the cluster.
	// If prune is true, previously applied objects that are no longer in the set will be deleted.
	Apply(ctx context.Context, prune bool) (*Result, error)

	// DryRun performs a dry-run apply operation without making actual changes.
	// If prune is true, shows what objects would be pruned.
	DryRun(ctx context.Context, prune bool) (*Result, error)

	// ID returns the unique identifier for this ApplySet.
	ID() string
}

// Result contains the outcome of an Apply or DryRun operation.
type Result struct {
	// Applied contains the successfully applied objects.
	Applied []appliedObject

	// Pruned contains the successfully pruned (deleted) objects.
	Pruned []PrunedObject

	// Errors contains any errors encountered during the operation.
	Errors []error

	mu sync.Mutex
}

// New creates a new ApplySet with the given parent appliedObject, client, and configuration.
//
// The parent appliedObject is used to identify the ApplySet and must already exist in the cluster.
// It can be any Kubernetes appliedObject (ConfigMap, Secret, or custom resource).
//
// Example:
//
//	parent := &corev1.ConfigMap{}
//	parent.SetName("my-applyset")
//	parent.SetNamespace("default")
//	parent.SetGroupVersionKind(schema.GroupVersionKind{
//	    Group: "",
//	    Version: "v1",
//	    Kind: "ConfigMap",
//	})
//
//	set, err := applyset.New(ctx, parent, mgr.GetClient(), mgr.GetRESTMapper(), applyset.Config{
//	    ToolingID: applyset.ToolingID{Name: "deployer", Version: "v1"},
//	    FieldManager: "deployer-controller",
//	})
func New(
	ctx context.Context,
	parent client.Object,
	k8sClient client.Client,
	restMapper meta.RESTMapper,
	config Config,
) (Set, error) {
	if config.ToolingID == (ToolingID{}) {
		return nil, fmt.Errorf("toolingID is required")
	}
	if config.FieldManager == "" {
		return nil, fmt.Errorf("fieldManager is required")
	}

	aset := &applySet{
		parent:              parent,
		toolingID:           config.ToolingID,
		fieldManager:        config.FieldManager,
		toolLabels:          config.ToolLabels,
		k8sClient:           k8sClient,
		restMapper:          restMapper,
		desiredRESTMappings: make(map[schema.GroupKind]*meta.RESTMapping),
		desiredNamespaces:   sets.New[string](),
		desiredObjects:      make([]*unstructured.Unstructured, 0),
		concurrency:         config.Concurrency,
	}

	// Read the parent's current state to get labels and annotations
	if err := aset.loadParentState(ctx); err != nil {
		return nil, err
	}

	return aset, nil
}

type applySet struct {
	parent       client.Object
	toolingID    ToolingID
	fieldManager string
	toolLabels   map[string]string

	k8sClient  client.Client
	restMapper meta.RESTMapper

	currentLabels      map[string]string
	currentAnnotations map[string]string

	desiredRESTMappings map[schema.GroupKind]*meta.RESTMapping
	desiredNamespaces   sets.Set[string]
	desiredObjects      []*unstructured.Unstructured

	supersetNamespaces sets.Set[string]
	supersetGKs        sets.Set[string]

	concurrency int
}

func (a *applySet) ID() string {
	return ComputeID(a.parent)
}

func (a *applySet) Add(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	logger := slogcontext.FromCtx(ctx).With(
		"operation", "add",
		"name", obj.GetName(),
		"namespace", obj.GetNamespace(),
		"gvk", obj.GetObjectKind().GroupVersionKind().String(),
	)

	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		gvk = schema.FromAPIVersionAndKind(obj.GetAPIVersion(), obj.GetKind())
	}

	// Get REST mapping for the appliedObject
	restMapping, err := a.getRESTMapping(gvk)
	if err != nil {
		return nil, err
	}

	// Record namespace if applicable
	if err := a.recordNamespace(obj, restMapping); err != nil {
		return nil, err
	}

	// Inject ApplySet labels
	obj.SetLabels(a.injectApplySetLabels(a.injectToolLabels(obj.GetLabels())))

	// Get current state from cluster using controller-runtime client
	observed := &unstructured.Unstructured{}
	observed.SetGroupVersionKind(gvk)

	// Set namespace if applicable
	ns := obj.GetNamespace()
	if ns == "" && restMapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns = a.parent.GetNamespace()
		if ns == "" {
			ns = metav1.NamespaceDefault
		}
	}
	if ns != obj.GetNamespace() {
		obj.SetNamespace(ns)
	}

	err = a.k8sClient.Get(ctx, client.ObjectKey{
		Name:      obj.GetName(),
		Namespace: ns,
	}, observed)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("object does not exist in cluster")
			observed = nil
		} else {
			return nil, fmt.Errorf("error getting object from cluster: %w", err)
		}
	}

	a.desiredObjects = append(a.desiredObjects, obj)
	logger.Info("added object to applyset")

	return observed, nil
}

func (a *applySet) getRESTMapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	gk := gvk.GroupKind()

	if mapping, found := a.desiredRESTMappings[gk]; found {
		return mapping, nil
	}

	mapping, err := a.restMapper.RESTMapping(gk, gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("error getting rest mapping for %v: %w", gvk, err)
	}

	a.desiredRESTMappings[gk] = mapping
	return mapping, nil
}

func (a *applySet) Apply(ctx context.Context, prune bool) (*Result, error) {
	return a.applyAndPrune(ctx, prune, false)
}

func (a *applySet) DryRun(ctx context.Context, prune bool) (*Result, error) {
	return a.applyAndPrune(ctx, prune, true)
}

func (a *applySet) applyAndPrune(ctx context.Context, prune bool, dryRun bool) (*Result, error) {
	logger := slogcontext.FromCtx(ctx).With("operation", "apply", "dryRun", dryRun, "prune", prune)

	result := &Result{
		Applied: make([]appliedObject, 0, len(a.desiredObjects)),
		Pruned:  make([]PrunedObject, 0),
		Errors:  make([]error, 0),
	}

	// Update parent with superset of desired and current GKs/namespaces before applying
	if !dryRun {
		if err := a.updateParentLabelsAndAnnotations(ctx, true); err != nil {
			return result, fmt.Errorf("unable to update parent: %w", err)
		}
	}

	// Apply all desired objects
	logger.Info("applying objects", "count", len(a.desiredObjects))
	if err := a.apply(ctx, result, dryRun); err != nil {
		logger.Error("error during apply", "error", err)
		return result, err
	}

	// Prune unwanted objects if requested
	if prune {
		logger.Info("pruning objects")
		if err := a.prune(ctx, result, dryRun); err != nil {
			logger.Error("error during prune", "error", err)
			return result, err
		}

		// Update parent with only the current set after pruning
		if !dryRun {
			if err := a.updateParentLabelsAndAnnotations(ctx, false); err != nil {
				logger.Error("error updating parent after prune", "error", err)
				return result, fmt.Errorf("unable to update parent after pruning: %w", err)
			}
		}
	}

	logger.Info("apply operation complete",
		"applied", len(result.Applied),
		"pruned", len(result.Pruned),
		"errors", len(result.Errors))

	return result, nil
}

func parseGroupKind(gkStr string) schema.GroupKind {
	parts := strings.SplitN(gkStr, ".", 2)
	if len(parts) == 1 {
		// Core API group (no dot)
		return schema.GroupKind{Kind: parts[0]}
	}
	// Kind.Group format
	return schema.GroupKind{Kind: parts[0], Group: parts[1]}
}
