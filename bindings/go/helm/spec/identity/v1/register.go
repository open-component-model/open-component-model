package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// MustRegisterIdentityType registers HelmChartRepository/v1 (with unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&HelmChartRepositoryIdentity{},
		VersionedType,
		Type, // backward-compat alias
	)
}
