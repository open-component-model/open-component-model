package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
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
type Filtered struct {
	// Components contains the surviving components in deterministic order.
	Components []FilteredComponent
	// Reason distinguishes an empty selector stage from an ordinary result.
	Reason EmptyReason
}

// FilteredComponent is one surviving component with its surviving resources.
type FilteredComponent struct {
	// Key identifies the component.
	Key ComponentKey
	// Raw is the filtered v2 descriptor serialized as deterministic JSON.
	Raw json.RawMessage
	// Descriptor is the full filtered v2 descriptor as a generic map.
	Descriptor map[string]any
	// Component is the inner component of Descriptor.
	Component map[string]any
	// Resources contains the surviving resources in declaration order.
	Resources []map[string]any
}

// Filter applies the reference, component, and resource selector stages of q
// to graph and returns the surviving components sorted lexicographically by
// (component.name, component.version). Input descriptors are never mutated.
//
// An empty reference or component stage is not an error: Filter returns a
// Filtered with no components and the corresponding EmptyReason. Selector
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
		return &Filtered{Components: []FilteredComponent{}, Reason: EmptyReasonNoReferencesMatched}, nil
	}

	survivors, err = q.filterComponents(ctx, survivors)
	if err != nil {
		return nil, err
	}
	if len(survivors) == 0 {
		return &Filtered{Components: []FilteredComponent{}, Reason: EmptyReasonNoComponentsMatched}, nil
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	filtered := make([]FilteredComponent, 0, len(survivors))
	for _, d := range survivors {
		fc, err := q.filterResources(ctx, d)
		if err != nil {
			return nil, err
		}
		filtered = append(filtered, *fc)
	}

	// Lexicographic order by (component.name, component.version).
	return &Filtered{Components: sortByComponentKey(filtered), Reason: EmptyReasonNone}, nil
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

// filterResources applies the resource selector stage to one component and
// materializes the result as v2 JSON and generic maps. The v2 conversion
// already copies, so the runtime input descriptor is never mutated. Components
// with zero surviving resources are kept.
func (q *Query) filterResources(ctx context.Context, d *descriptor.Descriptor) (*FilteredComponent, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	keep := make([]bool, len(d.Component.Resources))
	for i := range keep {
		keep[i] = true
	}
	if q.resources != nil {
		for i := range d.Component.Resources {
			res := &d.Component.Resources[i]
			match, err := q.resources.matches(ctx, resourceIdentity(res), labelValues(res.Labels))
			if err != nil {
				return nil, err
			}
			keep[i] = match
		}
	}

	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), d)
	if err != nil {
		return nil, fmt.Errorf("failed to convert descriptor %s to v2: %w", d.Component.String(), err)
	}
	if q.resources != nil {
		filteredResources := make([]v2.Resource, 0, len(v2desc.Component.Resources))
		for i, res := range v2desc.Component.Resources {
			if keep[i] {
				filteredResources = append(filteredResources, res)
			}
		}
		v2desc.Component.Resources = filteredResources
	}

	raw, err := json.Marshal(v2desc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal descriptor %s: %w", d.Component.String(), err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("failed to unmarshal descriptor %s into generic map: %w", d.Component.String(), err)
	}

	fc := &FilteredComponent{
		Key:        ComponentKey{Name: d.Component.Name, Version: d.Component.Version},
		Raw:        raw,
		Descriptor: generic,
	}
	if component, ok := generic["component"].(map[string]any); ok {
		fc.Component = component
		if resources, ok := component["resources"].([]any); ok {
			for _, res := range resources {
				if m, ok := res.(map[string]any); ok {
					fc.Resources = append(fc.Resources, m)
				}
			}
		}
	}
	return fc, nil
}

// sortByComponentKey sorts components lexicographically by
// (component.name, component.version) using plain string comparison; SemVer
// semantics apply to semverCheck only.
func sortByComponentKey(components []FilteredComponent) []FilteredComponent {
	slices.SortStableFunc(components, func(a, b FilteredComponent) int {
		if c := strings.Compare(a.Key.Name, b.Key.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Key.Version, b.Key.Version)
	})
	return components
}
