// Package sigstore provides a signing handler for the Open Component Model
// that implements Sigstore-based keyless signing and verification by delegating
// to the cosign CLI tool.
//
// This handler invokes cosign as an external process, keeping the transitive
// dependency footprint minimal while producing standard Sigstore protobuf
// bundles (v0.3).
//
// # Prerequisites
//
// Cosign >= v3.0.4 is required (introduces --signing-config).
// The tested/pinned version is defined in signing/handler/.env (COSIGN_VERSION).
// At runtime the handler hard-fails below the minimum and warns below the
// pinned version.
//
// # Handler Configuration Types
//
// The handler registers two config types in its runtime.Scheme:
//   - SigstoreSigningConfiguration/v1alpha1 — passed via --signer-spec
//   - SigstoreVerificationConfiguration/v1alpha1 — passed via --verifier-spec
//
// # Endpoint Discovery
//
// Signing endpoints (Fulcio, Rekor, TSA) are configured via a signing config
// file (cosign --signing-config). Create one with `cosign signing-config create`.
// When no signing config is provided, cosign fetches the public-good Sigstore
// signing config from its TUF repository.
//
// # Credential Consumer Identities
//
// The handler generates credential consumer identities with the following
// attributes for credential graph lookup:
//
// Signing (GetSigningCredentialConsumerIdentity):
//
//	type:      SigstoreSigner/v1alpha1
//	signature: <signature-name>
//	issuer:    <oidc-issuer>     (optional, from signer spec)
//	clientID:  <oauth2-client>   (optional, from signer spec)
//
// Verification (GetVerifyingCredentialConsumerIdentity):
//
//	type:      SigstoreVerifier/v1alpha1
//	signature: <signature-name>
//
// The minimal consumer identity contains only type and signature, which
// uses the public Sigstore infrastructure with default OIDC settings.
// For enterprise Sigstore stacks, set issuer and clientID in the signer spec;
// the handler emits them into the consumer identity so that .ocmconfig entries
// can distinguish between different Sigstore deployments.
//
// # Credential Keys
//
// Signing credentials (resolved via SigstoreSigner/v1alpha1 identity):
//   - token: OIDC identity token for Fulcio authentication
//   - trusted_root_json: inline trusted root JSON (for private infrastructure signing)
//   - trusted_root_json_file: path to trusted root JSON file (for private infrastructure signing)
//
// Verification credentials (resolved via SigstoreVerifier/v1alpha1 identity):
//   - trusted_root_json: inline trusted root JSON
//   - trusted_root_json_file: path to trusted root JSON file
//
// # Trusted Root Resolution
//
// Trusted root resolution order (first wins, applies to both signing and verification):
//  1. trusted_root_json credential — inline JSON written to a temp file
//  2. trusted_root_json_file credential — path passed as --trusted-root
//  3. "" — cosign falls back to public-good TUF default
//
// Note: TUF_ROOT and SIGSTORE_ROOT_FILE env vars control cosign's TUF cache
// and initialization, not the --trusted-root flag. They coexist with
// credential-provided trusted roots without conflict.
//
// # OIDC Token Acquisition
//
// OIDC token acquisition for keyless signing happens before cosign is invoked.
// The token must be resolved through the credential graph (configured as a
// consumer identity of type SigstoreSigner/v1alpha1 in .ocmconfig with a
// credential of type OIDCIdentityTokenProvider/v1alpha1 or Credentials/v1
// with a "token" property). The handler forwards the resolved token to cosign
// via the SIGSTORE_ID_TOKEN environment variable.
//
// If SIGSTORE_ID_TOKEN or ACTIONS_ID_TOKEN_REQUEST_TOKEN is already set in
// the process environment, the handler uses the ambient token and skips
// credential graph lookup. Otherwise the token must be resolved through
// the credential graph and is injected into the cosign subprocess via
// SIGSTORE_ID_TOKEN. The full parent process environment is forwarded to
// cosign without filtering.
package sigstore
