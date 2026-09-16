package v1

import "ocm.software/open-component-model/bindings/go/runtime"

var scheme = runtime.NewScheme()

func init() {
	MustRegisterIdentityType(scheme)
}

// MustRegisterIdentityType registers NpmRegistry/v1 (with unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&NPMRegistryIdentity{},
		VersionedType,
		Type, // backward-compat alias, matches the OCM v1 consumer type
	)
}
