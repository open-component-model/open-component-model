// Package credentials defines the consumer identity types for the sigstore signing handler.

package credentials

import "ocm.software/open-component-model/bindings/go/runtime"

// IdentityTypeSigstoreSigner identifies a consumer requesting credentials for a cosign signing operation.
// The matching credential is either:
//   - Credentials/v1 with a "token" property (direct OIDC token), or
//   - OIDCIdentityTokenProvider/v1alpha1 (interactive OIDC plugin)
//
// IdentityTypeSigstoreVerifier identifies a consumer requesting credentials for a cosign verification operation.
// The matching credential is Credentials/v1 with a "trusted_root_json" or "trusted_root_json_file" property.
var (
	IdentityTypeSigstoreSigner   = runtime.NewVersionedType("SigstoreSigner", "v1alpha1")
	IdentityTypeSigstoreVerifier = runtime.NewVersionedType("SigstoreVerifier", "v1alpha1")
)
