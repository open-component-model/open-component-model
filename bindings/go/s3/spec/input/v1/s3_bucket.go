package v1

import (
	"errors"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Type           = "S3Bucket"
	LowerCamelType = "s3Bucket"
)

// S3Bucket is the input method specification for sourcing a resource from a single
// blob (object) stored in an S3 or S3-compatible bucket during component construction.
// The object is downloaded and stored as a local blob in the component version, so the
// component version no longer depends on the bucket once it is built. It mirrors the
// fields of the S3Bucket access type.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type S3Bucket struct {
	// +ocm:jsonschema-gen:enum=S3Bucket/v1,s3Bucket/v1
	// +ocm:jsonschema-gen:enum:deprecated=S3Bucket,s3Bucket
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
}

// Validate verifies that the required fields of the S3Bucket input are set.
func (t *S3Bucket) Validate() error {
	if t.BucketName == "" {
		return errors.New("bucketName is required")
	}
	if t.ObjectKey == "" {
		return errors.New("objectKey is required")
	}
	return nil
}

func (t *S3Bucket) String() string {
	loc := t.BucketName + "/" + t.ObjectKey
	if t.Endpoint != "" {
		return strings.TrimSuffix(t.Endpoint, "/") + "/" + loc
	}
	return "s3://" + loc
}
