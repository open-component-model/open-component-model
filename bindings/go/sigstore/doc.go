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
// # Credentials
//
// The handler resolves a single typed credential per operation:
//
// Signing uses an OIDCIdentityToken/v1alpha1 credential
// (spec/credentials/oidcidentitytoken/v1alpha1.OIDCIdentityToken). Relevant fields:
//   - Token:     inline OIDC identity token for Fulcio authentication
//   - TokenFile: path to a file containing the OIDC identity token
//
// Verification uses a TrustedRoot/v1alpha1 credential
// (spec/credentials/trustedroot/v1alpha1.TrustedRoot). Relevant fields:
//   - TrustedRootJSON:     inline Sigstore trusted root JSON document
//   - TrustedRootJSONFile: path to a Sigstore trusted root JSON file
//
// Both credential types also accept Credentials/v1 DirectCredentials with the
// matching camelCase property keys for backwards compatibility.
//
// # Trusted Root Resolution
//
// Trusted root resolution applies to verification only; the signing path does
// not pass --trusted-root to cosign. Resolution order on verify (first wins):
//  1. TrustedRoot.TrustedRootJSON — inline JSON written to a temp file
//  2. TrustedRoot.TrustedRootJSONFile — path passed as --trusted-root
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
// credential of type OIDCIdentityToken/v1alpha1 providing OIDCIdentityToken.Token
// or OIDCIdentityToken.TokenFile). The handler forwards the resolved token to
// cosign via the SIGSTORE_ID_TOKEN environment variable.
//
// If SIGSTORE_ID_TOKEN or ACTIONS_ID_TOKEN_REQUEST_TOKEN is already set in
// the process environment, the handler uses the ambient token and skips
// credential graph lookup. Otherwise the token must be resolved through
// the credential graph and is injected into the cosign subprocess via
// SIGSTORE_ID_TOKEN. The full parent process environment is forwarded to
// cosign without filtering.
package sigstore
