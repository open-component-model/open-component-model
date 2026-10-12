package repository

import (
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"regexp"
	"strings"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// StoredComponentDescriptorReader is an optional interface of a [ComponentVersionRepository]
// that exposes the component descriptor exactly as it is stored.
type StoredComponentDescriptorReader interface {
	// GetStoredComponentDescriptor returns the stored component descriptor bytes of the
	// component version and the descriptor decoded from exactly these bytes, so that
	// a check against raw cannot be bypassed by a concurrent update of the version.
	// Implementations that wrap a repository without this capability return an error
	// matching [errors.ErrUnsupported].
	GetStoredComponentDescriptor(ctx context.Context, component, version string) (desc *descriptor.Descriptor, raw []byte, err error)
}

// digestPinPattern is the OCI digest grammar "[<normalisation>+]<hash>:<hex>",
// with "/" of the normalisation algorithm written as ".".
var digestPinPattern = regexp.MustCompile(`^(?:([A-Za-z][A-Za-z0-9]*(?:[-_.][A-Za-z][A-Za-z0-9]*)*)\+)?([A-Za-z][A-Za-z0-9]*):([0-9A-Fa-f]+)$`)

// digestPinHashes maps the hash names of a digest pin to the hash they select.
var digestPinHashes = map[string]struct {
	algorithm crypto.Hash
	new       func() hash.Hash
}{
	"sha256": {crypto.SHA256, sha256.New},
	"sha512": {crypto.SHA512, sha512.New},
}

// ParseDigestPin parses the digest of a pinned component reference, "[<normalisation>+]<hash>:<hex>".
//
// Without a normalisation the pin is raw: the hash of the component descriptor bytes as
// stored in the repository, and the returned digest has an empty NormalisationAlgorithm.
// With a normalisation, e.g. "jsonNormalisation.v4alpha1+sha256:<hex>", the pin is the
// hash of the descriptor normalised with that algorithm ("/" written as "."), i.e. the
// digest OCM signatures sign. The hash is sha256 or sha512; hash and hex are case-insensitive.
// Whether the normalisation algorithm exists is checked when the pin is verified.
func ParseDigestPin(pin string) (*descriptor.Digest, error) {
	match := digestPinPattern.FindStringSubmatch(pin)
	if match == nil {
		return nil, fmt.Errorf("invalid digest pin %q: expected [<normalisation>+]<hash>:<hex>", pin)
	}
	normalisationAlgorithm, hashName, value := match[1], strings.ToLower(match[2]), strings.ToLower(match[3])
	h, ok := digestPinHashes[hashName]
	if !ok {
		return nil, fmt.Errorf("invalid digest pin %q: unsupported hash %q, use sha256 or sha512", pin, match[2])
	}
	if size := h.algorithm.Size(); len(value) != 2*size {
		return nil, fmt.Errorf("invalid digest pin %q: %s digest must have %d hex characters, got %d", pin, hashName, 2*size, len(value))
	}
	return &descriptor.Digest{
		HashAlgorithm:          h.algorithm.String(),
		NormalisationAlgorithm: strings.ReplaceAll(normalisationAlgorithm, ".", "/"),
		Value:                  value,
	}, nil
}

// FormatDigestPin formats d in the notation [ParseDigestPin] accepts.
func FormatDigestPin(d *descriptor.Digest) string {
	var sb strings.Builder
	if d.NormalisationAlgorithm != "" {
		sb.WriteString(strings.ReplaceAll(d.NormalisationAlgorithm, "/", "."))
		sb.WriteByte('+')
	}
	sb.WriteString(strings.ToLower(strings.ReplaceAll(d.HashAlgorithm, "-", "")))
	sb.WriteByte(':')
	sb.WriteString(d.Value)
	return sb.String()
}

// GetPinnedComponentVersion returns the component version from repo, verified against pin
// (see [ParseDigestPin]). Without a pin it is [ComponentVersionRepository.GetComponentVersion].
//
// A raw pin is checked against the stored descriptor bytes, and the returned descriptor is
// decoded from these bytes, so it requires repo to implement [StoredComponentDescriptorReader].
// A normalized pin is checked against the normalisation of the fetched descriptor.
func GetPinnedComponentVersion(ctx context.Context, repo ComponentVersionRepository, component, version string, pin *descriptor.Digest) (*descriptor.Descriptor, error) {
	if pin == nil {
		return repo.GetComponentVersion(ctx, component, version)
	}
	h, ok := digestPinHashes[strings.ToLower(strings.ReplaceAll(pin.HashAlgorithm, "-", ""))]
	if !ok {
		return nil, fmt.Errorf("unsupported hash algorithm %q in digest pin: use sha256 or sha512", pin.HashAlgorithm)
	}

	var (
		desc    *descriptor.Descriptor
		content []byte
		err     error
	)
	if pin.NormalisationAlgorithm == "" {
		reader, ok := repo.(StoredComponentDescriptorReader)
		if ok {
			desc, content, err = reader.GetStoredComponentDescriptor(ctx, component, version)
		}
		if !ok || errors.Is(err, errors.ErrUnsupported) {
			return nil, fmt.Errorf("repository of component version %s:%s does not expose stored descriptors, "+
				"so the raw digest pin %s cannot be verified: pin the normalized digest (%s) instead",
				component, version, FormatDigestPin(pin),
				FormatDigestPin(&descriptor.Digest{NormalisationAlgorithm: v4alpha1.Algorithm, HashAlgorithm: pin.HashAlgorithm, Value: "<hex>"}))
		}
	} else {
		desc, err = repo.GetComponentVersion(ctx, component, version)
		if err == nil {
			content, err = normalisation.Normalise(desc, pin.NormalisationAlgorithm)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("getting component version %s:%s pinned to %s failed: %w", component, version, FormatDigestPin(pin), err)
	}

	hasher := h.new()
	hasher.Write(content)
	resolved := *pin
	resolved.Value = hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(resolved.Value, pin.Value) {
		return nil, fmt.Errorf("component version %s:%s does not match pinned digest %s: resolved digest is %s",
			component, version, FormatDigestPin(pin), FormatDigestPin(&resolved))
	}
	return desc, nil
}
