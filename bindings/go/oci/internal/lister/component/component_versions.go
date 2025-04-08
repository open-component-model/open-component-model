package component

import (
	"context"
	"fmt"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/oci/internal/lister"
	"ocm.software/open-component-model/bindings/go/oci/internal/resolver"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// ReferrerAnnotationVersionResolver creates a version resolver that extracts component versions
// from OCI referrer annotations. It validates that the component name matches the expected
// component and returns the version from the annotation.
//
// The annotation format is expected to be: "component-descriptor/<component>:<version>"
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
		annotation = strings.TrimPrefix(annotation, resolver.DefaultComponentDescriptorPathSuffix+"/")
		split := strings.Split(annotation, ":")
		if len(split) != 2 {
			return "", fmt.Errorf("skipping because not a valid semver tag: %q", annotation)
		}
		candidate := split[0]
		if candidate != component {
			return "", fmt.Errorf("skipping because component %q does not match %q", split[0], component)
		}
		version := split[1]
		if len(version) == 0 {
			return "", fmt.Errorf("skipping because version is empty")
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
// - Parse the provided reference
// - Resolve the tag to a descriptor
// - Validate the descriptor's media type and artifact type
// - Return the tag if valid, or an error if invalid
func ReferenceTagVersionResolver(ref string, store content.Resolver) (lister.TagVersionResolver, error) {
	pref, err := registry.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference for tag version resolution %q: %w", ref, err)
	}
	tagResolver := func(ctx context.Context, tag string) (string, error) {
		pref.Reference = tag
		desc, err := store.Resolve(ctx, pref.String())
		if err != nil {
			return "", fmt.Errorf("failed to resolve tag %q: %w", tag, err)
		}
		legacy := desc.MediaType == ociImageSpecV1.MediaTypeImageManifest && desc.ArtifactType == ""
		current := desc.MediaType == ociImageSpecV1.MediaTypeImageManifest && desc.ArtifactType == descriptor.MediaTypeComponentDescriptorV2 ||
			desc.MediaType == ociImageSpecV1.MediaTypeImageIndex && desc.ArtifactType == descriptor.MediaTypeComponentDescriptorV2
		if !(legacy || current) {
			return "", fmt.Errorf("skipping tag, not recognized as valid: %w", lister.ErrSkip)
		}

		return tag, nil
	}
	return tagResolver, nil
}
