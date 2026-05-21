package oidcidentitytoken

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	oidcv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken/v1"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	oidcToken := &oidcv1.SigstoreCredentials{}
	scheme.MustRegisterWithAlias(oidcToken,
		oidcv1.SigstoreCredentialsVersionedType,
		runtime.NewUnversionedType(oidcv1.SigstoreCredentialsType),
	)
}
