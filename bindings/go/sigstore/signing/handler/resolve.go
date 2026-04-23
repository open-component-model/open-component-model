package handler

import (
	"fmt"
	"net/url"
)

// validateHTTPSURL returns an error if rawURL is non-empty but not a valid https:// URL.
func validateHTTPSURL(field, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s: invalid URL %q: %w", field, rawURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s: URL %q must use https scheme", field, rawURL)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: URL %q has no host", field, rawURL)
	}
	return nil
}
