package v1

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Type           = "S3"
	LowerCamelType = "s3"
	Version        = "v1"
)

// S3 is the v1 wire representation of access to an S3 object.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type S3 struct {
	// +ocm:jsonschema-gen:enum=S3/v1,s3/v1
	Type runtime.Type `json:"type"`

	// Bucket is the name of the bucket holding the object.
	Bucket string `json:"bucket"`
	// Key is the object key within the bucket.
	Key string `json:"key"`
	// Region is the optional bucket region.
	Region string `json:"region,omitempty"`
	// Version pins an S3 object version (versionId).
	Version string `json:"version,omitempty"`
	// MediaType is the media type of the referenced object.
	MediaType string `json:"mediaType,omitempty"`
	// Endpoint extends the v1 format to support S3-compatible stores.
	Endpoint string `json:"endpoint,omitempty"`
	// UsePathStyle enables bucket addressing in the path rather than the host.
	UsePathStyle bool `json:"usePathStyle,omitempty"`
}

// Validate verifies that the required fields of the S3 access are set.
func (s *S3) Validate() error {
	if s.Bucket == "" {
		return errors.New("bucket is required")
	}
	if s.Key == "" {
		return errors.New("key is required")
	}
	return nil
}
