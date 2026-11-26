package signing

import (
	"context"
	"crypto"
	"crypto/fips140"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	// DefaultNormalisationAlgorithm is the normalisation used for new signatures
	// when a caller does not specify one (e.g. the `ocm sign` --normalisation
	// flag default), see algorithms.Default.
	DefaultNormalisationAlgorithm = algorithms.Default
	// LegacyNormalisationAlgo identifies the deprecated v3 JSON normalisation
	// algorithm. New digests requested with it are computed and recorded as
	// v4alpha2 (see ensureNormalisationAlgo); digests recorded with it are
	// verified as OCM v1 computed them (see algorithms.Verify).
	LegacyNormalisationAlgo = algorithms.LegacyV3
	// AccessTypeNone is the access type for resources without access.
	// It is used to prevent meaningless digest claims.
	AccessTypeNone = "None"
)

// ErrUnsupportedDigestHash is returned when a resource or component reference
// digest uses a hash algorithm other than SHA-256 or SHA-512.
var ErrUnsupportedDigestHash = errors.New("unsupported digest hash algorithm")

// ErrSigningUnsupported is returned by EnsureSignable for a normalisation
// algorithm this library supports for verification only. jsonNormalisation/v1
// is the legacy gardener/cc-utils format: OCM v2 verifies existing signatures
// that use it but must never mint new ones. Digest computation (GenerateDigest)
// and verification are unrestricted — only the signing boundary calls
// EnsureSignable.
//
// The reason is blast radius. Verify-only means OCM v2 never puts a v1 artifact
// into the wild, so nothing is bound to the exact bytes we emit: if our v1
// normalisation ever carries a regression or a latent incompatibility, we can
// simply patch it. Allowing signing would instead create a third class of
// persisted artifact (cc-utils-signed, plus OCM-v2-signed-correct and
// -buggy variants) that could not be fixed without invalidating signatures
// already out there. The known fidelity gap in the stored form (the store read
// collapses an integral float 1.0 to 1 before normalisation) is one such
// incompatibility signing would bake in, not the whole rationale.
//
// The v4 alphas (v4alpha1, v4alpha2 and the jsonNormalisation/v3 alias) are NOT
// in this set: they remain signable for backward compatibility. They are
// discouraged though — v4alpha1 drops label.version and v4alpha2 leaves
// reference labels unfiltered — so signing with any non-default algorithm emits
// a warning recommending the current default (v4alpha3, see
// DefaultNormalisationAlgorithm).
var ErrSigningUnsupported = errors.New("normalisation algorithm is supported for verification only, not signing")

// fipsEnforced reports whether the Go Cryptographic Module enforces FIPS 140-3
// mode (GODEBUG=fips140=only). It is a variable so tests can exercise both modes.
var fipsEnforced = fips140.Enforced

// DigestHashAlgorithmsEnforced reports whether reference and resource digests
// must use SHA-256 or SHA-512 (see ValidateDigestHashAlgorithms). This is the
// case with GODEBUG=fips140=only, where a signature must not rest on a
// non-approved hash. In the default fips140=on mode and outside FIPS mode,
// callers should log the violation instead, so existing component versions
// with MD5 or SHA-1 digests keep working.
func DigestHashAlgorithmsEnforced() bool {
	return fipsEnforced()
}

// VerifyDigestMatchesDescriptor ensures that a descriptor matches a digest
// provided by a signature. This validates descriptor integrity against the
// signature’s claimed digest.
//
// Steps:
// The digest is passed to algorithms.Verify with its recorded normalisation
// algorithm unchanged: a legacy jsonNormalisation/v3 digest is verified the
// way OCM v1 computed it (it keeps null label values), which v4alpha2 alone
// does not reproduce. See algorithms.Verify.
func VerifyDigestMatchesDescriptor(
	ctx context.Context,
	desc *descruntime.Descriptor,
	signature descruntime.Signature,
	logger *slog.Logger,
) error {
	if signature.Digest.NormalisationAlgorithm == LegacyNormalisationAlgo {
		logger.DebugContext(ctx, "legacy normalisation algorithm detected, verifying with the legacy v3 form",
			"legacy", LegacyNormalisationAlgo)
	}
	return algorithms.Verify(desc, signature.Digest)
}

