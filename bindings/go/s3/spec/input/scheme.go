package input

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/input/v1"
)

var V1VersionedType = runtime.NewVersionedType(v1.Type, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	spec := &v1.S3Bucket{}

	scheme.MustRegisterWithAlias(spec,
		V1VersionedType,
		runtime.NewUnversionedType(v1.Type),
		runtime.NewVersionedType(v1.LowerCamelType, v1.Version),
		runtime.NewUnversionedType(v1.LowerCamelType),
	)
}
