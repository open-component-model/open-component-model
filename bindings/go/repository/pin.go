package repository

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/opencontainers/go-digest"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

// DigestPinnableRepository is an optional interface of a [ComponentVersionRepository] that stores
// every component version as a content-addressed artifact: an OCI image index or manifest whose
// digest transitively covers the component descriptor and all local blobs (OCI registries and CTF archives).
type DigestPinnableRepository interface {
	// PinComponentVersion returns a view of the repository that reads component from the artifact
	// with the OCI digest ("<algorithm>:<hex>") instead of resolving a version tag. Every read of
	// component through the view (descriptor, local resources and sources) is served from that artifact
	// and fails unless the artifact holds component, in version if version is not empty.
	// Adding a version of component through the view fails if its version tag exists and resolves
	// to another artifact, so the view does not overwrite a tag that moved away from the pin.
	// Implementations that wrap a repository without this capability return an error matching
	// [errors.ErrUnsupported].
	PinComponentVersion(component, version, digest string) (ComponentVersionRepository, error)
	// ResolveComponentVersionDigest returns the OCI digest ("<algorithm>:<hex>") of the artifact the
	// version tag of component currently resolves to, the digest [PinComponentVersion] accepts.
	// Implementations that wrap a repository without this capability return an error matching
	// [errors.ErrUnsupported].
	ResolveComponentVersionDigest(ctx context.Context, component, version string) (string, error)
}

// PinRepository returns repo pinned to the OCI digest pin ("<algorithm>:<hex>") of component, see
// [DigestPinnableRepository.PinComponentVersion], so that reads of component, e.g. local resources,
// are served from the pinned artifact instead of resolving the version tag. An empty pin returns
// repo unchanged. It fails if repo cannot be pinned.
func PinRepository(repo ComponentVersionRepository, component, version, pin string) (ComponentVersionRepository, error) {
	if pin == "" {
		return repo, nil
	}
	pinnable, ok := repo.(DigestPinnableRepository)
	if !ok {
		return nil, fmt.Errorf("repository %T cannot read component version %s:%s pinned to %s", repo, component, version, pin)
	}
	pinned, err := pinnable.PinComponentVersion(component, version, pin)
	if err != nil {
		return nil, fmt.Errorf("pinning component version %s:%s to %s failed: %w", component, version, pin, err)
	}
	return pinned, nil
}

// digestPinPattern is the OCI digest grammar "[<normalisation>+]<hash>:<hex>",
// with "/" of the normalisation algorithm written as ".".
var digestPinPattern = regexp.MustCompile(`^(?:([A-Za-z][A-Za-z0-9]*(?:[-_.][A-Za-z][A-Za-z0-9]*)*)\+)?([A-Za-z][A-Za-z0-9]*):([0-9A-Fa-f]+)$`)

// hashAlgorithmNames maps the algorithms [verifiableAlgorithm] returns to the hash algorithm names
// OCM descriptors use.
var hashAlgorithmNames = map[digest.Algorithm]string{
	digest.SHA256: crypto.SHA256.String(),
	digest.SHA512: crypto.SHA512.String(),
}

// ParseDigestPin parses the digest of a pinned component reference, "[<normalisation>+]<hash>:<hex>".
//
// Without a normalisation the pin is an OCI digest: the digest of the OCI image index or manifest
// the component version is stored as (what its version tag resolves to), and the returned digest has
// an empty NormalisationAlgorithm. As the root of the stored artifact graph, it covers the descriptor
// and all local blobs. With a normalisation, e.g. "jsonNormalisation.v4alpha3+sha256:<hex>", the pin
// is the hash of the descriptor normalised with that algorithm ("/" written as "."), i.e. the digest
// OCM signatures sign. The hash is sha256 or sha512; hash and hex are case-insensitive.
// Whether the normalisation algorithm exists is checked when the pin is verified.
func ParseDigestPin(pin string) (*descriptor.Digest, error) {
	match := digestPinPattern.FindStringSubmatch(pin)
	if match == nil {
		return nil, fmt.Errorf("invalid digest pin %q: expected [<normalisation>+]<hash>:<hex>", pin)
	}
	normalisationAlgorithm, hashName, value := match[1], strings.ToLower(match[2]), strings.ToLower(match[3])
	alg, err := verifiableAlgorithm(hashName)
	if err != nil {
		return nil, fmt.Errorf("invalid digest pin %q: unsupported hash %q, use sha256 or sha512", pin, match[2])
	}
	if size := alg.Size(); len(value) != 2*size {
		return nil, fmt.Errorf("invalid digest pin %q: %s digest must have %d hex characters, got %d", pin, hashName, 2*size, len(value))
	}
	return &descriptor.Digest{
		HashAlgorithm:          hashAlgorithmNames[alg],
		NormalisationAlgorithm: strings.ReplaceAll(normalisationAlgorithm, ".", "/"),
		Value:                  value,
	}, nil
}

// FormatDigestPin formats d in the notation [ParseDigestPin] accepts. For an OCI digest pin
// (no normalisation algorithm) this is the OCI digest, "<algorithm>:<hex>".
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

