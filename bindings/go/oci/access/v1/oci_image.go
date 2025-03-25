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
type OCIImage struct {
	Type runtime.Type `json:"type"`
	// ImageReference is the actual reference to the oci image repository and tag.
	ImageReference string `json:"imageReference"`
}

// GetType returns the type definition of LocalBlob as per OCM's Type System.
// It is the type on which it will be registered with the dynamic type system.
func (t *OCIImage) GetType() runtime.Type {
	return t.Type
}

func (t *OCIImage) String() string {
	return t.ImageReference
}
