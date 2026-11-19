// Package algorithms is the single place that resolves the normalisation and
// hash algorithms of a component descriptor digest, and computes and verifies
// such digests.
//
// Importing it registers every normalisation algorithm that must stay
// verifiable (jsonNormalisation/v1, v4alpha1, v4alpha2, v4alpha3), so a caller
// never depends on which json/* packages happen to be imported elsewhere in
// the binary. It also owns the legacy jsonNormalisation/v3 alias and the
// accepted hash names, so signing, the constructor and repository pins agree
// on what a digest means.
package algorithms

import (
	"crypto"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	_ "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	jsonv1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha3"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	// Default is the normalisation for new digests: new signatures and
	// component reference digests minted by the constructor.
	Default = v4alpha3.Algorithm
	// LegacyV3 is the deprecated jsonNormalisation/v3 algorithm, read as
	// v4alpha2 (see Resolve).
	//
	// It maps to v4alpha2, not to Default (v4alpha3). v4alpha2 is the faithful
	// v3 re-implementation: it keeps label.version and emits reference labels
	// unfiltered, so it reproduces legacy v3 bytes exactly. Mapping v3 to
	// v4alpha1 (which drops label.version) breaks verification of v3 signatures
	// over versioned labels; mapping it to v4alpha3 (which filters reference
	// labels signing-only) breaks verification of v3 signatures that carry
	// reference labels.
	LegacyV3 = "jsonNormalisation/v3"
)

var (
	// ErrUnsupportedHash is returned for a hash algorithm other than SHA-256 or SHA-512.
	ErrUnsupportedHash = errors.New("unsupported hash algorithm")
	// ErrDigestMismatch is returned by Verify when the descriptor does not match the digest.
	ErrDigestMismatch = errors.New("digest mismatch")
)

// Resolve returns the registered normalisation algorithm an algorithm name is
// computed with, and whether the name was an alias. Only LegacyV3 is an alias.
func Resolve(normalisationAlgorithm string) (string, bool) {
	if normalisationAlgorithm == LegacyV3 {
		return v4alpha2.Algorithm, true
	}
	return normalisationAlgorithm, false
}

// VerifyOnly reports whether normalisationAlgorithm may only verify existing
// digests: no new signature or component reference digest may record it.
func VerifyOnly(normalisationAlgorithm string) bool {
	return normalisationAlgorithm == jsonv1.Algorithm //nolint:staticcheck // the deprecated v1 is the verify-only algorithm
}

// Hash resolves a hash algorithm name to SHA-256 or SHA-512. Case is ignored
// and the dash is optional: OCM writes "SHA-256"/"SHA-512", older descriptors
// carry "sha256"/"sha512", and both must keep verifying.
func Hash(name string) (crypto.Hash, error) {
	switch strings.ToUpper(name) {
	case "SHA-256", "SHA256":
		return crypto.SHA256, nil
	case "SHA-512", "SHA512":
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("%w %q: only SHA-256 and SHA-512 are supported", ErrUnsupportedHash, name)
	}
}

// Digest normalises desc with normalisationAlgorithm and hashes it with
// hashAlgorithm. The returned digest carries the resolved normalisation (an
// alias is labelled with the algorithm it resolves to) and the canonical hash
// name.
func Digest(desc *descruntime.Descriptor, normalisationAlgorithm, hashAlgorithm string) (*descruntime.Digest, error) {
	normalisationAlgorithm, _ = Resolve(normalisationAlgorithm)
	sum, hash, err := sum(desc, normalisationAlgorithm, hashAlgorithm)
	if err != nil {
		return nil, err
	}
	return &descruntime.Digest{
		HashAlgorithm:          hash.String(),
		NormalisationAlgorithm: normalisationAlgorithm,
		Value:                  hex.EncodeToString(sum),
	}, nil
}

// Verify recomputes the digest of desc under the normalisation and hash
// algorithms recorded in expected and compares it with expected.Value (hex,
// case-insensitive). A mismatch, including a value that is not hex, wraps
// ErrDigestMismatch.
func Verify(desc *descruntime.Descriptor, expected descruntime.Digest) error {
	normalisationAlgorithm, _ := Resolve(expected.NormalisationAlgorithm)
	fresh, _, err := sum(desc, normalisationAlgorithm, expected.HashAlgorithm)
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(fresh); !strings.EqualFold(got, expected.Value) {
		return fmt.Errorf("%w: descriptor %s vs expected %s", ErrDigestMismatch, got, expected.Value)
	}
	return nil
}

func sum(desc *descruntime.Descriptor, normalisationAlgorithm, hashAlgorithm string) ([]byte, crypto.Hash, error) {
	normalised, err := normalisation.Normalise(desc, normalisationAlgorithm)
	if err != nil {
		return nil, 0, fmt.Errorf("normalising component version failed: %w", err)
	}
	hash, err := Hash(hashAlgorithm)
	if err != nil {
		return nil, 0, err
	}
	// Use the implementations directly instead of hash.New: crypto.Hash.New
	// panics unless some package in the binary links the implementation.
	h := sha256.New()
	if hash == crypto.SHA512 {
		h = sha512.New()
	}
	if _, err := h.Write(normalised); err != nil {
		return nil, 0, fmt.Errorf("hashing component version failed: %w", err)
	}
	return h.Sum(nil), hash, nil
}
