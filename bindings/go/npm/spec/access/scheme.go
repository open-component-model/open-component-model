package access

import (
	v1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

// MustAddToScheme registers NPM/v1 together with the type names used by the
// OCM v1 npm access type.
func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1.NPM{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
		runtime.NewUnversionedType(v1.LegacyType),
		runtime.NewVersionedType(v1.LegacyType, v1.Version),
	)
}
