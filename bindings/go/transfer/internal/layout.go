package internal

import (
	"context"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
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

	source, err = sourceComponentVersion(val)
	if err != nil {
		return nil, false, err
	}
	return source, true, nil
}
