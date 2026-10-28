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
// digest d is not computed with Default (or missing). Reference digests are
// minted when the referencing component version is built, so rebuilding it
// with a current OCM CLI upgrades them. attrs identify the reference.
func WarnNonDefaultReferenceDigest(ctx context.Context, logger *slog.Logger, d descruntime.Digest, attrs ...any) {
	algo := d.NormalisationAlgorithm
	if d.Value == "" {
		algo = NoReferenceDigest
	}
	if algo == Default {
		return
	}
	logger.WarnContext(ctx,
		"component reference does not use the current normalisation algorithm; rebuild the referencing component version with a current OCM CLI to upgrade it",
		append(attrs, "normalisationAlgorithm", algo, "recommended", Default)...,
	)
}
