package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v89/github"
	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/github/internal/verify"
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
func clientFor(gitHub *v1.GitHub, u *url.URL, credentials *credsv1.GitHubCredentials, httpClient *http.Client) (*github.Client, error) {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	// WithHTTPClient copies the supplied client before NewClient wraps its
	// Transport for auth, so the shared, pooled client stays untouched and a
	// token cannot leak across downloads (TestFetch_SharedClientDoesNotLeakToken
	// pins this against future go-github bumps).
	opts := []github.ClientOptionsFunc{github.WithHTTPClient(httpClient)}
	if credentials != nil && credentials.Token != "" {
		opts = append(opts, github.WithAuthToken(credentials.Token))
	}

	if gitHub.APIHostname != "" || u.Hostname() != "github.com" {
		enterpriseHost := gitHub.APIHostname
		if enterpriseHost == "" {
			enterpriseHost = u.Host
		}
		base := (&url.URL{Scheme: u.Scheme, Host: enterpriseHost}).String()
		opts = append(opts, github.WithEnterpriseURLs(base, base))
	}

	gh, err := github.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("error building github client for %q: %w", gitHub.RepoURL, err)
	}
	return gh, nil
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
// REST API and starts downloading the gzipped tar archive, returning it as a
// streaming [verify.VerifiedStreamBlob] (media type application/x-tgz). The
// response body is not buffered: the blob digests the bytes as the consumer
// reads, and closing its reader verifies expected — empty means the digest is
// only computed.
//
// The same client serves the API call and the archive download; the link is a
// short-lived, pre-signed URL that needs no auth.
func fetch(ctx context.Context, gitHub *v1.GitHub, credentials *credsv1.GitHubCredentials, httpClient *http.Client, expected godigest.Digest) (blob.ReadOnlyBlob, error) {
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
		return nil, fmt.Errorf("error resolving github archive link for %s/%s@%s: %w", owner, repo, commit, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating github archive download request for %s/%s@%s: %w", owner, repo, commit, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// For a private repository GitHub signs the link with a short-lived token
		// in its query, and the *url.Error net/http returns renders the full
		// request URL. Report the cause against the commit coordinates instead,
		// so the token cannot reach a log.
		return nil, fmt.Errorf("error downloading github archive %s/%s@%s: %w", owner, repo, commit, errors.Unwrap(err))
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected status downloading github archive %s/%s@%s: %s", owner, repo, commit, resp.Status)
	}

	archive, err := verify.NewVerifiedStreamBlob(resp.Body, expected)
	if err != nil {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("invalid expected digest for github archive %s/%s@%s: %w", owner, repo, commit, err)
	}
	archive.SetMediaType(MediaTypeTGZ)
	return archive, nil
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
