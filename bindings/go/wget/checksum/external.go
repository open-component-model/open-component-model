package checksum

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxChecksumBodyBytes bounds how much of an external checksum response is read.
// Checksum files are tiny (a hex string, optionally with a filename); a hostile
// or misconfigured server must not be able to stream an unbounded body here.
const maxChecksumBodyBytes = 4 << 10

// ExternalFetcher retrieves "Remote External" checksums: sibling resources stored
// next to the artifact. The URL for each algorithm is resolved by the caller
// (see [Source.ResolveURL]), keeping this fetcher independent of any templating.
type ExternalFetcher struct {
	// Client performs the checksum requests when [Do] is nil. When both are nil,
	// http.DefaultClient is used. The caller is responsible for any transport
	// credentials attached to Client.
	Client *http.Client
	// Do dispatches a prepared checksum request and returns the response. When
	// non-nil it takes precedence over Client, allowing the caller to pick a
	// per-URL client (for example: credentialed for same-origin HTTPS URLs,
	// undecorated for cross-origin) and to attach or omit credentials based on
	// the request destination. Keeping the hook at request level lets the caller
	// scope credentials by destination without leaking runtime types into this
	// package.
	Do func(*http.Request) (*http.Response, error)
}

// FetchURL requests a single external checksum from checksumURL, expecting a body
// hex-encoded for alg. A 404/410 yields ok=false so [Resolve] can fall back to
// the next algorithm; other non-2xx statuses and malformed bodies are errors.
func (f *ExternalFetcher) FetchURL(ctx context.Context, checksumURL string, alg Algorithm) (Expected, bool, error) {
	value, found, err := f.fetchOne(ctx, checksumURL, alg)
	if err != nil {
		return Expected{}, false, err
	}
	if !found {
		return Expected{}, false, nil
	}
	return Expected{Algorithm: alg, Value: value}, true, nil
}

// do dispatches req via [ExternalFetcher.Do] when set, falling back to
// [ExternalFetcher.Client] and finally to http.DefaultClient.
func (f *ExternalFetcher) do(req *http.Request) (*http.Response, error) {
	if f.Do != nil {
		return f.Do(req)
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

// fetchOne GETs a single checksum URL. A 404/410 yields found=false (the checksum
// simply does not exist for that algorithm); other non-2xx statuses are errors.
func (f *ExternalFetcher) fetchOne(ctx context.Context, checksumURL string, alg Algorithm) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("error creating checksum request: %w", err)
	}
	resp, err := f.do(req)
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
