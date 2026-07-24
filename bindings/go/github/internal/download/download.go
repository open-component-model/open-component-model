// Package download fetches the source archive of a GitHub repository at a
// pinned commit via the GitHub REST API and streams it as a blob that computes
// its digest on the fly. The archive bytes are the exact gzipped tar GitHub
// serves, so that the resource content and its generic blob digest match what
// old OCM stored for a GitHub access.
package download

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
)

// MediaTypeTGZ is the media type of the GitHub source archive. It matches the
// MIME_TGZ old OCM assigned to the github access blob.
const MediaTypeTGZ = "application/x-tgz"

// HashAlgorithmSHA256 and NormalisationGenericBlobDigestV1 are the descriptor
// spelling of the digest this binding produces: a SHA-256 over the exact
// archive bytes, matching old OCM's default blob digester.
const (
	HashAlgorithmSHA256              = "SHA-256"
	NormalisationGenericBlobDigestV1 = "genericBlobDigest/v1"
)

// Download validates the GitHub access and starts fetching the source archive
// of its pinned commit, returning a single-use streaming blob (a
// verify.VerifiedStreamBlob, media type application/x-tgz) that serves the
// gzipped tar directly from GitHub while computing its SHA-256 digest on the
// fly. Nothing is buffered; the consumer must read and close the blob's
// reader exactly once.
//
// expected, when non-empty, is the digest the archive bytes must hash to;
// the blob's reader verifies it on Close after the stream has been fully
// read. An empty expected only computes the digest without verifying.
//
// An access without a resolved commit is rejected: a bare ref is mutable and
// cannot be materialized reproducibly.
//
// credentials and httpClient may be nil; see clientFor.
func Download(ctx context.Context, gitHub *v1.GitHub, credentials *credsv1.GitHubCredentials, httpClient *http.Client, expected godigest.Digest) (blob.ReadOnlyBlob, error) {
	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid GitHub access: %w", err)
	}
	if gitHub.Commit == "" {
		return nil, fmt.Errorf("GitHub access requires a pinned commit to download; ref %q has no resolved commit", gitHub.Ref)
	}
	// A malformed expected digest is rejected before any network work rather
	// than silently skipping the verification the caller asked for.
	if expected != "" {
		if err := expected.Validate(); err != nil {
			return nil, fmt.Errorf("invalid expected digest for GitHub commit archive: %w", err)
		}
	}

	slog.DebugContext(ctx, "Streaming GitHub commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit)

	return fetch(ctx, gitHub, credentials, httpClient, expected)
}
