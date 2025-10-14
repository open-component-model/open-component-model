package component

import (
	"encoding/json"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// MediaType is the media type for ComponentConfiguration
// see https://github.com/open-component-model/ocm/blob/4a1aa4fa4668b2a0758a0d0a2d2c0e7c5d180d7e/api/ocm/extensions/repositories/genericocireg/componentmapping/constants.go#L45
const MediaType = "application/vnd.ocm.software.component.config.v1+json"

// Config is a Component-Descriptor OCI configuration that is used to componentVersionStore the reference to the
// (pseudo-)layer used to componentVersionStore the Component-Descriptor in.
type Config struct {
	ComponentDescriptorLayer *ociImageSpecV1.Descriptor `json:"componentDescriptorLayer,omitempty"`
}

// New creates a Config from a ComponentDescriptorLayer descriptor.
// It returns the encoded Config, the descriptor of the Config and an error if any.
func New(componentDescriptorLayerOCIDescriptor ociImageSpecV1.Descriptor) (encoded []byte, descriptor ociImageSpecV1.Descriptor, err error) {
	// New and upload the component configuration.
	componentConfig := Config{
		ComponentDescriptorLayer: &componentDescriptorLayerOCIDescriptor,
	}
	componentConfigRaw, err := json.Marshal(componentConfig)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to marshal component config: %w", err)
	}

	return componentConfigRaw, content.NewDescriptorFromBytes(MediaType, componentConfigRaw), nil
}
