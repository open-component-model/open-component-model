package internal

import (
	"context"
	"fmt"
	"log/slog"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

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
// normalized component version from its source into the target. It depends on the upload
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
