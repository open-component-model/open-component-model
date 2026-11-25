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

// chooseTransferComponentVersionType returns the fused transformation that adds a component
// version stored in the normalized layout in the source repository to the target repository.
func chooseTransferComponentVersionType(source, target runtime.Typed) (runtime.Type, error) {
	sourceOCI, err := isOCIRepository(source)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("source repository: %w", err)
	}
	targetOCI, err := isOCIRepository(target)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("target repository: %w", err)
	}
	switch {
	case sourceOCI && targetOCI:
		return ociv1alpha1.TransferOCIComponentVersionToOCIV1alpha1, nil
	case sourceOCI:
		return ociv1alpha1.TransferOCIComponentVersionToCTFV1alpha1, nil
	case targetOCI:
		return ociv1alpha1.TransferCTFComponentVersionToOCIV1alpha1, nil
	default:
		return ociv1alpha1.TransferCTFComponentVersionToCTFV1alpha1, nil
	}
}

// componentVersionGraphSource returns the source of the fused transformation that adds the
// component version in val to the target toSpec by copying the OCI graph it is stored as, which
// keeps its signature referrers. ok is false if the component version is added from its
// descriptor alone instead: only a normalized source has a manifest that stays the same in every
// repository, and only OCI and CTF repositories store such graphs.
func componentVersionGraphSource(ctx context.Context, val *discoveryValue, toSpec runtime.Typed) (source map[string]any, ok bool, err error) {
	if val.SourceLayout != layout.ComponentVersionLayoutNormalized || !isOCIOrCTF(toSpec) {
		return nil, false, nil
	}
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version

	if val.SourceRepository == nil {
		slog.WarnContext(ctx, "the source repository specification is unknown, so signature referrers of the normalized source component version are not transferred",
			"component", component, "version", version)
		return nil, false, nil
	}
	if !isOCIOrCTF(val.SourceRepository) {
		slog.DebugContext(ctx, "source repository cannot hold signature referrers, adding the component version from its descriptor",
			"component", component, "version", version, "sourceType", val.SourceRepository.GetType().String())
		return nil, false, nil
	}

	sourceRepo, err := asUnstructured(val.SourceRepository)
	if err != nil {
		return nil, false, fmt.Errorf("cannot convert source repository spec to unstructured: %w", err)
	}
	source = map[string]any{
		"repository": sourceRepo.Data,
		"component":  component,
		"version":    version,
	}
	if val.Pin != "" {
		source["digest"] = val.Pin
	}
	return source, true, nil
}