// VerifySignatureDigest verifies that a descriptor matches the digest of one of
// its signatures, like VerifyDigestMatchesDescriptor. Use it on signature
// verification paths only; reference digest checks call
// VerifyDigestMatchesDescriptor directly.
//
// On a digest mismatch it adds a hint for a known OCM v1 interop limit: every
// OCM v1 signing run rewrites the component reference digests with its own hash
// and normalisation algorithms, and OCM v1 recomputes them on verification. OCM
// v2 verifies the stored reference digests, so of several OCM v1 signatures
// with different algorithms only the one created last can be verified. The hint
// is only added when the descriptor matches that pattern: another signature
// uses the algorithms of all stored reference digests, its digest matches the
// descriptor, and the failing signature uses other algorithms. The mismatch
// stays an error either way.
func VerifySignatureDigest(
	ctx context.Context,
	desc *descruntime.Descriptor,
	signature descruntime.Signature,
	logger *slog.Logger,
) error {
	err := VerifyDigestMatchesDescriptor(ctx, desc, signature, logger)
	if !errors.Is(err, algorithms.ErrDigestMismatch) {
		return err
	}
	if other := rewrittenReferencesSignature(desc, signature); other != nil {
		return fmt.Errorf("%w (possible cause: if this component version was signed several times with OCM v1 "+
			"using different hash or normalisation algorithms, OCM v1 rewrote its component reference digests "+
			"with the algorithms of signature %q (%s/%s) when creating it; OCM v1 recomputes reference digests "+
			"during verification, OCM v2 verifies the stored ones, so signature %q (%s/%s) cannot be verified "+
			"by OCM v2)",
			err, other.Name, other.Digest.NormalisationAlgorithm, other.Digest.HashAlgorithm,
			signature.Name, signature.Digest.NormalisationAlgorithm, signature.Digest.HashAlgorithm)
	}
	return err
}

// rewrittenReferencesSignature returns another signature of desc whose
// algorithms match all stored component reference digests and whose digest
// matches desc, provided the given signature uses other algorithms than those
// reference digests. It returns nil otherwise.
func rewrittenReferencesSignature(desc *descruntime.Descriptor, signature descruntime.Signature) *descruntime.Signature {
	refs := desc.Component.References
	if len(refs) == 0 {
		return nil
	}
	refsMatch := func(d descruntime.Digest) bool {
		for _, r := range refs {
			if !sameDigestAlgorithms(r.Digest, d) {
				return false
			}
		}
		return true
	}
	if refsMatch(signature.Digest) {
		return nil
	}
	for i := range desc.Signatures {
		other := &desc.Signatures[i]
		if other.Name == signature.Name || sameDigestAlgorithms(other.Digest, signature.Digest) || !refsMatch(other.Digest) {
			continue
		}
		if algorithms.Verify(desc, other.Digest) == nil {
			return other
		}
	}
	return nil
}

// sameDigestAlgorithms reports whether two digests use the same normalisation
// (aliases resolved) and hash algorithm (SHA-256 and sha256 are equal).
func sameDigestAlgorithms(a, b descruntime.Digest) bool {
	na, _ := algorithms.Resolve(a.NormalisationAlgorithm)
	nb, _ := algorithms.Resolve(b.NormalisationAlgorithm)
	hash := func(h string) string { return strings.ToUpper(strings.ReplaceAll(h, "-", "")) }
	return na == nb && hash(a.HashAlgorithm) == hash(b.HashAlgorithm)
}

// SignableNormalisationAlgorithms returns the normalisation algorithms a
// signing entry point offers for new signatures, DefaultNormalisationAlgorithm
// first. It excludes the verify-only jsonNormalisation/v1 and the legacy
// jsonNormalisation/v3 alias (sign with v4alpha2 instead).
func SignableNormalisationAlgorithms() []string {
	return []string{DefaultNormalisationAlgorithm, v4alpha2.Algorithm, v4alpha1.Algorithm}
}

