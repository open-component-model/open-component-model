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
