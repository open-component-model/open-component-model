// Package digest provides the digest processor for the gitHub access type.
// It downloads the repository tree at the pinned commit (via the github
// resource repository) and computes a generic blob digest over the resulting
// tar archive, so that by-reference github resources carry the same digest
// they would have as an embedded local blob.
package digest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/github/internal/archive"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	hashAlgorithmSHA256              = "SHA-256"
	normalisationGenericBlobDigestV1 = "genericBlobDigest/v1"
)

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for gitHub access types by downloading the
// pinned commit's tar archive and hashing it.
type DigestProcessor struct {
	resourceRepository *resource.ResourceRepository
}

// NewDigestProcessor creates a new GitHub digest processor. filesystemConfig is
// forwarded to the underlying resource repository; the archive is currently
// buffered in memory, so its TempFolder is not used yet.
func NewDigestProcessor(filesystemConfig *filesystemv1alpha1.Config) *DigestProcessor {
	return &DigestProcessor{
		resourceRepository: resource.NewResourceRepository(filesystemConfig),
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

// convertAccess converts the resource's access into a typed *v1.GitHub.
func (p *DigestProcessor) convertAccess(res *descriptor.Resource) (*v1.GitHub, error) {
	if res == nil || res.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	var gitHub v1.GitHub
	if err := access.Scheme.Convert(res.Access, &gitHub); err != nil {
		return nil, fmt.Errorf("error converting access to github spec: %w", err)
	}
	return &gitHub, nil
}

// ProcessResourceDigest resolves a by-reference github access to a concrete
// commit — pinning it when unset, or verifying an already-set commit against
// the resolved sha — then downloads the archive at that commit and applies or
// verifies its generic blob digest. The returned resource carries both the
// pinned access and the digest; the input resource is never mutated.
func (p *DigestProcessor) ProcessResourceDigest(
	ctx context.Context, res *descriptor.Resource, credentials runtime.Typed,
) (*descriptor.Resource, error) {
	gitHub, err := p.convertAccess(res)
	if err != nil {
		return nil, err
	}
	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid github access: %w", err)
	}

	// Resolve a reference to a concrete commit and pin (or verify) it, so a
	// by-reference resource carries an authoritative commit and downloads
	// reproducibly by that commit — mirroring OCI tag->digest pinning.
	if gitHub.Ref != "" {
		token, err := archive.TokenFromCredentials(credentials)
		if err != nil {
			return nil, fmt.Errorf("error resolving github credentials: %w", err)
		}
		resolved, err := archive.ResolveCommit(ctx, gitHub.RepoURL, gitHub.APIHostname, gitHub.Ref, token)
		if err != nil {
			return nil, fmt.Errorf("error resolving github ref to commit: %w", err)
		}
		switch {
		case gitHub.Commit == "":
			gitHub.Commit = resolved
		case !strings.EqualFold(gitHub.Commit, resolved):
			return nil, fmt.Errorf("github commit mismatch: access pins %s but ref %q resolves to %s",
				gitHub.Commit, gitHub.Ref, resolved)
		}
	}

	res = res.DeepCopy()
	res.Access = gitHub

	downloaded, err := p.resourceRepository.DownloadResource(ctx, res, credentials)
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

	if res.Digest.Value != resolvedDigest.Encoded() {
		return nil, fmt.Errorf("digest value mismatch: expected %s, got %s", res.Digest.Value, resolvedDigest.Encoded())
	}
	if res.Digest.HashAlgorithm != hashAlgorithmSHA256 {
		return nil, fmt.Errorf("hash algorithm mismatch: expected %s, got %s", hashAlgorithmSHA256, res.Digest.HashAlgorithm)
	}

	return res, nil
}