// EnsureSignable is the signing boundary for the verify-only policy: it reports
// whether a fresh signature may be minted with the given normalisation
// algorithm. jsonNormalisation/v1 is rejected (see ErrSigningUnsupported);
// everything else is allowed, including the v4 alphas (they stay signable for
// backward compatibility — callers should warn on non-default choices, see
// WarnDiscouragedNormalisation). Digest computation and verification do not call
// this, so verifying existing v1 signatures, pins and reference digests keeps
// working. Any signing entry point must call it before producing a signature.
func EnsureSignable(normalisationAlgorithm string) error {
	if algorithms.VerifyOnly(normalisationAlgorithm) {
		return fmt.Errorf("%w: %q", ErrSigningUnsupported, normalisationAlgorithm)
	}
	return nil
}

// WarnDiscouragedNormalisation logs a warning when a new signature is minted
// with a normalisation algorithm other than DefaultNormalisationAlgorithm. The
// v4 alphas stay signable for backward compatibility, but v4alpha1 does not
// cover label.version and v4alpha2 (= legacy v3) leaves reference labels
// unfiltered. It is a no-op for the default algorithm.
func WarnDiscouragedNormalisation(ctx context.Context, logger *slog.Logger, normalisationAlgorithm string) {
	if normalisationAlgorithm == DefaultNormalisationAlgorithm {
		return
	}
	logger.WarnContext(ctx, "signing with an older normalisation algorithm",
		"algorithm", normalisationAlgorithm, "recommended", DefaultNormalisationAlgorithm, "migration", algorithms.MigrationGuide)
}

// GenerateDigest computes a new digest for a descriptor with the given
// normalisation and hashing algorithms.
//
// Steps:
//  1. Resolve the normalisation algorithm (legacy → v4alpha2 if required).
//  2. Normalise the descriptor.
//  3. Select the requested hash algorithm.
//  4. Hash the normalised descriptor.
//  5. Encode the digest as lowercase hex.
//
// Returns a Digest object embedding algorithm identifiers and the hex digest.
//
// Fails if:
//   - Normalisation fails,
//   - The hash algorithm is unsupported,
//   - Hashing fails.
func GenerateDigest(
	ctx context.Context,
	desc *descruntime.Descriptor,
	logger *slog.Logger,
	normalisationAlgorithm string,
	hashAlgorithm string,
) (*descruntime.Digest, error) {
	normalisationAlgorithm = ensureNormalisationAlgo(ctx, normalisationAlgorithm, logger)
	return algorithms.Digest(desc, normalisationAlgorithm, hashAlgorithm)
}

// IsSafelyDigestible validates that a component’s references and resources
// contain consistent digests according to OCM rules:
//
//   - Component references: every reference must define HashAlgorithm,
//     NormalisationAlgorithm, and Value. Missing values are invalid.
//   - Resources with access: if a resource has an Access type other than "None",
//     it must also have a complete digest.
//   - Resources without access: they must not carry a digest (enforced to prevent
//     meaningless digest claims).
//   - Digest algorithms (GODEBUG=fips140=only, see DigestHashAlgorithmsEnforced):
//     reference and resource digests must use SHA-256 or SHA-512. The signature
//     covers resources and references only through these digests, so a weak
//     hash such as MD5 or SHA-1 would let content be swapped under a valid
//     signature. Resources explicitly excluded from the signature
//     (NO-DIGEST / EXCLUDE-FROM-SIGNATURE) are exempt.
//
// Returns nil if all rules are satisfied, otherwise returns the first violation.
func IsSafelyDigestible(cd *descruntime.Component) error {
	if DigestHashAlgorithmsEnforced() {
		if err := ValidateDigestHashAlgorithms(cd); err != nil {
			return err
		}
	}

	for _, reference := range cd.References {
		if reference.Digest.HashAlgorithm == "" ||
			reference.Digest.NormalisationAlgorithm == "" ||
			reference.Digest.Value == "" {
			return fmt.Errorf("missing digest in componentReference for %s:%s", reference.Name, reference.Version)
		}
	}

	for _, res := range cd.Resources {
		if hasUsableAccess(res) {
			if res.Digest == nil ||
				res.Digest.HashAlgorithm == "" ||
				res.Digest.NormalisationAlgorithm == "" ||
				res.Digest.Value == "" {
				return fmt.Errorf("missing digest in resource for %s:%s", res.Name, res.Version)
			}
		} else if res.Digest != nil {
			return fmt.Errorf("digest for resource with empty access not allowed %s:%s", res.Name, res.Version)
		}
	}
	return nil
}

