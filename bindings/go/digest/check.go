package digest

import (
	"crypto"
	"errors"
	"fmt"
	"slices"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// ErrNormalisationMismatch is returned when the rule does not accept the recorded normalisation.
var ErrNormalisationMismatch = errors.New("normalisation algorithm mismatch")

// Computed is the digest a binding resolved or hashed for a resource, for example the
// manifest digest of an OCI artifact or the hash of downloaded bytes.
type Computed struct {
	Hash crypto.Hash
	// Value is the hex encoded digest.
	Value string
}

// Rule tells [Check] which normalisation labels describe what a binding computed.
type Rule struct {
	// Normalisation is written on new and completed digests and accepted on recorded ones.
	Normalisation string
	// Accepted lists further labels accepted on recorded digests, but never written.
	Accepted []string
}

func (r Rule) accepts(normalisation string) bool {
	return normalisation == r.Normalisation || slices.Contains(r.Accepted, normalisation)
}

// Mode selects how [Check] treats a complete recorded digest whose label the rule does not accept.
type Mode int

const (
	// Strict rejects it, as processing and uploading resources do.
	Strict Mode = iota
	// Lenient keeps it unverified, as packing local blobs does.
	Lenient
)

// Decision is what [Check] decided for the recorded digest.
type Decision string

const (
	// Generate means nothing was recorded and the digest was generated from the computed one.
	Generate Decision = "Generate"
	// Complete means the missing fields of an incomplete digest were filled in.
	Complete Decision = "Complete"
	// Keep means a complete digest was verified and is kept exactly as recorded.
	Keep Decision = "Keep"
	// KeepUnverified means a complete digest with a label the rule does not accept is kept
	// exactly as recorded without verification (lenient mode only).
	KeepUnverified Decision = "KeepUnverified"
	// Excluded means the resource is excluded from the signature and its marker is kept.
	Excluded Decision = "Excluded"
)

// Result is the outcome of [Check]. Digest is the digest the binding records; it is a copy
// that never aliases the recorded digest.
type Result struct {
	Digest   *descriptor.Digest
	Decision Decision
}

// Check decides what happens to the recorded digest of a resource, given the digest the binding
// computed and the rule for that content. It writes nothing; the binding applies the result.
//
// A complete recorded digest is never rewritten, not even its label or hash spelling, because it
// may be signed. An incomplete one cannot be signed, so it is completed in the spec spelling with
// the rule's normalisation. An excluded resource is kept as is and computed is not used, so
// callers may skip computing a digest when [IsExcluded] reports true.
func Check(recorded *descriptor.Digest, computed Computed, rule Rule, mode Mode) (Result, error) {
	switch {
	case IsExcluded(recorded):
		return Result{Digest: recorded.DeepCopy(), Decision: Excluded}, nil
	case recorded == nil || (recorded.HashAlgorithm == "" && recorded.NormalisationAlgorithm == "" && recorded.Value == ""):
		generated, err := generate(computed, rule)
		if err != nil {
			return Result{}, err
		}
		return Result{Digest: generated, Decision: Generate}, nil
	case recorded.HashAlgorithm != "" && recorded.NormalisationAlgorithm != "" && recorded.Value != "":
		if !rule.accepts(recorded.NormalisationAlgorithm) {
			if mode == Lenient {
				return Result{Digest: recorded.DeepCopy(), Decision: KeepUnverified}, nil
			}
			return Result{}, fmt.Errorf("%w: expected %s, got %s", ErrNormalisationMismatch, rule.Normalisation, recorded.NormalisationAlgorithm)
		}
		if err := matchHash(recorded, computed); err != nil {
			return Result{}, err
		}
		return Result{Digest: recorded.DeepCopy(), Decision: Keep}, nil
	default:
		if set := recorded.NormalisationAlgorithm; set != "" && !rule.accepts(set) {
			return Result{}, fmt.Errorf("%w: expected %s, got %s", ErrNormalisationMismatch, rule.Normalisation, set)
		}
		if err := matchHash(recorded, computed); err != nil {
			return Result{}, err
		}
		completed, err := generate(computed, rule)
		if err != nil {
			return Result{}, err
		}
		return Result{Digest: completed, Decision: Complete}, nil
	}
}

func generate(computed Computed, rule Rule) (*descriptor.Digest, error) {
	hashAlgorithm, err := FormatHashAlgorithm(computed.Hash)
	if err != nil {
		return nil, err
	}
	if computed.Value == "" {
		return nil, fmt.Errorf("computed digest has no value")
	}
	return &descriptor.Digest{HashAlgorithm: hashAlgorithm, NormalisationAlgorithm: rule.Normalisation, Value: computed.Value}, nil
}

// matchHash checks the hash algorithm and value that are set on recorded against computed.
func matchHash(recorded *descriptor.Digest, computed Computed) error {
	if recorded.Value != "" && recorded.Value != computed.Value {
		return fmt.Errorf("digest value mismatch: expected %s, got %s", recorded.Value, computed.Value)
	}
	if recorded.HashAlgorithm == "" {
		return nil
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
