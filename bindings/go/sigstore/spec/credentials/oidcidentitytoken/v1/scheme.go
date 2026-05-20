package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	oidcToken := &OIDCIdentityToken{}
	scheme.MustRegisterWithAlias(oidcToken,
		runtime.NewVersionedType(OIDCIdentityTokenType, Version),
		runtime.NewUnversionedType(OIDCIdentityTokenType),
	)
}
