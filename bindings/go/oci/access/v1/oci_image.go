package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	LegacyType         = "ociArtifact"
	LegacyTypeVersion  = "v1"
	LegacyType2        = "ociRegistry"
	LegacyType2Version = "v1"
	LegacyType3        = "ociImage"
	LegacyType3Version = "v1"
)

// OCIImage describes the access for a oci registry.
type OCIImage struct {
	runtime.Type `json:"type"`
	// ImageReference is the actual reference to the oci image repository and tag.
	ImageReference string `json:"imageReference"`
}

func (a *OCIImage) GetType() runtime.Type {
	return a.Type
}

func (a *OCIImage) String() string {
	return a.ImageReference
}
