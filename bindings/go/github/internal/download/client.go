package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v92/github"

	"ocm.software/open-component-model/bindings/go/credentials"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

// defaultHTTPClient is the client used for both the GitHub REST calls and the
// archive download when the caller supplies none. It comes from the OCM http
// binding rather than http.DefaultClient so that retries and transport
// timeouts apply. Per-host timeout and TLS configuration is not plumbed
// through yet, so the binding's defaults apply.
func defaultHTTPClient() *http.Client {
	return ocmhttp.New()
}

// archiveMaxRedirects bounds redirect following when resolving the archive
// link. GitHub answers with a 302 that GetArchiveLink returns as a Location
// without following; this only guards against a 301 chain.
const archiveMaxRedirects = 1

// ownerRepo extracts the owner and repository from a parsed GitHub repository
// URL, whose path must have the form <owner>/<repo> (with or without a .git
// suffix). repoURL is the caller's original, un-normalized URL, used only so a
// rejection quotes the string the caller actually supplied.
func ownerRepo(u *url.URL, repoURL string) (owner, repo string, err error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository url %q must have the form <host>/<owner>/<repo>", repoURL)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// clientFor builds a GitHub REST client for the repository at u. For github.com
// the public API is used; for any other host, or when the access sets
// APIHostname, the client targets that GitHub Enterprise API host. httpClient,
// when nil, falls back to defaultHTTPClient.
//
// The owner and repository coordinates the API calls need come from ownerRepo,
// which the caller applies to the same parsed URL.
//
// Credentials that are nil, or that carry no token, leave the client anonymous
// rather than failing: like the helm and OCI bindings, a token-less credential
// falls back to an unauthenticated request. The fallback is silent, so a token
// that never reached the client makes a private repository answer 404 rather
// than an auth error.
//
// The token is served per host by credentialSource instead of being handed to
// go-github, which attaches it to whatever host the client ends up addressing.
func clientFor(gitHub *v1.GitHub, u *url.URL, creds *credsv1.GitHubCredentials, httpClient *http.Client, resolver credentials.Resolver) (*github.Client, error) {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}

	var enterpriseURLs []github.ClientOptionsFunc
	if gitHub.APIHostname != "" || u.Hostname() != "github.com" {
		enterpriseHost := gitHub.APIHostname
		if enterpriseHost == "" {
			enterpriseHost = u.Host
		}
		base := (&url.URL{Scheme: u.Scheme, Host: enterpriseHost}).String()
		enterpriseURLs = append(enterpriseURLs, github.WithEnterpriseURLs(base, base))
	}

	// WithHTTPClient copies the supplied client, so the shared, pooled client
	// stays untouched and a token cannot leak across downloads
	// (TestFetch_SharedClientDoesNotLeakToken pins this against future
	// go-github bumps).
	client := ocmhttp.WithCredentialSource(httpClient, credentialSource(u, creds, resolver))
	opts := append([]github.ClientOptionsFunc{github.WithHTTPClient(client)}, enterpriseURLs...)
	gh, err := github.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("error building github client for %q: %w", gitHub.RepoURL, err)
	}
	return gh, nil
}

