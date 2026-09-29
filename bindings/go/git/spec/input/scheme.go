package input

import (
	v1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var V1VersionedType = runtime.NewVersionedType(v1.Type, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

// MustAddToScheme registers the Git access type aliases plus git/v1 from the OCM v1 input type.
func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.Git{},
		V1VersionedType,
		runtime.NewUnversionedType(v1.Type),
		runtime.NewUnversionedType(v1.LegacyType),
		runtime.NewVersionedType(v1.LegacyType, v1.Version),
		runtime.NewVersionedType(v1.LegacyType, "v1alpha1"),
		runtime.NewVersionedType(v1.Type, "v1alpha1"),
	)
}
