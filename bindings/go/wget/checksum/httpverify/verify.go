// Package httpverify runs a resolved [checksum.Policy] over a downloaded blob
// or a source's advertised response headers (RFC 9530 Content-Digest and the
// x-checksum-* family). Verify reads an already-downloaded response; Peek
// issues a single credentialed HEAD. Both the wget input method and the access
// resource repository use it so both paths verify identically.
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
// [checksum.All]). Redirects are refused when NoRedirect is set or credentials
// are attached, so a 3xx cannot forward Authorization across an origin change.
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
