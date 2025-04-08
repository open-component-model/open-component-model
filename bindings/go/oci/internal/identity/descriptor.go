package identity

import (
	"fmt"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
)

// platformAttributeMapper defines the mapping between resource identity attributes and OCI platform fields
type platformAttributeMapper struct {
	attribute string
	setter    func(platform *ociImageSpecV1.Platform, value string)
}

var mappings = []platformAttributeMapper{
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

// AdoptAsResource modifies the provided OCI descriptor to represent a resource.
// It sets the platform fields based on the resource's extra identity attributes
// and adds a annotations.ArtifactOCIAnnotation to indicate that the descriptor
// is a annotations.ArtifactKindResource.
func AdoptAsResource(desc *ociImageSpecV1.Descriptor, resource *descriptor.Resource) error {
	// Apply platform mappings
	for _, mapping := range mappings {
		if value, exists := resource.ExtraIdentity[mapping.attribute]; exists {
			if desc.Platform == nil {
				desc.Platform = &ociImageSpecV1.Platform{}
			}
			mapping.setter(desc.Platform, value)
		}
	}
	if err := (&annotations.ArtifactOCIAnnotation{
		Identity: resource.ToIdentity(),
		Kind:     annotations.ArtifactKindResource,
	}).AddToDescriptor(desc); err != nil {
		return fmt.Errorf("failed to add resource artifact annotation to manifest: %w", err)
	}

	return nil
}
