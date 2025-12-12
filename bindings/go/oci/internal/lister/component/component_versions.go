package component

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/oci/internal/lister"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
)

// ReferrerAnnotationVersionResolver creates a version resolver that extracts component versions
// from OCI referrer annotations. It validates that the component name matches the expected
// component and returns the version from the annotation.
//
// The annotation format is expected to be: "annotations.DefaultComponentDescriptorPath/<component>:<version>".
// The input format can be either annotations.DefaultComponentDescriptorPath/<component> or simply <component>.
// Returns lister.ErrSkip if the annotation is not present or not in the correct format.
func ReferrerAnnotationVersionResolver(component string) lister.ReferrerVersionResolver {
	referrerResolver := func(ctx context.Context, descriptor ociImageSpecV1.Descriptor) (string, error) {
		if descriptor.Annotations == nil {
			return "", lister.ErrSkip
		}
		annotation, ok := descriptor.Annotations[annotations.OCMComponentVersion]
		if !ok {
			return "", lister.ErrSkip
		}

		candidate, version, err := annotations.ParseComponentVersionAnnotation(annotation)
		if err != nil {
			return "", fmt.Errorf("failed to parse component version annotation: %w", err)
		}

		if redundantPrefix := path.DefaultComponentDescriptorPath + "/"; strings.HasPrefix(component, redundantPrefix) {
			component = strings.TrimPrefix(component, redundantPrefix)
		}

		if candidate != component {
			return "", fmt.Errorf("component %q from annotation does not match %q: %w", candidate, component, lister.ErrSkip)
		}

		return version, nil
	}
	return referrerResolver
}

// ReferenceTagVersionResolver creates a version resolver that validates OCI tags
// by checking if they reference valid component descriptors. It supports both
// legacy and current OCI manifest formats.
//
// The resolver will:
//   - Parse the provided reference
//   - Resolve the tag to a descriptor
//   - Validate the descriptor's media type
//   - Return the tag if valid, or an error if invalid
func ReferenceTagVersionResolver(component string, store interface {
	content.Resolver
	content.Fetcher
},
) lister.TagVersionResolver {
	tagResolver := func(ctx context.Context, tag string) (string, error) {
		// This resolve call's descriptor is a virtual descriptor. It will never have the annotation
		// we are looking for, and that's why we do a second Fetch inside the switch to get the actual descriptor that
		// does contain the annotation we are looking for.
		desc, err := store.Resolve(ctx, tag)
		if err != nil {
			return "", fmt.Errorf("failed to resolve tag %q: %w", tag, err)
		}

		var (
			manifestAnnotations    map[string]string
			data                   io.ReadCloser
			oldOCMComponentVersion bool
		)
		switch desc.MediaType {
		case ociImageSpecV1.MediaTypeImageManifest:
			data, err = store.Fetch(ctx, desc)
			if err != nil {
				return "", fmt.Errorf("failed to fetch descriptor for tag %q: %w", tag, err)
			}
			var manifest ociImageSpecV1.Manifest
			if err := json.NewDecoder(data).Decode(&manifest); err != nil {
				return "", errors.Join(fmt.Errorf("failed to decode manifest for tag %q: %w", tag, err), data.Close())
			}
			manifestAnnotations = manifest.Annotations
			// Checks for old component versions pre-2024 which didn't have an annotation.
			// In that case, we check if the manifest has a config of type ocm config and if yes, we can just return
			// the tag as valid. We only do this for manifest layers and not index layers because pre-2024 component versions
			// didn't have indexes.
			oldOCMComponentVersion = manifest.Config.MediaType == componentConfig.MediaType
		case ociImageSpecV1.MediaTypeImageIndex:
			data, err = store.Fetch(ctx, desc)
			if err != nil {
				return "", fmt.Errorf("failed to fetch descriptor for tag %q: %w", tag, err)
			}
			var index ociImageSpecV1.Index
			if err := json.NewDecoder(data).Decode(&index); err != nil {
				return "", errors.Join(fmt.Errorf("failed to decode index for tag %q: %w", tag, err), data.Close())
			}
			manifestAnnotations = index.Annotations

		default:
			return "", fmt.Errorf("unsupported media type %q for tag %q: %w", desc.MediaType, tag, lister.ErrSkip)
		}
		if err = data.Close(); err != nil {
			return "", fmt.Errorf("failed to close descriptor reader for tag %q: %w", tag, err)
		}
		annotation, ok := manifestAnnotations[annotations.OCMComponentVersion]
		if !ok {
			if oldOCMComponentVersion {
				return tag, nil
			}

			return "", fmt.Errorf("failed to find %q annotation for tag %q: %w", annotations.OCMComponentVersion, tag, lister.ErrSkip)
		}

		candidate, version, err := annotations.ParseComponentVersionAnnotation(annotation)
		if err != nil {
			return "", fmt.Errorf("failed to parse component version annotation: %w", err)
		}

		if strings.HasPrefix(component, path.DefaultComponentDescriptorPath+"/") {
			component = strings.TrimPrefix(component, path.DefaultComponentDescriptorPath+"/")
		}

		if candidate != component {
			return "", fmt.Errorf("component %q from annotation does not match %q: %w", candidate, component, lister.ErrSkip)
		}

		return version, nil
	}
	return tagResolver
}
