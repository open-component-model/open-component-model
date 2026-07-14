package source

import (
	"context"
	"fmt"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/internal/download"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SourceRepository implements a source repository for GitHub repositories. It
// serves the same commit archive as the resource repository, but anonymously:
// [repository.SourceRepository] carries no credentials.
type SourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
	httpConfig       *httpv1alpha1.Config
	httpClient       *http.Client
}

// Option configures a SourceRepository.
type Option func(*SourceRepository)

// WithHTTPConfig sets the HTTP client configuration used for the GitHub REST
// calls and the archive download. When nil, the http binding's defaults apply
// (retries on 408, 429 and 5xx, plus transport timeouts).
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(r *SourceRepository) {
		r.httpConfig = cfg
	}
}

var _ repository.SourceRepository = (*SourceRepository)(nil)

// NewSourceRepository creates a SourceRepository. Archives are buffered under
// the TempFolder of filesystemConfig, or the OS temporary directory when unset.
// The HTTP client is built once, so its connection pool is reused across
// downloads.
func NewSourceRepository(filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *SourceRepository {
	r := &SourceRepository{
		filesystemConfig: filesystemConfig,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.httpClient = ocmhttp.New(ocmhttp.WithConfig(r.httpConfig))
	return r
}

// tempFolder returns the directory archives are buffered under. An empty
// string lets os.CreateTemp fall back to the OS temporary directory.
func (r *SourceRepository) tempFolder() string {
	if r.filesystemConfig == nil {
		return ""
	}
	return r.filesystemConfig.TempFolder
}

// DownloadSource fetches the archive of the commit pinned in the source's GitHub
// access as a gzipped tar blob (application/x-tgz). The ref is never resolved, so
// a source without a pinned commit is rejected: without it there is nothing
// immutable to materialize. The request is anonymous and counts against GitHub's
// per-IP rate limit.
func (r *SourceRepository) DownloadSource(ctx context.Context, source *descriptor.Source) (blob.ReadOnlyBlob, error) {
	gitHub, err := githubinternal.AccessFrom(source.Access)
	if err != nil {
		return nil, fmt.Errorf("error resolving GitHub access for download: %w", err)
	}

	return download.Download(ctx, gitHub, nil, r.tempFolder(), r.httpClient)
}

// UploadSource is not supported: the GitHub access type is a read-only
// reference; content reaches GitHub through git, not through OCM.
func (r *SourceRepository) UploadSource(_ context.Context, _ runtime.Typed, _ *descriptor.Source, _ blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, fmt.Errorf("github repositories do not support upload operations")
}
