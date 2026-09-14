package input

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/spec/input/v2"
)

var V2VersionedType = runtime.NewVersionedType(v2.Type, v2.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	spec := &v2.S3{}

	scheme.MustRegisterWithAlias(spec,
		V2VersionedType,
		runtime.NewUnversionedType(v2.Type),
		runtime.NewVersionedType(v2.LowerCamelType, v2.Version),
		runtime.NewUnversionedType(v2.LowerCamelType),
	)
}
