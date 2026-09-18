package checksum

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxChecksumBodyBytes bounds how much of an external checksum response is read.
// Checksum files are tiny (a hex string, optionally with a filename); a hostile
// or misconfigured server must not be able to stream an unbounded body here.
const maxChecksumBodyBytes = 4 << 10

// ExternalFetcher retrieves "Remote External" checksums: sibling resources stored
// next to the artifact, addressed by appending an algorithm extension to the
// artifact URL (Maven's convention, e.g. <url>.sha1).
type ExternalFetcher struct {
	// Client performs the checksum requests. When nil, http.DefaultClient is used.
	Client *http.Client
	// URLTemplate builds the checksum URL from the artifact URL and an extension.
	// The tokens {{.url}} and {{.ext}} are substituted. When empty it defaults to
	// "{{.url}}.{{.ext}}".
	URLTemplate string
}

// Fetch requests the external checksum for baseURL using the given algorithms in
// order, returning the first that resolves. ok is false when none is available
// (all requests 404 or are absent); an error is returned only for transport or
// malformed-response failures.
func (f *ExternalFetcher) Fetch(ctx context.Context, baseURL string, algs []Algorithm) (Expected, bool, error) {
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	tmpl := f.URLTemplate
	if tmpl == "" {
		tmpl = "{{.url}}.{{.ext}}"
	}
	for _, alg := range algs {
		checksumURL, err := renderChecksumURL(tmpl, baseURL, alg.Extension)
		if err != nil {
			return Expected{}, false, err
		}
		value, found, err := fetchOne(ctx, client, checksumURL, alg)
		if err != nil {
			return Expected{}, false, err
		}
		if found {
			return Expected{Algorithm: alg, Value: value}, true, nil
		}
	}
	return Expected{}, false, nil
}

// renderChecksumURL substitutes the {{.url}} and {{.ext}} tokens. It avoids
// text/template so the extension cannot inject template actions and to keep the
// substitution obvious.
func renderChecksumURL(tmpl, baseURL, ext string) (string, error) {
	rendered := strings.ReplaceAll(tmpl, "{{.url}}", baseURL)
	rendered = strings.ReplaceAll(rendered, "{{.ext}}", ext)
	if strings.Contains(rendered, "{{") {
		return "", fmt.Errorf("unsupported token in checksum urlTemplate %q (only {{.url}} and {{.ext}} are allowed)", tmpl)
	}
	if _, err := url.Parse(rendered); err != nil {
		return "", fmt.Errorf("invalid checksum url %q: %w", rendered, err)
	}
	return rendered, nil
}

// fetchOne GETs a single checksum URL. A 404/410 yields found=false (the checksum
// simply does not exist for that algorithm); other non-2xx statuses are errors.
func fetchOne(ctx context.Context, client *http.Client, checksumURL string, alg Algorithm) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("error creating checksum request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("error fetching checksum from %s: %w", checksumURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return "", false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false, fmt.Errorf("checksum request to %s returned status %d", checksumURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumBodyBytes))
	if err != nil {
		return "", false, fmt.Errorf("error reading checksum body from %s: %w", checksumURL, err)
	}
	value, err := parseChecksumFile(string(body), alg)
	if err != nil {
		return "", false, fmt.Errorf("error parsing checksum from %s: %w", checksumURL, err)
	}
	return value, true, nil
}

// parseChecksumFile extracts the hex checksum from a checksum file body. It
// accepts both a bare hex string and the GNU coreutils format ("<hex>  <name>"),
// taking the first whitespace-separated token of the first non-empty line.
func parseChecksumFile(body string, alg Algorithm) (string, error) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		token := strings.Fields(line)[0]
		token = strings.ToLower(strings.TrimPrefix(token, "\\"))
		if !isHex(token, alg.Hash.Size()) {
			return "", fmt.Errorf("checksum %q is not a valid %s hex digest", token, alg.OCMName)
		}
		return token, nil
	}
	return "", fmt.Errorf("checksum file is empty")
}
