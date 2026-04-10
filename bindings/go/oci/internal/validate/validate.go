package validate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
)

// ErrInvalidComponentVersion indicates that an OCI descriptor does not represent
// a valid OCM component version (wrong media type, missing annotation, or
// component name mismatch).
var ErrInvalidComponentVersion = errors.New("not an OCM component version")

// ComponentVersionDescriptor validates whether a pre-resolved OCI descriptor
// references a valid OCM component version for the given component. It fetches the
// manifest or index content and checks for the OCM component version annotation.
//
// This function supports both legacy (pre-2024) and current OCM manifest formats.
// For legacy manifests without annotations, it falls back to checking the config media type.
//
// The ref parameter is used for error messages and should identify the descriptor being validated.
//
// Returns the validated version string, or an error wrapping [ErrInvalidComponentVersion]
// if the descriptor does not represent a valid OCM component version.
func ComponentVersionDescriptor(
	ctx context.Context,
	fetcher content.Fetcher,
	desc ociImageSpecV1.Descriptor,
	component,
	ref string,
) (string, error) {
	var (
		manifestAnnotations    map[string]string
		data                   io.ReadCloser
		oldOCMComponentVersion bool
		err                    error
	)
	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest:
		data, err = fetcher.Fetch(ctx, desc)
		if err != nil {
			return "", fmt.Errorf("failed to fetch descriptor for reference %q: %w", ref, err)
		}
		var manifest ociImageSpecV1.Manifest
		if err := json.NewDecoder(data).Decode(&manifest); err != nil {
			return "", errors.Join(fmt.Errorf("failed to decode manifest for reference %q: %w", ref, err), data.Close())
		}
		manifestAnnotations = manifest.Annotations
		// Checks for old component versions pre-2024 which didn't have an annotation.
		// In that case, we check if the manifest has a config of type ocm config and if yes, we can just return
		// the tag as valid. We only do this for manifest layers and not index layers because pre-2024 component versions
		// didn't have indexes.
		oldOCMComponentVersion = manifest.Config.MediaType == componentConfig.MediaType
	case ociImageSpecV1.MediaTypeImageIndex:
		data, err = fetcher.Fetch(ctx, desc)
		if err != nil {
			return "", fmt.Errorf("failed to fetch descriptor for reference %q: %w", ref, err)
		}
		var index ociImageSpecV1.Index
		if err := json.NewDecoder(data).Decode(&index); err != nil {
			return "", errors.Join(fmt.Errorf("failed to decode index for reference %q: %w", ref, err), data.Close())
		}
		manifestAnnotations = index.Annotations

	default:
		return "", fmt.Errorf("unsupported media type %q for reference %q: %w", desc.MediaType, ref, ErrInvalidComponentVersion)
	}
	if err = data.Close(); err != nil {
		return "", fmt.Errorf("failed to close descriptor reader for reference %q: %w", ref, err)
	}
	annotation, ok := manifestAnnotations[annotations.OCMComponentVersion]
	if !ok {
		if oldOCMComponentVersion {
			return ref, nil
		}

		return "", fmt.Errorf("failed to find %q annotation for tag %q: %w", annotations.OCMComponentVersion, ref, ErrInvalidComponentVersion)
	}

	candidate, version, err := annotations.ParseComponentVersionAnnotation(annotation)
	if err != nil {
		return "", fmt.Errorf("failed to parse component version annotation: %w", err)
	}

	if strings.HasPrefix(component, path.DefaultComponentDescriptorPath+"/") {
		component = strings.TrimPrefix(component, path.DefaultComponentDescriptorPath+"/")
	}

	if candidate != component {
		return "", fmt.Errorf("component %q from annotation does not match %q: %w", candidate, component, ErrInvalidComponentVersion)
	}

	return version, nil
}
