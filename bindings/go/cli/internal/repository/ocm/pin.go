package ocm

import (
	"context"
	"crypto"
	"fmt"
	"log/slog"
	"strings"

	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/signing"
)

// pinHashes maps the algorithm of a component reference digest ("<algorithm>:<hex>")
// to the hash used for the component version digest.
var pinHashes = map[string]crypto.Hash{
	"sha256": crypto.SHA256,
	"sha512": crypto.SHA512,
}

// ValidateDigestPin rejects a digest-pinned component reference without a version.
// A pin identifies exactly one component version, so it cannot be combined with
// version listing or latest-version resolution.
func ValidateDigestPin(ref *compref.Ref) error {
	if ref.Digest != "" && ref.Version == "" {
		return fmt.Errorf("component reference %q is pinned to digest %q but has no version: a digest-pinned reference must name the version", ref.String(), ref.Digest)
	}
	return nil
}

// PinnedDigest returns the component version digest the reference pins, or nil if it
// carries no digest.
//
// The pin is the OCM component version digest: the hash of the descriptor normalised
// with jsonNormalisation/v4alpha1, the same value OCM signatures sign. It does not depend
// on access information, so it stays the same when the component version is transferred.
func PinnedDigest(ref *compref.Ref) (*descruntime.Digest, error) {
	if ref.Digest == "" {
		return nil, nil
	}
	if err := ValidateDigestPin(ref); err != nil {
		return nil, err
	}
	algorithm, value, _ := strings.Cut(ref.Digest, ":")
	hash, ok := pinHashes[strings.ToLower(algorithm)]
	if !ok {
		return nil, fmt.Errorf("unsupported digest algorithm %q in component reference %q: use sha256 or sha512", algorithm, ref.String())
	}
	return &descruntime.Digest{
		HashAlgorithm:          hash.String(),
		NormalisationAlgorithm: v4alpha1.Algorithm,
		Value:                  strings.ToLower(value),
	}, nil
}

// VerifyDigestPin fails unless desc is the component version the digest of ref pins
// (see PinnedDigest). A reference without a digest passes.
func VerifyDigestPin(ctx context.Context, ref *compref.Ref, desc *descruntime.Descriptor) error {
	pinned, err := PinnedDigest(ref)
	if err != nil || pinned == nil {
		return err
	}
	actual, err := signing.GenerateDigest(ctx, desc, slog.Default(), pinned.NormalisationAlgorithm, pinned.HashAlgorithm)
	if err != nil {
		return fmt.Errorf("computing digest of component version %s:%s failed: %w", desc.Component.Name, desc.Component.Version, err)
	}
	if actual.Value != pinned.Value {
		algorithm, _, _ := strings.Cut(ref.Digest, ":")
		return fmt.Errorf("component version %s:%s does not match pinned digest %s: resolved digest is %s:%s",
			desc.Component.Name, desc.Component.Version, ref.Digest, strings.ToLower(algorithm), actual.Value)
	}
	return nil
}
