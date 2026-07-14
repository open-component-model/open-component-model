package access

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

const (
	// S3ConsumerType is the OCM type name for the S3 access type.
	S3ConsumerType = "S3"
)

var V1VersionedType = runtime.NewVersionedType(S3ConsumerType, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	spec := &v1.S3{}

	lowerCaseConsumerType := strings.ToLower(S3ConsumerType)
	scheme.MustRegisterWithAlias(spec,
		V1VersionedType,
		runtime.NewUnversionedType(S3ConsumerType),
		runtime.NewVersionedType(lowerCaseConsumerType, v1.Version),
		runtime.NewUnversionedType(lowerCaseConsumerType),
	)
}
