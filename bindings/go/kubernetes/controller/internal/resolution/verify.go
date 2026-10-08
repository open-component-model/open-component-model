package resolution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

// ErrNotSafelyDigestible is a sentinel error reported when a component version cannot be safely digested.
var ErrNotSafelyDigestible = errors.New("not safely digestible")

// resolveOptions contains everything required to fetch and verify a single component version.
type resolveOptions struct {
	component  string
	version    string
	repository repository.ComponentVersionRepository
	// verifications are the component signatures to verify.
	verifications []verification.Verification
	// credentialGraph resolves the public keys and trust material used to verify signatures.
	credentialGraph credentials.Resolver
	// digest is used to verify the integrity of a referenced component version.
	digest          *v2.Digest
	signingRegistry *signinghandler.SigningRegistry
}

// getComponentVersion performs the actual component version resolution. If verifications or a digest from a component
// reference from a parent component are provided, it performs the necessary integrity and signature verification.
func getComponentVersion(ctx context.Context, opts resolveOptions) (*descriptor.Descriptor, error) {
	logger := log.FromContext(ctx)

	desc, err := opts.repository.GetComponentVersion(ctx, opts.component, opts.version)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s: %w", opts.component, opts.version, err)
	}

	if opts.digest != nil && len(opts.verifications) > 0 {
		return nil, fmt.Errorf(
			"invalid resolve options for %s:%s: digest and verifications are mutually exclusive",
			opts.component, opts.version,
		)
	}

	switch {
	case opts.digest != nil:
		return compareDigest(ctx, desc, opts.digest)
	case len(opts.verifications) > 0:
		// If verifications are requested, we need to verify that the component version is safely digestible.
		// TODO(Skarlso): This contradicts a bit with our config now. Wondering if we should still leave this be.
		// Weak digest hashes fail IsSafelyDigestible only with GODEBUG=fips140=only; otherwise they are logged.
		if err := signing.ValidateDigestHashAlgorithms(&desc.Component); err != nil && !signing.DigestHashAlgorithmsEnforced() {
			logger.Info("component version uses a weak digest hash algorithm (GODEBUG=fips140=only rejects it)",
				"component", opts.component, "version", opts.version, "error", err.Error())
		}
		if err := signing.IsSafelyDigestible(&desc.Component); err != nil {
			return desc, fmt.Errorf("%w: %w", ErrNotSafelyDigestible, err)
		}

		if opts.signingRegistry == nil {
			return nil, fmt.Errorf("signing registry is required when verifications are configured")
		}

		return verifySignatures(ctx, desc, opts.verifications, opts.signingRegistry, opts.credentialGraph)
	default:
		logger.V(1).Info("no digest or verifications provided, skipping integrity and signature verification",
			"component", opts.component, "version", opts.version)
		return desc, nil
	}
}

// verifySignatures performs signature verification for the provided component version descriptor and the list of
// verifications. The verifier comes from the OCM configuration, its public key from the credential graph.
func verifySignatures(
	ctx context.Context,
	desc *descriptor.Descriptor,
	verifications []verification.Verification,
	signingRegistry *signinghandler.SigningRegistry,
	graph credentials.Resolver,
) (*descriptor.Descriptor, error) {
	logger := log.FromContext(ctx)
	logger.Info("verifying signature", "component", desc.Component.Name, "version", desc.Component.Version)

	for _, v := range verifications {
		var descSig *descriptor.Signature
		for i := range desc.Signatures {
			if desc.Signatures[i].Name == v.Signature {
				descSig = &desc.Signatures[i]
				break
			}
		}

		if descSig == nil {
			return nil, fmt.Errorf("signature %s not found in component %s", v.Signature, desc.Component.Name)
		}

		if err := signing.VerifyDigestMatchesDescriptor(ctx, desc, *descSig, slog.New(logr.ToSlogHandler(logger))); err != nil {
			return nil, fmt.Errorf("digest verification failed for signature %q: %w", descSig.Name, err)
		}

		handler, err := signingRegistry.GetPlugin(ctx, v.Verifier)
		if err != nil {
			return nil, fmt.Errorf("failed to get signing handler plugin for signature %q: %w", v.Signature, err)
		}

		creds, err := verificationCredentials(ctx, handler, *descSig, v.Verifier, graph, logger)
		if err != nil {
			return nil, err
		}

		if err := handler.Verify(ctx, *descSig, v.Verifier, creds); err != nil {
			return nil, fmt.Errorf("signature verification failed for signature %s: %w", v.Signature, err)
		}
	}

	return desc, nil
}

// verificationCredentials resolves the public key or trust material the handler needs from the credential graph.
func verificationCredentials(
	ctx context.Context,
	handler signing.Handler,
	signature descriptor.Signature,
	verifierSpec runtime.Typed,
	graph credentials.Resolver,
	logger logr.Logger,
) (runtime.Typed, error) {
	consumerID, err := handler.GetVerifyingCredentialConsumerIdentity(ctx, signature, verifierSpec)
	if err != nil {
		logger.V(1).Info("handler requires no credentials for verification", "signature", signature.Name)

		return nil, nil
	}

	if graph == nil {
		return nil, fmt.Errorf("credential graph is required to verify signature %q", signature.Name)
	}

	creds, err := graph.Resolve(ctx, consumerID)
	switch {
	case err == nil:
		return creds, nil
	case errors.Is(err, credentials.ErrNotFound):
		logger.V(1).Info("no credentials found for signature verification", "signature", signature.Name)

		return nil, nil
	default:
		return nil, fmt.Errorf("failed to resolve credentials for signature %q: %w", signature.Name, err)
	}
}

// compareDigest performs integrity verification using the provided digest against a fresh calculated digest of
// the passed descriptor.
func compareDigest(ctx context.Context, desc *descriptor.Descriptor, digest *v2.Digest) (*descriptor.Descriptor, error) {
	logger := log.FromContext(ctx)

	logger.Info("verifying integrity with provided digest",
		"component", desc.Component.Name, "version", desc.Component.Version)

	digestDesc, err := signing.GenerateDigest(ctx, desc, slog.New(logr.ToSlogHandler(logger)),
		digest.NormalisationAlgorithm, digest.HashAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to generate digest for component version %s:%s: %w",
			desc.Component.Name, desc.Component.Version, err)
	}

	if digestDesc.Value != digest.Value {
		return nil, fmt.Errorf("digest mismatch (%s/%s) for component version %s:%s: expected %s, got %s",
			digest.NormalisationAlgorithm, digest.HashAlgorithm, desc.Component.Name, desc.Component.Version,
			digest.Value, digestDesc.Value)
	}

	return desc, nil
}

func verificationState(verifications []verification.Verification, digest *v2.Digest) string {
	hasVerifications := len(verifications) != 0
	hasDigest := digest != nil

	switch {
	case hasVerifications && hasDigest:
		return "unknown"
	case hasVerifications || hasDigest:
		return "verified"
	default:
		return "unverified"
	}
}
