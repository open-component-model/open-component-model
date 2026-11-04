package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v92/github"

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
// A token is bound to the repository host rather than handed to go-github,
// which would attach it to whatever host the client ends up addressing:
// GetResourceCredentialConsumerIdentity resolves the token from repoUrl alone,
// while APIHostname — part of the same, untrusted access spec — decides where
// the API calls go. An access that points its API at an unrelated host is
// rejected instead of being served anonymously, so a GitHub Enterprise
// deployment with a misconfigured apiHostname fails with its cause named
// rather than with a 404.
func clientFor(gitHub *v1.GitHub, u *url.URL, credentials *credsv1.GitHubCredentials, httpClient *http.Client) (*github.Client, error) {
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
	newClient := func(client *http.Client) (*github.Client, error) {
		opts := append([]github.ClientOptionsFunc{github.WithHTTPClient(client)}, enterpriseURLs...)
		gh, err := github.NewClient(opts...)
		if err != nil {
			return nil, fmt.Errorf("error building github client for %q: %w", gitHub.RepoURL, err)
		}
		return gh, nil
	}

	gh, err := newClient(httpClient)
	if err != nil {
		return nil, err
	}
	if credentials == nil || credentials.Token == "" {
		return gh, nil
	}

	// Ask go-github which hosts it resolved rather than deriving them again:
	// the public API and upload hosts are its defaults, not ours.
	for _, apiURL := range []string{gh.BaseURL(), gh.UploadURL()} {
		parsed, err := url.Parse(apiURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing the github api url %q of %q: %w", apiURL, gitHub.RepoURL, err)
		}
		if !ocmhttp.BoundToHost(u.Host, parsed.Host) {
			return nil, fmt.Errorf("refusing to use the github credential of %q with api host %q: "+
				"the credential is resolved for the repository host and must not be sent to another host", u.Host, parsed.Host)
		}
	}
	return newClient(ocmhttp.BindCredential(httpClient, ocmhttp.BearerToken(credentials.Token), u.Host))
}

// ResolveCommit resolves the access's git reference (a branch, tag, or fully
// qualified ref like refs/heads/main) to its full commit SHA via the GitHub
// REST API. credentials and httpClient may be nil; see clientFor.
func ResolveCommit(ctx context.Context, gitHub *v1.GitHub, credentials *credsv1.GitHubCredentials, httpClient *http.Client) (string, error) {
	u, err := parseRepoURL(gitHub.RepoURL)
	if err != nil {
		return "", err
	}
	owner, repo, err := ownerRepo(u, gitHub.RepoURL)
	if err != nil {
		return "", err
	}
	gh, err := clientFor(gitHub, u, credentials, httpClient)
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
func fetch(ctx context.Context, gitHub *v1.GitHub, credentials *credsv1.GitHubCredentials, httpClient *http.Client) (io.ReadCloser, error) {
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
	gh, err := clientFor(gitHub, u, credentials, httpClient)
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
