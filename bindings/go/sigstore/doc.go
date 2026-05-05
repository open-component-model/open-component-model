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
// Cosign >= v3.0.4 is required (introduces --use-signing-config).
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
// # Credential Consumer Identities
//
// The handler generates credential consumer identities with the following
// attributes for credential graph lookup:
//
// Signing (GetSigningCredentialConsumerIdentity):
//
//	type:      OIDCIdentityToken/v1alpha1
//	algorithm: sigstore
//	signature: <signature-name>
//
// Verification (GetVerifyingCredentialConsumerIdentity):
//
//	type:      TrustedRoot/v1alpha1
//	algorithm: sigstore
//	signature: <signature-name>
//
// The "algorithm" and "signature" attributes are mandatory for credential
// matching (the credential graph uses strict equality on all identity
// attributes). The .ocmconfig consumer identity must include all three
// attributes to match.
//
// # Credential Keys
//
// Signing credentials (resolved via OIDCIdentityToken/v1alpha1 identity):
//   - token: OIDC identity token for Fulcio authentication
//
// Verification credentials (resolved via TrustedRoot/v1alpha1 identity):
//   - trusted_root_json: inline trusted root JSON
//   - trusted_root_json_file: path to trusted root JSON file
//
// Credentials take precedence over the TrustedRoot field in the verifier
// config. Resolution order: inline JSON credential > file credential >
// config field > cosign default (public-good TUF).
//
// # OIDC Token Acquisition
//
// OIDC token acquisition for keyless signing happens before cosign is invoked.
// The token must be resolved through the credential graph (configured as a
// consumer identity of type OIDCIdentityToken/v1alpha1 in .ocmconfig). The
// handler forwards the resolved token to cosign internally via the
// SIGSTORE_ID_TOKEN environment variable.
//
// The SIGSTORE_ID_TOKEN environment variable is deliberately excluded from
// the general environment allowlist passed to cosign. The handler never reads
// or forwards ambient SIGSTORE_ID_TOKEN values — the token always originates
// from the OCM credential graph. This prevents accidental use of stale or
// unintended tokens from the parent process environment.
package sigstore
