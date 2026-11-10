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
		return "", fmt.Errorf("unsupported URI scheme %q in %q, expected an absolute \"file\" URI such as file:///dir/file", parsed.Scheme, uri)
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
	return FilePathFromURLPath(parsed.Path), nil
}

// FilePathFromURLPath turns the path component of a file URL into an OS path.
// A Windows drive is carried as "/C:/dir" in a URL, which is not a usable path
// on its own, so the leading slash is dropped.
func FilePathFromURLPath(p string) string {
	// VolumeName is only non-empty on Windows, where "/C:/dir" must become "C:/dir".
	if len(p) > 1 && p[0] == '/' && filepath.VolumeName(p[1:]) != "" {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// URLPathFromFilePath turns an absolute OS path into the path component of a
// file URL: slash separated and rooted, so a Windows path becomes "/C:/dir".
// The result is not escaped; URIFromFilePath does that.
func URLPathFromFilePath(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// URIFromFilePath returns the file:// URI for an absolute filesystem path, e.g.
// "file:///dir/file" for "/dir/file" and "file:///C:/dir/file" for "C:\dir\file".
// It is the inverse of FilePathFromURI.
//
// Relative paths are rejected rather than silently rooted: a file URI is absolute
// by definition (RFC 8089), and only the caller knows what to resolve against.
func URIFromFilePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file URI requires an absolute path, got %q", path)
	}
	return (&url.URL{Scheme: "file", Path: URLPathFromFilePath(path)}).String(), nil
}
