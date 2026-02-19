//go:build windows

package compref

import (
	"strings"
	"unicode"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func init() {
	guessTypeOS = func(repository string) (string, bool) {
		if isWindowsAbsPath(repository) {
			return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), true
		}
		return "", false
	}
	normalizePath = normalizeOS
}

// isWindowsAbsPath checks if the string looks like a Windows absolute path (e.g., C:\foo or D:/bar).
// This is needed because url.Parse interprets the single-letter drive prefix as a URL scheme.
func isWindowsAbsPath(s string) bool {
	return len(s) >= 3 && unicode.IsLetter(rune(s[0])) && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
}

// normalizeOS normalizes Windows-style backslash path separators to forward slashes.
// This is a pure string operation available on all platforms.
func normalizeOS(path string) string {
	return strings.ReplaceAll(path, `\`, `/`)
}
