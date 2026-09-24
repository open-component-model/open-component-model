package http

import (
	"errors"
	nethttp "net/http"
)

// WithHTTPSDowngradeProtection returns a shallow copy of client that rejects
// redirects to HTTP if any previous request in the redirect chain used HTTPS.
// A nil client uses New(). The transport and cookie jar remain shared with the
// original client, which is not modified.
//
// Downgrade protection runs before the original CheckRedirect callback. If no
// callback is configured, the standard library's limit of 10 redirects applies.
func WithHTTPSDowngradeProtection(client *nethttp.Client) *nethttp.Client {
	if client == nil {
		client = New()
	}

	protected := *client
	checkRedirect := client.CheckRedirect
	protected.CheckRedirect = func(req *nethttp.Request, via []*nethttp.Request) error {
		if req.URL.Scheme == "http" {
			for _, previous := range via {
				if previous.URL.Scheme == "https" {
					return errors.New("redirect from HTTPS to HTTP is not allowed")
				}
			}
		}
		if checkRedirect != nil {
			return checkRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &protected
}
