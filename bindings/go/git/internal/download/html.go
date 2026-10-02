package download

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
)

var errHTMLDiscovery = errors.New("git ref discovery returned HTML")

// rejectHTMLDiscovery fails ref discovery answered with an HTML page. go-git
// reads any answer that is not a smart advertisement as a dumb-protocol ref
// list, so a sign-in page becomes a repository without refs.
func rejectHTMLDiscovery(c *http.Client) *http.Client {
	guarded := *c
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	guarded.Transport = htmlDiscoveryGuard{base: base}

	return &guarded
}

type htmlDiscoveryGuard struct {
	base http.RoundTripper
}

func (g htmlDiscoveryGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := g.base.RoundTrip(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/info/refs") || resp.StatusCode/100 != 2 {
		return resp, err
	}

	if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType == "text/html" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w (HTTP %d from %s)", errHTMLDiscovery, resp.StatusCode, req.URL.Host)
	}

	return resp, nil
}
