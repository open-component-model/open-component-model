package algorithms

import (
	"context"
	"log/slog"

	jsonv1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
)

// uncovered lists, per normalisation algorithm, the signing-relevant
// descriptor fields its digest does not cover: changing them keeps the digest.
// Algorithms not listed cover every field Default covers.
var uncovered = map[string]string{
	jsonv1.Algorithm:   "sources", //nolint:staticcheck // the deprecated verify-only v1 is exactly what this reports on
	v4alpha1.Algorithm: "label.version",
}

// Uncovered returns the descriptor fields a digest under normalisationAlgorithm
// does not cover, or "" if it covers everything Default covers.
func Uncovered(normalisationAlgorithm string) string {
	resolved, _ := Resolve(normalisationAlgorithm)
	return uncovered[resolved]
}

// VerifyOnly reports whether normalisationAlgorithm may only verify existing
// digests: no new signature or component reference digest may record it.
func VerifyOnly(normalisationAlgorithm string) bool {
	return normalisationAlgorithm == jsonv1.Algorithm //nolint:staticcheck // the deprecated v1 is the verify-only algorithm
}

// WarnUncovered logs a warning if a digest under normalisationAlgorithm, which
// is about to be trusted, does not cover some descriptor fields (see
// Uncovered): whoever can write the descriptor can change those fields without
// the digest noticing. attrs identify what is trusted.
func WarnUncovered(ctx context.Context, logger *slog.Logger, normalisationAlgorithm string, attrs ...any) {
	fields := Uncovered(normalisationAlgorithm)
	if fields == "" {
		return
	}
	logger.WarnContext(ctx,
		"trusted digest does not cover all descriptor fields: changes to the uncovered fields are not detected",
		append(attrs, "normalisationAlgorithm", normalisationAlgorithm, "uncovered", fields, "recommended", Default)...,
	)
}
