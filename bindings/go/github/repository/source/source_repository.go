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

// SourceRepository implements a source repository for GitHub repositories. A
// git repository is source code, so the GitHub access type's natural home is
// a component source; downloading it serves the same commit archive as the
// resource repository.
//
// The repository.SourceRepository interface carries no credentials (see
// https://github.com/open-component-model/ocm-project/issues/857), so source
// downloads are anonymous until that is resolved.
type SourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
	httpConfig       *httpv1alpha1.Config
	httpClient       *http.Client
}

// Option configures a SourceRepository.
type Option func(*SourceRepository)

// WithHTTPConfig sets the HTTP client configuration used for the GitHub REST
// calls and the archive download. When nil, the http binding's defaults apply
// (retries on 408, 429 and 5xx, plus transport timeouts). Accepts the
// serialisable config type so that external plugins can round-trip it over the
// wire and reconstruct an equivalent client.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(r *SourceRepository) {
		r.httpConfig = cfg
	}
}

var _ repository.SourceRepository = (*SourceRepository)(nil)

// NewSourceRepository creates a SourceRepository. The TempFolder of
// filesystemConfig is where downloaded archives are buffered; when it is nil
// or empty the operating system's temporary directory is used.
//
// The HTTP client is built once here rather than per request, so that its
// connection pool is reused across downloads.
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

// DownloadSource fetches the source archive of the commit pinned in the
// source's GitHub access and returns it as a gzipped tar blob (media type
// application/x-tgz). The commit is always used and the ref is ignored
// entirely (never resolved); a ref-only source is rejected, since without a
// pinned commit there is nothing immutable to materialize.
//
// The request carries no token (see the type doc), so it counts against GitHub's
// anonymous rate limit, which is shared per client IP. A run pulling many
// sources at once, such as a CI job, can exhaust it.
//
// The archive is streamed into a temporary file under the configured
// TempFolder rather than held in memory, since a repository archive can be
// large; see download.Download for the lifetime of that file.
func (r *SourceRepository) DownloadSource(ctx context.Context, source *descriptor.Source) (blob.ReadOnlyBlob, error) {
	gitHub, err := githubinternal.AccessFrom(source.Access)
	if err != nil {
		return nil, fmt.Errorf("error resolving GitHub access for download: %w", err)
	}

	return download.Download(ctx, gitHub, nil, r.tempFolder(), r.httpClient)
}

// UploadSource is not supported for GitHub repositories and always returns an
// error: the GitHub access type is a read-only source reference; content is
// pushed to GitHub through git, not through OCM.
func (r *SourceRepository) UploadSource(_ context.Context, _ runtime.Typed, _ *descriptor.Source, _ blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, fmt.Errorf("github repositories do not support upload operations")
}
