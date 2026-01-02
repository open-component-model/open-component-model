package applyset

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"sync"

	slogcontext "github.com/veqryn/slog-context"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ApplySetIDPartDelimiter is the delimiter used when constructing an ApplySet ID.
	ApplySetIDPartDelimiter = "."

	// V1ApplySetIdFormat is the format string for v1 ApplySet IDs.
	V1ApplySetIdFormat = "applyset-%s"

	// ApplySetParentIDLabel is the label applied to parent objects to identify an ApplySet.
	// The value is the unique ID for the ApplySet.
	ApplySetParentIDLabel = "applyset.k8s.io/id"

	// ApplySetPartOfLabel is the label applied to member objects to indicate they are part of an ApplySet.
	// The value matches the ApplySet ID on the parent object.
	ApplySetPartOfLabel = "applyset.k8s.io/part-of"

	// ApplySetToolingAnnotation is the annotation on the parent object indicating which tool is managing the ApplySet.
	ApplySetToolingAnnotation = "applyset.k8s.io/tooling"

	// ApplySetGKsAnnotation is an optional "hint" annotation listing all GroupKinds in the ApplySet.
	// This helps optimize discovery of member objects.
	ApplySetGKsAnnotation = "applyset.k8s.io/contains-group-kinds"

	// ApplySetAdditionalNamespacesAnnotation extends the scope of an ApplySet to include additional namespaces.
	ApplySetAdditionalNamespacesAnnotation = "applyset.k8s.io/additional-namespaces"
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
	// Add registers a new object as part of the ApplySet.
	// Returns the current state of the object in the cluster, or nil if it doesn't exist.
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
	Applied []AppliedObject

	// Pruned contains the successfully pruned (deleted) objects.
	Pruned []PrunedObject

	// Errors contains any errors encountered during the operation.
	Errors []error

	mu sync.Mutex
}

// AppliedObject represents an object that was applied.
type AppliedObject struct {
	Object *unstructured.Unstructured
	Error  error
}

// PrunedObject represents an object that was pruned.
type PrunedObject struct {
	Name      string
	Namespace string
	GVK       schema.GroupVersionKind
	UID       types.UID
	Error     error
}

