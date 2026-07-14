package v1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// S3 is the input method specification for sourcing a resource from a single blob
// (object) stored in an S3 or S3-compatible bucket during component construction.
// The downloaded object is stored as a local blob in the component version. It
// mirrors the fields of the S3 access type.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type S3 struct {
	// +ocm:jsonschema-gen:enum=S3/v1,s3/v1
	// +ocm:jsonschema-gen:enum:deprecated=S3,s3
	Type runtime.Type `json:"type"`

	// Region is the region of the bucket. Optional; when empty it is resolved from
	// the environment or defaulted, and is typically ignored for custom endpoints.
	Region string `json:"region,omitempty"`

	// BucketName is the name of the bucket that holds the object.
	BucketName string `json:"bucketName"`

	// ObjectKey is the key (path) of the object within the bucket.
	ObjectKey string `json:"objectKey"`

	// MediaType is the media type of the referenced object.
	MediaType string `json:"mediaType,omitempty"`

	// Version pins a specific S3 object version (versionId). When empty the latest
	// version is read.
	Version string `json:"version,omitempty"`

	// Endpoint is the base endpoint of an S3-compatible store (e.g. MinIO, Ceph,
	// R2). When empty, AWS S3 is targeted.
	Endpoint string `json:"endpoint,omitempty"`

	// UsePathStyle enables path-style addressing (bucket in the path instead of the
	// host). Required by most self-hosted S3-compatible stores.
	UsePathStyle bool `json:"usePathStyle,omitempty"`

	// InsecureSkipTLSVerify disables TLS certificate verification for the endpoint.
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`
}

// Validate verifies that the required fields of the S3 input are set.
func (t *S3) Validate() error {
	if t.BucketName == "" {
		return fmt.Errorf("bucketName is required")
	}
	if t.ObjectKey == "" {
		return fmt.Errorf("objectKey is required")
	}
	return nil
}

func (t *S3) String() string {
	loc := t.BucketName + "/" + t.ObjectKey
	if t.Endpoint != "" {
		return t.Endpoint + "/" + loc
	}
	return "s3://" + loc
}
