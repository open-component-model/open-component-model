package discovery

import (
	"context"
	"slices"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// ComponentKey identifies a component by name and version.
type ComponentKey struct {
	Name    string
	Version string
}

func (k ComponentKey) String() string {
	return k.Name + ":" + k.Version
}

// Graph is a fully resolved transitive component graph.
type Graph struct {
	// Root identifies the traversal root within Descriptors.
	Root ComponentKey
	// Descriptors contains the resolved descriptors, including the root.
	Descriptors []*descriptor.Descriptor
}

// Filtered is the selector-filtered view of a Graph, sorted lexicographically
// by (component.name, component.version).
//
// Filtered is a read-only view, not an isolated snapshot. Filter owns the
// Descriptors slice and shallow-copies each surviving descriptor with its own
// resource slice, but untouched nested data (labels, accesses, references,
// sources, repository contexts, signatures) is shared read-only with the input
// graph. Callers must not mutate a Filtered result or its descriptors.
type Filtered struct {
	// Descriptors contains the surviving runtime descriptors in deterministic
	// order. It is always non-nil, even when empty.
	Descriptors []*descriptor.Descriptor
	// Reason distinguishes an empty selector stage from an ordinary result.
	Reason EmptyReason
}

// Filter applies the reference, component, and resource selector stages of q
// to graph and returns the surviving descriptors sorted lexicographically by
// (component.name, component.version). Neither the graph nor its descriptors
// are mutated: Filter performs no v2 conversion or serialization, only label
// decoding for selector evaluation.
//
// An empty reference or component stage is not an error: Filter returns a
// Filtered with no descriptors and the corresponding EmptyReason. Selector
// compilation or evaluation failures are returned as *SelectorError.
func (q *Query) Filter(ctx context.Context, graph Graph) (*Filtered, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	byKey := make(map[ComponentKey]*descriptor.Descriptor, len(graph.Descriptors))
	for _, d := range graph.Descriptors {
		if d == nil {
			continue
		}
		key := ComponentKey{Name: d.Component.Name, Version: d.Component.Version}
		if _, exists := byKey[key]; !exists {
			byKey[key] = d
		}
	}

	survivors, err := q.filterReferences(ctx, byKey)
	if err != nil {
		return nil, err
	}
	if len(survivors) == 0 && q.references != nil {
		return &Filtered{Descriptors: []*descriptor.Descriptor{}, Reason: EmptyReasonNoReferencesMatched}, nil
	}

	survivors, err = q.filterComponents(ctx, survivors)
	if err != nil {
		return nil, err
	}
	if len(survivors) == 0 {
		return &Filtered{Descriptors: []*descriptor.Descriptor{}, Reason: EmptyReasonNoComponentsMatched}, nil
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	filtered := make([]*descriptor.Descriptor, 0, len(survivors))
	for _, d := range survivors {
		out, err := q.filterResources(ctx, d)
		if err != nil {
			return nil, err
		}
		filtered = append(filtered, out)
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	// Lexicographic order by (component.name, component.version).
	return &Filtered{Descriptors: sortByComponentKey(filtered), Reason: EmptyReasonNone}, nil
}

// filterReferences applies the reference selector stage. Without a selector all
// descriptors survive, including the root. Otherwise exactly the targets with at
// least one matching incoming reference survive; the root is only kept when it is
// such a target itself.
func (q *Query) filterReferences(ctx context.Context, byKey map[ComponentKey]*descriptor.Descriptor) ([]*descriptor.Descriptor, error) {
	if q.references == nil {
		all := make([]*descriptor.Descriptor, 0, len(byKey))
		for _, d := range byKey {
			all = append(all, d)
		}
		return all, nil
	}

	targets := make(map[ComponentKey]struct{})
	for _, d := range byKey {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		for i := range d.Component.References {
			ref := &d.Component.References[i]
			match, err := q.references.matches(ctx, referenceIdentity(ref), labelValues(ref.Labels))
			if err != nil {
				return nil, err
			}
			if match {
				targets[ComponentKey{Name: ref.Component, Version: ref.Version}] = struct{}{}
			}
		}
	}

	// Deduplicate targets, not reference entries.
	survivors := make([]*descriptor.Descriptor, 0, len(targets))
	for key := range targets {
		if d, ok := byKey[key]; ok {
			survivors = append(survivors, d)
		}
	}
	return survivors, nil
}

func (q *Query) filterComponents(ctx context.Context, survivors []*descriptor.Descriptor) ([]*descriptor.Descriptor, error) {
	if q.components == nil {
		return survivors, nil
	}
	kept := make([]*descriptor.Descriptor, 0, len(survivors))
	for _, d := range survivors {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		match, err := q.components.matches(ctx, componentIdentity(&d.Component), labelValues(d.Component.Labels))
		if err != nil {
			return nil, err
		}
		if match {
			kept = append(kept, d)
		}
	}
	return kept, nil
}

// filterResources applies the resource selector stage to one descriptor. It
// returns a shallow copy with its own resource slice; all other descriptor data
// is shared read-only with the input, which is never mutated. Components with
// zero surviving resources are kept.
//
// Without an active selector the input resource nilness is retained (a nil
// slice stays nil). With an active selector an empty result is a non-nil slice,
// preserving the v2 null-versus-[] distinction downstream.
func (q *Query) filterResources(ctx context.Context, d *descriptor.Descriptor) (*descriptor.Descriptor, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	out := *d
	if q.resources == nil {
		out.Component.Resources = slices.Clone(d.Component.Resources)
		return &out, nil
	}

	kept := make([]descriptor.Resource, 0, len(d.Component.Resources))
	for i := range d.Component.Resources {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		res := &d.Component.Resources[i]
		match, err := q.resources.matches(ctx, resourceIdentity(res), labelValues(res.Labels))
		if err != nil {
			return nil, err
		}
		if match {
			kept = append(kept, *res)
		}
	}
	out.Component.Resources = kept
	return &out, nil
}

// sortByComponentKey sorts descriptors lexicographically by
// (component.name, component.version) using plain string comparison; SemVer
// semantics apply to semverCheck only.
func sortByComponentKey(descriptors []*descriptor.Descriptor) []*descriptor.Descriptor {
	slices.SortStableFunc(descriptors, func(a, b *descriptor.Descriptor) int {
		if c := strings.Compare(a.Component.Name, b.Component.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Component.Version, b.Component.Version)
	})
	return descriptors
}
