// Package httpverify runs a resolved [checksum.Policy] over a downloaded blob
// using OCM credentials, with strict destination scoping.
//
// It is used by both the wget input method (Wget/v1 in a component-constructor)
// and the wget access resource repository (Wget/v1 access on an existing
// component version), so those two paths verify a fetched blob against the
// same policy in the same way.
//
// Destination scoping (CWE-200 mitigation)
//
// A checksum policy's externalUrl source may resolve to any URL (the wget
// config lets operators configure per-host defaults for the wget resources
// they ingest). Sending the
// artifact's OCM credentials to arbitrary URLs would leak them. This package
// scopes credentials tightly: they are attached only when the resolved
// checksum URL exactly matches the artifact URL's origin (same scheme, host,
// port) and uses HTTPS. Cross-origin or plain-HTTP checksum URLs get an
// undecorated client and no Authorization header. Redirects on the
// credentialed client are hard-disabled so a 3xx cannot forward Authorization
// to a different origin.
package httpverify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
)

// Verify resolves policy against data using OCM credentials scoped to the
// artifact URL's origin. Same-origin HTTPS checksum requests reuse credentials;
// every other destination gets an undecorated request.
//
// A nil policy is a no-op (returns nil). A [*checksum.Policy] whose Sources
// list is empty follows the OnMissing branch and either returns nil (Compute)
// or an error (Fail). A resolved external checksum whose value mismatches the
// downloaded bytes is a hard error.
//
// baseClient is the untouched HTTP client used for cross-origin and non-HTTPS
// checksum URLs; it never sees the credentials. When nil, http.DefaultClient
// is used.
func Verify(
	ctx context.Context,
	baseClient *http.Client,
	credentials runtime.Typed,
	artifactURL string,
	policy checksum.Policy,
	data *download.Blob,
) error {
	if baseClient == nil {
		baseClient = http.DefaultClient
	}
	parsedArtifact, err := url.Parse(artifactURL)
	if err != nil {
		return fmt.Errorf("invalid artifact url %q: %w", artifactURL, err)
	}

	// Pre-materialise the mTLS-decorated client once. Its transport is only
	// used against the artifact origin; using it against unrelated hosts would
	// present the configured client certificate there.
	credentialedClient := baseClient
	if credentials != nil {
		bootstrap, berr := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
		if berr != nil {
			return fmt.Errorf("cannot bootstrap credentials for checksum fetch: %w", berr)
		}
		if err := download.ApplyCredentials(ctx, bootstrap, &credentialedClient, credentials); err != nil {
			return fmt.Errorf("cannot apply credentials for checksum fetch: %w", err)
		}
	}
	// Disable redirects on the credentialed client: a 3xx that changes origin
	// would let Go forward the Authorization header (same-host or sub-host) to
	// an attacker-controlled destination.
	if credentials != nil {
		noRedirect := *credentialedClient
		noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		credentialedClient = &noRedirect
	}

	fetcher := &checksum.ExternalFetcher{
		Client: baseClient,
		Do: func(req *http.Request) (*http.Response, error) {
			if credentials == nil || !sameOriginHTTPS(parsedArtifact, req.URL) {
				return baseClient.Do(req) //nolint:gosec // G704: URL is user-provided by design (checksumPolicy.sources[].url); credentials are stripped for non-same-origin.
			}
			throwaway := credentialedClient
			if err := download.ApplyCredentials(ctx, req, &throwaway, credentials); err != nil {
				return nil, err
			}
			return credentialedClient.Do(req) //nolint:gosec // G704: URL is user-provided by design (checksumPolicy.sources[].url); credentials are attached only for same-origin HTTPS.
		},
	}
	_, _, err = checksum.Resolve(ctx, policy, checksum.Input{
		URL:      artifactURL,
		Headers:  data.Headers(),
		Computed: data.Digests(),
		FetchURL: fetcher.FetchURL,
	})
	return err
}

