package v1

import (
	"fmt"

	"github.com/opencontainers/go-digest"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocalBlobAccessType was the Type information of OCIImageLayer in the old CLI.
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
type OCIImageLayer struct {
	runtime.Type `json:"type"`
	// Reference is the oci reference to the OCI repository
	Reference string `json:"ref"`
	// MediaType is the media type of the object this schema refers to.
	MediaType string `json:"mediaType,omitempty"`
	// Digest is the digest of the targeted content.
	Digest digest.Digest `json:"digest"`
	// Size specifies the size in bytes of the blob.
	Size int64 `json:"size"`
}

func (o *OCIImageLayer) Validate() error {
	if err := o.Digest.Validate(); err != nil {
		return err
	}
	if o.Size < 0 {
		return fmt.Errorf("size %d is invalid, must be greater than 0", o.Size)
	}
	if o.Reference == "" {
		return fmt.Errorf("reference is empty")
	}
	ref, err := registry.ParseReference(o.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference %q: %w", o.Reference, err)
	}
	if dig, err := ref.Digest(); err == nil && dig != o.Digest {
		return fmt.Errorf("digest field value %q does not match digest contained in reference %q", o.Digest, o.Reference)
	}

	return nil
}
