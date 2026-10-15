package signing

import (
	"bytes"
	"context"
	"crypto"
	"crypto/fips140"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	// LegacyNormalisationAlgo identifies the deprecated v3 JSON normalisation algorithm.
	// It is replaced by v4alpha1.Algorithm. Calls using this value are transparently
	// mapped to v4alpha1 with a warning.
	LegacyNormalisationAlgo = "jsonNormalisation/v3"
	// AccessTypeNone is the access type for resources without access.
	// It is used to prevent meaningless digest claims.
	AccessTypeNone = "None"
)

// ErrUnsupportedDigestHash is returned when a resource or component reference
// digest uses a hash algorithm other than SHA-256 or SHA-512.
var ErrUnsupportedDigestHash = errors.New("unsupported digest hash algorithm")

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
//  1. Resolve the normalisation algorithm (legacy → v4alpha1 if required).
//  2. Normalise the descriptor with that algorithm.
//  3. Select the hash algorithm from supported list (SHA256, SHA512).
//  4. Hash the normalised descriptor.
//  5. Decode the digest value from the signature.
//  6. Compare the freshly computed digest against the signature digest.
func VerifyDigestMatchesDescriptor(
	ctx context.Context,
	desc *descruntime.Descriptor,
	signature descruntime.Signature,
	logger *slog.Logger,
) error {
	signature.Digest.NormalisationAlgorithm = ensureNormalisationAlgo(ctx, signature.Digest.NormalisationAlgorithm, logger)

	normalised, err := normalisation.Normalise(desc, signature.Digest.NormalisationAlgorithm)
	if err != nil {
		return fmt.Errorf("normalising component version failed: %w", err)
	}

	hash, err := getSupportedHash(signature.Digest.HashAlgorithm)
	if err != nil {
		return err
	}

	h := hash.New()
	if _, err := h.Write(normalised); err != nil {
		return fmt.Errorf("hashing component version failed: %w", err)
	}
	freshDigest := h.Sum(nil)

	digestFromSignature, err := hex.DecodeString(signature.Digest.Value)
	if err != nil {
		return fmt.Errorf("decoding digest from signature failed: %w", err)
	}

	if !bytes.Equal(freshDigest, digestFromSignature) {
		return fmt.Errorf("digest mismatch: descriptor %x vs signature %x", freshDigest, digestFromSignature)
	}
	return nil
}

// GenerateDigest computes a new digest for a descriptor with the given
// normalisation and hashing algorithms.
//
// Steps:
//  1. Resolve the normalisation algorithm (legacy → v4alpha1 if required).
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

	normalised, err := normalisation.Normalise(desc, normalisationAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("normalising component version failed: %w", err)
	}

	hash, err := getSupportedHash(hashAlgorithm)
	if err != nil {
		return nil, err
	}

	h := hash.New()
	if _, err := h.Write(normalised); err != nil {
		return nil, fmt.Errorf("hashing component version failed: %w", err)
	}
	freshDigest := h.Sum(nil)

	return &descruntime.Digest{
		HashAlgorithm:          hash.String(),
		NormalisationAlgorithm: normalisationAlgorithm,
		Value:                  hex.EncodeToString(freshDigest),
	}, nil
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
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "")) {
	case "SHA256", "SHA512":
		return true
	default:
		return false
	}
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

// ensureNormalisationAlgo resolves the effective normalisation algorithm.
// If the provided value is the legacy v3 algorithm, it logs a warning and
// returns v4alpha1.Algorithm instead. Otherwise, it returns the original value.
// this is to ensure compatibility with old ocm v1 style signatures.
func ensureNormalisationAlgo(ctx context.Context, algo string, logger *slog.Logger) string {
	if algo == LegacyNormalisationAlgo {
		logger.WarnContext(ctx,
			"legacy normalisation algorithm detected, using v4alpha1",
			"legacy", LegacyNormalisationAlgo,
			"new", v4alpha1.Algorithm,
		)
		return v4alpha1.Algorithm
	}
	return algo
}

// supportedHashes lists supported hashing algorithms keyed by their identifier
var supportedHashes = map[string]crypto.Hash{
	crypto.SHA256.String(): crypto.SHA256,
	crypto.SHA512.String(): crypto.SHA512,
}

// getSupportedHash looks up a crypto.Hash from its string identifier.
// Returns an error if the identifier is not in supportedHashes.
func getSupportedHash(name string) (crypto.Hash, error) {
	n := strings.ToUpper(name)
	h, ok := supportedHashes[n]
	if !ok {
		return 0, fmt.Errorf("unsupported hash algorithm %q (use: %q)", n, supportedHashes)
	}

	return h, nil
}
