package resource

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/internal/download"
	githubaccess "ocm.software/open-component-model/bindings/go/github/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository implements a resource repository for GitHub repositories.
// It downloads the source archive of a pinned commit via the GitHub REST API
// (matching old OCM, so the content and its digest are identical) and resolves
// credential consumer identities for authentication.
type ResourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
	httpConfig       *httpv1alpha1.Config
	httpClient       *http.Client
}

// Option configures a ResourceRepository.
type Option func(*ResourceRepository)

// WithHTTPConfig sets the HTTP client configuration used for the GitHub REST
// calls and the archive download. When nil, the http binding's defaults apply
// (retries on 408, 429 and 5xx, plus transport timeouts). Accepts the
// serialisable config type so that external plugins can round-trip it over the
// wire and reconstruct an equivalent client.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(r *ResourceRepository) {
		r.httpConfig = cfg
	}
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a ResourceRepository. The TempFolder of
// filesystemConfig is where downloaded archives are buffered; when it is nil
// or empty the operating system's temporary directory is used.
//
// The HTTP client is built once here rather than per request, so that its
// connection pool is reused across downloads.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *ResourceRepository {
	r := &ResourceRepository{
		filesystemConfig: filesystemConfig,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.httpClient = ocmhttp.New(ocmhttp.WithConfig(r.httpConfig))
	return r
}

// ResolveCommit resolves the access's ref to the commit SHA it currently
// points at, using this repository's HTTP client. The digest processor uses it
// to pin a ref-only access before downloading.
func (r *ResourceRepository) ResolveCommit(ctx context.Context, gitHub *v1.GitHub, token string) (string, error) {
	return download.ResolveCommit(ctx, gitHub, token, r.httpClient)
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
		resolved, err := r.ResolveCommit(ctx, gitHub, token)
		if err != nil {
			return nil, fmt.Errorf("error resolving GitHub ref to commit: %w", err)
		}
		gitHub.Commit = resolved
	case gitHub.Ref != "":
		if resolved, err := r.ResolveCommit(ctx, gitHub, token); err != nil {
			slog.DebugContext(ctx, "could not resolve GitHub ref to check the pinned commit", "ref", gitHub.Ref, "error", err)
		} else if resolved != gitHub.Commit {
			slog.WarnContext(ctx, "GitHub ref no longer points at the pinned commit; downloading the pinned commit",
				"ref", gitHub.Ref, "refCommit", resolved, "commit", gitHub.Commit)
		}
	}

	return download.CommitArchive(ctx, gitHub, token, r.tempFolder(), r.httpClient)
}

// UploadResource is not supported for GitHub repositories and always returns
// an error: the GitHub access type is a read-only source reference; content
// is pushed to GitHub through git, not through OCM.
func (r *ResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("github repositories do not support upload operations")
}
