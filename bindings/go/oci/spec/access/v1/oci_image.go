package v1

import (
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	OCIImageType       = "OCIImage"
	LegacyType         = "ociArtifact"
	LegacyTypeVersion  = "v1"
	LegacyType2        = "ociRegistry"
	LegacyType2Version = "v1"
	LegacyType3        = "ociImage"
	LegacyType3Version = "v1"
)

// OCIImage describes the access for a oci registry.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIImage struct {
	// +ocm:jsonschema-gen:enum=OCIImage/v1,ociArtifact/v1,ociRegistry/v1,ociImage/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCIImage,ociArtifact,ociRegistry,ociImage
	Type runtime.Type `json:"type"`
	// ImageReference is the actual reference to the oci image repository and tag.
	ImageReference string `json:"imageReference"`
}

// Validate verifies that the image reference of the OCIImage access is set and well formed.
func (t *OCIImage) Validate() error {
	if t.ImageReference == "" {
		return errors.New("imageReference is required")
	}
	if _, err := looseref.ParseReference(t.ImageReference); err != nil {
		return fmt.Errorf("invalid imageReference %q: %w", t.ImageReference, err)
	}
	return nil
}

func (t *OCIImage) String() string {
	return t.ImageReference
}
