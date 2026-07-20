package resource

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/internal/download"
	githubaccess "ocm.software/open-component-model/bindings/go/github/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository implements a resource repository for GitHub repositories.
// It streams the source archive of a pinned commit via the GitHub REST API,
// serving the exact tarball GitHub does, so content and digest match old OCM.
type ResourceRepository struct {
	httpConfig *httpv1alpha1.Config
	httpClient *http.Client
}

// Option configures a ResourceRepository.
type Option func(*ResourceRepository)

// WithHTTPConfig sets the HTTP client configuration used for the GitHub REST
// calls and the archive download. When nil, the http binding's defaults apply
// (retries on 408, 429 and 5xx, plus transport timeouts).
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(r *ResourceRepository) {
		r.httpConfig = cfg
	}
}

// WithHTTPClient sets the HTTP client used for the GitHub REST calls and the
// archive download, taking precedence over WithHTTPConfig. A client supplied
// here is used as-is, so it does not get the http binding's retry and timeout
// defaults unless it was built with them.
func WithHTTPClient(client *http.Client) Option {
	return func(r *ResourceRepository) {
		r.httpClient = client
	}
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a ResourceRepository. Downloads stream
// straight from GitHub without buffering, so no filesystem configuration is
// needed. The HTTP client is built once, so its connection pool is reused
// across downloads.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	r := &ResourceRepository{}
	for _, opt := range opts {
		opt(r)
	}
	if r.httpClient == nil {
		r.httpClient = ocmhttp.New(ocmhttp.WithConfig(r.httpConfig))
	}
	return r
}

// ResolveCommit resolves the access's ref to the commit SHA it currently points
// at, using this repository's HTTP client. The digest processor uses it to pin a
// ref-only access before downloading. Nil credentials resolve anonymously.
func (r *ResourceRepository) ResolveCommit(ctx context.Context, gitHub *v1.GitHub, credentials *credsv1.GitHubCredentials) (string, error) {
	return download.ResolveCommit(ctx, gitHub, credentials, r.httpClient)
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

// DownloadResource fetches the archive of the commit pinned in the resource's
// GitHub access as a gzipped tar blob (application/x-tgz) streamed directly
// from GitHub. When the resource carries a generic blob digest, the stream is
// verified against it as it is read: the digest is computed on the fly and
// checked when the consumer closes the reader. A digest under another
// algorithm cannot be checked against the archive bytes and is ignored.
//
// Nothing is buffered, so the returned blob serves its reader exactly once —
// a second ReadCloser call fails — and the consumer must read the stream
// fully and close it.
//
// A ref-only access is resolved to the commit the ref points at now, so the
// download is a snapshot; the digest processor is what pins it for
// reproducibility. When both are set the commit wins, and the ref is
// re-resolved only to warn about drift — drift never fails the download.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	gitHub, err := githubinternal.AccessFrom(resource.Access)
	if err != nil {
		return nil, fmt.Errorf("error resolving GitHub access for download: %w", err)
	}

	gitHubCredentials, err := credsv1.ConvertToGitHubCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error resolving GitHub credentials: %w", err)
	}

	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid GitHub access: %w", err)
	}

	switch {
	case gitHub.Commit == "":
		resolved, err := r.ResolveCommit(ctx, gitHub, gitHubCredentials)
		if err != nil {
			return nil, fmt.Errorf("error resolving GitHub ref to commit: %w", err)
		}
		gitHub.Commit = resolved
	case gitHub.Ref != "":
		if resolved, err := r.ResolveCommit(ctx, gitHub, gitHubCredentials); err != nil {
			slog.DebugContext(ctx, "could not resolve GitHub ref to check the pinned commit", "ref", gitHub.Ref, "error", err)
		} else if resolved != gitHub.Commit {
			slog.WarnContext(ctx, "GitHub ref no longer points at the pinned commit; downloading the pinned commit",
				"ref", gitHub.Ref, "refCommit", resolved, "commit", gitHub.Commit)
		}
	}

	// Only a generic blob digest is a statement about the archive bytes; a
	// digest under another hash or normalisation algorithm (e.g. an old-OCM
	// ociArtifactDigest) cannot be checked against the stream, only ignored.
	// The value is not validated here: Download rejects a malformed digest, so
	// a corrupt descriptor fails loudly instead of skipping the verification
	// it asked for.
	var expected godigest.Digest
	switch d := resource.Digest; {
	case d == nil:
	case d.HashAlgorithm == download.HashAlgorithmSHA256 && d.NormalisationAlgorithm == download.NormalisationGenericBlobDigestV1:
		expected = godigest.NewDigestFromEncoded(godigest.SHA256, d.Value)
	default:
		slog.DebugContext(ctx, "GitHub resource digest cannot be verified against the archive stream",
			"hashAlgorithm", d.HashAlgorithm, "normalisationAlgorithm", d.NormalisationAlgorithm)
	}

	return download.Download(ctx, gitHub, gitHubCredentials, r.httpClient, expected)
}

// UploadResource is not supported: the GitHub access type is a read-only
// reference; content reaches GitHub through git, not through OCM.
func (r *ResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("github repositories do not support upload operations")
}
