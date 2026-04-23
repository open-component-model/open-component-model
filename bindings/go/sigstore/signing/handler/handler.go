package handler

import (
	"context"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

var _ signing.Handler = (*Handler)(nil)

const (
	IdentityAttributeAlgorithm = "algorithm"
	IdentityAttributeSignature = "signature"

	CredentialKeyOIDCToken           = "token"
	CredentialKeyTrustedRootJSON     = "trusted_root_json"
	CredentialKeyTrustedRootJSONFile = CredentialKeyTrustedRootJSON + "_file"
)

// Handler implements signing.Handler by delegating to the cosign CLI.
type Handler struct {
	executor Executor
}

// New returns a Handler that uses the default cosign executor (shells out to "cosign" in PATH).
func New() (*Handler, error) {
	exec, err := NewDefaultExecutor()
	if err != nil {
		return nil, err
	}
	return &Handler{executor: exec}, nil
}

// NewWithExecutor returns a Handler with a custom executor (for testing).
func NewWithExecutor(exec Executor) *Handler {
	return &Handler{executor: exec}
}

func (h *Handler) GetSigningHandlerScheme() *runtime.Scheme {
	return v1alpha1.Scheme
}

func (h *Handler) Sign(
	ctx context.Context,
	unsigned descruntime.Digest,
	rawCfg runtime.Typed,
	creds map[string]string,
) (descruntime.SignatureInfo, error) {
	var cfg v1alpha1.SignConfig
	if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("convert config: %w", err)
	}

	return doSign(ctx, unsigned, &cfg, creds, h.executor)
}

func (h *Handler) Verify(
	ctx context.Context,
	signed descruntime.Signature,
	rawCfg runtime.Typed,
	creds map[string]string,
) error {
	var cfg v1alpha1.VerifyConfig
	if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
		return fmt.Errorf("convert config: %w", err)
	}

	return doVerify(ctx, signed, &cfg, creds, h.executor)
}

func (*Handler) GetSigningCredentialConsumerIdentity(
	_ context.Context,
	name string,
	_ descruntime.Digest,
	rawCfg runtime.Typed,
) (runtime.Identity, error) {
	// Validate that rawCfg is a recognized signing config type.
	var cfg v1alpha1.SignConfig
	if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
		return nil, fmt.Errorf("convert config: %w", err)
	}
	_ = cfg // validated; only identity fields are needed below
	id := signingIdentity()
	id[IdentityAttributeSignature] = name
	return id, nil
}

func (*Handler) GetVerifyingCredentialConsumerIdentity(
	_ context.Context,
	signature descruntime.Signature,
	_ runtime.Typed,
) (runtime.Identity, error) {
	if signature.Signature.MediaType != v1alpha1.MediaTypeSigstoreBundle {
		return nil, fmt.Errorf("unsupported media type %q for sigstore verification", signature.Signature.MediaType)
	}
	id := verifyingIdentity()
	id[IdentityAttributeSignature] = signature.Name
	return id, nil
}

func signingIdentity() runtime.Identity {
	id := runtime.Identity{IdentityAttributeAlgorithm: v1alpha1.AlgorithmSigstore}
	id.SetType(v1alpha1.IdentityTypeOIDCIdentityToken)
	return id
}

func verifyingIdentity() runtime.Identity {
	id := runtime.Identity{IdentityAttributeAlgorithm: v1alpha1.AlgorithmSigstore}
	id.SetType(v1alpha1.IdentityTypeTrustedRoot)
	return id
}
