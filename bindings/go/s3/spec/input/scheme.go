package input

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/input/v1"
)

const (
	// S3InputType is the OCM type name for the S3 input method.
	S3InputType = "S3"
)

var V1VersionedType = runtime.NewVersionedType(S3InputType, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	spec := &v1.S3{}

	lowerCaseInputType := strings.ToLower(S3InputType)
	scheme.MustRegisterWithAlias(spec,
		V1VersionedType,
		runtime.NewUnversionedType(S3InputType),
		runtime.NewVersionedType(lowerCaseInputType, v1.Version),
		runtime.NewUnversionedType(lowerCaseInputType),
	)
}
