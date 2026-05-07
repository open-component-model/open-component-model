package handler

import "regexp"

var stderrScrubbers = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)bearer\s+\S+`), "bearer [REDACTED]"},
	{regexp.MustCompile(`SIGSTORE_ID_TOKEN=\S+`), "SIGSTORE_ID_TOKEN=[REDACTED]"},
	// Matches key=value where value may be quoted (single or double) or unquoted.
	{regexp.MustCompile(`(?i)(token|secret|password|key)="[^"]*"`), "${1}=[REDACTED]"},
	{regexp.MustCompile(`(?i)(token|secret|password|key)='[^']*'`), "${1}=[REDACTED]"},
	{regexp.MustCompile(`(?i)(token|secret|password|key)=\S+`), "${1}=[REDACTED]"},
	// Bare JWT tokens (header.payload.signature) — common in cosign error messages.
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), "[REDACTED-JWT]"},
}

func scrubStderr(s string) string {
	for _, sc := range stderrScrubbers {
		s = sc.pattern.ReplaceAllString(s, sc.replacement)
	}
	return s
}
