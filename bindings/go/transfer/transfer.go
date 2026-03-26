package transfer

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// BuildGraphDefinition constructs a [transformv1alpha1.TransformationGraphDefinition] that
// describes how to transfer component versions between repositories.
//
// Transfer mappings must be specified via [WithTransfer].
// Each mapping pairs source components with a target repository and a resolver,
// enabling N:M routing where different sources feed different targets.
func BuildGraphDefinition(
	ctx context.Context,
	opts ...Option,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	o := Options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	roots, err := collectTransferRoots(ctx, &o)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "building transfer graph definition",
		"roots", len(roots),
		"recursive", o.Recursive,
		"copyMode", o.CopyMode,
		"uploadType", o.UploadType)

	return internal.BuildGraphDefinition(ctx, roots, o.Recursive, int(o.CopyMode), int(o.UploadType))
}

// collectTransferRoots resolves all transfer mappings into internal TransferRoots.
func collectTransferRoots(ctx context.Context, o *Options) (map[string]internal.TransferRoot, error) {
	if len(o.Mappings) == 0 {
		return nil, fmt.Errorf("no transfer mappings specified: use WithTransfer")
	}

	type rootData struct {
		targets  []runtime.Typed
		resolver resolvers.ComponentVersionRepositoryResolver
	}

	byKey := make(map[string]*rootData)

	for i, m := range o.Mappings {
		if m.Target == nil {
			return nil, fmt.Errorf("mapping %d has no target: use ToRepositorySpec() in WithTransfer", i)
		}
		if m.Resolver == nil {
			return nil, fmt.Errorf("mapping %d has no resolver: use FromResolver() or FromRepository() in WithTransfer", i)
		}

		ids, err := resolveMapping(ctx, &m)
		if err != nil {
			return nil, fmt.Errorf("mapping %d: %w", i, err)
		}

		slog.DebugContext(ctx, "resolved transfer mapping",
			"mapping", i,
			"components", len(ids),
			"target", fmt.Sprintf("%T", m.Target))

		for _, id := range ids {
			key := id.String()
			rd, exists := byKey[key]
			if !exists {
				rd = &rootData{resolver: m.Resolver}
				byKey[key] = rd
			} else if rd.resolver != m.Resolver {
				return nil, fmt.Errorf("conflicting resolvers for component %s: each component must use the same resolver across all mappings", key)
			}
			rd.targets = internal.AppendUniqueRepositories(rd.targets, []runtime.Typed{m.Target})
		}
	}

	roots := make(map[string]internal.TransferRoot, len(byKey))
	for key, rd := range byKey {
		roots[key] = internal.TransferRoot{
			RootComponentKey: key,
			Targets:          rd.targets,
			SourceResolver:   rd.resolver,
		}
	}
	return roots, nil
}

// resolveMapping extracts ComponentIDs from a Mapping.
func resolveMapping(ctx context.Context, m *Mapping) ([]ComponentID, error) {
	if m.ComponentLister != nil && len(m.Components) > 0 {
		return nil, fmt.Errorf("cannot combine Component/FromComponents with FromLister in the same mapping")
	}

	if m.ComponentLister != nil {
		var ids []ComponentID
		if err := m.ComponentLister.ListComponentVersions(ctx, func(batch []ComponentID) error {
			ids = append(ids, batch...)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("listing components failed: %w", err)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("component lister returned no components")
		}
		return ids, nil
	}

	if len(m.Components) == 0 {
		return nil, fmt.Errorf("no components specified: use Component()")
	}
	return m.Components, nil
}
