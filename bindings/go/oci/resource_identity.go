package oci

import (
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// PlatformAttributeMapper defines the mapping between resource identity attributes and OCI platform fields
type PlatformAttributeMapper struct {
	attribute string
	setter    func(platform *ociImageSpecV1.Platform, value string)
}

// newLocalResourceLayer creates an OCI layer descriptor for a resource.
// It maps resource identity attributes to OCI platform fields and adds appropriate annotations.
// The function takes:
// - scheme: The runtime scheme used for type conversion
// - size: The size of the blob
// - resource: The resource descriptor
// Returns an OCI descriptor and any error that occurred during processing.
func newLocalResourceLayer(scheme *runtime.Scheme, size int64, resource *descriptor.Resource) (ociImageSpecV1.Descriptor, error) {
	access, err := getLocalBlobAccess(scheme, resource)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to get local blob access: %w", err)
	}

	layer := ociImageSpecV1.Descriptor{
		// TODO(jakobmoellerdev): We might need to think
		//  about which mediaType we use, that of the blob or the resource
		//  currently only the resource is respected.
		MediaType: access.MediaType,
		Digest:    digest.Digest(access.LocalReference),
		Size:      size,
	}

	identity := resource.ToIdentity()

	// Define platform attribute mappings
	platformMappings := []PlatformAttributeMapper{
		{
			attribute: "architecture",
			setter: func(platform *ociImageSpecV1.Platform, value string) {
				platform.Architecture = value
			},
		},
		{
			attribute: "os",
			setter: func(platform *ociImageSpecV1.Platform, value string) {
				platform.OS = value
			},
		},
		{
			attribute: "variant",
			setter: func(platform *ociImageSpecV1.Platform, value string) {
				platform.Variant = value
			},
		},
		{
			attribute: "os.features",
			setter: func(platform *ociImageSpecV1.Platform, value string) {
				platform.OSFeatures = strings.Split(value, ",")
			},
		},
		{
			attribute: "os.version",
			setter: func(platform *ociImageSpecV1.Platform, value string) {
				platform.OSVersion = value
			},
		},
	}

	// Apply platform mappings
	for _, mapping := range platformMappings {
		if value, exists := resource.ExtraIdentity[mapping.attribute]; exists {
			if layer.Platform == nil {
				layer.Platform = &ociImageSpecV1.Platform{}
			}
			mapping.setter(layer.Platform, value)
		}
	}

	if err := (&ArtifactOCILayerAnnotation{
		Identity: identity,
		Kind:     ArtifactKindResource,
	}).AddToDescriptor(&layer); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to add resource artifact annotation to descriptor: %w", err)
	}

	return layer, nil
}
