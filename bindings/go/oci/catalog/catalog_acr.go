package catalog

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
)

// acrPageSize is the number of repositories the cataloger requests per page.
// Azure Container Registry may return fewer entries per page.
const acrPageSize = 100

func init() {
	RegisterCataloger(acrFactory{})
}

// acrFactory builds an Azure Container Registry cataloger for hosts under
// azurecr.io.
type acrFactory struct{}

var _ CatalogerFactory = acrFactory{}

// Match reports whether host is an Azure Container Registry host. ACR hosts
// are subdomains of azurecr.io, for example myregistry.azurecr.io. The match
// is case-insensitive.
func (acrFactory) Match(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), ".azurecr.io")
}

// New builds an ACR cataloger. ACR serves a richer catalog endpoint at
// /acr/v1/_catalog with n and last pagination and a Link rel="next" header.
// The cataloger authenticates through opts.AuthClient, which already runs the
// Distribution token exchange for the catalog scope. When the /acr/v1 endpoint
// is missing, the cataloger falls back to the standard Distribution
// /v2/_catalog endpoint.
func (acrFactory) New(opts CatalogerOptions) (RepositoryCataloger, error) {
	scheme := "https"
	if opts.PlainHTTP {
		scheme = "http"
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(slog.String("realm", "acr-cataloger"), slog.String("host", opts.Host))

	return &acrCataloger{
		opts:       opts,
		scheme:     scheme,
		host:       opts.Host,
		httpClient: opts.HTTPClient,
		authClient: opts.AuthClient,
		logger:     logger,
	}, nil
}

// acrCataloger lists repository names through the ACR /acr/v1/_catalog
// endpoint. It follows the Link rel="next" header of each page until the
// endpoint reports no further page. When the endpoint is missing, it delegates
// to the standard Distribution cataloger.
type acrCataloger struct {
	opts       CatalogerOptions
	scheme     string
	host       string
	httpClient *http.Client
	authClient *auth.Client
	logger     *slog.Logger
}

var _ RepositoryCataloger = (*acrCataloger)(nil)

// acrPage is one page of the ACR catalog response.
type acrPage struct {
	Repositories []string `json:"repositories"`
}

// Repositories lists the raw repository names of the registry. The generic
// ComponentLister applies the component-descriptors/ filter and the subPath
// strip, so this method returns the raw repository names. ACR paginates
// through the Link rel="next" header, so the cataloger follows it internally.
// It uses last as the start cursor of the first page. When the /acr/v1
// endpoint returns HTTP 404, the cataloger falls back to the standard
// Distribution /v2/_catalog endpoint.
func (c *acrCataloger) Repositories(ctx context.Context, last string, fn func(repos []string) error) error {
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	rawURL := c.catalogURL(last)
	page, next, status, err := c.fetchPage(ctx, httpClient, rawURL)
	if err != nil {
		return err
	}

	if status == http.StatusNotFound {
		c.logger.DebugContext(ctx, "acr catalog endpoint returned 404, falling back to distribution catalog")
		fallback, err := NewDistributionCataloger(c.opts)
		if err != nil {
			return err
		}
		return fallback.Repositories(ctx, last, fn)
	}

	if err := fn(page.Repositories); err != nil {
		return err
	}

	for next != "" {
		page, next, _, err = c.fetchPage(ctx, httpClient, next)
		if err != nil {
			return err
		}
		if err := fn(page.Repositories); err != nil {
			return err
		}
	}

	return nil
}

// catalogURL builds the /acr/v1/_catalog URL for the given last cursor. An
// empty last starts the listing from the top.
func (c *acrCataloger) catalogURL(last string) string {
	query := url.Values{}
	query.Set("n", strconv.Itoa(acrPageSize))
	if last != "" {
		query.Set("last", last)
	}
	return c.scheme + "://" + c.host + "/acr/v1/_catalog?" + query.Encode()
}

// fetchPage fetches one catalog page from rawURL, decodes it, and returns the
// decoded page, the resolved URL of the next page from the Link rel="next"
// header, and the HTTP status code. On HTTP 404 it returns the status without
// an error so the caller can fall back to the Distribution endpoint.
func (c *acrCataloger) fetchPage(ctx context.Context, httpClient *http.Client, rawURL string) (page acrPage, next string, status int, err error) {
	c.logger.DebugContext(ctx, "listing acr repositories", "url", rawURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return acrPage{}, "", 0, fmt.Errorf("failed to create acr request for %q: %w", rawURL, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(httpClient, req)
	if err != nil {
		return acrPage{}, "", 0, fmt.Errorf("failed to request acr repositories from %q: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return acrPage{}, "", resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return acrPage{}, "", resp.StatusCode, fmt.Errorf("acr returned unexpected status %d for %q", resp.StatusCode, rawURL)
	}

	if err := json.UnmarshalRead(resp.Body, &page); err != nil {
		return acrPage{}, "", resp.StatusCode, fmt.Errorf("failed to decode acr repository page: %w", err)
	}

	return page, acrNextLink(resp.Header.Get("Link"), rawURL), resp.StatusCode, nil
}

// do sends the request through the ORAS auth client when one is present, so
// the Distribution token exchange is reused. Otherwise it uses the plain HTTP
// client.
func (c *acrCataloger) do(httpClient *http.Client, req *http.Request) (*http.Response, error) {
	if c.authClient != nil {
		return c.authClient.Do(req)
	}
	return httpClient.Do(req)
}

// acrNextLink returns the absolute URL of the next page from an ACR Link
// header, or an empty string when the header carries no rel="next" entry. ACR
// returns a relative next URL, so acrNextLink resolves it against baseURL. A
// Link header is a comma-separated list of entries, each shaped as
// <url>; rel="name".
func acrNextLink(header, baseURL string) string {
	raw := ""
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}

		candidate := strings.TrimSpace(segments[0])
		candidate = strings.TrimPrefix(candidate, "<")
		candidate = strings.TrimSuffix(candidate, ">")

		for _, attr := range segments[1:] {
			attr = strings.TrimSpace(attr)
			if attr == `rel="next"` || attr == "rel=next" {
				raw = candidate
			}
		}
	}
	if raw == "" {
		return ""
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}
