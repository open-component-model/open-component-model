package oci

import (
	"encoding/json"
	"fmt"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// MediaTypeComponentConfig is the media type for ComponentConfiguration
const MediaTypeComponentConfig = "application/vnd.ocm.software/ocm.component.config.v1+json"

// ComponentConfig is a Component-Descriptor OCI configuration that is used to componentVersionStore the reference to the
// (pseudo-)layer used to componentVersionStore the Component-Descriptor in.
type ComponentConfig struct {
	ComponentDescriptorLayer *ociImageSpecV1.Descriptor `json:"componentDescriptorLayer,omitempty"`
}

// createComponentConfig creates a ComponentConfig from a ComponentDescriptorLayer descriptor.
// It returns the encoded ComponentConfig, the descriptor of the ComponentConfig and an error if any.
func createComponentConfig(componentDescriptorLayerOCIDescriptor ociImageSpecV1.Descriptor) (encoded []byte, descriptor ociImageSpecV1.Descriptor, err error) {
	// Create and upload the component configuration.
	componentConfig := ComponentConfig{
		ComponentDescriptorLayer: &componentDescriptorLayerOCIDescriptor,
	}
	componentConfigRaw, err := json.Marshal(componentConfig)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to marshal component config: %w", err)
	}
	componentConfigDigest := digest.FromBytes(componentConfigRaw)
	componentConfigDescriptor := ociImageSpecV1.Descriptor{
		MediaType: MediaTypeComponentConfig,
		Digest:    componentConfigDigest,
		Size:      int64(len(componentConfigRaw)),
	}
	return componentConfigRaw, componentConfigDescriptor, nil
}