func (r *Result) recordApplied(obj *unstructured.Unstructured, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Applied = append(r.Applied, AppliedObject{Object: obj, Error: err})
	if err != nil {
		r.Errors = append(r.Errors, err)
	}
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

// New creates a new ApplySet with the given parent object, client, and configuration.
//
// The parent object is used to identify the ApplySet and must already exist in the cluster.
// It can be any Kubernetes object (ConfigMap, Secret, or custom resource).
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

func (a *applySet) loadParentState(ctx context.Context) error {
	// Get the current state of the parent from the cluster
	parentObj := &unstructured.Unstructured{}
	parentObj.SetGroupVersionKind(a.parent.GetObjectKind().GroupVersionKind())

	if err := a.k8sClient.Get(ctx, client.ObjectKeyFromObject(a.parent), parentObj); err != nil {
		if apierrors.IsNotFound(err) {
			// Parent doesn't exist yet, that's okay
			a.currentLabels = make(map[string]string)
			a.currentAnnotations = make(map[string]string)
			return nil
		}
		return fmt.Errorf("failed to get parent object: %w", err)
	}

	a.currentLabels = parentObj.GetLabels()
	if a.currentLabels == nil {
		a.currentLabels = make(map[string]string)
	}
	a.currentAnnotations = parentObj.GetAnnotations()
	if a.currentAnnotations == nil {
		a.currentAnnotations = make(map[string]string)
	}

	return nil
}

func (a *applySet) ID() string {
	return ComputeID(a.parent)
}

// ComputeID computes an ApplySet identifier for a given parent object.
// Format: base64(sha256(<name>.<namespace>.<kind>.<group>)), using the URL safe encoding of RFC4648.
// @see https://github.com/kubernetes/enhancements/blob/master/keps/sig-cli/3659-kubectl-apply-prune/README.md#applyset-identification
func ComputeID(parent client.Object) string {
	gvk := parent.GetObjectKind().GroupVersionKind()
	unencoded := strings.Join([]string{
		parent.GetName(),
		parent.GetNamespace(),
		gvk.Kind,
		gvk.Group,
	}, ApplySetIDPartDelimiter)

	hashed := sha256.Sum256([]byte(unencoded))
	b64 := base64.RawURLEncoding.EncodeToString(hashed[:])

	// Label values must start and end with alphanumeric values
	return fmt.Sprintf(V1ApplySetIdFormat, b64)
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

	// Get REST mapping for the object
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

func (a *applySet) recordNamespace(obj *unstructured.Unstructured, mapping *meta.RESTMapping) error {
	gvk := obj.GetObjectKind().GroupVersionKind()

	switch mapping.Scope.Name() {
	case meta.RESTScopeNameNamespace:
		namespace := obj.GetNamespace()
		if namespace == "" {
			namespace = a.parent.GetNamespace()
			if namespace == "" {
				namespace = metav1.NamespaceDefault
			}
		}
		a.desiredNamespaces.Insert(namespace)
	case meta.RESTScopeNameRoot:
		if obj.GetNamespace() != "" {
			return fmt.Errorf("namespace was provided for cluster-scoped object %v %v", gvk, obj.GetName())
		}
	default:
		return fmt.Errorf("unknown scope for gvk %s: %q", gvk, mapping.Scope.Name())
	}

	return nil
}

func (a *applySet) injectToolLabels(labels map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	if a.toolLabels != nil {
		for k, v := range a.toolLabels {
			labels[k] = v
		}
	}
	return labels
}

func (a *applySet) injectApplySetLabels(labels map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[ApplySetPartOfLabel] = a.ID()
	return labels
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
		Applied: make([]AppliedObject, 0, len(a.desiredObjects)),
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
		return result, err
	}

	// Prune unwanted objects if requested
	if prune {
		logger.Info("pruning objects")
		if err := a.prune(ctx, result, dryRun); err != nil {
			return result, err
		}

		// Update parent with only the current set after pruning
		if !dryRun {
			if err := a.updateParentLabelsAndAnnotations(ctx, false); err != nil {
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

func (a *applySet) apply(ctx context.Context, result *Result, dryRun bool) error {
	concurrency := a.concurrency
	if concurrency <= 0 {
		concurrency = len(a.desiredObjects)
	}

	eg, egctx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrency)

	for _, obj := range a.desiredObjects {
		eg.Go(func() error {
			applied, err := a.applyObject(egctx, obj, dryRun)
			result.recordApplied(applied, err)
			return nil // Don't stop on individual errors
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

	// Make a copy to apply
	appliedObj := obj.DeepCopy()

	err := a.k8sClient.Patch(ctx, appliedObj, client.Apply, patchOptions...)
	if err != nil {
		logger.Error("failed to apply object", "error", err)
		return nil, err
	}

	logger.Info("applied object", "resourceVersion", appliedObj.GetResourceVersion())
	return appliedObj, nil
}

func (a *applySet) prune(ctx context.Context, result *Result, dryRun bool) error {
	logger := slogcontext.FromCtx(ctx)

	// Find all objects that should be pruned
	pruneObjects, err := a.findObjectsToPrune(ctx, result.AppliedUIDs())
	if err != nil {
		return err
	}

	logger.Info("found objects to prune", "count", len(pruneObjects))

	deleteOptions := []client.DeleteOption{}
	if dryRun {
		deleteOptions = append(deleteOptions, client.DryRunAll)
	}

	for _, obj := range pruneObjects {
		// Create an unstructured object for deletion
		deleteObj := &unstructured.Unstructured{}
		deleteObj.SetGroupVersionKind(obj.GVK)
		deleteObj.SetName(obj.Name)
		deleteObj.SetNamespace(obj.Namespace)

		err := a.k8sClient.Delete(ctx, deleteObj, deleteOptions...)

		result.recordPruned(obj, err)

		if err != nil && !apierrors.IsNotFound(err) {
			logger.Error("failed to prune object",
				"name", obj.Name,
				"namespace", obj.Namespace,
				"gvk", obj.GVK.String(),
				"error", err)
		} else {
			logger.Info("pruned object",
				"name", obj.Name,
				"namespace", obj.Namespace,
				"gvk", obj.GVK.String())
		}
	}

	return nil
}

// PrunableObject represents an object that can be pruned.
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
	eg.SetLimit(10) // Limit concurrent list operations

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

func (a *applySet) listObjectsForGK(ctx context.Context, mapping *meta.RESTMapping, namespaces sets.Set[string]) ([]*unstructured.Unstructured, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			ApplySetPartOfLabel: a.ID(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create label selector: %w", err)
	}

	listOptions := &client.ListOptions{
		LabelSelector: labelSelector,
	}

	var allObjects []*unstructured.Unstructured

	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// List in each namespace
		for ns := range namespaces {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(mapping.GroupVersionKind)

			listOptions.Namespace = ns
			err := a.k8sClient.List(ctx, list, listOptions)
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue // Namespace might not exist
				}
				return nil, fmt.Errorf("failed to list %s in namespace %s: %w", mapping.GroupVersionKind, ns, err)
			}
			for i := range list.Items {
				allObjects = append(allObjects, &list.Items[i])
			}
		}
	} else {
		// Cluster-scoped resource
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(mapping.GroupVersionKind)

		err := a.k8sClient.List(ctx, list, listOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to list cluster-scoped %s: %w", mapping.GroupVersionKind, err)
		}
		for i := range list.Items {
			allObjects = append(allObjects, &list.Items[i])
		}
	}

	return allObjects, nil
}

func (a *applySet) getGKsFromAnnotations() []string {
	gksAnnotation := a.currentAnnotations[ApplySetGKsAnnotation]
	if gksAnnotation == "" {
		return nil
	}

	gks := strings.Split(gksAnnotation, ",")
	result := make([]string, 0, len(gks))
	for _, gk := range gks {
		gk = strings.TrimSpace(gk)
		if gk != "" {
			result = append(result, gk)
		}
	}
	return result
}

func (a *applySet) getNamespacesToCheck() sets.Set[string] {
	namespaces := sets.New[string]()

	// Add parent's namespace if namespaced
	if a.parent.GetNamespace() != "" {
		namespaces.Insert(a.parent.GetNamespace())
	}

	// Add additional namespaces from annotation
	nsAnnotation := a.currentAnnotations[ApplySetAdditionalNamespacesAnnotation]
	if nsAnnotation != "" {
		for _, ns := range strings.Split(nsAnnotation, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				namespaces.Insert(ns)
			}
		}
	}

	return namespaces
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

func (a *applySet) updateParentLabelsAndAnnotations(ctx context.Context, useSuperset bool) error {
	logger := slogcontext.FromCtx(ctx)

	// Generate desired labels and annotations
	desiredLabels := a.desiredParentLabels()
	desiredAnnotations, namespaces, gks := a.desiredParentAnnotations(useSuperset)

	// Track superset for later use
	if useSuperset {
		a.supersetNamespaces = namespaces
		a.supersetGKs = gks
	}

	// Check if we need to update
	if equality.Semantic.DeepEqual(a.currentLabels, desiredLabels) &&
		equality.Semantic.DeepEqual(a.currentAnnotations, desiredAnnotations) {
		logger.Info("parent labels and annotations unchanged, skipping update")
		return nil
	}

	// Create a patch for the parent using controller-runtime client
	parentPatch := &unstructured.Unstructured{}
	parentPatch.SetGroupVersionKind(a.parent.GetObjectKind().GroupVersionKind())
	parentPatch.SetName(a.parent.GetName())
	parentPatch.SetNamespace(a.parent.GetNamespace())
	parentPatch.SetLabels(desiredLabels)
	parentPatch.SetAnnotations(desiredAnnotations)

	// Apply using controller-runtime client
	err := a.k8sClient.Patch(ctx, parentPatch, client.Apply,
		client.FieldOwner(a.fieldManager+"-parent"))
	if err != nil {
		return fmt.Errorf("error updating parent: %w", err)
	}

	logger.Info("updated parent labels and annotations")

	// Update current state
	a.currentLabels = desiredLabels
	a.currentAnnotations = desiredAnnotations

	return nil
}

func (a *applySet) desiredParentLabels() map[string]string {
	labels := make(map[string]string)
	labels[ApplySetParentIDLabel] = a.ID()
	return labels
}

func (a *applySet) desiredParentAnnotations(useSuperset bool) (map[string]string, sets.Set[string], sets.Set[string]) {
	annotations := make(map[string]string)
	annotations[ApplySetToolingAnnotation] = a.toolingID.String()

	// Generate sorted comma-separated list of GKs
	gks := sets.New[string]()
	for gk := range a.desiredRESTMappings {
		gks.Insert(gk.String())
	}

	if useSuperset {
		// Include current GKs from annotations
		for _, gk := range strings.Split(a.currentAnnotations[ApplySetGKsAnnotation], ",") {
			gk = strings.TrimSpace(gk)
			if gk != "" {
				gks.Insert(gk)
			}
		}
	}

	gksList := gks.UnsortedList()
	sort.Strings(gksList)
	annotations[ApplySetGKsAnnotation] = strings.Join(gksList, ",")

	// Generate sorted comma-separated list of namespaces
	nss := a.desiredNamespaces.Clone()

	if useSuperset {
		// Include current namespaces from annotations
		for _, ns := range strings.Split(a.currentAnnotations[ApplySetAdditionalNamespacesAnnotation], ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				nss.Insert(ns)
			}
		}
	}

	// Remove the parent's namespace from the list (it's implicit)
	if a.parent.GetNamespace() != "" {
		nss.Delete(a.parent.GetNamespace())
	}

	if len(nss) > 0 {
		nsList := nss.UnsortedList()
		sort.Strings(nsList)
		annotations[ApplySetAdditionalNamespacesAnnotation] = strings.Join(nsList, ",")
	}

	return annotations, nss, gks
}
