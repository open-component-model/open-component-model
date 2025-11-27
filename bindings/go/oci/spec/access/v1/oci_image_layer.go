package v1

import (
	"fmt"

	"github.com/opencontainers/go-digest"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// LegacyOCIBlobAccessType and LegacyOCIBlobAccessTypeVersion were the Type information of OCIImageLayer in the old CLI.
const (
	LegacyOCIBlobAccessType        = "ociBlob"
	LegacyOCIBlobAccessTypeVersion = "v1"
)

// OCIImageLayer describes the access for a local blob as an OCI Layer.
// Note that an OCIImageLayer itself usually needs to be resolved through the manifest
// to determine the mediaType of the Layer.
// To avoid this lookup necessity to interpret the layer, the mediaType
// can be set in the OCIImageLayer directly and can be used instead of a manifest lookup.
// Note however, that presence of the layer in OCI is only guaranteed if a Manifest
// is present in the repository that references the layer.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIImageLayer struct {
	// +ocm:jsonschema-gen:enum=OCIImageLayer/v1,ociBlob/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCIImageLayer,ociBlob
	Type runtime.Type `json:"type"`
	// Reference is the oci reference to the OCI repository
	Reference string `json:"ref"`
	// MediaType is the media type of the object this schema refers to.
	MediaType string `json:"mediaType,omitempty"`
	// Digest is the digest of the targeted content.
	Digest digest.Digest `json:"digest"`
	// Size specifies the size in bytes of the blob.
	// +ocm:jsonschema-gen:minimum=0
	Size int64 `json:"size"`
}

func (t *OCIImageLayer) Validate() error {
	if err := t.Digest.Validate(); err != nil {
		return err
	}
	if t.Size < 0 {
		return fmt.Errorf("size %d is invalid, must be greater than 0", t.Size)
	}
	if t.Reference == "" {
		return fmt.Errorf("reference is empty")
	}
	ref, err := registry.ParseReference(t.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference %q: %w", t.Reference, err)
	}
	if dig, err := ref.Digest(); err == nil && dig != t.Digest {
		return fmt.Errorf("digest field value %q does not match digest contained in reference %q", t.Digest, t.Reference)
	}

	return nil
}
