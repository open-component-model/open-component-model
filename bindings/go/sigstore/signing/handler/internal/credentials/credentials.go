// Package credentials defines the consumer identity types for the sigstore signing handler.

package credentials

import "ocm.software/open-component-model/bindings/go/runtime"

// IdentityTypeSigstoreSigner identifies a consumer requesting credentials for a cosign signing operation.
// The matching credential type is OIDCIdentityToken/<version>
//
// IdentityTypeSigstoreVerifier identifies a consumer requesting credentials for a cosign verification operation.
// The matching credential type is TrustedRoot/<version>
var (
	IdentityTypeSigstoreSigner   = runtime.NewVersionedType("SigstoreSigner", "v1alpha1")
	IdentityTypeSigstoreVerifier = runtime.NewVersionedType("SigstoreVerifier", "v1alpha1")
)
