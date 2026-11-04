package download

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// authenticatedHTTPClient limits redirects to the credential's original origin.
// Clone the client so concurrent downloads retain the caller's redirect policy.
func authenticatedHTTPClient(original *http.Client) *http.Client {
	client := *original
	next := original.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if next != nil {
			if err := next(req, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		// Check after the caller's callback in case it changes the target URL.
		if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
			return fmt.Errorf("authenticated git redirect changes origin")
		}
		return nil
	}
	return &client
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && originPort(a) == originPort(b)
}

func originPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}
