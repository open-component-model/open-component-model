package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v66/github"

	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

// defaultHTTPClient is the client used for both the GitHub REST calls and the
// archive download when the caller supplies none. It comes from the OCM http
// binding rather than http.DefaultClient so that retries and transport
// timeouts apply. Per-host timeout and TLS configuration is not plumbed
// through yet; a nil config yields the binding's defaults.
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

// clientFor splits the access's repository URL into owner and repository and
// builds a GitHub REST client for it, authenticated with token when non-empty.
// For github.com the public API is used; for any other host, or when the
// access sets APIHostname, the client targets that GitHub Enterprise API host.
// httpClient, when nil, falls back to defaultHTTPClient.
func clientFor(gitHub *v1.GitHub, token string, httpClient *http.Client) (gh *github.Client, owner, repo string, err error) {
	u, err := parseRepoURL(gitHub.RepoURL)
	if err != nil {
		return nil, "", "", err
	}
	if owner, repo, err = ownerRepo(u, gitHub.RepoURL); err != nil {
		return nil, "", "", err
	}

	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	gh = github.NewClient(httpClient)
	if token != "" {
		gh = gh.WithAuthToken(token)
	}

	if gitHub.APIHostname == "" && u.Hostname() == "github.com" {
		return gh, owner, repo, nil
	}

	enterpriseHost := gitHub.APIHostname
	if enterpriseHost == "" {
		enterpriseHost = u.Host
	}
	base := (&url.URL{Scheme: u.Scheme, Host: enterpriseHost}).String()
	if gh, err = gh.WithEnterpriseURLs(base, base); err != nil {
		return nil, "", "", fmt.Errorf("error configuring github enterprise client for %q: %w", enterpriseHost, err)
	}
	return gh, owner, repo, nil
}

// ResolveCommit resolves the access's git reference (a branch, tag, or fully
// qualified ref like refs/heads/main) to its full commit SHA via the GitHub
// REST API, authenticated with token when non-empty. httpClient, when nil,
// falls back to defaultHTTPClient.
func ResolveCommit(ctx context.Context, gitHub *v1.GitHub, token string, httpClient *http.Client) (string, error) {
	gh, owner, repo, err := clientFor(gitHub, token, httpClient)
	if err != nil {
		return "", err
	}
	sha, _, err := gh.Repositories.GetCommitSHA1(ctx, owner, repo, gitHub.Ref, "")
	if err != nil {
		return "", fmt.Errorf("error resolving github ref %q for %s/%s: %w", gitHub.Ref, owner, repo, err)
	}
	return sha, nil
}

// fetch resolves the archive link for the access's pinned commit via the
// GitHub REST API, authenticated with token when non-empty, and starts
// downloading the gzipped tar archive, returning the response body. The caller
// must close it. The body is streamed rather than read into memory so the
// archive can be buffered to the filesystem.
//
// httpClient, when nil, falls back to defaultHTTPClient. The same client serves
// the API call and the archive download; the link is a short-lived, pre-signed
// URL that needs no auth.
func fetch(ctx context.Context, gitHub *v1.GitHub, token string, httpClient *http.Client) (io.ReadCloser, error) {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	gh, owner, repo, err := clientFor(gitHub, token, httpClient)
	if err != nil {
		return nil, err
	}

	commit := gitHub.Commit
	link, _, err := gh.Repositories.GetArchiveLink(ctx, owner, repo, github.Tarball,
		&github.RepositoryContentGetOptions{Ref: commit}, archiveMaxRedirects)
	if err != nil {
		return nil, fmt.Errorf("error resolving github archive link for %s/%s@%s: %w", owner, repo, commit, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating github archive download request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error downloading github archive: %w", err)
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
