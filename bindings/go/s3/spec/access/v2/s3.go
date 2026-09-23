package v2

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Type           = "S3"
	LowerCamelType = "s3"
)

// S3 describes access to a single blob (object) stored in an S3 or S3-compatible
// bucket. It references exactly one object; it is not a repository/storage backend.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=s3.schema.json
type S3 struct {
	// +ocm:jsonschema-gen:enum=S3/v2,s3/v2
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
}

// UnmarshalJSON accepts legacy bucket/key fields only for the unversioned aliases.
func (t *S3) UnmarshalJSON(data []byte) error {
	type wire S3
	decoded := wire{Type: t.Type}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Type.Version == "v1" {
		return errors.New("v1 S3 must be decoded using the v1 wire type")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	legacy := decoded.Type.Version == "" && (decoded.Type.Name == Type || decoded.Type.Name == LowerCamelType)
	for _, pair := range []struct {
		old, current string
		value        *string
	}{
		{"bucket", "bucketName", &decoded.BucketName},
		{"key", "objectKey", &decoded.ObjectKey},
	} {
		for name, raw := range fields {
			if !strings.EqualFold(name, pair.old) {
				continue
			}
			if !legacy {
				return fmt.Errorf("%s is only supported by unversioned S3 aliases or S3/v1", pair.old)
			}

			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("decode %s: %w", pair.old, err)
			}
			for other, otherRaw := range fields {
				if !strings.EqualFold(other, pair.current) && !strings.EqualFold(other, pair.old) {
					continue
				}
				var otherValue string
				if err := json.Unmarshal(otherRaw, &otherValue); err != nil {
					return fmt.Errorf("decode %s: %w", other, err)
				}
				if value != otherValue {
					return fmt.Errorf("conflicting %s and %s values", name, other)
				}
			}
			*pair.value = value
		}
	}

	*t = S3(decoded)
	return nil
}

// Validate verifies that the required fields of the S3 access are set.
func (t *S3) Validate() error {
	if t.BucketName == "" {
		return errors.New("bucketName is required")
	}
	if t.ObjectKey == "" {
		return errors.New("objectKey is required")
	}
	return nil
}

func (t *S3) String() string {
	loc := t.BucketName + "/" + t.ObjectKey
	if t.Endpoint != "" {
		return strings.TrimSuffix(t.Endpoint, "/") + "/" + loc
	}
	return "s3://" + loc
}