// GetPinnedComponentVersion returns the component version from repo, verified against pin (see
// [ParseDigestPin]), together with the repository to read the component version through afterwards.
//
//   - Without a pin it is [ComponentVersionRepository.GetComponentVersion], and repo is returned.
//   - An OCI digest pin requires repo to implement [DigestPinnableRepository]. The component version is
//     read from the artifact with that digest and the returned repository serves every further read of
//     component (e.g. local resources) from the same artifact. The version tag is never consulted, so
//     version may be empty; if set, the artifact must hold that version.
//   - A normalized pin needs version. The descriptor read by version is normalised and hashed,
//     and repo is returned.
func GetPinnedComponentVersion(ctx context.Context, repo ComponentVersionRepository, component, version string, pin *descriptor.Digest) (ComponentVersionRepository, *descriptor.Descriptor, error) {
	if pin == nil {
		desc, err := repo.GetComponentVersion(ctx, component, version)
		return repo, desc, err
	}
	if _, err := verifiableAlgorithm(pin.HashAlgorithm); err != nil {
		return nil, nil, fmt.Errorf("unsupported hash algorithm %q in digest pin: use sha256 or sha512", pin.HashAlgorithm)
	}

	if pin.NormalisationAlgorithm == "" {
		pinnable, ok := repo.(DigestPinnableRepository)
		var (
			pinned ComponentVersionRepository
			err    error
		)
		if ok {
			pinned, err = pinnable.PinComponentVersion(component, version, FormatDigestPin(pin))
		}
		if !ok || errors.Is(err, errors.ErrUnsupported) {
			return nil, nil, fmt.Errorf("repository of component %s does not store component versions content-addressed, "+
				"so the OCI digest pin %s cannot be used: pin the normalized digest (%s) instead",
				component, FormatDigestPin(pin),
				FormatDigestPin(&descriptor.Digest{NormalisationAlgorithm: signing.DefaultNormalisationAlgorithm, HashAlgorithm: pin.HashAlgorithm, Value: "<hex>"}))
		}
		if err != nil {
			return nil, nil, fmt.Errorf("pinning component %s to %s failed: %w", component, FormatDigestPin(pin), err)
		}
		desc, err := pinned.GetComponentVersion(ctx, component, version)
		if err != nil {
			return nil, nil, fmt.Errorf("getting component %s pinned to %s failed: %w", component, FormatDigestPin(pin), err)
		}
		return pinned, desc, nil
	}

	if version == "" {
		return nil, nil, fmt.Errorf("component %s is pinned to the normalized digest %s, which needs the version: name it in the reference",
			component, FormatDigestPin(pin))
	}
	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, nil, fmt.Errorf("getting component version %s:%s pinned to %s failed: %w", component, version, FormatDigestPin(pin), err)
	}
	if err := verifyNormalizedPin(ctx, desc, pin); err != nil {
		return nil, nil, err
	}
	return repo, desc, nil
}

// verifyNormalizedPin verifies desc against the normalized pin. It hashes like signature verification,
// which reads the legacy jsonNormalisation/v3 as jsonNormalisation/v4alpha2, so a pin taken from a
// v1 signature or component reference verifies as it does in transfer.
func verifyNormalizedPin(ctx context.Context, desc *descriptor.Descriptor, pin *descriptor.Digest) error {
	component, version := desc.Component.Name, desc.Component.Version
	resolved, err := signing.GenerateDigest(ctx, desc, slog.Default(), pin.NormalisationAlgorithm, pin.HashAlgorithm)
	if err != nil {
		return fmt.Errorf("getting component version %s:%s pinned to %s failed: %w", component, version, FormatDigestPin(pin), err)
	}
	if !strings.EqualFold(resolved.Value, pin.Value) {
		return fmt.Errorf("component version %s:%s does not match pinned digest %s: resolved digest is %s",
			component, version, FormatDigestPin(pin), FormatDigestPin(resolved))
	}
	return nil
}

// GetComponentVersionByArtifact is [GetPinnedComponentVersion] that, unless pin is an OCI digest
// pin, also binds the read to an artifact: if repo implements [DigestPinnableRepository], the
// version tag is resolved once and the component version is read from the artifact it resolved
// to, never through the tag again. A normalized pin is verified against the descriptor read.
//
// It returns the OCI digest pin of the artifact read, which names exactly the returned
// descriptor, or nil if repo does not store component versions content-addressed (or version is
// empty without a pin).
func GetComponentVersionByArtifact(ctx context.Context, repo ComponentVersionRepository, component, version string, pin *descriptor.Digest) (ComponentVersionRepository, *descriptor.Descriptor, *descriptor.Digest, error) {
	if pin != nil && pin.NormalisationAlgorithm == "" {
		pinned, desc, err := GetPinnedComponentVersion(ctx, repo, component, version, pin)
		if err != nil {
			return nil, nil, nil, err
		}
		return pinned, desc, pin, nil
	}
	unbound := func() (ComponentVersionRepository, *descriptor.Descriptor, *descriptor.Digest, error) {
		r, desc, err := GetPinnedComponentVersion(ctx, repo, component, version, pin)
		return r, desc, nil, err
	}
	pinnable, ok := repo.(DigestPinnableRepository)
	if !ok || version == "" {
		return unbound()
	}
	dgst, err := pinnable.ResolveComponentVersionDigest(ctx, component, version)
	if errors.Is(err, errors.ErrUnsupported) {
		return unbound()
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolving version tag of %s:%s failed: %w", component, version, err)
	}
	artifact, err := ParseDigestPin(dgst)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("version tag of %s:%s resolves to an unsupported digest: %w", component, version, err)
	}
	pinned, desc, err := GetPinnedComponentVersion(ctx, repo, component, version, artifact)
	if err != nil {
		return nil, nil, nil, err
	}
	if pin != nil {
		if _, err := verifiableAlgorithm(pin.HashAlgorithm); err != nil {
			return nil, nil, nil, fmt.Errorf("unsupported hash algorithm %q in digest pin: use sha256 or sha512", pin.HashAlgorithm)
		}
		if err := verifyNormalizedPin(ctx, desc, pin); err != nil {
			return nil, nil, nil, err
		}
	}
	return pinned, desc, artifact, nil
}
