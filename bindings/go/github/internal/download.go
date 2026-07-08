package internal

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/github/internal/archive"
	"ocm.software/open-component-model/bindings/go/github/internal/tempblob"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
)

// DownloadArchive validates the GitHub access and fetches the source archive
// of its pinned commit, returning it as a gzipped tar blob (media type
// application/x-tgz). An access without a resolved commit is rejected: a bare
// ref is mutable and cannot be materialized reproducibly.
//
// The archive is streamed into a temporary file under tempFolder rather than
// held in memory, since a repository archive can be large. The returned blob
// is an io.Closer: closing it reclaims that file immediately, and a blob that
// is never closed reclaims it once unreachable.
func DownloadArchive(ctx context.Context, gitHub *v1.GitHub, token, tempFolder string) (blob.ReadOnlyBlob, error) {
	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid GitHub access: %w", err)
	}
	if gitHub.Commit == "" {
		return nil, fmt.Errorf("GitHub access requires a pinned commit to download; ref %q has no resolved commit", gitHub.Ref)
	}

	slog.DebugContext(ctx, "Downloading GitHub commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit)

	stream, err := archive.Fetch(ctx, gitHub.RepoURL, gitHub.APIHostname, gitHub.Commit, token)
	if err != nil {
		return nil, fmt.Errorf("error downloading GitHub commit archive: %w", err)
	}
	defer func() {
		if err := stream.Close(); err != nil {
			slog.WarnContext(ctx, "error closing GitHub archive stream", "error", err)
		}
	}()

	buffered, err := tempblob.New(tempFolder, "github-archive-*.tgz", stream, archive.MediaTypeTGZ)
	if err != nil {
		return nil, fmt.Errorf("error buffering GitHub commit archive: %w", err)
	}

	slog.DebugContext(ctx, "Downloaded GitHub commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit, "bytes", buffered.Size())

	return buffered, nil
}
