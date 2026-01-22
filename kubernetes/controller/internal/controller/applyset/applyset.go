package applyset

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Compile-time check that ApplySet implements Interface.
var _ Interface = (*ApplySet)(nil)

// Interface defines server-side apply and pruning operations with KEP-3659 ApplySet
// membership tracking. It is implemented by ApplySet struct and is intentionally stateless:
// each method is a pure function of its inputs, with no internal state carried between
// calls. The caller is responsible for orchestrating the workflow correctly: calling
// Project() to get union metadata, patching the parent object with that metadata before
// apply/prune, passing the correct PruneScope from Project() to Prune(), and shrinking
// parent annotations after successful prune. Hopefully this design will make it easier
// for callers to implement custom workflows around the core functionality.
type Interface interface {
	// Project computes metadata as union of current resources + parent annotations.
	// Returns GKs/namespaces from both batch AND parent memory (for prune scope).
	// Returns error if RESTMapping fails for any resource.
	Project(resources []Resource) (Metadata, error)

	// Apply runs SSA on resources and returns batch-only metadata.
	// Batch metadata contains only GKs/namespaces from THIS apply (not parent memory).
	Apply(ctx context.Context, resources []Resource, mode ApplyMode) (*ApplyResult, Metadata, error)

	// Prune deletes orphaned resources (those with applyset label but not in KeepUIDs).
	// Pass Project().PruneScope() to search both current batch locations AND parent memory.
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
}

// Resource is an input to Apply.
type Resource struct {
	// ID is a stable identifier provided by the controller (e.g., "deployment" or "workers-0").
	ID string
	// Object is the desired state to apply (GVK/ns/name must be set correctly by the caller).
	Object *unstructured.Unstructured
	// CurrentRevision optionally carries the resourceVersion from the controller's GET.
	// If set, ApplyResultItem.Changed will be false when SSA returns the same RV; if empty,
	// every apply is treated as changed for reporting.
	CurrentRevision string
	// SkipApply excludes the resource from SSA and from the current GK/namespace set.
	// Prune relies on the parent annotation "memory" from previous reconciles to
	// delete these resources if they were previously applied. Use for includeWhen=false.
	SkipApply bool
}

// ApplyMode controls Apply behavior.
type ApplyMode struct {
	Concurrency int // 0 = len(resources)
}

// PruneOptions controls Prune behavior.
type PruneOptions struct {
	// KeepUIDs are UIDs of resources that should NOT be pruned.
	// Typically from ApplyResult.ObservedUIDs().
	KeepUIDs sets.Set[types.UID]
	// Scope defines GKs and namespaces to prune from (required).
	// Use Metadata.PruneScope() to get the scope from Project() output.
	// Pass the superset scope (union of batch + parent) to ensure
	// prune finds all orphans.
	Scope *PruneScope
	// Concurrency limits parallel delete operations. 0 = len(candidates).
	Concurrency int
}

// PruneScope defines the search space for orphan detection.
type PruneScope struct {
	GroupKinds sets.Set[schema.GroupKind]
	Namespaces sets.Set[string] // required for namespace-scoped RBAC compatibility
}

// Metadata contains the computed ApplySet state.
// Controller decides how to store it (annotations, labels, status, etc).
type Metadata struct {
	ID                   string
	Tooling              string
	GroupKinds           sets.Set[schema.GroupKind]
	AdditionalNamespaces sets.Set[string] // excludes parent namespace per KEP-3659
}

// GroupKindsString returns GKs as comma-separated "Kind.group" for KEP-3659 annotation.
func (m Metadata) GroupKindsString() string {
	var gkStrings []string
	for gk := range m.GroupKinds {
		if gk.Group == "" {
			gkStrings = append(gkStrings, gk.Kind)
		} else {
			gkStrings = append(gkStrings, fmt.Sprintf("%s.%s", gk.Kind, gk.Group))
		}
	}
	sort.Strings(gkStrings)
	return strings.Join(gkStrings, ",")
}

// NamespacesString returns namespaces as comma-separated for KEP-3659 annotation.
func (m Metadata) NamespacesString() string {
	nsList := m.AdditionalNamespaces.UnsortedList()
	sort.Strings(nsList)
	return strings.Join(nsList, ",")
}

// Labels returns the KEP-3659 parent labels.
func (m Metadata) Labels() map[string]string {
	return map[string]string{
		ApplySetParentIDLabel: m.ID,
	}
}

// Annotations returns the KEP-3659 parent annotations.
func (m Metadata) Annotations() map[string]string {
	return map[string]string{
		ApplySetToolingAnnotation:              m.Tooling,
		ApplySetGKsAnnotation:                  m.GroupKindsString(),
		ApplySetAdditionalNamespacesAnnotation: m.NamespacesString(),
	}
}

