package compref

import (
	"net/url"
	"path/filepath"
	"strings"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// guessTypeOS is a platform-specific hook for detecting OS-specific path formats.
// On Unix-like systems, the default always returns ("", false).
// On Windows, init() overrides this to detect Windows absolute paths (e.g., C:\path).
// This is a variable to allow simulating platform-specific behavior in tests.
var guessTypeOS = func(repository string) (string, bool) {
	return "", false
}

// normalizePath is a platform-specific hook for normalizing OS path separators.
// On Unix-like systems, the default returns the path unchanged.
// On Windows, init() overrides this to normalize backslashes to forward slashes.
// This is a variable to allow simulating platform-specific behavior in tests.
var normalizePath = func(path string) string {
	return path
}

// guessType tries to guess the repository type ("ctf" or "oci")
// from an untyped repository specification string.
//
// You may ask yourself why this is needed.
// The reason is that there are some repository strings that are indistinguishable from being either
// a CTF or OCI repository. For example,
// "github.com/organization/repository" could be an OCI repository without a Scheme,
// but it could also be a file path to a CTF in the subfolders "github.com", "organization" and "repository".
//
// It uses a practical set of heuristics:
//   - If it is an OS specific absolute path in windows (e.g., "C:\path" on Windows) assume CTF
//   - If it has a URL scheme ("file://"), assume CTF
//   - If it's an absolute filesystem path, assume CTF
//   - If it contains a colon (e.g., "localhost:5000"), assume OCI
//   - If it looks like an archive file (tar.gz, tgz or tar), assume CTF
//   - If it looks like a domain (contains dots like ".com", ".io", etc.), assume OCI
//   - Otherwise fallback to CTF
func guessType(repository string) (string, error) {
	if path, ok := guessTypeOS(repository); ok {
		return path, nil
	}

	// Try parsing as URL first
	if u, err := url.Parse(repository); err == nil {
		if u.Scheme == "file" {
			return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
		}
		if u.Scheme != "" {
			// Any other scheme (e.g., https) implies OCI
			return runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(), nil
		}
	}

	cleaned := filepath.Clean(repository)

	// Absolute filesystem path → assume CTF
	if filepath.IsAbs(cleaned) {
		return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
	}

	// Contains colon (e.g., localhost:5000), or is localhost without port → assume OCI
	if strings.Contains(cleaned, ":") || cleaned == "localhost" {
		return runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(), nil
	}

	// Check if it looks like an archive file → assume CTF
	if looksLikeArchive(cleaned) {
		return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
	}

	// Contains domain-looking part (e.g., github.com, ghcr.io) → assume OCI
	if looksLikeDomain(cleaned) {
		return runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(), nil
	}

	// Default fallback: assume CTF
	return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
}
