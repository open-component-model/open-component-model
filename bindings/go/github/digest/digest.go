// Package digest provides the digest processor for the GitHub access type.
// It streams the repository tree at the pinned commit (via the github
// resource repository) and computes a generic blob digest over the tar
// archive on the fly, so that by-reference github resources carry the same
// digest they would have as an embedded local blob.
package digest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/internal/download"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for GitHub access types by streaming the
// pinned commit's tar archive and hashing it as it passes through.
type DigestProcessor struct {
	resourceRepository *resource.ResourceRepository
}

// NewDigestProcessor creates a new GitHub digest processor. opts are forwarded
// to the underlying resource repository, whose HTTP client performs both the
// ref resolution and the archive download. The archive is never buffered: its
// digest is computed on the fly while the stream is drained.
func NewDigestProcessor(opts ...resource.Option) *DigestProcessor {
	return &DigestProcessor{
		resourceRepository: resource.NewResourceRepository(opts...),
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
// currently resolves to, then streams the archive at the pinned commit and
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

	// A pre-set digest under another algorithm can never verify against the
	// archive bytes, so reject it before spending a download on it. A
	// hand-written resource.Digest in a component constructor cannot know the
	// normalisation algorithm — the blob is never normalised — and need not
	// restate the hash. An unset field means "fill it in with what we
	// compute"; only a field pinned to a *different* algorithm is a genuine
	// conflict. Spelling is not a conflict either: comparisons ignore case,
	// so "sha-256" or an uppercase hex value verifies instead of failing
	// while an absent field would have been accepted.
	if res.Digest != nil {
		if res.Digest.HashAlgorithm != "" && !strings.EqualFold(res.Digest.HashAlgorithm, download.HashAlgorithmSHA256) {
			return nil, fmt.Errorf("hash algorithm mismatch: expected %s, got %s", download.HashAlgorithmSHA256, res.Digest.HashAlgorithm)
		}
		if res.Digest.NormalisationAlgorithm != "" && !strings.EqualFold(res.Digest.NormalisationAlgorithm, download.NormalisationGenericBlobDigestV1) {
			return nil, fmt.Errorf("normalisation algorithm mismatch: expected %s, got %s", download.NormalisationGenericBlobDigestV1, res.Digest.NormalisationAlgorithm)
		}
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

	// Canonicalize the accepted spellings so descriptors do not vary by
	// author, and so the download below verifies the canonical value: the
	// resource repository only checks a digest it recognizes as a generic
	// blob SHA-256, which the accepted variants now are.
	if res.Digest != nil {
		res.Digest.HashAlgorithm = download.HashAlgorithmSHA256
		res.Digest.NormalisationAlgorithm = download.NormalisationGenericBlobDigestV1
		res.Digest.Value = strings.ToLower(res.Digest.Value)
	}

	// Download the pinned commit directly. The commit is authoritative now — we
	// just resolved the ref, or the caller pinned it — so carrying the ref into
	// the download would only make DownloadResource re-resolve it to check for
	// drift, a redundant API call on the digest path. The returned resource
	// keeps the ref informationally; the download copy drops it.
	dlAccess := gitHub.DeepCopy()
	dlAccess.Ref = ""
	dlResource := res.DeepCopy()
	dlResource.Access = dlAccess

	// The full archive is fetched only to be hashed and thrown away. That cost
	// is easy to miss from the outside, so it is called out loudly rather than
	// hidden in debug logs.
	slog.WarnContext(ctx, "computing the digest of a github resource downloads the full commit archive and discards it after hashing",
		"repoUrl", gitHub.RepoURL, "commit", gitHub.Commit)

	downloaded, err := p.resourceRepository.DownloadResource(ctx, dlResource, credentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading github resource for digest processing: %w", err)
	}
	reader, err := downloaded.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error reading downloaded github archive: %w", err)
	}

	// Drain the stream: the blob's verify reader computes the digest as the
	// bytes pass through, and its Close verifies a pre-set digest — a mismatch
	// surfaces here, not as a warning.
	_, copyErr := io.Copy(io.Discard, reader)
	if err := errors.Join(copyErr, reader.Close()); err != nil {
		return nil, fmt.Errorf("error digesting github archive: %w", err)
	}

	if res.Digest != nil {
		// The pre-set digest was verified byte-for-byte by the reader's Close.
		return res, nil
	}

	digestAware, ok := downloaded.(blob.DigestAware)
	if !ok {
		return nil, fmt.Errorf("downloaded github archive blob does not expose a digest")
	}
	computed, known := digestAware.Digest()
	if !known {
		return nil, fmt.Errorf("downloaded github archive digest is unknown after reading the archive")
	}
	parsed, err := godigest.Parse(computed)
	if err != nil {
		return nil, fmt.Errorf("error parsing computed github archive digest: %w", err)
	}

	res.Digest = &descriptor.Digest{
		HashAlgorithm:          download.HashAlgorithmSHA256,
		NormalisationAlgorithm: download.NormalisationGenericBlobDigestV1,
		Value:                  parsed.Encoded(),
	}
	return res, nil
}
