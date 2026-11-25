package catalog

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// quayAPIBaseURL is the base URL of the Quay REST API. It is a package-private
// var so a unit test can point it at an httptest.Server.
var quayAPIBaseURL = "https://quay.io"

func init() {
	RegisterCataloger(quayFactory{})
}

// quayFactory builds a Quay cataloger for host quay.io and its subdomains.
type quayFactory struct{}

var _ CatalogerFactory = quayFactory{}

// Match reports whether host is the Quay registry host or one of its
// subdomains. The match is case-insensitive.
func (quayFactory) Match(host string) bool {
	host = strings.ToLower(host)
	return host == "quay.io" || strings.HasSuffix(host, ".quay.io")
}

// New builds a Quay cataloger. Quay restricts the standard Distribution
// catalog endpoint for public use, so the cataloger uses the Quay REST API
// scoped to one namespace. The namespace is the first segment of the OCM
// subPath. New returns an error when the subPath carries no namespace.
func (quayFactory) New(opts CatalogerOptions) (RepositoryCataloger, error) {
	namespace := quayNamespace(opts.SubPath)
	if namespace == "" {
		return nil, fmt.Errorf("quay cannot list repositories without a namespace: set the OCM subPath to the quay namespace")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(slog.String("realm", "quay-cataloger"), slog.String("namespace", namespace))

	return &quayCataloger{
		httpClient:  opts.HTTPClient,
		baseURL:     quayAPIBaseURL,
		namespace:   namespace,
		credentials: opts.Credentials,
		logger:      logger,
	}, nil
}

// quayCataloger lists repository names of one Quay namespace through the Quay
// REST API. It follows the next_page token of each page until the API reports
// no further page.
type quayCataloger struct {
	httpClient  *http.Client
	baseURL     string
	namespace   string
	credentials *credsv1.OCICredentials
	logger      *slog.Logger
}

var _ RepositoryCataloger = (*quayCataloger)(nil)

// quayRepository is one entry of a Quay repository page.
type quayRepository struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// quayPage is one page of the Quay repository list. NextPage holds the token
// of the following page, or an empty string on the last page.
type quayPage struct {
	Repositories []quayRepository `json:"repositories"`
	NextPage     string           `json:"next_page"`
}

// Repositories lists the repository names of the namespace as
// <namespace>/<name>. The generic ComponentLister applies the
// component-descriptors/ filter and the subPath strip, so this method returns
// the raw repository names. Quay paginates through a next_page token, so the
// cataloger follows next_page internally and ignores the last cursor.
func (c *quayCataloger) Repositories(ctx context.Context, _ string, fn func(repos []string) error) error {
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	token := quayToken(c.credentials)

	nextPage := ""
	for {
		page, err := c.fetchPage(ctx, httpClient, token, nextPage)
		if err != nil {
			return err
		}

		repos := make([]string, 0, len(page.Repositories))
		for _, r := range page.Repositories {
			namespace := r.Namespace
			if namespace == "" {
				namespace = c.namespace
			}
			repos = append(repos, namespace+"/"+r.Name)
		}

		if err := fn(repos); err != nil {
			return err
		}

		if page.NextPage == "" {
			return nil
		}
		nextPage = page.NextPage
	}
}

// fetchPage fetches and decodes one repository page. It scopes the request to
// the namespace and appends the next_page token when it is not empty. When
// token is not empty it sends the Quay OAuth token as a Bearer Authorization
// header.
func (c *quayCataloger) fetchPage(ctx context.Context, httpClient *http.Client, token, nextPage string) (*quayPage, error) {
	query := url.Values{}
	query.Set("namespace", c.namespace)
	if nextPage != "" {
		query.Set("next_page", nextPage)
	}
	rawURL := strings.TrimRight(c.baseURL, "/") + "/api/v1/repository?" + query.Encode()

	c.logger.DebugContext(ctx, "listing quay repositories", "url", rawURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create quay request for %q: %w", rawURL, err)
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request quay repositories from %q: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("quay returned unexpected status %d for %q", resp.StatusCode, rawURL)
	}

	var page quayPage
	if err := json.UnmarshalRead(resp.Body, &page); err != nil {
		return nil, fmt.Errorf("failed to decode quay repository page: %w", err)
	}

	return &page, nil
}

// quayToken returns the Quay OAuth token from the credentials. It prefers the
// AccessToken and falls back to the Password. It returns an empty string when
// no token is present, so the cataloger lists public repositories without
// authentication.
func quayToken(credentials *credsv1.OCICredentials) string {
	if credentials == nil {
		return ""
	}
	if credentials.AccessToken != "" {
		return credentials.AccessToken
	}
	return credentials.Password
}

// quayNamespace returns the Quay namespace of the OCM subPath. It is the first
// path segment of the subPath, or an empty string when the subPath carries no
// segment.
func quayNamespace(subPath string) string {
	return firstPathSegment(subPath)
}
