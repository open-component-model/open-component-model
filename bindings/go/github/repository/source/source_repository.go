package source

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/internal/download"
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
}

var _ repository.SourceRepository = (*SourceRepository)(nil)

// NewSourceRepository creates a SourceRepository. The TempFolder of
// filesystemConfig is where downloaded archives are buffered; when it is nil
// or empty the operating system's temporary directory is used.
func NewSourceRepository(filesystemConfig *filesystemv1alpha1.Config) *SourceRepository {
	return &SourceRepository{
		filesystemConfig: filesystemConfig,
	}
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
// See download.Archive for the buffering and cleanup semantics
// of the returned blob.
func (r *SourceRepository) DownloadSource(ctx context.Context, source *descriptor.Source) (blob.ReadOnlyBlob, error) {
	gitHub, err := githubinternal.AccessFrom(source.Access)
	if err != nil {
		return nil, fmt.Errorf("error resolving GitHub access for download: %w", err)
	}

	return download.Archive(ctx, gitHub, "", r.tempFolder())
}

// UploadSource is not supported for GitHub repositories and always returns an
// error: the GitHub access type is a read-only source reference; content is
// pushed to GitHub through git, not through OCM.
func (r *SourceRepository) UploadSource(_ context.Context, _ runtime.Typed, _ *descriptor.Source, _ blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, fmt.Errorf("github repositories do not support upload operations")
}
