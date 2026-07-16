// Package digest provides the digest processor for the GitHub access type.
// It downloads the repository tree at the pinned commit (via the github
// resource repository) and computes a generic blob digest over the resulting
// tar archive, so that by-reference github resources carry the same digest
// they would have as an embedded local blob.
package digest

import (
	"context"
	"fmt"
	"log/slog"

	godigest "github.com/opencontainers/go-digest"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	hashAlgorithmSHA256              = "SHA-256"
	normalisationGenericBlobDigestV1 = "genericBlobDigest/v1"
)

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for GitHub access types by downloading the
// pinned commit's tar archive and hashing it.
type DigestProcessor struct {
	resourceRepository *resource.ResourceRepository
}

// NewDigestProcessor creates a new GitHub digest processor. filesystemConfig
// and opts are forwarded to the underlying resource repository, whose HTTP
// client performs both the ref resolution and the archive download; the
// archive is hashed from its temp-file buffer (see download.Download).
func NewDigestProcessor(filesystemConfig *filesystemv1alpha1.Config, opts ...resource.Option) *DigestProcessor {
	return &DigestProcessor{
		resourceRepository: resource.NewResourceRepository(filesystemConfig, opts...),
	}
}

func (p *DigestProcessor) GetResourceRepositoryScheme() *runtime.Scheme {
	return access.Scheme
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the
// credential consumer identity (type GitHubRepository) for digest processing.
func (p *DigestProcessor) GetResourceDigestProcessorCredentialConsumerIdentity(
	ctx context.Context, resource *descriptor.Resource,
) (runtime.Identity, error) {
	return p.resourceRepository.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest pins a ref-only github access to the commit the ref
// currently resolves to, then downloads the archive at the pinned commit and
// applies or verifies its generic blob digest. The returned resource carries
// both the pinned access and the digest; the input resource is never mutated.
func (p *DigestProcessor) ProcessResourceDigest(
	ctx context.Context, res *descriptor.Resource, credentials runtime.Typed,
) (*descriptor.Resource, error) {
	gitHub, err := githubinternal.AccessFrom(res.Access)
	if err != nil {
		return nil, err
	}
	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid github access: %w", err)
	}

	// A set Commit is authoritative and Ref is informational, so only an
	// unpinned access resolves its ref — mirroring OCI tag->digest pinning.
	// Re-resolving an already pinned commit would make the digest depend on
	// where the ref points today, and every branch that moves past the pinned
	// commit (or is deleted after a merge) would break verification of a
	// component version that has not changed.
	if gitHub.Commit == "" {
		gitHubCredentials, err := credsv1.ConvertToGitHubCredentials(credentials)
		if err != nil {
			return nil, fmt.Errorf("error resolving github credentials: %w", err)
		}
		resolved, err := p.resourceRepository.ResolveCommit(ctx, gitHub, gitHubCredentials)
		if err != nil {
			return nil, fmt.Errorf("error resolving github ref to commit: %w", err)
		}
		gitHub.Commit = resolved
	}

	res = res.DeepCopy()
	res.Access = gitHub

	// Download the pinned commit directly. The commit is authoritative now — we
	// just resolved the ref, or the caller pinned it — so carrying the ref into
	// the download would only make DownloadResource re-resolve it to check for
	// drift, a redundant API call on the digest path. The returned resource
	// keeps the ref informationally; the download copy drops it.
	dlAccess := gitHub.DeepCopy()
	dlAccess.Ref = ""
	dlResource := res.DeepCopy()
	dlResource.Access = dlAccess

	downloaded, err := p.resourceRepository.DownloadResource(ctx, dlResource, credentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading github resource for digest processing: %w", err)
	}
	reader, err := downloaded.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error reading downloaded github archive: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.WarnContext(ctx, "error closing downloaded github archive", "error", err)
		}
	}()

	resolvedDigest, err := godigest.FromReader(reader)
	if err != nil {
		return nil, fmt.Errorf("error digesting downloaded github archive: %w", err)
	}

	if res.Digest == nil {
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: normalisationGenericBlobDigestV1,
			Value:                  resolvedDigest.Encoded(),
		}
		return res, nil
	}

	if res.Digest.HashAlgorithm != hashAlgorithmSHA256 {
		return nil, fmt.Errorf("hash algorithm mismatch: expected %s, got %s", hashAlgorithmSHA256, res.Digest.HashAlgorithm)
	}
	if res.Digest.NormalisationAlgorithm != normalisationGenericBlobDigestV1 {
		return nil, fmt.Errorf("normalisation algorithm mismatch: expected %s, got %s", normalisationGenericBlobDigestV1, res.Digest.NormalisationAlgorithm)
	}
	if res.Digest.Value != resolvedDigest.Encoded() {
		return nil, fmt.Errorf("digest value mismatch: expected %s, got %s", res.Digest.Value, resolvedDigest.Encoded())
	}

	return res, nil
}
