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

// S3Bucket is the input method specification for a resource that comes from a single
// blob (object) in an S3 or S3-compatible bucket. OCM downloads the object during the
// component construction and stores it as a local blob in the component version. The
// component version therefore does not depend on the bucket after the build. It holds
// the same fields as the S3Bucket access type.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type S3Bucket struct {
	// +ocm:jsonschema-gen:enum=S3Bucket/v1,s3Bucket/v1
	// +ocm:jsonschema-gen:enum:deprecated=S3Bucket,s3Bucket
	Type runtime.Type `json:"type"`

	// Region is the region of the bucket. It is optional. When it is empty, OCM reads
	// it from the environment or applies a default. Most custom endpoints ignore it.
	Region string `json:"region,omitempty"`

	// BucketName is the name of the bucket that holds the object.
	BucketName string `json:"bucketName"`

	// ObjectKey is the key (path) of the object in the bucket.
	ObjectKey string `json:"objectKey"`

	// MediaType is the media type of the referenced object.
	MediaType string `json:"mediaType,omitempty"`

	// Version pins one S3 object version (versionId). When it is empty, OCM reads the
	// latest version.
	Version string `json:"version,omitempty"`

	// Endpoint is the base endpoint of an S3-compatible store, for example MinIO, Ceph
	// or R2. When it is empty, OCM uses AWS S3.
	Endpoint string `json:"endpoint,omitempty"`

	// UsePathStyle puts the bucket in the path instead of in the host. Most self-hosted
	// S3-compatible stores need this.
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
