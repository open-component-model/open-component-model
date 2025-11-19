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
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIImage struct {
	Type runtime.Type `json:"type"`
	// ImageReference is the actual reference to the oci image repository and tag.
	ImageReference string `json:"imageReference"`
}

func (t *OCIImage) String() string {
	return t.ImageReference
}
