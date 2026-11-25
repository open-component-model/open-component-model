package catalog

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// ErrCatalogUnauthenticated reports that a registry needs credentials to list
// repositories and none were provided. GHCR cannot list packages anonymously,
// so the cataloger returns this typed error when no token is present.
var ErrCatalogUnauthenticated = errors.New("registry requires authentication to list repositories")

// githubAPIBaseURL is the base URL of the GitHub REST API. It is a
// package-private var so a unit test can point it at an httptest.Server.
var githubAPIBaseURL = "https://api.github.com"

// ghcrPageSize is the number of packages the cataloger requests per page.
// GitHub may return fewer entries per page.
const ghcrPageSize = 100

func init() {
	RegisterCataloger(ghcrFactory{})
}

// ghcrFactory builds a GitHub Container Registry cataloger for host ghcr.io.
type ghcrFactory struct{}

var _ CatalogerFactory = ghcrFactory{}

// Match reports whether host is the GitHub Container Registry host.
func (ghcrFactory) Match(host string) bool {
	return strings.ToLower(host) == "ghcr.io"
}

// New builds a GHCR cataloger. GHCR does not serve the standard Distribution
// catalog endpoint, so the cataloger uses the GitHub Packages REST API. The
// owner is the first segment of the OCM subPath. A GitHub token is required.
// New returns an error when the subPath carries no owner, and the cataloger
// returns ErrCatalogUnauthenticated when no token is present.
func (ghcrFactory) New(opts CatalogerOptions) (RepositoryCataloger, error) {
	owner := ghcrOwner(opts.SubPath)
	if owner == "" {
		return nil, fmt.Errorf("github container registry cannot list repositories without an owner: set the OCM subPath to the github owner")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(slog.String("realm", "ghcr-cataloger"), slog.String("owner", owner))

	return &ghcrCataloger{
		httpClient:  opts.HTTPClient,
		baseURL:     githubAPIBaseURL,
		owner:       owner,
		credentials: opts.Credentials,
		logger:      logger,
	}, nil
}

// ghcrCataloger lists container package names of one GitHub owner through the
// GitHub Packages REST API. It tries the organization endpoint first and falls
// back to the user endpoint on HTTP 404. It follows the Link rel="next" header
// of each page until the API reports no further page.
type ghcrCataloger struct {
	httpClient  *http.Client
	baseURL     string
	owner       string
	credentials *credsv1.OCICredentials
	logger      *slog.Logger
}

var _ RepositoryCataloger = (*ghcrCataloger)(nil)

// ghcrPackage is one entry of a GitHub container package page.
type ghcrPackage struct {
	Name string `json:"name"`
}

// Repositories lists the repository names of the owner as <owner>/<name>. The
// generic ComponentLister applies the component-descriptors/ filter and the
// subPath strip, so this method returns the raw repository names. GitHub
// paginates through the Link rel="next" header, so the cataloger follows it
// internally and ignores the last cursor.
func (c *ghcrCataloger) Repositories(ctx context.Context, _ string, fn func(repos []string) error) error {
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	token := ghcrToken(c.credentials)
	if token == "" {
		return ErrCatalogUnauthenticated
	}

	// Try the organization endpoint first. When the owner is a user and not an
	// organization, GitHub answers the org endpoint with HTTP 404, so the
	// cataloger falls back to the user endpoint.
	base := strings.TrimRight(c.baseURL, "/")
	orgURL := fmt.Sprintf("%s/orgs/%s/packages?package_type=container&per_page=%d",
		base, url.PathEscape(c.owner), ghcrPageSize)
	userURL := fmt.Sprintf("%s/users/%s/packages?package_type=container&per_page=%d",
		base, url.PathEscape(c.owner), ghcrPageSize)

	next, status, err := c.fetchPage(ctx, httpClient, orgURL, token, fn)
	if err == nil && status == http.StatusNotFound {
		c.logger.DebugContext(ctx, "org packages endpoint returned 404, falling back to user endpoint")
		next, _, err = c.fetchPage(ctx, httpClient, userURL, token, fn)
	}
	if err != nil {
		return err
	}

	for next != "" {
		next, _, err = c.fetchPage(ctx, httpClient, next, token, fn)
		if err != nil {
			return err
		}
	}

	return nil
}

// fetchPage fetches one package page from rawURL, decodes it, and calls fn with
// the assembled <owner>/<name> repository names. It returns the URL of the next
// page from the Link rel="next" header, the HTTP status code, and an error. On
// HTTP 404 it returns the status without calling fn so the caller can fall back
// to another endpoint.
func (c *ghcrCataloger) fetchPage(ctx context.Context, httpClient *http.Client, rawURL, token string, fn func(repos []string) error) (next string, status int, err error) {
	c.logger.DebugContext(ctx, "listing github container packages", "url", rawURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create github packages request for %q: %w", rawURL, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to request github packages from %q: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", resp.StatusCode, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", resp.StatusCode, fmt.Errorf("%w: github packages returned status %d for %q", ErrCatalogUnauthenticated, resp.StatusCode, rawURL)
	}
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, fmt.Errorf("github packages returned unexpected status %d for %q", resp.StatusCode, rawURL)
	}

	var packages []ghcrPackage
	if err := json.UnmarshalRead(resp.Body, &packages); err != nil {
		return "", resp.StatusCode, fmt.Errorf("failed to decode github packages page: %w", err)
	}

	repos := make([]string, 0, len(packages))
	for _, p := range packages {
		repos = append(repos, c.owner+"/"+p.Name)
	}

	if err := fn(repos); err != nil {
		return "", resp.StatusCode, err
	}

	return nextLink(resp.Header.Get("Link")), resp.StatusCode, nil
}

// nextLink returns the URL of the next page from a GitHub Link header, or an
// empty string when the header carries no rel="next" entry. A Link header is a
// comma-separated list of entries, each shaped as <url>; rel="name".
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}

		rawURL := strings.TrimSpace(segments[0])
		rawURL = strings.TrimPrefix(rawURL, "<")
		rawURL = strings.TrimSuffix(rawURL, ">")

		for _, attr := range segments[1:] {
			attr = strings.TrimSpace(attr)
			if attr == `rel="next"` || attr == "rel=next" {
				return rawURL
			}
		}
	}
	return ""
}

// ghcrToken returns the GitHub token from the credentials. It prefers the
// AccessToken and falls back to the Password. It returns an empty string when
// no token is present.
func ghcrToken(credentials *credsv1.OCICredentials) string {
	if credentials == nil {
		return ""
	}
	if credentials.AccessToken != "" {
		return credentials.AccessToken
	}
	return credentials.Password
}

// ghcrOwner returns the GitHub owner of the OCM subPath. It is the first path
// segment of the subPath, or an empty string when the subPath carries no
// segment.
func ghcrOwner(subPath string) string {
	return firstPathSegment(subPath)
}
