// Package digest provides a resource digest processor for the Maven access type.
// It resolves a resource's content digest by downloading the artifact and
// computing a generic blob digest (SHA-256), so that `ocm add component-version`
// can pin/verify the digest of a by-reference Maven resource.
package digest

import (
	"context"
	"fmt"

	godigest "github.com/opencontainers/go-digest"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/internal"
	"ocm.software/open-component-model/bindings/go/maven/repository/resource"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	mavenv1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	hashAlgorithmSHA256      = "SHA-256"
	normalisationGenericBlob = "genericBlobDigest/v1"
)

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for Maven artifact access types by
// downloading the artifact and hashing its bytes.
type DigestProcessor struct {
	repo *resource.ResourceRepository
}

// NewDigestProcessor creates a Maven digest processor. Options are forwarded to
// the internal resource repository used for downloading.
func NewDigestProcessor(opts ...resource.Option) *DigestProcessor {
	return &DigestProcessor{repo: resource.NewResourceRepository(opts...)}
}

// GetResourceRepositoryScheme returns the Maven access scheme.
func (p *DigestProcessor) GetResourceRepositoryScheme() *ocmruntime.Scheme {
	return mavenaccess.Scheme
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential
// consumer identity for digest processing (the same MavenRepository identity
// used for download).
func (p *DigestProcessor) GetResourceDigestProcessorCredentialConsumerIdentity(
	ctx context.Context, res *descriptor.Resource,
) (ocmruntime.Identity, error) {
	var m mavenv1.Maven
	if res == nil || res.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	if err := mavenaccess.Scheme.Convert(res.Access, &m); err != nil {
		return nil, fmt.Errorf("error converting resource access to maven spec: %w", err)
	}
	return internal.CredentialConsumerIdentity(m.RepoURL)
}

// ProcessResourceDigest downloads the Maven artifact and applies (or verifies)
// its SHA-256 generic blob digest on the resource.
func (p *DigestProcessor) ProcessResourceDigest(
	ctx context.Context, res *descriptor.Resource, credentials ocmruntime.Typed,
) (*descriptor.Resource, error) {
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

	res = res.DeepCopy()
	if res.Digest == nil {
		res.Digest = &descriptor.Digest{}
	} else if res.Digest.Value != "" && res.Digest.Value != d.Encoded() {
		return nil, fmt.Errorf("digest mismatch for maven artifact: expected %s, got %s", res.Digest.Value, d.Encoded())
	}
	res.Digest.HashAlgorithm = hashAlgorithmSHA256
	res.Digest.NormalisationAlgorithm = normalisationGenericBlob
	res.Digest.Value = d.Encoded()
	return res, nil
}
