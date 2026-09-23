// Package httpverify runs a resolved [checksum.Policy] over a downloaded blob
// or a source's advertised headers, using OCM credentials for the HEAD peek.
//
// Used by both the wget input method and the wget access resource repository
// so both paths verify against the same policy in the same way.
//
// The reduced checksum configuration verifies only against response headers
// (RFC 9530 Content-Digest and the x-checksum-* family), so no sibling
// checksum URL is ever fetched: Verify reads the already-downloaded response,
// and Peek issues a single credentialed HEAD to the artifact URL itself.
package httpverify

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
)

// Verify resolves policy against the already-downloaded blob. A checksum
// advertised in the response headers whose value mismatches the downloaded
// bytes is a hard error. The client and credentials are unused (headers are
// read from data) and kept for signature symmetry with [Peek].
func Verify(
	ctx context.Context,
	_ *http.Client,
	_ runtime.Typed,
	artifactURL string,
	policy checksum.Policy,
	data *download.Blob,
) error {
	_, _, err := checksum.Resolve(ctx, policy, checksum.Input{
		URL:      artifactURL,
		Headers:  data.Headers(),
		Computed: data.Digests(),
	})
	return err
}

// Peek resolves policy against the source WITHOUT downloading the body: a
// single credentialed HEAD harvests response headers for httpHeader sources.
// Returns the first advertised digest whose algorithm appears in prefer; empty
// prefer means [checksum.All].
//
// Used by the Wget/v1 access-side digest processor to pin the resource digest
// from what the source claims, without fetching or hashing the body.
func Peek(
	ctx context.Context,
	baseClient *http.Client,
	credentials runtime.Typed,
	artifactURL string,
	policy checksum.Policy,
	prefer []checksum.Algorithm,
) (checksum.Expected, bool, error) {
	if baseClient == nil {
		baseClient = http.DefaultClient
	}

	// HEAD the artifact URL to harvest response headers. A failure (e.g. 405)
	// is not fatal: fall through with empty headers so ResolveAdvertised can
	// report "nothing advertised".
	headers := http.Header{}
	req, rerr := http.NewRequestWithContext(ctx, http.MethodHead, artifactURL, nil)
	if rerr != nil {
		return checksum.Expected{}, false, fmt.Errorf("cannot build HEAD request for %q: %w", artifactURL, rerr)
	}
	client := baseClient
	if credentials != nil {
		credentialedClient := baseClient
		if err := download.ApplyCredentials(ctx, req, &credentialedClient, credentials); err != nil {
			return checksum.Expected{}, false, fmt.Errorf("cannot apply credentials for checksum peek: %w", err)
		}
		client = credentialedClient
	}
	if resp, herr := client.Do(req); herr == nil {
		headers = resp.Header
		_ = resp.Body.Close()
	} else {
		slog.DebugContext(ctx, "httpverify: HEAD failed; falling through with empty headers",
			"url", artifactURL, "err", herr)
	}

	return checksum.ResolveAdvertised(ctx, policy, checksum.Input{
		URL:     artifactURL,
		Headers: headers,
	}, prefer)
}