// PruneScope returns a PruneScope from this Metadata for use with Prune().
// Note: This only includes AdditionalNamespaces. Prune() will fall back to
// parent namespace if the scope is empty.
func (m Metadata) PruneScope() *PruneScope {
	return &PruneScope{
		GroupKinds: m.GroupKinds.Clone(),
		Namespaces: m.AdditionalNamespaces.Clone(),
	}
}

// Config for creating an ApplySet.
type Config struct {
	Client          client.Client
	RESTMapper      meta.RESTMapper
	Log             logr.Logger
	ParentNamespace string // fallback namespace for namespaced resources without namespace set
}

// New creates an ApplySet for a specific parent (instance).
// Parent GKNN (name, namespace, kind, group) is used to generate the ApplySet ID per KEP-3659.
// Namespaces for pruning are derived from resources passed to Apply.
func New(cfg Config, parent interface {
	metav1.Object
	schema.ObjectKind
},
) *ApplySet {
	applySetID := ID(parent)
	return &ApplySet{
		client:            cfg.Client,
		restMapper:        cfg.RESTMapper,
		log:               cfg.Log,
		applySetID:        applySetID,
		labelSelector:     fmt.Sprintf("%s=%s", ApplysetPartOfLabel, applySetID),
		parentNamespace:   cfg.ParentNamespace,
		parentAnnotations: maps.Clone(parent.GetAnnotations()),
	}
}

// ApplySet implements Interface for server-side apply with membership tracking.
type ApplySet struct {
	client            client.Client
	restMapper        meta.RESTMapper
	log               logr.Logger
	applySetID        string
	labelSelector     string
	parentNamespace   string
	parentAnnotations map[string]string
}

// Project computes metadata as union of current resources + parent annotations.
// This gives the full scope needed for pruning (current batch + memory of previous reconciles).
func (a *ApplySet) Project(resources []Resource) (Metadata, error) {
	gks := sets.New[schema.GroupKind]()
	namespaces := sets.New[string]()

	// Collect GKs and namespaces from current resources
	for _, r := range resources {
		if r.SkipApply {
			continue
		}
		gvk := r.Object.GroupVersionKind()
		gks.Insert(gvk.GroupKind())

		mapping, err := a.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return Metadata{}, fmt.Errorf("RESTMapping failed for %v: %w", gvk, err)
		}
		if ns, ok := a.resolvedNamespace(mapping, r.Object); ok && ns != a.parentNamespace {
			namespaces.Insert(ns)
		}
	}

	// Union with parent annotations (memory from previous reconciles)
	parentGKs, parentNamespaces := a.parentAnnotationSets()
	for gk := range parentGKs {
		gks.Insert(gk)
	}
	for ns := range parentNamespaces {
		namespaces.Insert(ns)
	}

	return a.buildMetadata(gks, namespaces), nil
}

// Apply runs SSA on all resources and returns batch-only metadata.
// Caller should call Prune separately after Apply succeeds.
func (a *ApplySet) Apply(ctx context.Context, resources []Resource, mode ApplyMode) (*ApplyResult, Metadata, error) {
	result := &ApplyResult{}

	// Collect GKs and namespaces for batch metadata
	desiredGKs := sets.New[schema.GroupKind]()
	desiredNamespaces := sets.New[string]()

	// Resources with resolved mappings, ready to apply
	toApply := make([]struct {
		resource Resource
		mapping  *meta.RESTMapping
	}, 0, len(resources))

	for _, r := range resources {
		// SkipApply resources may have nil Object (unresolved), skip entirely
		if r.SkipApply {
			continue
		}

		gvk := r.Object.GroupVersionKind()

		mapping, err := a.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return result, Metadata{}, fmt.Errorf("failed to get REST mapping for %v: %w", gvk, err)
		}

		// Only include GKs for resources actually being applied
		desiredGKs.Insert(gvk.GroupKind())

		if ns, ok := a.resolvedNamespace(mapping, r.Object); ok && ns != a.parentNamespace {
			desiredNamespaces.Insert(ns)
		}

		// Membership labels are injected just-in-time inside applyResource.
		toApply = append(toApply, struct {
			resource Resource
			mapping  *meta.RESTMapping
		}{resource: r, mapping: mapping})
	}

	// Apply resources
	concurrency := mode.Concurrency
	if concurrency <= 0 {
		concurrency = len(toApply)
	}
	if concurrency == 0 {
		concurrency = 1
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrency)

	var mu sync.Mutex
	applyOptions := metav1.ApplyOptions{
		FieldManager: FieldManager,
		Force:        true,
	}

	for _, entry := range toApply {
		eg.Go(func() error {
			item := a.applyResource(egCtx, entry.resource, entry.mapping, applyOptions)
			mu.Lock()
			result.Applied = append(result.Applied, item)
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return result, Metadata{}, err
	}

	// Return batch-only metadata. Caller decides whether to union with parent
	// based on prune outcome.
	return result, a.buildMetadata(desiredGKs, desiredNamespaces), nil
}

// Prune deletes orphaned resources (those with applyset label but not in KeepUIDs).
func (a *ApplySet) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	scopeGKs := opts.Scope.GroupKinds

	// Always include parent namespace in prune scope
	scopeNamespaces := opts.Scope.Namespaces.Clone()
	if a.parentNamespace != "" {
		scopeNamespaces.Insert(a.parentNamespace)
	} else {
		scopeNamespaces.Insert(metav1.NamespaceDefault)
	}

	// Convert GKs to RESTMappings
	pruneMappings := make([]*meta.RESTMapping, 0, len(scopeGKs))
	for gk := range scopeGKs {
		mapping, err := a.restMapper.RESTMapping(gk)
		if err != nil {
			return nil, fmt.Errorf("RESTMapping failed for %v: %w", gk, err)
		}
		pruneMappings = append(pruneMappings, mapping)
	}

	// List and delete orphans
	pruned, err := a.prune(ctx, pruneMappings, scopeNamespaces, opts.KeepUIDs, opts.Concurrency)
	if err != nil {
		return nil, err
	}

	return &PruneResult{Pruned: pruned}, nil
}

