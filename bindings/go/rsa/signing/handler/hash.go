package handler

import (
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

var (
	ErrMissingHashAlg     = errors.New("missing hash algorithm")
	ErrMissingDigestValue = errors.New("missing digest value")
)

// parseDigest extracts hash function and raw digest bytes from a descriptor digest.
func parseDigest(d descruntime.Digest) (crypto.Hash, []byte, error) {
	if d.HashAlgorithm == "" {
		return 0, nil, ErrMissingHashAlg
	}
	if d.Value == "" {
		return 0, nil, ErrMissingDigestValue
	}
	b, err := hex.DecodeString(d.Value)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid hex digest: %w", err)
	}
	h, err := hashFromString(d.HashAlgorithm)
	if err != nil {
		return 0, nil, err
	}
	return h, b, nil
}

// hashFromString maps common names to crypto.Hash.
func hashFromString(hashAlgorithm string) (crypto.Hash, error) {
	// Fallback to crypto.Hash.String() values.
	switch hashAlgorithm {
	case crypto.SHA256.String():
		return crypto.SHA256, nil
	case crypto.SHA384.String():
		return crypto.SHA384, nil
	case crypto.SHA512.String():
		return crypto.SHA512, nil
	}
	return 0, fmt.Errorf("unsupported hash algorithm %q", hashAlgorithm)
}
