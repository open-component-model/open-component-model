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
	"regexp"

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
		// The read runs against the signed archive link, so a transport failure
		// here can carry it the same way the download itself can; see redact.
		return nil, fmt.Errorf("error buffering GitHub commit archive: %w", redact(err))
	}

	slog.DebugContext(ctx, "Downloaded GitHub commit archive", "repoUrl", gitHub.RepoURL, "commit", gitHub.Commit, "bytes", archive.Size())

	return archive, nil
}

// redactedToken stands in for the signed link's token in a rendered error. It
// is not a valid token, so a reader can tell the value was removed rather than
// never set.
const redactedToken = "REDACTED"

// queryToken matches the token parameter of a URL query as net/http renders one
// into an error message. The value runs to the next parameter, to whitespace,
// or to the quote that closes a quoted URL, which is how *url.Error and a
// quoted response header both present it. The match is case-insensitive because
// nothing guarantees the casing a GitHub Enterprise deployment redirects with.
var queryToken = regexp.MustCompile(`(?i)([?&]token=)[^&\s"]*`)

// redact returns err with the value of every token query parameter in its
// message replaced by redactedToken, or err itself when its message holds none.
//
// GitHub authorizes the archive download for a private repository with a
// short-lived token in the link's query string, and net/http renders the full
// request URL into the *url.Error it returns — so wrapping such a failure
// verbatim publishes a live credential into the caller's logs. Unwrapping the
// *url.Error is not enough on its own: go-github reads the link out of a
// Location header, and a response net/http cannot parse quotes that header,
// token and all, in the innermost error's own text.
//
// Only the token parameter is redacted, so the host, path, and any other
// parameter stay legible for diagnosis. That is scoped to what GitHub actually
// signs with today: a deployment that authorized an archive under some other
// parameter name would not be covered, and this pattern would have to grow to
// match it.
//
// The original error stays reachable through Unwrap, so a caller keeps
// errors.Is against context.Canceled and errors.As against
// *github.ErrorResponse; only the rendered text changes.
func redact(err error) error {
	msg := err.Error()
	redacted := queryToken.ReplaceAllString(msg, "${1}"+redactedToken)
	if redacted == msg {
		return err
	}
	return &redactedError{msg: redacted, cause: err}
}

// redactedError renders a scrubbed message while keeping its cause in the error
// chain.
type redactedError struct {
	msg   string
	cause error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Unwrap() error { return e.cause }
