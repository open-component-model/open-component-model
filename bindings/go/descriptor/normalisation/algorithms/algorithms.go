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
	// unfiltered, so it reproduces legacy v3 bytes exactly, except for
	// null-valued entries (see below). Mapping v3 to v4alpha1 (which drops
	// label.version) breaks verification of v3 signatures over versioned
	// labels; mapping it to v4alpha3 (which filters reference labels
	// signing-only) breaks verification of v3 signatures that carry reference
	// labels.
	//
	// One difference remains: legacy v3 keeps null-valued entries such as a
	// signed label with value: null, which v4alpha2 drops. v4alpha2 stays
	// byte-frozen so its recorded digests keep verifying; Verify checks a
	// digest recorded as LegacyV3 against v4alpha2.NormaliseLegacyV3 instead.
	LegacyV3 = "jsonNormalisation/v3"
	// LegacyV2 is the deprecated jsonNormalisation/v2 algorithm of legacy OCM
	// v1. It is neither computed nor verified: the spec only states that it
	// differs from v3 by the legacy extra-identity defaulting, which is not
	// reproduced here. Digest and Verify reject it with ErrUnsupportedNormalisation.
	LegacyV2 = "jsonNormalisation/v2"
)

var (
	// ErrUnsupportedHash is returned for a hash algorithm other than SHA-256 or SHA-512.
	ErrUnsupportedHash = errors.New("unsupported hash algorithm")
	// ErrUnsupportedNormalisation is returned for a normalisation algorithm that
	// is known but no longer supported. It wraps normalisation.ErrUnknownNormalisationAlgorithm.
	ErrUnsupportedNormalisation = fmt.Errorf("%w: unsupported", normalisation.ErrUnknownNormalisationAlgorithm)
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

// CheckSupported returns ErrUnsupportedNormalisation for an algorithm that is
// known but can neither be verified nor created (jsonNormalisation/v2).
func CheckSupported(normalisationAlgorithm string) error {
	if normalisationAlgorithm == LegacyV2 {
		return fmt.Errorf("%w: %q is deprecated and can neither be verified nor created; re-sign the component version with %q (see %s)",
			ErrUnsupportedNormalisation, LegacyV2, Default, MigrationGuide)
	}
	return nil
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
	if err := CheckSupported(normalisationAlgorithm); err != nil {
		return nil, err
	}
	normalisationAlgorithm, _ = Resolve(normalisationAlgorithm)
	sum, hash, err := sum(registered(normalisationAlgorithm), desc, hashAlgorithm)
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
//
// A digest recorded as LegacyV3 was minted by legacy OCM v1, which keeps
// null-valued entries such as a signed label with value: null. It is verified
// against v4alpha2.NormaliseLegacyV3, and, failing that, against the v4alpha2
// bytes it has always been verified against, so no digest that verified before
// stops verifying. The two differ only for a descriptor with a null-valued
// entry. Digest never records LegacyV3 (it labels such digests v4alpha2), so
// minting is unaffected.
//
// A digest recorded as jsonNormalisation/v1 is verified as cc-utils computes it
// first and, failing that, as the OCM v1 CLI computes it (see
// jsonv1.NormaliseOCMv1), so signatures of both tools verify. The OCM v1
// fallback runs only when every number in the descriptor survives a float64
// round trip exactly (jsonv1.OCMv1Applicable): OCM v1 maps different numbers
// such as 2^53 and 2^53+1 to the same bytes, so trying it otherwise would let
// a changed number pass a cc-utils signature. An OCM v1 signature over such a
// number therefore fails with a re-sign error. Likewise the fallback is not
// tried when dropping unsigned provider labels makes the OCM v1 form equal to
// the cc-utils form of the descriptor without them
// (jsonv1.ErrAmbiguousProviderLabels): those bytes could be a cc-utils
// signature, and accepting them would let a provider label be injected. Nor is
// it tried when a signed value holds an object inside an array
// (jsonv1.ErrAmbiguousArrayObjects): OCM v1 rewrites such objects into the form
// cc-utils gives a differently shaped value. In every failure case the error
// wraps ErrDigestMismatch and points to re-signing.
func Verify(desc *descruntime.Descriptor, expected descruntime.Digest) error {
	if err := CheckSupported(expected.NormalisationAlgorithm); err != nil {
		return err
	}
	if expected.NormalisationAlgorithm == LegacyV3 {
		err := verify(v4alpha2.NormaliseLegacyV3, desc, expected)
		if !errors.Is(err, ErrDigestMismatch) {
			return err
		}
		if fallback := verify(registered(v4alpha2.Algorithm), desc, expected); fallback == nil {
			return nil
		}
		return err
	}
	if expected.NormalisationAlgorithm == jsonv1.Algorithm { //nolint:staticcheck // verify-only legacy algorithm
		err := verify(registered(jsonv1.Algorithm), desc, expected) //nolint:staticcheck // verify-only legacy algorithm
		if !errors.Is(err, ErrDigestMismatch) {
			return err
		}
		if gate := jsonv1.OCMv1Applicable(desc); gate != nil {
			if !errors.Is(gate, jsonv1.ErrInexactNumber) && !errors.Is(gate, jsonv1.ErrAmbiguousProviderLabels) &&
				!errors.Is(gate, jsonv1.ErrAmbiguousArrayObjects) {
				return fmt.Errorf("%w; checking the OCM v1 variant of %s failed: %w", err, jsonv1.Algorithm, gate) //nolint:staticcheck // see above
			}
			return fmt.Errorf("%w; %w, so the OCM v1 variant was not tried; if the signature was created with the OCM v1 CLI, re-sign the component version with %q (see %s)",
				err, gate, Default, MigrationGuide)
		}
		fallback := verify(jsonv1.NormaliseOCMv1, desc, expected)
		if fallback == nil {
			return nil
		}
		if !errors.Is(fallback, ErrDigestMismatch) {
			return fmt.Errorf("%w; computing the OCM v1 variant of %s failed: %w", err, jsonv1.Algorithm, fallback) //nolint:staticcheck // see above
		}
		return fmt.Errorf("%w; neither the cc-utils nor the OCM v1 variant of %s matches; a signature created with the OCM v1 CLI may use parts of the OCM v1 variant that are not reproduced here: re-sign the component version with %q (see %s)",
			err, jsonv1.Algorithm, Default, MigrationGuide) //nolint:staticcheck // see above
	}
	return verify(registered(expected.NormalisationAlgorithm), desc, expected)
}

func verify(normalise normaliseFunc, desc *descruntime.Descriptor, expected descruntime.Digest) error {
	fresh, _, err := sum(normalise, desc, expected.HashAlgorithm)
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(fresh); !strings.EqualFold(got, expected.Value) {
		return fmt.Errorf("%w: descriptor %s vs expected %s", ErrDigestMismatch, got, expected.Value)
	}
	return nil
}

type normaliseFunc func(*descruntime.Descriptor) ([]byte, error)

// registered returns the registered normalisation algorithm, alias resolved.
func registered(normalisationAlgorithm string) normaliseFunc {
	normalisationAlgorithm, _ = Resolve(normalisationAlgorithm)
	return func(desc *descruntime.Descriptor) ([]byte, error) {
		return normalisation.Normalise(desc, normalisationAlgorithm)
	}
}

func sum(normalise normaliseFunc, desc *descruntime.Descriptor, hashAlgorithm string) ([]byte, crypto.Hash, error) {
	normalised, err := normalise(desc)
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
