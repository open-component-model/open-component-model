package digest

import (
	"crypto"
	"errors"
	"fmt"
	"slices"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

var (
	// ErrNormalisationMismatch is returned when the rule does not accept the recorded normalisation.
	ErrNormalisationMismatch = errors.New("normalisation algorithm mismatch")
	// errIncomplete is returned for a recorded digest with some but not all fields set. Its
	// missing fields are never inferred: they are part of the signed descriptor, so filling
	// them in breaks a signature or a reference digest that already covers the partial digest.
	errIncomplete = errors.New("a digest must be complete or absent")
)

// Computed is the digest a binding resolved or hashed for a resource, for example the
// manifest digest of an OCI artifact or the hash of downloaded bytes.
type Computed struct {
	Hash crypto.Hash
	// Value is the hex encoded digest.
	Value string
}

// Rule tells [Check] which normalisation labels describe what a binding computed.
type Rule struct {
	// Normalisation is written on generated digests and accepted on recorded ones.
	Normalisation string
	// Accepted lists further labels accepted on recorded digests, but never written.
	Accepted []string
}

func (r Rule) accepts(normalisation string) bool {
	return normalisation == r.Normalisation || slices.Contains(r.Accepted, normalisation)
}

// Result is the outcome of [Check]. Digest is the digest the binding records; it is a copy
// that never aliases the recorded digest. Generated reports whether an absent digest was
// generated, allowing callers to preserve an existing digest object.
type Result struct {
	Digest    *descriptor.Digest
	Generated bool
}

// Check decides what happens to the recorded digest of a resource, given the digest the binding
// computed and the rule for that content. It writes nothing; the binding applies the result.
//
// A recorded digest is either absent, and generated from computed, or complete, and kept
// exactly as recorded, not even its label or hash spelling rewritten, because it may be signed.
// A partial digest and a complete digest whose label the rule does not accept are rejected.
// An excluded resource is kept as is and computed is not used, so callers may skip computing
// a digest when [IsExcluded] reports true.
func Check(recorded *descriptor.Digest, computed Computed, rule Rule) (Result, error) {
	switch {
	case IsExcluded(recorded):
		return Result{Digest: recorded.DeepCopy()}, nil
	case recorded == nil || (recorded.HashAlgorithm == "" && recorded.NormalisationAlgorithm == "" && recorded.Value == ""):
		generated, err := generate(computed, rule)
		if err != nil {
			return Result{}, err
		}
		return Result{Digest: generated, Generated: true}, nil
	case recorded.HashAlgorithm == "" || recorded.NormalisationAlgorithm == "" || recorded.Value == "":
		return Result{}, fmt.Errorf("%w: hashAlgorithm=%q, normalisationAlgorithm=%q, value=%q",
			errIncomplete, recorded.HashAlgorithm, recorded.NormalisationAlgorithm, recorded.Value)
	default:
		if !rule.accepts(recorded.NormalisationAlgorithm) {
			return Result{}, fmt.Errorf("%w: expected %s, got %s", ErrNormalisationMismatch, rule.Normalisation, recorded.NormalisationAlgorithm)
		}
		if err := matchHash(recorded, computed); err != nil {
			return Result{}, err
		}
		return Result{Digest: recorded.DeepCopy()}, nil
	}
}

func generate(computed Computed, rule Rule) (*descriptor.Digest, error) {
	hashAlgorithm, err := formatHashAlgorithm(computed.Hash)
	if err != nil {
		return nil, err
	}
	if computed.Value == "" {
		return nil, fmt.Errorf("computed digest has no value")
	}
	return &descriptor.Digest{HashAlgorithm: hashAlgorithm, NormalisationAlgorithm: rule.Normalisation, Value: computed.Value}, nil
}

// matchHash checks the hash algorithm and value of a complete recorded digest against computed.
func matchHash(recorded *descriptor.Digest, computed Computed) error {
	if recorded.Value != computed.Value {
		return fmt.Errorf("digest value mismatch: expected %s, got %s", recorded.Value, computed.Value)
	}
	hash, err := ParseHashAlgorithm(recorded.HashAlgorithm)
	if err != nil {
		return fmt.Errorf("hash algorithm mismatch: %w", err)
	}
	if hash != computed.Hash {
		return fmt.Errorf("hash algorithm mismatch: expected %s, got %s", recorded.HashAlgorithm, computed.Hash)
	}
	return nil
}
