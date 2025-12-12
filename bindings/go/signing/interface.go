package signing

import (
	"context"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Handler groups signing and verification.
// Implementations MUST be able to verify descriptors they produce via Sign.
type Handler interface {
	Signer
	Verifier
}

// Signer signs a normalized Component Descriptor.
//
// Implementations MUST:
// - Expect that ALL unsigned signature digests were already precomputed from scratch for artifacts and component references BEFORE calling Sign.
// See: https://ocm.software/docs/getting-started/sign-component-versions/
// - Reject signature specifications without a precalculated digest specification
// - Not modify the given signature digest specification in any way when signing
//
// Implementations SHOULD:
// - Use a well-known registered default configuration and be modifiable in their behavior, assuming sane defaults.
// - Offer versioned, stable signature implementations differentiated by the config type.
// - Reject signing specifications if there is no credential available that is required for the handler.
//
// The returned signature SHOULD be attached to the descriptor `signatures` field after a successful call to Sign.
type Signer interface {
	// GetSigningCredentialConsumerIdentity resolves the credential consumer identity
	// for an unsigned digest named `name` that should be signed with the given configuration.
	// If successful, the returned identity SHOULD be used for credential resolution. (i.e. against the OCM credential graph)
	// If unsuccessful, an error MUST be returned, and Sign MAY be called without credentials.
	GetSigningCredentialConsumerIdentity(ctx context.Context, name string, unsigned descruntime.Digest, config runtime.Typed) (identity runtime.Identity, err error)

	// Sign signs the descriptor using the provided config.
	// An extensible config SHOULD support media type and algorithm selection, if multiple are availalbe.
	//
	// Configurations MUST NOT contain any private key or otherwise sensitive material. This is a security risk.
	// Instead, the signer MUST use the provided credentials and well-known attributes to sign the digest specification.
	// The signer SHOULD fallback to environment or implementation
	// defaults based on its configuration when no credentials are provided.
	Sign(ctx context.Context, unsigned descruntime.Digest, config runtime.Typed, credentials map[string]string) (signed descruntime.SignatureInfo, err error)
}

// Verifier validates signatures and digests for a Component Descriptor.
//
// Implementations MUST:
// - Verify the cryptographic signature over the normalized digest using the provided configuration.
// - Return an error if any selected signature or required digest check fails.
//
// Implementations SHOULD:
// - Use a well-known registered default configuration derived from configuration and specification and be modifiable in their behavior, assuming sane defaults.
// - Offer versioned, stable verification implementations differentiated by the config type.
// - Reject verification specifications if there is no credential available that is required for the handler to verify the signature.
//
// See: https://ocm.software/docs/reference/ocm-cli/verify/componentversions/
type Verifier interface {
	// GetVerifyingCredentialConsumerIdentity resolves the credential consumer identity of
	// the signature that should be verified with the given configuration.
	// If successful, the returned identity SHOULD be used for credential resolution (i.e. against the OCM credential graph)
	// If unsuccessful, an error MUST be returned, and Verify CAN be called without credentials.
	GetVerifyingCredentialConsumerIdentity(ctx context.Context, signed descruntime.Signature, config runtime.Typed) (identity runtime.Identity, err error)

	// Verify performs signature and digest checks using the provided config.
	//
	// An extensible config SHOULD support timeout / limit configurations for signature validation.
	// Configurations MUST NOT contain any key or otherwise sensitive material. This is a security risk.
	// Instead, the verifier MUST use the provided credentials and well-known attributes to verify the signature.
	// If the media type cannot be verified, the signature verification MUST fail.
	// The verifier SHOULD fallback to environment or implementation
	// defaults based on its configuration when no credentials are provided.
	Verify(ctx context.Context, signed descruntime.Signature, config runtime.Typed, credentials map[string]string) error
}