// Peek resolves policy against the source WITHOUT downloading the body: it
// issues a HEAD to the artifact URL (or reuses the caller-supplied headers) to
// materialise response headers for httpHeader sources, and reuses the same
// credential-scoped [checksum.ExternalFetcher] for externalUrl sources. The
// first digest a source advertises is returned; ok is false when no source
// yields one.
//
// prefer restricts and orders which algorithms Peek accepts from the sources
// — the first entry in prefer that a source offers wins. Empty prefer means
// "any supported algorithm, strongest first" (see [checksum.All]).
//
// Peek is meant for the Wget/v1 access-side digest processor when the operator
// opts into pinning the resource digest from the source's advertised checksum;
// see the checksum-http config's [AccessDigest] surface. It does not fetch or
// hash the body.
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
	parsedArtifact, err := url.Parse(artifactURL)
	if err != nil {
		return checksum.Expected{}, false, fmt.Errorf("invalid artifact url %q: %w", artifactURL, err)
	}

	// Materialise the credentialed client once (same shape as Verify): only
	// used against the artifact origin, redirects disabled to avoid leaking
	// Authorization cross-origin.
	credentialedClient := baseClient
	if credentials != nil {
		bootstrap, berr := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
		if berr != nil {
			return checksum.Expected{}, false, fmt.Errorf("cannot bootstrap credentials for checksum peek: %w", berr)
		}
		if err := download.ApplyCredentials(ctx, bootstrap, &credentialedClient, credentials); err != nil {
			return checksum.Expected{}, false, fmt.Errorf("cannot apply credentials for checksum peek: %w", err)
		}
		noRedirect := *credentialedClient
		noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		credentialedClient = &noRedirect
	}

	// HEAD the artifact URL to harvest response headers without pulling the body.
	// Servers that reject HEAD (405) simply yield no advertised digest here —
	// the caller falls back to a downloading path.
	headers := http.Header{}
	{
		req, rerr := http.NewRequestWithContext(ctx, http.MethodHead, artifactURL, nil)
		if rerr != nil {
			return checksum.Expected{}, false, fmt.Errorf("cannot build HEAD request for %q: %w", artifactURL, rerr)
		}
		client := baseClient
		if credentials != nil {
			throwaway := credentialedClient
			if err := download.ApplyCredentials(ctx, req, &throwaway, credentials); err != nil {
				return checksum.Expected{}, false, err
			}
			client = credentialedClient
		}
		resp, herr := client.Do(req)
		if herr == nil {
			headers = resp.Header
			_ = resp.Body.Close()
		}
		// A HEAD failure is not fatal: fall through with empty headers; the
		// externalUrl branch may still resolve a sidecar. The caller handles
		// "no advertised digest" via its OnMissing branch.
	}

	fetcher := &checksum.ExternalFetcher{
		Client: baseClient,
		Do: func(req *http.Request) (*http.Response, error) {
			if credentials == nil || !sameOriginHTTPS(parsedArtifact, req.URL) {
				return baseClient.Do(req) //nolint:gosec // G704: URL is user-provided by design (checksumPolicy.sources[].url); credentials are stripped for non-same-origin.
			}
			throwaway := credentialedClient
			if err := download.ApplyCredentials(ctx, req, &throwaway, credentials); err != nil {
				return nil, err
			}
			return credentialedClient.Do(req) //nolint:gosec // G704: URL is user-provided by design (checksumPolicy.sources[].url); credentials are attached only for same-origin HTTPS.
		},
	}
	return checksum.ResolveAdvertised(ctx, policy, checksum.Input{
		URL:      artifactURL,
		Headers:  headers,
		FetchURL: fetcher.FetchURL,
	}, prefer)
}

// sameOriginHTTPS reports whether checksumURL is an https URL with the exact
// same host and port as artifactURL. Only https qualifies for credential reuse:
// http traffic can be observed and credentials must not leak on non-TLS hops.
// Port comparison uses url.URL.Port() so implicit :443 matches an explicit :443.
func sameOriginHTTPS(artifactURL, checksumURL *url.URL) bool {
	if checksumURL == nil || checksumURL.Scheme != "https" || artifactURL.Scheme != "https" {
		return false
	}
	if !strings.EqualFold(artifactURL.Hostname(), checksumURL.Hostname()) {
		return false
	}
	if defaultedPort(artifactURL) != defaultedPort(checksumURL) {
		return false
	}
	return true
}

// defaultedPort returns u.Port(), falling back to the scheme's default (443 for
// https, 80 for http). Making the default explicit lets sameOriginHTTPS treat
// `https://example.com` and `https://example.com:443` as one origin.
func defaultedPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}
