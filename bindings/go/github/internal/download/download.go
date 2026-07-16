// Package download fetches the source archive of a GitHub repository at a
// pinned commit via the GitHub REST API and returns it as a file-backed blob
// buffered in a temp directory. The archive bytes are the exact gzipped
// tar GitHub serves, so that the resource content and its generic blob digest
// match what old OCM stored for a GitHub access.
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
)

// MediaTypeTGZ is the media type of the GitHub source archive. It matches the
// MIME_TGZ old OCM assigned to the github access blob.
const MediaTypeTGZ = "application/x-tgz"

// Download validates the GitHub access and fetches the source archive of its
// pinned commit, returning it as a gzipped tar blob (media type
// application/x-tgz) buffered to a file in tempDir. An empty tempDir uses
// the OS default temp directory (os.TempDir).
// An access without a resolved commit is rejected: a bare ref is mutable and
// cannot be materialized reproducibly.
//
// The temp file is not removed here: the returned blob reads from it on
// demand, so it must outlive this call, and the blob contract has no signal
// for when the consumer is done. Its lifetime is owned by the temp
// directory cleanup.
//
// credentials and httpClient may be nil; see clientFor.
func Download(ctx context.Context, gitHub *v1.GitHub, credentials *credsv1.GitHubCredentials, httpClient *http.Client, tempDir string) (blob.ReadOnlyBlob, error) {
	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid GitHub access: %w", err)
	}
	if gitHub.Commit == "" {
		return nil, fmt.Errorf("GitHub access requires a pinned commit to download; ref %q has no resolved commit", gitHub.Ref)
	}

	slog.DebugContext(ctx, "Downloading GitHub commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit)

	stream, err := fetch(ctx, gitHub, credentials, httpClient)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := stream.Close(); err != nil {
			slog.WarnContext(ctx, "error closing GitHub archive stream", "error", err)
		}
	}()

	tmpFile, err := os.CreateTemp(tempDir, "github-archive-*.tgz")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for GitHub commit archive: %w", err)
	}
	written, copyErr := io.Copy(tmpFile, stream)
	if err := errors.Join(copyErr, tmpFile.Close()); err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, fmt.Errorf("error buffering GitHub commit archive: %w", err)
	}

	slog.DebugContext(ctx, "Downloaded GitHub commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit, "bytes", written)

	buffered, err := filesystem.GetBlobFromOSPath(tmpFile.Name())
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, fmt.Errorf("error creating blob from buffered GitHub commit archive: %w", err)
	}
	buffered.SetMediaType(MediaTypeTGZ)

	return buffered, nil
}
