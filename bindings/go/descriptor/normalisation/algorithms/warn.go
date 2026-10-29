package algorithms

import (
	"context"
	"log/slog"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// MigrationGuide documents how to move component versions to Default. Every
// warning about an older normalisation algorithm links to it.
const MigrationGuide = "https://ocm.software/docs/how-to/migrate-normalisation-algorithms/"

// NoReferenceDigest is the normalisation algorithm WarnNonDefaultReferenceDigest
// reports for a component reference without a digest.
const NoReferenceDigest = "none"

// WarnNonDefaultReferenceDigest logs a warning if the component reference
// digest d is missing or not computed with Default. attrs identify the
// reference.
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
	logger.WarnContext(ctx, "component reference digest was computed with an older normalisation algorithm",
		append(attrs, "normalisationAlgorithm", d.NormalisationAlgorithm, "recommended", Default, "migration", MigrationGuide)...)
}

// WarnNonDefaultDigest logs a warning if a digest about to be trusted (a
// signature digest or a digest pin) was computed with a normalisation algorithm
// other than Default. attrs identify what is trusted.
func WarnNonDefaultDigest(ctx context.Context, logger *slog.Logger, normalisationAlgorithm string, attrs ...any) {
	if resolved, _ := Resolve(normalisationAlgorithm); resolved == Default {
		return
	}
	logger.WarnContext(ctx, "trusted digest was computed with an older normalisation algorithm",
		append(attrs, "normalisationAlgorithm", normalisationAlgorithm, "recommended", Default, "migration", MigrationGuide)...)
}
