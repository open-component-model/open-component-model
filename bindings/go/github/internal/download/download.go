// Package download fetches the source archive of a GitHub repository at a
// pinned commit via the GitHub REST API and returns it as an in-memory blob.
// The archive bytes are the exact gzipped tar GitHub serves, so that the
// resource content and its generic blob digest match what old OCM stored for
// a GitHub access.
package download

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
)

// MediaTypeTGZ is the media type of the GitHub source archive. It matches the
// MIME_TGZ old OCM assigned to the github access blob.
const MediaTypeTGZ = "application/x-tgz"

// Download validates the GitHub access and fetches the source archive of its
// pinned commit, returning it as a gzipped tar blob (media type
// application/x-tgz) buffered eagerly in memory — the helm binding's pattern.
// The returned blob is self-contained: it satisfies the full
// [blob.ReadOnlyBlob] contract (any number of readers, each from the start),
// needs no cleanup, and holds the whole archive in memory until released.
// An access without a resolved commit is rejected: a bare ref is mutable and
// cannot be materialized reproducibly.
//
// credentials and httpClient may be nil; see clientFor.
func Download(ctx context.Context, gitHub *v1.GitHub, credentials *credsv1.GitHubCredentials, httpClient *http.Client) (blob.ReadOnlyBlob, error) {
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

	archive := inmemory.New(stream, inmemory.WithMediaType(MediaTypeTGZ))
	// Load eagerly so the network stream can be closed on return and a
	// download failure surfaces here instead of on the first read.
	if err := archive.Load(); err != nil {
		return nil, fmt.Errorf("error buffering GitHub commit archive: %w", err)
	}

	slog.DebugContext(ctx, "Downloaded GitHub commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit, "bytes", archive.Size())

	return archive, nil
}