func (a *ApplySet) applyResource(
	ctx context.Context,
	r Resource,
	mapping *meta.RESTMapping,
	options metav1.ApplyOptions,
) ApplyResultItem {
	item := ApplyResultItem{ID: r.ID}

	// Inject applyset membership label (required for prune to find managed resources)
	labels := r.Object.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	// Check for ApplySet membership conflict: if the resource already belongs to
	// a different ApplySet, fail rather than silently overwriting the membership.
	// This prevents accidental takeover of resources managed by other instances/controllers.
	if existingID, exists := labels[ApplysetPartOfLabel]; exists && existingID != a.applySetID {
		item.Error = &ApplySetConflictError{
			ResourceName:      r.Object.GetName(),
			ResourceNamespace: r.Object.GetNamespace(),
			ResourceGVK:       r.Object.GroupVersionKind().String(),
			CurrentApplySetID: existingID,
			DesiredApplySetID: a.applySetID,
		}
		a.log.V(2).Info("applyset conflict",
			"id", r.ID,
			"name", r.Object.GetName(),
			"namespace", r.Object.GetNamespace(),
			"gvk", r.Object.GroupVersionKind().String(),
			"existingApplySetID", existingID,
			"desiredApplySetID", a.applySetID,
		)
		return item
	}

	// Handle applyset label conflicts/overwrites
	labels[ApplysetPartOfLabel] = a.applySetID
	r.Object.SetLabels(labels)

	// Set namespace for namespaced resources
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		r.Object.SetNamespace(a.resolveNamespace(r.Object.GetNamespace()))
	}

	// Desired reflects what we're actually sending (with label injected)
	item.Desired = r.Object.DeepCopy()

	// Apply using controller-runtime client with Server-Side Apply
	patchOptions := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(options.FieldManager),
	}

	err := a.client.Patch(ctx, r.Object, client.Apply, patchOptions...)
	if err != nil {
		item.Error = err
		a.log.V(2).Info("apply failed",
			"id", r.ID,
			"gvr", mapping.Resource.String(),
			"namespace", r.Object.GetNamespace(),
			"name", r.Object.GetName(),
			"objectGVK", r.Object.GroupVersionKind().String(),
			"error", err,
		)
		return item
	}

	item.Observed = r.Object
	// Compare with revision passed by controller (from their GET for CEL evaluation)
	item.Changed = r.CurrentRevision == "" || r.Object.GetResourceVersion() != r.CurrentRevision

	a.log.V(2).Info("applied resource",
		"id", r.ID,
		"gvr", mapping.Resource.String(),
		"name", r.Object.GetName(),
		"namespace", r.Object.GetNamespace(),
		"changed", item.Changed,
	)

	return item
}

func (a *ApplySet) resolvedNamespace(mapping *meta.RESTMapping, obj *unstructured.Unstructured) (string, bool) {
	if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
		return "", false
	}
	return a.resolveNamespace(obj.GetNamespace()), true
}

func (a *ApplySet) resolveNamespace(ns string) string {
	if ns == "" {
		ns = a.parentNamespace
	}
	if ns == "" {
		ns = metav1.NamespaceDefault
	}
	return ns
}

