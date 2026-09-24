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
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
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

// PeekRequest describes the representation-selecting inputs of the artifact
// download so the HEAD peek asks the source for the same representation the
// body download would fetch. Response-varying headers (e.g. Accept,
// Authorization is applied separately), the verb, an optional body, and the
// access spec's NoRedirect flag are all mirrored.
type PeekRequest struct {
	// URL is the artifact URL (http/https).
	URL string
	// Header carries the same request headers as the body download so a
	// header-selected representation is HEAD-probed consistently.
	Header map[string][]string
	// Body is the optional request body mirrored from the download request.
	Body []byte
	// NoRedirect disables following redirects, mirroring the access spec.
	NoRedirect bool
}

// Peek resolves policy against the source WITHOUT downloading the body: a
// single credentialed HEAD harvests response headers for httpHeader sources.
// Returns the first advertised digest whose algorithm appears in prefer; empty
// prefer means [checksum.All].
//
// The HEAD mirrors the artifact download's representation-selecting headers and
// body so a header-selected resource is probed as the bytes that would actually
// be downloaded. Redirects are handled credential-safely: when the access spec
// sets NoRedirect, or when credentials are attached, redirects are refused so a
// 3xx cannot forward the Authorization header across an origin change (e.g.
// HTTPS→HTTP). Otherwise same-origin redirects are followed.
//
// Used by the Wget/v1 access-side digest processor to pin the resource digest
// from what the source claims, without fetching or hashing the body.
func Peek(
	ctx context.Context,
	baseClient *http.Client,
	credentials runtime.Typed,
	req PeekRequest,
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
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, rerr := http.NewRequestWithContext(ctx, http.MethodHead, req.URL, body)
	if rerr != nil {
		return checksum.Expected{}, false, fmt.Errorf("cannot build HEAD request for %q: %w", req.URL, rerr)
	}
	for k, vals := range req.Header {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}
	client := baseClient
	if credentials != nil {
		credentialedClient := baseClient
		if err := download.ApplyCredentials(ctx, httpReq, &credentialedClient, credentials); err != nil {
			return checksum.Expected{}, false, fmt.Errorf("cannot apply credentials for checksum peek: %w", err)
		}
		client = credentialedClient
	}
	// Refuse redirects when the access spec forbids them, or when credentials
	// are attached: a followed 3xx would otherwise forward Authorization across
	// an origin change (e.g. HTTPS→HTTP). Stopping at the first response keeps
	// the peek credential-safe and consistent with the body download.
	if req.NoRedirect || credentials != nil {
		clone := *client
		clone.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		client = &clone
	}
	if resp, herr := client.Do(httpReq); herr == nil {
		headers = resp.Header
		_ = resp.Body.Close()
	} else {
		slog.DebugContext(ctx, "httpverify: HEAD failed; falling through with empty headers",
			"url", req.URL, "err", herr)
	}

	return checksum.ResolveAdvertised(ctx, policy, checksum.Input{
		URL:     req.URL,
		Headers: headers,
	}, prefer)
}

// PolicyForMode adapts a wire [checksumhttpv1alpha1.ChecksumMode] to the
// checksum package's Policy for a body-verification path (input method and the
// by-value access transfer). Require and Prefer verify the downloaded bytes
// against the built-in source set — Require fails when no checksum is
// advertised, Prefer records SHA-256 unverified — while Skip performs no
// verification (ok=false). The empty mode resolves to Prefer.
func PolicyForMode(mode checksumhttpv1alpha1.ChecksumMode) (checksum.Policy, bool) {
	switch mode.Normalize() {
	case checksumhttpv1alpha1.ChecksumModeRequire:
		return checksum.Policy{Sources: checksum.BuiltinSources(), OnMissing: checksum.Fail}, true
	case checksumhttpv1alpha1.ChecksumModePrefer:
		return checksum.Policy{Sources: checksum.BuiltinSources(), OnMissing: checksum.Compute}, true
	default: // Skip
		return checksum.Policy{}, false
	}
}

// DigestAlgorithms maps a policy's required algorithms to download digest
// options keyed by OCM name, so the download computes them all in one pass.
func DigestAlgorithms(policy checksum.Policy) []download.DigestAlgorithm {
	required := checksum.RequiredAlgorithms(policy)
	out := make([]download.DigestAlgorithm, 0, len(required))
	for _, alg := range required {
		out = append(out, download.DigestAlgorithm{
			Name: alg.OCMName,
			New:  alg.New,
		})
	}
	return out
}
