package algorithms

import (
	"context"
	"log/slog"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// NoReferenceDigest is the normalisation algorithm WarnNonDefaultReferenceDigest
// reports for a component reference without a digest.
const NoReferenceDigest = "none"

// WarnNonDefaultReferenceDigest logs a warning if the component reference
// digest d is missing or not computed with Default. Reference digests are
// minted when the referencing component version is built, so rebuilding it
// with a current OCM CLI upgrades them, unless the constructor pins the
// reference digest. attrs identify the reference.
func WarnNonDefaultReferenceDigest(ctx context.Context, logger *slog.Logger, d descruntime.Digest, attrs ...any) {
	if d.Value == "" {
		// Older tools (e.g. cc-utils) computed reference digests while signing
		// without writing them into the descriptor; nothing adds them later.
		logger.WarnContext(ctx,
			"component reference has no digest: the referenced component version is not verified against it, and a signature of the referencing component version cannot be verified",
			append(attrs, "normalisationAlgorithm", NoReferenceDigest)...,
		)
		return
	}
	if d.NormalisationAlgorithm == Default {
		return
	}
	logger.WarnContext(ctx,
		"component reference does not use the current normalisation algorithm; rebuild the referencing component version with a current OCM CLI, without pinning the reference digest, to upgrade it",
		append(attrs, "normalisationAlgorithm", d.NormalisationAlgorithm, "recommended", Default)...,
	)
}
