package scanner

import (
	"path"
	"strings"
)

// dirOf returns the repo-relative slash directory of a repo-relative file path,
// normalising the repo root to ".".
func dirOf(rel string) string {
	d := path.Dir(rel)
	if d == "" || d == "/" {
		return "."
	}
	return d
}

// baseName returns the final path element of a repo-relative path.
func baseName(rel string) string { return path.Base(rel) }

// sibling returns the repo-relative path of file name in the same directory as
// rel (rel is a repo-relative file path).
func sibling(rel, name string) string {
	d := dirOf(rel)
	if d == "." {
		return name
	}
	return d + "/" + name
}

// dedup returns hints with duplicates removed, preserving first-seen order.
func dedup(hints []string) []string {
	if len(hints) < 2 {
		return hints
	}
	seen := make(map[string]bool, len(hints))
	out := hints[:0]
	for _, h := range hints {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// firstSubmatch returns the first capture group of the first regexp match in b,
// or "" when there is no match. trimmed of surrounding quotes.
func firstSubmatch(matches [][]string) string {
	if len(matches) == 0 || len(matches[0]) < 2 {
		return ""
	}
	return strings.Trim(matches[0][1], `"'`)
}
