package filesystem

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// FilePathFromURI extracts the filesystem path from a file:// URI.
// Only local file URIs are accepted:
//   - scheme must be "file"
//   - opaque form (e.g. "file:relative/path") is rejected
//   - host must be empty or "localhost"
//   - path must be non-empty
//
// The returned path uses the separators of the host OS. On Windows the leading
// slash before a drive letter is dropped, so "file:///C:/dir/file" yields
// "C:\dir\file".
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
	path := parsed.Path
	// VolumeName is only non-empty on Windows, where "/C:/dir" must become "C:/dir".
	if len(path) > 1 && path[0] == '/' && filepath.VolumeName(path[1:]) != "" {
		path = path[1:]
	}
	return filepath.FromSlash(path), nil
}

// URIFromFilePath returns the file:// URI for an absolute filesystem path, e.g.
// "file:///dir/file" for "/dir/file" and "file:///C:/dir/file" for "C:\dir\file".
// It is the inverse of FilePathFromURI.
func URIFromFilePath(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
