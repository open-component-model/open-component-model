package scanner

import (
	"regexp"
	"strings"
)

// Artifact is a CI/release output a repository publishes (a container image, a
// released binary, an archive), discovered from release/CI configuration rather
// than from a source manifest. Artifacts enrich a component with additional
// resources beyond its primary (dominant-kind) deliverable.
type Artifact struct {
	Kind      string   // ociImage | executable | archive
	Name      string   // stable resource name (e.g. "image", "binary-linux-amd64")
	Ref       string   // image reference (ociImage); empty for local blobs
	Platforms []string // GOOS/GOARCH pairs for a binary matrix, best-effort
	Source    string   // discovery source: goreleaser | github-actions | makefile | taskfile | ko
	Guessed   bool     // true when Ref could not be fully resolved (flag as TODO)
	Note      string   // human-facing rationale
}

// tmplVarRE matches a goreleaser/Go-template variable reference like {{.Version}}
// or {{ .Tag }} (optionally with surrounding whitespace).
var tmplVarRE = regexp.MustCompile(`\{\{\s*\.([A-Za-z]+)\s*\}\}`)

// resolveTemplate substitutes the known template variables (Version, Tag) into a
// goreleaser/metadata-action-style template string using the resolved version.
// It reports resolved=false when any unresolved template construct remains
// (an unknown variable, or a template function/pipeline) so the caller can flag
// the reference as a guess rather than emit a wrong value.
func resolveTemplate(tmpl, version string) (out string, resolved bool) {
	if tmpl == "" {
		return "", false
	}
	out = tmplVarRE.ReplaceAllStringFunc(tmpl, func(m string) string {
		sub := tmplVarRE.FindStringSubmatch(m)
		switch sub[1] {
		case "Version":
			return version
		case "Tag":
			// A tag is conventionally the version with a leading v; callers pass
			// the already-normalised version, so reuse it.
			return version
		default:
			return m // leave unknown vars intact -> resolved=false below
		}
	})
	// Any remaining {{ ... }} (unknown var, function, or pipeline) means the
	// reference is not fully resolved.
	resolved = !strings.Contains(out, "{{")
	return out, resolved
}
