package bar

import (
	"fmt"
	"strings"
)

// Formatter converts event data to a display name shown in the progress log.
// Example: func(t *Transform) string { return t.Name }
type Formatter[T any] func(T) string

// ErrorFormatter converts event data and error to a string for the error summary.
// Use this to show context-specific error details (e.g., transformation spec).
type ErrorFormatter[T any] func(T, error) string

// TreeErrorFormatter formats an error chain as an indented tree.
// Each ": " separator in the error message creates a new indented level.
//
// Example output:
//
//	↳ failed to push blob
//	  ↳ connection refused
func TreeErrorFormatter(err error) string {
	if err == nil {
		return ""
	}

	// Split error message on ": " to get chain segments
	parts := strings.Split(err.Error(), ": ")

	var sb strings.Builder
	base := "    " // 4 spaces base indent
	for i, part := range parts {
		indent := strings.Repeat("  ", i)
		// Handle newlines within a part - indent continuation lines
		lines := strings.Split(part, "\n")
		for j, line := range lines {
			if line == "" {
				continue
			}
			if j == 0 {
				sb.WriteString(fmt.Sprintf("%s%s%s↳%s %s\n", base, indent, dim, reset, line))
			} else {
				// Continuation lines get extra indent
				sb.WriteString(fmt.Sprintf("%s%s  %s%s\n", base, indent, dim, line+reset))
			}
		}
	}

	return sb.String()
}

// FramedText wraps text in a Unicode box (┌─┐│└─┘) with an optional title.
// If title is provided, it's shown above the box with a ▶ icon.
//
// Example output:
//
//	▶ Transformation failed
//	┌──────────────────┐
//	│ {"name": "foo"}  │
//	└──────────────────┘
func FramedText(title string, content string, baseIndent int) string {
	indent := strings.Repeat(" ", baseIndent)
	var sb strings.Builder

	// Add title with icon if provided
	if title != "" {
		titleLines := strings.Split(title, "\n")
		for i, line := range titleLines {
			if i == 0 {
				sb.WriteString(fmt.Sprintf("%s▶ %s\n", indent, line))
			} else {
				sb.WriteString(fmt.Sprintf("%s  %s\n", indent, line)) // align with icon width
			}
		}
	}

	// Build the framed box
	lines := strings.Split(content, "\n")

	// Find max line width
	maxWidth := 0
	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}

	// Top border
	sb.WriteString(fmt.Sprintf("%s┌%s┐\n", indent, strings.Repeat("─", maxWidth+2)))

	// Content lines
	for _, line := range lines {
		padding := strings.Repeat(" ", maxWidth-len(line))
		sb.WriteString(fmt.Sprintf("%s│ %s%s │\n", indent, line, padding))
	}

	// Bottom border
	sb.WriteString(fmt.Sprintf("%s└%s┘\n", indent, strings.Repeat("─", maxWidth+2)))

	return sb.String()
}
