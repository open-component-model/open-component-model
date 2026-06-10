package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"helm.sh/helm/v4/pkg/getter"
)

// httpConfigGetter is a custom Helm getter.Getter that uses a pre-built
// *http.Client instead of constructing one from a *http.Transport. This
// allows callers to supply a client whose RoundTripper chain includes
// per-host routing (e.g. ocmhttp.New with cfg.Hosts populated), which
// getter.WithTransport cannot support because it requires *http.Transport
// rather than http.RoundTripper.
//
// All credential and header values are captured at construction time via
// HTTPConfigGetterOpts. Since getter.Option is func(*getterOptions) with an
// unexported type, its values cannot be extracted by inspection. Instead,
// GetterProviders captures the known values from download.go directly —
// username, password, baseURL — so no getter.Option introspection is needed.
// Per-call options appended by ChartDownloader (WithBasicAuth, WithURL, etc.)
// are intentionally ignored because those same values are already present in
// the opts struct supplied at construction time.
type httpConfigGetter struct {
	client *http.Client
	opts   HTTPConfigGetterOpts
}

// HTTPConfigGetterOpts carries the request-level values that httpConfigGetter
// applies when building each outbound request.
type HTTPConfigGetterOpts struct {
	// username and password are forwarded as HTTP Basic Auth when the request
	// URL host matches baseURL host, or when passCredentialsAll is true.
	username           string
	password           string
	passCredentialsAll bool
	// baseURL is the chart repository URL used for the same-host credential
	// scoping check (credentials are only sent to the same host as baseURL).
	baseURL string
	// userAgent overrides the Go default User-Agent when non-empty.
	userAgent string
}

// NewHTTPConfigGetter creates a Getter backed by the provided client and opts.
// getter.Option values passed at construction or per-call are ignored;
// all request decoration is driven by opts.
func NewHTTPConfigGetter(client *http.Client, opts HTTPConfigGetterOpts) (getter.Getter, error) {
	return &httpConfigGetter{client: client, opts: opts}, nil
}

// Get implements getter.Getter.
func (g *httpConfigGetter) Get(href string, _ ...getter.Option) (*bytes.Buffer, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, href, nil)
	if err != nil {
		return nil, err
	}

	if g.opts.userAgent != "" {
		req.Header.Set("User-Agent", g.opts.userAgent)
	}
	req.Header.Set("Accept", "application/gzip,application/octet-stream")

	if g.opts.username != "" && g.opts.password != "" {
		baseURL, err := url.Parse(g.opts.baseURL)
		if err != nil {
			return nil, fmt.Errorf("httpConfigGetter: parse base URL: %w", err)
		}
		fetchURL, err := url.Parse(href)
		if err != nil {
			return nil, fmt.Errorf("httpConfigGetter: parse fetch URL: %w", err)
		}
		if g.opts.passCredentialsAll || (baseURL.Scheme == fetchURL.Scheme && baseURL.Host == fetchURL.Host) {
			req.SetBasicAuth(g.opts.username, g.opts.password)
		}
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s : %s", href, resp.Status)
	}

	buf := bytes.NewBuffer(nil)
	_, err = io.Copy(buf, resp.Body)
	return buf, err
}
