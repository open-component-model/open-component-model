package internal

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// resolveTargetLayout returns the target spec a component version is written to, with its
// component version layout resolved, and that layout. Precedence: the layout from the transfer
// config, then the layout set on the target spec, then the layout of the source (only a normalized
// source is inherited, so the signatures made on its stable digest stay valid in the target).
//
// The target spec is copied when the layout changes, as one target spec is shared by all
// component versions transferred to it. Targets other than OCI and CTF are returned unchanged;
// they have no layout, and the upload rejects them.
func resolveTargetLayout(target runtime.Typed, configured, source layout.ComponentVersionLayout) (runtime.Typed, layout.ComponentVersionLayout, error) {
	concrete, err := convertToConcreteRepo(target)
	if err != nil {
		// Unsupported targets fail when the upload type is chosen.
		return target, "", nil
	}

	var onTarget layout.ComponentVersionLayout
	switch repo := concrete.(type) {
	case *oci.Repository:
		onTarget = repo.ComponentVersionLayout
	case *ctfv1.Repository:
		onTarget = repo.ComponentVersionLayout
	}
	if err := onTarget.Validate(); err != nil {
		return nil, "", fmt.Errorf("target repository: %w", err)
	}

	resolved := onTarget
	switch {
	case configured != "":
		resolved = configured
	case onTarget == "" && source == layout.ComponentVersionLayoutNormalized:
		resolved = layout.ComponentVersionLayoutNormalized
	}
	if resolved == onTarget {
		return target, resolved, nil
	}

	switch repo := concrete.(type) {
	case *oci.Repository:
		cp := repo.DeepCopy()
		cp.ComponentVersionLayout = resolved
		return cp, resolved, nil
	case *ctfv1.Repository:
		cp := repo.DeepCopy()
		cp.ComponentVersionLayout = resolved
		return cp, resolved, nil
	}
	return target, onTarget, nil
}

func isOCIOrCTF(spec runtime.Typed) bool {
	concrete, err := convertToConcreteRepo(spec)
	if err != nil {
		return false
	}
	switch concrete.(type) {
	case *oci.Repository, *ctfv1.Repository:
		return true
	default:
		return false
	}
}

// chooseCarryType returns the carry transformation type for a target repository.
func chooseCarryType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := convertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCICarryComponentSignaturesV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFCarryComponentSignaturesV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for carry component signatures operation", concreteRepo)
	}
}

// addCarryTransformation appends the transformation copying the signature referrers of a
// component version from its source into a normalized-layout target. It depends on the upload
// transformation of the same (component, target) pair through the CEL references to its spec.
// Only OCI and CTF sources can hold signature referrers; for other sources nothing is added.
func addCarryTransformation(ctx context.Context, val *discoveryValue, id string, toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition, label string) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version

	if val.SourceRepository == nil {
		slog.WarnContext(ctx, "the source repository specification is unknown, so signatures of the source component version are not carried to the normalized target",
			"component", component, "version", version)
		return nil
	}
	if !isOCIOrCTF(val.SourceRepository) {
		slog.DebugContext(ctx, "source repository cannot hold signature referrers, not carrying signatures",
			"component", component, "version", version, "sourceType", val.SourceRepository.GetType().String())
		return nil
	}

	carryType, err := chooseCarryType(toSpec)
	if err != nil {
		return fmt.Errorf("choosing carry type for target repository: %w", err)
	}
	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}
	fromRepo, err := asUnstructured(val.SourceRepository)
	if err != nil {
		return fmt.Errorf("cannot convert source spec to unstructured: %w", err)
	}

	uploadID := id + "Upload"
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  carryType,
			ID:    id + "Carry",
			Label: label,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository":       toRepo.Data,
			"sourceRepository": fromRepo.Data,
			"component":        fmt.Sprintf("${%s.spec.descriptor.component.name}", uploadID),
			"version":          fmt.Sprintf("${%s.spec.descriptor.component.version}", uploadID),
		}},
	})
	return nil
}
