// Package digest provides the resource digest processor for the maven/v2alpha1
// access type, so `ocm add component-version` can record and verify the digest
// of a by-reference Maven resource.
//
// The digest is SHA-256 over the application/x-tgz the resource repository
// produces, with the genericBlobDigest/v1 normalisation, because that archive
// is the blob a by-value transfer stores and later verifies. The repository
// builds the archive with stored gzip blocks and fixed tar headers, so the
// bytes depend only on the files fetched, not on the Go release. They do still
// depend on which sibling files (signature and checksums) the repository
// serves, so a repository that adds a checksum file later changes the digest.
// Only pinned versions are accepted: LATEST, RELEASE and a SNAPSHOT resolve to
// different files over time, and a digest over them would break verification
// of a component version that has not changed.
package digest

import (
	"context"
	"fmt"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/internal"
	"ocm.software/open-component-model/bindings/go/maven/repository/resource"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	hashAlgorithmSHA256      = "SHA-256"
	normalisationGenericBlob = "genericBlobDigest/v1"
)

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for Maven artifact access types by
// downloading the artifact archive and hashing its bytes.
type DigestProcessor struct {
	repo *resource.ResourceRepository
}

// NewDigestProcessor creates a Maven digest processor. Options are forwarded to
// the resource repository used for downloading, so the CLI can pass the same
// configured HTTP client it gives the repository.
func NewDigestProcessor(opts ...resource.Option) *DigestProcessor {
	return &DigestProcessor{repo: resource.NewResourceRepository(opts...)}
}

// GetResourceRepositoryScheme returns the Maven access scheme.
func (p *DigestProcessor) GetResourceRepositoryScheme() *ocmruntime.Scheme {
	return mavenaccess.Scheme
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the same
// MavenRepository identity the download uses.
func (p *DigestProcessor) GetResourceDigestProcessorCredentialConsumerIdentity(
	ctx context.Context, res *descriptor.Resource,
) (ocmruntime.Identity, error) {
	return p.repo.GetResourceCredentialConsumerIdentity(ctx, res)
}

// ProcessResourceDigest downloads the Maven artifact archive and applies (or
// verifies) its SHA-256 generic blob digest on the resource.
func (p *DigestProcessor) ProcessResourceDigest(
	ctx context.Context, res *descriptor.Resource, credentials ocmruntime.Typed,
) (*descriptor.Resource, error) {
	m, err := internal.ConvertAccess(res)
	if err != nil {
		return nil, err
	}
	if res.Digest != nil && res.Digest.NormalisationAlgorithm == descriptor.ExcludeFromSignature {
		// The author opted the content out of the signature; there is nothing
		// to compute and no reason to download.
		return res.DeepCopy(), nil
	}
	if !m.IsPinnedVersion() {
		return nil, fmt.Errorf("cannot compute a digest for maven version %q: pin LATEST, RELEASE or a SNAPSHOT to a release version first", m.Version)
	}

	b, err := p.repo.DownloadResource(ctx, res, credentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading maven artifact for digest: %w", err)
	}
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error reading maven artifact: %w", err)
	}
	defer func() { _ = rc.Close() }()

	d, err := godigest.FromReader(rc)
	if err != nil {
		return nil, fmt.Errorf("error computing maven artifact digest: %w", err)
	}
	value := d.Encoded()

	res = res.DeepCopy()
	if res.Digest == nil {
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: normalisationGenericBlob,
			Value:                  value,
		}
		return res, nil
	}

	// A hand-written digest in a component constructor need not name the
	// algorithms: an unset field is filled in, only a field pinned to a
	// different algorithm is a conflict. Spelling is not a conflict either.
	if res.Digest.HashAlgorithm != "" && !strings.EqualFold(res.Digest.HashAlgorithm, hashAlgorithmSHA256) {
		return nil, fmt.Errorf("hash algorithm mismatch: expected %s, got %s", hashAlgorithmSHA256, res.Digest.HashAlgorithm)
	}
	if res.Digest.NormalisationAlgorithm != "" && !strings.EqualFold(res.Digest.NormalisationAlgorithm, normalisationGenericBlob) {
		return nil, fmt.Errorf("normalisation algorithm mismatch: expected %s, got %s", normalisationGenericBlob, res.Digest.NormalisationAlgorithm)
	}
	if res.Digest.Value != "" && !strings.EqualFold(res.Digest.Value, value) {
		return nil, fmt.Errorf("digest value mismatch: expected %s, got %s", res.Digest.Value, value)
	}

	// Canonicalize the accepted spellings so descriptors do not vary by author.
	res.Digest.HashAlgorithm = hashAlgorithmSHA256
	res.Digest.NormalisationAlgorithm = normalisationGenericBlob
	res.Digest.Value = value
	return res, nil
}