// ValidateDigestHashAlgorithms checks that every set resource and component
// reference digest uses SHA-256 or SHA-512, and returns an error wrapping
// ErrUnsupportedDigestHash for the first one that does not. It checks
// regardless of FIPS mode; use DigestHashAlgorithmsEnforced to decide whether a
// violation is fatal or only logged. Empty digests and resources excluded from
// the signature are not its concern; IsSafelyDigestible covers completeness.
func ValidateDigestHashAlgorithms(cd *descruntime.Component) error {
	for _, reference := range cd.References {
		if alg := reference.Digest.HashAlgorithm; alg != "" && !isApprovedDigestHash(alg) {
			return fmt.Errorf("%w %q in componentReference for %s:%s (use SHA-256 or SHA-512)",
				ErrUnsupportedDigestHash, alg, reference.Name, reference.Version)
		}
	}
	for _, res := range cd.Resources {
		if res.Digest == nil || res.Digest.HashAlgorithm == "" || isExcludedFromSignature(res.Digest) {
			continue
		}
		if !isApprovedDigestHash(res.Digest.HashAlgorithm) {
			return fmt.Errorf("%w %q in resource for %s:%s (use SHA-256 or SHA-512)",
				ErrUnsupportedDigestHash, res.Digest.HashAlgorithm, res.Name, res.Version)
		}
	}
	return nil
}

// isApprovedDigestHash reports whether a reference or resource digest uses
// SHA-256 or SHA-512. OCM writes "SHA-256"/"SHA-512"; the legacy names
// "sha256"/"sha512" appear in older signed descriptors, which OCM v1
// reproduces when verifying them (LegacyHashAlgorithm). They must keep
// verifying, so the comparison ignores case and dashes.
func isApprovedDigestHash(name string) bool {
	_, err := algorithms.Hash(name)
	return err == nil
}

// isExcludedFromSignature reports whether a resource digest is the explicit
// marker that keeps the resource content out of the signature. It matches the
// marker the same way the repository's download verification does.
func isExcludedFromSignature(d *descruntime.Digest) bool {
	return strings.EqualFold(d.HashAlgorithm, descruntime.NoDigest) ||
		strings.EqualFold(d.NormalisationAlgorithm, descruntime.ExcludeFromSignature)
}

// hasUsableAccess checks if a resource has an access type other than "None".
func hasUsableAccess(res descruntime.Resource) bool {
	return res.Access != nil && res.Access.GetType().String() != AccessTypeNone
}

// ensureNormalisationAlgo resolves the normalisation algorithm a new digest is
// computed and recorded with. If the provided value is the legacy v3
// algorithm, it returns v4alpha2.Algorithm instead. Verification does not use
// it: see VerifyDigestMatchesDescriptor. The resolution
// is logged at debug level only: callers that trust a digest already warn about
// any non-default algorithm (see algorithms.LogNonDefaultDigest).
func ensureNormalisationAlgo(ctx context.Context, algo string, logger *slog.Logger) string {
	resolved, aliased := algorithms.Resolve(algo)
	if aliased {
		logger.DebugContext(ctx,
			"legacy normalisation algorithm detected, using v4alpha2",
			"legacy", algo,
			"new", resolved,
		)
	}
	return resolved
}

// GetSupportedHash looks up a crypto.Hash from its string identifier,
// see algorithms.Hash.
func GetSupportedHash(name string) (crypto.Hash, error) {
	return algorithms.Hash(name)
}
