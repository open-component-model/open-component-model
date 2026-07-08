package resource

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/internal/archive"
	githubaccess "ocm.software/open-component-model/bindings/go/github/spec/access"
	"ocm.software/open-component-model/bindings/go/github/spec/access/v1"
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

// NewResourceRepository creates a ResourceRepository. filesystemConfig is
// accepted for parity with the other resource repositories; the github archive
// is buffered in memory and no temporary directory is currently required.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config) *ResourceRepository {
	if filesystemConfig == nil {
		filesystemConfig = &filesystemv1alpha1.Config{}
	}
	return &ResourceRepository{
		filesystemConfig: filesystemConfig,
	}
}

// GetResourceRepositoryScheme returns the GitHub access scheme containing the
// gitHub/v1 type and its aliases.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return githubaccess.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer
// identity (type GitHubRepository) for the given github resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(_ context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	gitHub, err := r.convertAccess(resource)
	if err != nil {
		return nil, fmt.Errorf("error converting resource access to github spec: %w", err)
	}

	return githubinternal.CredentialConsumerIdentity(gitHub.RepoURL)
}

// DownloadResource fetches the source archive of the commit pinned in the
// resource's github access and returns it as a gzipped tar blob
// (media type application/x-tgz).
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	gitHub, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}
	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid github access: %w", err)
	}
	if gitHub.Commit == "" {
		return nil, fmt.Errorf("github access requires a pinned commit to download; ref %q has no resolved commit", gitHub.Ref)
	}

	token, err := archive.TokenFromCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error resolving github credentials: %w", err)
	}

	slog.DebugContext(ctx, "Downloading github commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit)

	data, err := archive.Fetch(ctx, gitHub.RepoURL, gitHub.APIHostname, gitHub.Commit, token)
	if err != nil {
		return nil, fmt.Errorf("error downloading github commit archive: %w", err)
	}

	slog.DebugContext(ctx, "Downloaded github commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit, "bytes", len(data))

	return inmemory.New(bytes.NewReader(data), inmemory.WithMediaType(archive.MediaTypeTGZ)), nil
}

// UploadResource is not supported for GitHub repositories and always returns
// an error: the gitHub access type is a read-only source reference; content
// is pushed to GitHub through git, not through OCM.
func (r *ResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("github repositories do not support upload operations")
}

func (r *ResourceRepository) convertAccess(resource *descriptor.Resource) (*v1.GitHub, error) {
	if resource == nil || resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	var gitHub v1.GitHub
	if err := githubaccess.Scheme.Convert(resource.Access, &gitHub); err != nil {
		return nil, fmt.Errorf("error converting access to github spec: %w", err)
	}
	return &gitHub, nil
}
