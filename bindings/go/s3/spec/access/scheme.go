package access

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

const (
	S3BucketConsumerType = "S3Bucket"
)

var V1VersionedType = runtime.NewVersionedType(v1.Type, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	spec := &v1.S3Bucket{}

	lowerCaseAccessType := strings.ToLower(v1.Type)
	scheme.MustRegisterWithAlias(spec,
		V1VersionedType,
		runtime.NewUnversionedType(v1.Type),
		runtime.NewVersionedType(lowerCaseAccessType, v1.Version),
		runtime.NewUnversionedType(lowerCaseAccessType),
	)
}
