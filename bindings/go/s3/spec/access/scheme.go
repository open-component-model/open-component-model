package access

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

var V1VersionedType = runtime.NewVersionedType(v1.Type, v1.Version)

var V2VersionedType = runtime.NewVersionedType(v2.Type, v2.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.S3{},
		V1VersionedType,
		runtime.NewVersionedType(v1.LowerCamelType, v1.Version),
	)
	spec := &v2.S3{}

	scheme.MustRegisterWithAlias(spec,
		V2VersionedType,
		runtime.NewUnversionedType(v2.Type),
		runtime.NewVersionedType(v2.LowerCamelType, v2.Version),
		runtime.NewUnversionedType(v2.LowerCamelType),
	)
}

// ConvertToV2 resolves the source wire representation before normalizing it to v2.
// The returned spec is independent of the input; validation remains the caller's responsibility.
func ConvertToV2(spec runtime.Typed) (*v2.S3, error) {
	switch s := spec.(type) {
	case *runtime.Raw:
		if s == nil {
			return nil, fmt.Errorf("S3 access spec must not be nil")
		}
	case *runtime.Unstructured:
		if s == nil {
			return nil, fmt.Errorf("S3 access spec must not be nil")
		}
	case *v1.S3:
		if s == nil {
			return nil, fmt.Errorf("S3 access spec must not be nil")
		}
		if !s.Type.IsEmpty() && ((s.Type.Name != v1.Type && s.Type.Name != v1.LowerCamelType) || s.Type.Version != v1.Version) {
			return nil, fmt.Errorf("invalid type %q for v1 S3 spec", s.Type)
		}
		return &v2.S3{
			Type:         V2VersionedType,
			BucketName:   s.Bucket,
			ObjectKey:    s.Key,
			Region:       s.Region,
			Version:      s.Version,
			MediaType:    s.MediaType,
			Endpoint:     s.Endpoint,
			UsePathStyle: s.UsePathStyle,
		}, nil
	case *v2.S3:
		if s == nil {
			return nil, fmt.Errorf("S3 access spec must not be nil")
		}
		if !s.Type.IsEmpty() && ((s.Type.Name != v2.Type && s.Type.Name != v2.LowerCamelType) || (s.Type.Version != "" && s.Type.Version != v2.Version)) {
			return nil, fmt.Errorf("invalid type %q for v2 S3 spec", s.Type)
		}
		out := &v2.S3{}
		if err := Scheme.Convert(s, out); err != nil {
			return nil, err
		}
		// Unversioned aliases may have been decoded from either wire format.
		// Normalize here, not in UnmarshalJSON, so Scheme.Decode retains its type check.
		if out.Type.Version == "" {
			out.Type = V2VersionedType
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported S3 access spec %T", spec)
	}

	obj, err := Scheme.NewObject(spec.GetType())
	if err != nil {
		return nil, fmt.Errorf("resolve S3 access spec: %w", err)
	}
	if err := Scheme.Convert(spec, obj); err != nil {
		return nil, fmt.Errorf("decode S3 access spec: %w", err)
	}
	return ConvertToV2(obj)
}