// credentialSource serves the token every request the GitHub client makes
// carries, for the host that request addresses.
//
// The repository host, and any subdomain of it, is served the token the caller
// already resolved: the consumer identity comes from repoUrl, and the public
// API lives on api.github.com while the token is configured for github.com.
//
// Any other host is a host this access steers to on its own — the APIHostname
// override, or a redirect — and gets whatever the configuration holds for it
// under the same GitHubRepository consumer type. Without a resolver, or with
// nothing configured for that host, the request goes out unauthenticated
// rather than carrying a token resolved for somewhere else.
func credentialSource(u *url.URL, creds *credsv1.GitHubCredentials, resolver credentials.Resolver) ocmhttp.CredentialSource {
	var upfront ocmhttp.Credential
	if creds != nil && creds.Token != "" {
		upfront = ocmhttp.BearerToken(creds.Token)
	}
	return func(ctx context.Context, host string) (ocmhttp.Credential, error) {
		if ocmhttp.BoundToHost(u.Host, host) {
			return upfront, nil
		}
		if resolver == nil {
			return nil, nil
		}
		identity, err := githubinternal.CredentialConsumerIdentity((&url.URL{Scheme: u.Scheme, Host: host}).String())
		if err != nil {
			return nil, fmt.Errorf("error building a github consumer identity for %q: %w", host, err)
		}
		resolved, err := resolver.Resolve(ctx, identity)
		if err != nil {
			if errors.Is(err, credentials.ErrNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("error resolving github credentials for %q: %w", host, err)
		}
		hostCreds, err := credsv1.ConvertToGitHubCredentials(resolved)
		if err != nil {
			return nil, fmt.Errorf("error converting the github credentials of %q: %w", host, err)
		}
		if hostCreds == nil || hostCreds.Token == "" {
			return nil, nil
		}
		return ocmhttp.BearerToken(hostCreds.Token), nil
	}
}

// ResolveCommit resolves the access's git reference (a branch, tag, or fully
// qualified ref like refs/heads/main) to its full commit SHA via the GitHub
// REST API. credentials and httpClient may be nil; see clientFor.
func ResolveCommit(ctx context.Context, gitHub *v1.GitHub, creds *credsv1.GitHubCredentials, httpClient *http.Client, resolver credentials.Resolver) (string, error) {
	u, err := parseRepoURL(gitHub.RepoURL)
	if err != nil {
		return "", err
	}
	owner, repo, err := ownerRepo(u, gitHub.RepoURL)
	if err != nil {
		return "", err
	}
	gh, err := clientFor(gitHub, u, creds, httpClient, resolver)
	if err != nil {
		return "", err
	}
	sha, _, err := gh.Repositories.GetCommitSHA1(ctx, owner, repo, gitHub.Ref, "")
	if err != nil {
		return "", fmt.Errorf("error resolving github ref %q for %s/%s: %w", gitHub.Ref, owner, repo, err)
	}
	return sha, nil
}

// fetch resolves the archive link for the access's pinned commit via the GitHub
// REST API and starts downloading the gzipped tar archive, returning the
// response body for the caller to close. fetch does not buffer the body; how
// it is consumed is the caller's decision, not part of this contract.
//
// The same client serves the API call and the archive download; the link is a
// short-lived, pre-signed URL that needs no auth.
func fetch(ctx context.Context, gitHub *v1.GitHub, creds *credsv1.GitHubCredentials, httpClient *http.Client, resolver credentials.Resolver) (io.ReadCloser, error) {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	u, err := parseRepoURL(gitHub.RepoURL)
	if err != nil {
		return nil, err
	}
	owner, repo, err := ownerRepo(u, gitHub.RepoURL)
	if err != nil {
		return nil, err
	}
	gh, err := clientFor(gitHub, u, creds, httpClient, resolver)
	if err != nil {
		return nil, err
	}

	commit := gitHub.Commit
	link, _, err := gh.Repositories.GetArchiveLink(ctx, owner, repo, github.Tarball,
		&github.RepositoryContentGetOptions{Ref: commit}, archiveMaxRedirects)
	if err != nil {
		// The failure can quote the Location header this call was reading, and
		// for a private repository that header is a signed link; see redact.
		return nil, fmt.Errorf("error resolving github archive link for %s/%s@%s: %w", owner, repo, commit, redact(err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating github archive download request for %s/%s@%s: %w", owner, repo, commit, redact(err))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error downloading github archive %s/%s@%s: %w", owner, repo, commit, redact(err))
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected status downloading github archive %s/%s@%s: %s", owner, repo, commit, resp.Status)
	}

	return resp.Body, nil
}

// parseRepoURL parses repoURL, defaulting the scheme to https when absent.
func parseRepoURL(repoURL string) (*url.URL, error) {
	unparsed := repoURL
	if !strings.HasPrefix(unparsed, "http://") && !strings.HasPrefix(unparsed, "https://") {
		unparsed = "https://" + unparsed
	}
	u, err := url.Parse(unparsed)
	if err != nil {
		return nil, fmt.Errorf("error parsing repository url %q: %w", repoURL, err)
	}
	return u, nil
}
