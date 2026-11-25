package catalog

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// dockerHubBaseURL is the base URL of the Docker Hub web API. It is a
// package-private var so a unit test can point it at an httptest.Server.
var dockerHubBaseURL = "https://hub.docker.com"

// dockerHubPageSize is the number of repositories the cataloger requests per
// page. Docker Hub may return fewer entries per page.
const dockerHubPageSize = 100

// dockerHubHosts are the registry hosts that Docker Hub serves.
var dockerHubHosts = map[string]struct{}{
	"docker.io":            {},
	"index.docker.io":      {},
	"registry-1.docker.io": {},
}

func init() {
	RegisterCataloger(dockerHubFactory{})
}

// dockerHubFactory builds a Docker Hub cataloger for the Docker Hub hosts.
type dockerHubFactory struct{}

var _ CatalogerFactory = dockerHubFactory{}

// Match reports whether host is one of the Docker Hub registry hosts.
func (dockerHubFactory) Match(host string) bool {
	_, ok := dockerHubHosts[strings.ToLower(host)]
	return ok
}

// New builds a Docker Hub cataloger. Docker Hub does not serve the standard
// Distribution catalog endpoint, and its web API can only list one namespace.
// The namespace is the first segment of the OCM subPath. New returns an error
// when the subPath carries no namespace.
func (dockerHubFactory) New(opts CatalogerOptions) (RepositoryCataloger, error) {
	namespace := dockerHubNamespace(opts.SubPath)
	if namespace == "" {
		return nil, fmt.Errorf("docker hub cannot list repositories without a namespace: set the OCM subPath to the docker hub namespace")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(slog.String("realm", "dockerhub-cataloger"), slog.String("namespace", namespace))

	return &dockerHubCataloger{
		httpClient:  opts.HTTPClient,
		baseURL:     dockerHubBaseURL,
		namespace:   namespace,
		credentials: opts.Credentials,
		logger:      logger,
	}, nil
}

// dockerHubCataloger lists repository names of one Docker Hub namespace through
// the Docker Hub web API. It follows the next link of each page until the API
// reports no further page.
type dockerHubCataloger struct {
	httpClient  *http.Client
	baseURL     string
	namespace   string
	credentials *credsv1.OCICredentials
	logger      *slog.Logger
}

var _ RepositoryCataloger = (*dockerHubCataloger)(nil)

// dockerHubRepository is one entry of a Docker Hub repository page.
type dockerHubRepository struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// dockerHubPage is one page of the Docker Hub repository list. Next holds the
// full URL of the following page, or an empty string on the last page.
type dockerHubPage struct {
	Count   int                   `json:"count"`
	Next    string                `json:"next"`
	Results []dockerHubRepository `json:"results"`
}

// dockerHubLoginRequest is the body of the Docker Hub login request.
type dockerHubLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// dockerHubLoginResponse is the body of the Docker Hub login response.
type dockerHubLoginResponse struct {
	Token string `json:"token"`
}

// Repositories lists the repository names of the namespace as
// <namespace>/<name>. The generic ComponentLister applies the
// component-descriptors/ filter and the subPath strip, so this method returns
// the raw repository names. Docker Hub paginates through a next link, so the
// cataloger follows next internally and ignores the last cursor.
func (c *dockerHubCataloger) Repositories(ctx context.Context, _ string, fn func(repos []string) error) error {
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	token, err := c.login(ctx, httpClient)
	if err != nil {
		return err
	}

	next := fmt.Sprintf("%s/v2/repositories/%s/?page_size=%d",
		strings.TrimRight(c.baseURL, "/"), url.PathEscape(c.namespace), dockerHubPageSize)

	for next != "" {
		c.logger.DebugContext(ctx, "listing docker hub repositories", "url", next)

		page, err := c.fetchPage(ctx, httpClient, next, token)
		if err != nil {
			return err
		}

		repos := make([]string, 0, len(page.Results))
		for _, r := range page.Results {
			namespace := r.Namespace
			if namespace == "" {
				namespace = c.namespace
			}
			repos = append(repos, namespace+"/"+r.Name)
		}

		if err := fn(repos); err != nil {
			return err
		}

		next = page.Next
	}

	return nil
}

// login exchanges the username and password for a Docker Hub JWT. It returns
// an empty token when no username and password are present, so the cataloger
// lists public repositories without authentication.
func (c *dockerHubCataloger) login(ctx context.Context, httpClient *http.Client) (string, error) {
	if c.credentials == nil || c.credentials.Username == "" || c.credentials.Password == "" {
		return "", nil
	}

	body, err := json.Marshal(dockerHubLoginRequest{
		Username: c.credentials.Username,
		Password: c.credentials.Password,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode docker hub login request: %w", err)
	}

	loginURL := strings.TrimRight(c.baseURL, "/") + "/v2/users/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create docker hub login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to log in to docker hub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker hub login returned unexpected status %d", resp.StatusCode)
	}

	var out dockerHubLoginResponse
	if err := json.UnmarshalRead(resp.Body, &out); err != nil {
		return "", fmt.Errorf("failed to decode docker hub login response: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("docker hub login returned an empty token")
	}

	return out.Token, nil
}

// fetchPage fetches and decodes one repository page from rawURL. When token is
// not empty it sends the Docker Hub JWT as the Authorization header.
func (c *dockerHubCataloger) fetchPage(ctx context.Context, httpClient *http.Client, rawURL, token string) (*dockerHubPage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker hub request for %q: %w", rawURL, err)
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "JWT "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request docker hub repositories from %q: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker hub returned unexpected status %d for %q", resp.StatusCode, rawURL)
	}

	var page dockerHubPage
	if err := json.UnmarshalRead(resp.Body, &page); err != nil {
		return nil, fmt.Errorf("failed to decode docker hub repository page: %w", err)
	}

	return &page, nil
}

// dockerHubNamespace returns the Docker Hub namespace of the OCM subPath. It
// is the first path segment of the subPath, or an empty string when the
// subPath carries no segment.
func dockerHubNamespace(subPath string) string {
	return firstPathSegment(subPath)
}
