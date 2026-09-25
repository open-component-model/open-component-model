package download

import (
	"net/url"
	"regexp"
	"strings"
)

// safeURLString is the raw-string counterpart of safeURL. Invalid URLs are
// omitted entirely because parsing cannot reliably locate their credentials.
func safeURLString(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[redacted URL]"
	}
	return safeURL(u)
}

// Match URLs even in nested transport errors and quoted Location headers.
var errorURL = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s"'<>]+`)

// redact changes only error rendering. The original chain remains available to
// errors.Is/As, including the original *url.Error, as in the GitHub downloader.
func redact(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// URL errors may also carry relative URLs, which the textual matcher misses.
	var visit func(error)
	visit = func(cause error) {
		if u, ok := cause.(*url.Error); ok && u.URL != "" { //nolint:errorlint // Inspect this node only; recursion below visits every wrapped error.
			msg = strings.ReplaceAll(msg, u.URL, safeURLString(u.URL))
		}
		switch e := cause.(type) { //nolint:errorlint // Traverse immediate children, including joined errors, rather than searching past this node.
		case interface{ Unwrap() error }:
			visit(e.Unwrap())
		case interface{ Unwrap() []error }:
			for _, child := range e.Unwrap() {
				visit(child)
			}
		}
	}
	visit(err)
	msg = errorURL.ReplaceAllStringFunc(msg, safeURLString)
	if msg == err.Error() {
		return err
	}
	return &redactedError{msg: msg, cause: err}
}

type redactedError struct {
	msg   string
	cause error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.cause }
