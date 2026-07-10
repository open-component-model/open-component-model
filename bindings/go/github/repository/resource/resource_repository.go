package resource

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/internal/download"
	githubaccess "ocm.software/open-component-model/bindings/go/github/spec/access"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository implements a resource repository for GitHub repositories.
// It downloads the source archive of a pinned commit via the GitHub REST API
// (matching old OCM, so the content and its digest are identical) and resolves
// credential consumer identities for authentication.
type ResourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a ResourceRepository. The TempFolder of
// filesystemConfig is where downloaded archives are buffered; when it is nil
// or empty the operating system's temporary directory is used.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config) *ResourceRepository {
	return &ResourceRepository{
		filesystemConfig: filesystemConfig,
	}
}

// tempFolder returns the directory archives are buffered under. An empty
// string lets os.CreateTemp fall back to the OS temporary directory.
func (r *ResourceRepository) tempFolder() string {
	if r.filesystemConfig == nil {
		return ""
	}
	return r.filesystemConfig.TempFolder
}

// GetResourceRepositoryScheme returns the GitHub access scheme containing the
// gitHub/v1 type and its aliases.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return githubaccess.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer
// identity (type GitHubRepository) for the given GitHub resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(_ context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	gitHub, err := githubinternal.AccessFrom(resource.Access)
	if err != nil {
		return nil, err
	}

	return githubinternal.CredentialConsumerIdentity(gitHub.RepoURL)
}

// DownloadResource fetches the source archive of the commit pinned in the
// resource's GitHub access and returns it as a gzipped tar blob
// (media type application/x-tgz).
//
// A ref-only access (no commit) is resolved to the commit the ref currently
// points at, so the download is a snapshot of "now". Reproducible,
// digest-verifiable content comes from the digest processor, which pins the
// resolved commit into the descriptor at construction time.
//
// When both are set the commit takes precedence; the ref is only re-resolved
// to warn when it no longer points at the pinned commit, and drift never
// fails the download.
//
// The archive is streamed into a temporary file under the configured
// TempFolder rather than held in memory, since a repository archive can be
// large. The returned blob is an io.Closer: closing it reclaims that file
// immediately, and a blob that is never closed reclaims it once unreachable.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	gitHub, err := githubinternal.AccessFrom(resource.Access)
	if err != nil {
		return nil, fmt.Errorf("error resolving GitHub access for download: %w", err)
	}

	token, err := githubinternal.TokenFromCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error resolving GitHub credentials: %w", err)
	}

	switch {
	case gitHub.Commit == "":
		if err := gitHub.Validate(); err != nil {
			return nil, fmt.Errorf("invalid GitHub access: %w", err)
		}
		resolved, err := download.ResolveCommit(ctx, gitHub.RepoURL, gitHub.APIHostname, gitHub.Ref, token)
		if err != nil {
			return nil, fmt.Errorf("error resolving GitHub ref to commit: %w", err)
		}
		gitHub.Commit = resolved
	case gitHub.Ref != "":
		if resolved, err := download.ResolveCommit(ctx, gitHub.RepoURL, gitHub.APIHostname, gitHub.Ref, token); err != nil {
			slog.DebugContext(ctx, "could not resolve GitHub ref to check the pinned commit", "ref", gitHub.Ref, "error", err)
		} else if resolved != gitHub.Commit {
			slog.WarnContext(ctx, "GitHub ref no longer points at the pinned commit; downloading the pinned commit",
				"ref", gitHub.Ref, "refCommit", resolved, "commit", gitHub.Commit)
		}
	}

	return download.Archive(ctx, gitHub, token, r.tempFolder())
}

// UploadResource is not supported for GitHub repositories and always returns
// an error: the GitHub access type is a read-only source reference; content
// is pushed to GitHub through git, not through OCM.
func (r *ResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("github repositories do not support upload operations")
}
