package attestation

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ocm.software/open-component-model/bindings/go/repository"
)

// Write writes every discovered attestation whose document was fetched into dir, one file
// per document, byte for byte as published, creating dir if it does not exist. It returns
// the paths written, in the order the attestations were given. Attestations with no Data
// (metadata-only, or a source whose document could not be fetched) are skipped: the caller
// reports those, Write never creates empty files.
func Write(atts []repository.Attestation, dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating attestation output directory %q failed: %w", dir, err)
	}

	written := make([]string, 0, len(atts))
	taken := make(map[string]struct{}, len(atts))
	for _, a := range atts {
		if a.Data == nil {
			continue
		}
		path := filepath.Join(dir, fileName(a, taken))
		if filepath.Dir(path) != filepath.Clean(dir) {
			return nil, fmt.Errorf("refusing to write attestation %q outside of %q", a, dir)
		}
		if err := os.WriteFile(path, a.Data, 0o600); err != nil {
			return nil, fmt.Errorf("writing attestation %q failed: %w", a, err)
		}
		written = append(written, path)
	}

	return written, nil
}

// fileName derives the name of one attestation's file: a friendly label (format, else
// category, else the last path segment of the raw type), a short id disambiguator, and a
// media-type-derived extension. It suffixes the name when an earlier document already took
// it, so documents are never silently overwritten.
func fileName(a repository.Attestation, taken map[string]struct{}) string {
	category, format := Classify(a)
	var base string
	switch {
	// format equals the raw type only for an unrecognised attestation (Classify's
	// default); then it is not a friendly label and the raw type is used instead.
	case format != "" && format != a.Type():
		base = format
	case category != "":
		base = category
	default:
		base = safe(lastPathSegment(a.Type()))
	}
	if base == "" {
		base = "attestation"
	}

	stem := base
	// Key the short id on the content digest so the file name matches the CONTENT-DIGEST
	// column of "ocm get attestation"; fall back to the attestation ID when a source
	// exposes no content digest.
	if id := shortID(cmp.Or(a.ContentDigest, a.ID)); id != "" {
		stem = base + "-" + id
	}

	extension := ".bin"
	if strings.Contains(strings.ToLower(a.MediaType+" "+a.Type()), "json") {
		extension = ".json"
	}

	candidate := stem + extension
	for i := 2; ; i++ {
		if _, exists := taken[candidate]; !exists {
			taken[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", stem, i, extension)
	}
}

// shortID is the disambiguating tail of an attestation ID: the last 12 hex characters of a
// "sha256:<hex>" digest (referrer and in-toto layer IDs), or the folded identity string of
// an ocm-local attestation, which is not a digest.
func shortID(id string) string {
	const digestPrefix = "sha256:"
	if hex := strings.TrimPrefix(id, digestPrefix); hex != id && isHex(hex) {
		if len(hex) > 12 {
			return hex[len(hex)-12:]
		}
		return hex
	}
	return safe(id)
}

// isHex reports whether s is a non-empty string of hexadecimal digits.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// lastPathSegment returns the part of s after its last slash, or s when it has none. It
// turns a predicate URL or slashed media type into a short label.
func lastPathSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// safe folds everything that is not plainly a file name character to an underscore.
func safe(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
