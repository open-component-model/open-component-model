package filesystem

import (
	"fmt"
	"net/url"
	"strings"
)

// FilePathFromURI extracts the filesystem path from a file:// URI.
// Only local file URIs are accepted:
//   - scheme must be "file"
//   - opaque form (e.g. "file:relative/path") is rejected
//   - host must be empty or "localhost"
//   - path must be non-empty
func FilePathFromURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported URI scheme %q, expected \"file\"", parsed.Scheme)
	}
	if parsed.Opaque != "" {
		return "", fmt.Errorf("opaque file URI %q not supported, use file:///path form", uri)
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return "", fmt.Errorf("remote file URI %q not supported, host must be empty or localhost", uri)
	}
	if parsed.Path == "" {
		return "", fmt.Errorf("file URI %q has no path", uri)
	}
	return parsed.Path, nil
}
