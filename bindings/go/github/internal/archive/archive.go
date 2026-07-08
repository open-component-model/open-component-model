// Package archive fetches the source archive of a GitHub repository at a
// pinned commit via the GitHub REST API. The returned bytes are the exact
// gzipped tar archive GitHub serves, so that the resource content and its
// generic blob digest match what old OCM stored for a gitHub access.
package archive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v66/github"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MediaTypeTGZ is the media type of the GitHub source archive. It matches the
// MIME_TGZ old OCM assigned to the github access blob.
const MediaTypeTGZ = "application/x-tgz"

const (
	credentialKeyToken       = "token"
	credentialKeyAccessToken = "accessToken"
)

// archiveMaxRedirects bounds redirect following when resolving the archive
// link. GitHub answers with a 302 that GetArchiveLink returns as a Location
// without following; this only guards against a 301 chain.
const archiveMaxRedirects = 1

var credentialScheme = runtime.NewScheme()

func init() {
	credv1.MustRegister(credentialScheme)
}

// TokenFromCredentials extracts a GitHub access token from OCM credentials.
// A nil input or an empty type means anonymous access (empty token, no error).
// Credentials that are present but carry no usable token are rejected rather
// than silently downgraded to an unauthenticated request.
func TokenFromCredentials(credentials runtime.Typed) (string, error) {
	if credentials == nil || credentials.GetType().String() == "" {
		return "", nil
	}

	typed, err := credentialScheme.NewObject(credentials.GetType())
	if err != nil {
		return "", fmt.Errorf("error converting credential type: %w", err)
	}
	if err := credentialScheme.Convert(credentials, typed); err != nil {
		return "", fmt.Errorf("error converting credential type: %w", err)
	}
	direct, ok := typed.(*credv1.DirectCredentials)
	if !ok {
		return "", fmt.Errorf("unsupported credential type for github access: %v", credentials.GetType())
	}

	if token := direct.Properties[credentialKeyToken]; token != "" {
		return token, nil
	}
	if token := direct.Properties[credentialKeyAccessToken]; token != "" {
		return token, nil
	}
	return "", fmt.Errorf("credentials were provided but contain no github token; refusing to fall back to anonymous access")
}

// ParseOwnerRepo extracts the owner and repository from a GitHub repository URL
// of the form <host>/<owner>/<repo> (with or without a scheme or .git suffix).
func ParseOwnerRepo(repoURL string) (owner, repo string, err error) {
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

// NewGitHubClient builds a GitHub REST client for repoURL, authenticated with
// token when non-empty. For github.com the public API is used; for any other
// host, or when apiHostname is set, the client targets that GitHub Enterprise
// API host.
func NewGitHubClient(repoURL, apiHostname, token string, httpClient *http.Client) (*github.Client, error) {
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

// FetchCommitArchive resolves the archive link for the given commit via the
// GitHub REST API and downloads the gzipped tar archive, returning its raw
// bytes. httpClient, when nil, defaults to http.DefaultClient for the archive
// download (the link is a short-lived, pre-signed URL that needs no auth).
func FetchCommitArchive(ctx context.Context, gh *github.Client, httpClient *http.Client, owner, repo, commit string) ([]byte, error) {
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status downloading github archive %s/%s@%s: %s", owner, repo, commit, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading github archive: %w", err)
	}
	return data, nil
}

// ResolveCommit resolves a git reference (a branch, tag, or fully qualified
// ref like refs/heads/main) to its full commit SHA via the GitHub REST API,
// authenticated with token when non-empty.
func ResolveCommit(ctx context.Context, repoURL, apiHostname, ref, token string) (string, error) {
	owner, repo, err := ParseOwnerRepo(repoURL)
	if err != nil {
		return "", err
	}
	gh, err := NewGitHubClient(repoURL, apiHostname, token, nil)
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
// commit, authenticated with token when non-empty.
func Fetch(ctx context.Context, repoURL, apiHostname, commit, token string) ([]byte, error) {
	owner, repo, err := ParseOwnerRepo(repoURL)
	if err != nil {
		return nil, err
	}
	gh, err := NewGitHubClient(repoURL, apiHostname, token, nil)
	if err != nil {
		return nil, err
	}
	return FetchCommitArchive(ctx, gh, nil, owner, repo, commit)
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
