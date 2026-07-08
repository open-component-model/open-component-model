// Package archive fetches the source archive of a GitHub repository at a
// pinned commit via the GitHub REST API. The returned bytes are the exact
// gzipped tar archive GitHub serves, so that the resource content and its
// generic blob digest match what old OCM stored for a GitHub access.
package archive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v66/github"
)

// MediaTypeTGZ is the media type of the GitHub source archive. It matches the
// MIME_TGZ old OCM assigned to the github access blob.
const MediaTypeTGZ = "application/x-tgz"

// archiveMaxRedirects bounds redirect following when resolving the archive
// link. GitHub answers with a 302 that GetArchiveLink returns as a Location
// without following; this only guards against a 301 chain.
const archiveMaxRedirects = 1

// parseOwnerRepo extracts the owner and repository from a GitHub repository URL
// of the form <host>/<owner>/<repo> (with or without a scheme or .git suffix).
func parseOwnerRepo(repoURL string) (owner, repo string, err error) {
	u, err := parseRepoURL(repoURL)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository url %q must have the form <host>/<owner>/<repo>", repoURL)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// newGitHubClient builds a GitHub REST client for repoURL, authenticated with
// token when non-empty. For github.com the public API is used; for any other
// host, or when apiHostname is set, the client targets that GitHub Enterprise
// API host.
func newGitHubClient(repoURL, apiHostname, token string, httpClient *http.Client) (*github.Client, error) {
	u, err := parseRepoURL(repoURL)
	if err != nil {
		return nil, err
	}

	client := github.NewClient(httpClient)
	if token != "" {
		client = client.WithAuthToken(token)
	}

	if apiHostname == "" && u.Hostname() == "github.com" {
		return client, nil
	}

	enterpriseHost := apiHostname
	if enterpriseHost == "" {
		enterpriseHost = u.Host
	}
	base := (&url.URL{Scheme: u.Scheme, Host: enterpriseHost}).String()
	client, err = client.WithEnterpriseURLs(base, base)
	if err != nil {
		return nil, fmt.Errorf("error configuring github enterprise client for %q: %w", enterpriseHost, err)
	}
	return client, nil
}

// clientFor parses repoURL into owner and repository and builds a GitHub REST
// client for it, authenticated with token when non-empty.
func clientFor(repoURL, apiHostname, token string) (gh *github.Client, owner, repo string, err error) {
	owner, repo, err = parseOwnerRepo(repoURL)
	if err != nil {
		return nil, "", "", err
	}
	gh, err = newGitHubClient(repoURL, apiHostname, token, nil)
	if err != nil {
		return nil, "", "", err
	}
	return gh, owner, repo, nil
}

// fetchCommitArchive resolves the archive link for the given commit via the
// GitHub REST API and starts downloading the gzipped tar archive, returning
// the response body. The caller must close it. The body is streamed rather
// than read into memory so the archive can be buffered to the filesystem.
//
// httpClient, when nil, defaults to http.DefaultClient for the archive
// download (the link is a short-lived, pre-signed URL that needs no auth).
func fetchCommitArchive(ctx context.Context, gh *github.Client, httpClient *http.Client, owner, repo, commit string) (io.ReadCloser, error) {
	link, _, err := gh.Repositories.GetArchiveLink(ctx, owner, repo, github.Tarball,
		&github.RepositoryContentGetOptions{Ref: commit}, archiveMaxRedirects)
	if err != nil {
		return nil, fmt.Errorf("error resolving github archive link for %s/%s@%s: %w", owner, repo, commit, err)
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
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

// ResolveCommit resolves a git reference (a branch, tag, or fully qualified
// ref like refs/heads/main) to its full commit SHA via the GitHub REST API,
// authenticated with token when non-empty.
func ResolveCommit(ctx context.Context, repoURL, apiHostname, ref, token string) (string, error) {
	gh, owner, repo, err := clientFor(repoURL, apiHostname, token)
	if err != nil {
		return "", err
	}
	sha, _, err := gh.Repositories.GetCommitSHA1(ctx, owner, repo, ref, "")
	if err != nil {
		return "", fmt.Errorf("error resolving github ref %q for %s/%s: %w", ref, owner, repo, err)
	}
	return sha, nil
}

// Fetch composes client construction and archive download for repoURL at
// commit, authenticated with token when non-empty. The caller must close the
// returned stream.
func Fetch(ctx context.Context, repoURL, apiHostname, commit, token string) (io.ReadCloser, error) {
	gh, owner, repo, err := clientFor(repoURL, apiHostname, token)
	if err != nil {
		return nil, err
	}
	return fetchCommitArchive(ctx, gh, nil, owner, repo, commit)
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