func (a *ApplySet) prune(
	ctx context.Context,
	mappings []*meta.RESTMapping,
	namespaces sets.Set[string],
	keepUIDs sets.Set[types.UID],
	concurrency int,
) ([]PruneResultItem, error) {
	// Track candidates for deletion
	type pruneCandidate struct {
		obj *unstructured.Unstructured
		gvk schema.GroupVersionKind
	}

	// Build list tasks
	type listTask struct {
		gvk       schema.GroupVersionKind
		namespace string // empty for cluster-scoped
		scoped    bool
	}
	var tasks []listTask
	for _, mapping := range mappings {
		gvk := mapping.GroupVersionKind
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			for ns := range namespaces {
				tasks = append(tasks, listTask{gvk: gvk, namespace: ns, scoped: true})
			}
		} else {
			tasks = append(tasks, listTask{gvk: gvk, scoped: false})
		}
	}

	// List resources in parallel
	var listMu sync.Mutex
	var candidates []pruneCandidate

	listGroup, listCtx := errgroup.WithContext(ctx)
	if concurrency > 0 {
		listGroup.SetLimit(concurrency)
	}

	// Parse label selector
	labelSelector, err := labels.Parse(a.labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to parse label selector: %w", err)
	}

	for _, task := range tasks {
		listGroup.Go(func() error {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(task.gvk)

			listOpts := &client.ListOptions{
				LabelSelector: labelSelector,
			}
			if task.scoped {
				listOpts.Namespace = task.namespace
			}

			err := a.client.List(listCtx, list, listOpts)
			if err != nil {
				if task.scoped {
					return fmt.Errorf("list %v in %s: %w", task.gvk, task.namespace, err)
				}
				return fmt.Errorf("list %v: %w", task.gvk, err)
			}

			var local []pruneCandidate
			for i := range list.Items {
				obj := &list.Items[i]
				if !keepUIDs.Has(obj.GetUID()) {
					local = append(local, pruneCandidate{obj: obj, gvk: task.gvk})
				}
			}

			listMu.Lock()
			candidates = append(candidates, local...)
			listMu.Unlock()
			return nil
		})
	}
	if err := listGroup.Wait(); err != nil {
		return nil, err
	}

	// Delete candidates concurrently
	if concurrency <= 0 {
		concurrency = len(candidates)
	}
	if concurrency == 0 {
		concurrency = 1
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrency)

	var mu sync.Mutex
	var results []PruneResultItem

	for _, c := range candidates {
		eg.Go(func() error {
			err := a.client.Delete(egCtx, c.obj)
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete %s/%s: %w", c.obj.GetNamespace(), c.obj.GetName(), err)
			}

			mu.Lock()
			results = append(results, PruneResultItem{Object: c.obj})
			mu.Unlock()

			a.log.V(2).Info("pruned resource",
				"name", c.obj.GetName(),
				"namespace", c.obj.GetNamespace(),
				"gvk", c.obj.GroupVersionKind().String(),
			)
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

func (a *ApplySet) parentAnnotationSets() (sets.Set[schema.GroupKind], sets.Set[string]) {
	gks := sets.New[schema.GroupKind]()
	namespaces := sets.New[string]()

	if len(a.parentAnnotations) == 0 {
		return gks, namespaces
	}

	// Parse GKs from standard KEP annotation
	if raw := a.parentAnnotations[ApplySetGKsAnnotation]; raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.SplitN(entry, ".", 2)
			gk := schema.GroupKind{Kind: parts[0]}
			if len(parts) == 2 {
				gk.Group = parts[1]
			}
			if gk.Kind != "" {
				gks.Insert(gk)
			}
		}
	}

	if raw := a.parentAnnotations[ApplySetAdditionalNamespacesAnnotation]; raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			namespaces.Insert(entry)
		}
	}

	return gks, namespaces
}

func (a *ApplySet) buildMetadata(
	gks sets.Set[schema.GroupKind],
	additionalNamespaces sets.Set[string],
) Metadata {
	return Metadata{
		ID:                   a.applySetID,
		Tooling:              ToolingID(),
		GroupKinds:           gks.Clone(),
		AdditionalNamespaces: additionalNamespaces.Clone(),
	}
}

// ID computes an ApplySet identifier for a given parent object.
// Format: applyset-<base64(sha256(<name>.<namespace>.<kind>.<group>))>-v1
// This follows the KEP-3659 specification using GKNN (name, namespace, kind, group).
func ID(parent interface {
	metav1.Object
	schema.ObjectKind
},
) string {
	unencoded := strings.Join([]string{
		parent.GetName(),
		parent.GetNamespace(),
		parent.GroupVersionKind().Kind,
		parent.GroupVersionKind().Group,
	}, ApplySetIDPartDelimiter)
	hashed := sha256.Sum256([]byte(unencoded))
	b64 := base64.RawURLEncoding.EncodeToString(hashed[:])
	return fmt.Sprintf(V1ApplySetIdFormat, b64)
}
