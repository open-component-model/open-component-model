// Package httpverify runs a resolved [checksum.Policy] over a downloaded blob
// using OCM credentials, with strict destination scoping.
//
// Used by both the wget input method and the wget access resource repository
// so both paths verify a fetched blob against the same policy in the same way.
//
// # Destination scoping (CWE-200)
//
// An externalUrl source may resolve to any URL, so credentials are attached
// only when the resolved checksum URL exactly matches the artifact URL's
// origin (same scheme, host, port) and uses HTTPS. Cross-origin or plain-HTTP
// checksum URLs get an undecorated client and no Authorization header.
// Redirects on the credentialed client are hard-disabled so a 3xx cannot
// forward Authorization to a different origin.
package httpverify

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
)

// Verify resolves policy against data using OCM credentials scoped to the
// artifact URL's origin. A nil policy is a no-op. A resolved external checksum
// whose value mismatches the downloaded bytes is a hard error.
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

	credentialedClient := baseClient
	if credentials != nil {
		bootstrap, berr := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
		if berr != nil {
			return fmt.Errorf("cannot bootstrap credentials for checksum fetch: %w", berr)
		}
		if err := download.ApplyCredentials(ctx, bootstrap, &credentialedClient, credentials); err != nil {
			return fmt.Errorf("cannot apply credentials for checksum fetch: %w", err)
		}
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
				return baseClient.Do(req) //nolint:gosec // G704: URL is user-provided; credentials stripped for non-same-origin.
			}
			throwaway := credentialedClient
			if err := download.ApplyCredentials(ctx, req, &throwaway, credentials); err != nil {
				return nil, err
			}
			return credentialedClient.Do(req) //nolint:gosec // G704: URL is user-provided; credentials attached only for same-origin HTTPS.
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

// Peek resolves policy against the source WITHOUT downloading the body: HEAD
// harvests response headers for httpHeader sources; the credential-scoped
// [checksum.ExternalFetcher] handles externalUrl sources. Returns the first
// advertised digest whose algorithm appears in prefer; empty prefer means
// [checksum.All].
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
	parsedArtifact, err := url.Parse(artifactURL)
	if err != nil {
		return checksum.Expected{}, false, fmt.Errorf("invalid artifact url %q: %w", artifactURL, err)
	}

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

	// HEAD the artifact URL to harvest response headers. A failure (e.g. 405)
	// is not fatal: fall through with empty headers so externalUrl sources can
	// still resolve.
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
		} else {
			slog.DebugContext(ctx, "httpverify: HEAD failed; falling through with empty headers",
				"url", artifactURL, "err", herr)
		}
	}

	fetcher := &checksum.ExternalFetcher{
		Client: baseClient,
		Do: func(req *http.Request) (*http.Response, error) {
			if credentials == nil || !sameOriginHTTPS(parsedArtifact, req.URL) {
				return baseClient.Do(req) //nolint:gosec // G704: URL is user-provided; credentials stripped for non-same-origin.
			}
			throwaway := credentialedClient
			if err := download.ApplyCredentials(ctx, req, &throwaway, credentials); err != nil {
				return nil, err
			}
			return credentialedClient.Do(req) //nolint:gosec // G704: URL is user-provided; credentials attached only for same-origin HTTPS.
		},
	}
	return checksum.ResolveAdvertised(ctx, policy, checksum.Input{
		URL:      artifactURL,
		Headers:  headers,
		FetchURL: fetcher.FetchURL,
	}, prefer)
}

// sameOriginHTTPS reports whether checksumURL is an https URL with the same
// host and port as artifactURL. Implicit :443 matches explicit :443.
func sameOriginHTTPS(artifactURL, checksumURL *url.URL) bool {
	if checksumURL == nil || checksumURL.Scheme != "https" || artifactURL.Scheme != "https" {
		return false
	}
	if !strings.EqualFold(artifactURL.Hostname(), checksumURL.Hostname()) {
		return false
	}
	return defaultedPort(artifactURL) == defaultedPort(checksumURL)
}

// defaultedPort returns u.Port() with the scheme default filled in.
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
