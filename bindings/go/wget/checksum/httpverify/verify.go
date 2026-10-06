// Package httpverify runs a resolved [checksum.Policy] over a downloaded blob
// or a source's advertised checksums (RFC 9530 Content-Digest, the
// x-checksum-* family and content-addressed URLs). Verify reads an
// already-downloaded response; Peek issues a single credentialed HEAD. Both
// the wget input method and the access resource repository use it so both
// paths verify identically.
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
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
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

// PeekRequest mirrors the artifact download's representation-selecting inputs
// so the HEAD probes the same representation the body download would fetch.
type PeekRequest struct {
	URL        string
	Header     map[string][]string
	Body       []byte
	NoRedirect bool
}

// Peek resolves policy against the source WITHOUT downloading the body: a
// single credentialed HEAD harvests response headers and returns the first
// advertised digest whose algorithm appears in prefer (empty means
// [checksum.All]). The HEAD follows redirects like the download does: never
// with NoRedirect, and with credentials only as [httpauth.Apply] allows. A
// HEAD that fails or does not end in a 2xx advertises nothing, so a URL
// source never vouches for content the server did not resolve.
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
	// is not fatal: it reports "nothing advertised".
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
	if req.NoRedirect {
		client = download.CloneClientWithNoRedirect(client)
	}
	if err := httpauth.Apply(ctx, httpReq, &client, credentials); err != nil {
		return checksum.Expected{}, false, fmt.Errorf("cannot apply credentials for checksum peek: %w", err)
	}
	resp, herr := client.Do(httpReq)
	if herr != nil {
		slog.DebugContext(ctx, "httpverify: HEAD failed; nothing advertised", "url", req.URL, "err", herr)
		return checksum.Expected{}, false, nil
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.DebugContext(ctx, "httpverify: HEAD returned no content; nothing advertised", "url", req.URL, "status", resp.StatusCode)
		return checksum.Expected{}, false, nil
	}

	return checksum.ResolveAdvertised(ctx, policy, checksum.Input{
		URL:     req.URL,
		Headers: resp.Header,
	}, prefer)
}

// PolicyForMode adapts a wire [checksumhttpv1alpha1.ChecksumMode] to a body-
// verification Policy: Require fails when nothing is advertised, Prefer records
// SHA-256 unverified, Skip performs no verification (ok=false). Empty is Prefer.
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
