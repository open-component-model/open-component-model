package sigstore

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	creds "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/sigstore/v1"
)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	sigstoreCreds := &creds.SigstoreCredentials{}
	scheme.MustRegisterWithAlias(sigstoreCreds,
		creds.SigstoreCredentialsVersionedType,
		runtime.NewUnversionedType(creds.SigstoreCredentialsType),
	)
}
