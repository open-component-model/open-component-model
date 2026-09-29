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

// MustAddToScheme registers the input types accepted by OCM v1: Git/v1, Git, git and git/v1.
func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.Git{},
		V1VersionedType,
		runtime.NewUnversionedType(v1.Type),
		runtime.NewUnversionedType(v1.LegacyType),
		runtime.NewVersionedType(v1.LegacyType, v1.Version),
	)
}
