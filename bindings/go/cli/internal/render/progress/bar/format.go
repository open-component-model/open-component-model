package bar

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

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
		lines := strings.Split(part, "\n")
		for j, line := range lines {
			if line == "" {
				continue
			}
			if j == 0 {
				fmt.Fprintf(&sb, "%s%s%s↳%s %s\n", base, indent, dim, Reset, line)
			} else {
				fmt.Fprintf(&sb, "%s%s  %s%s\n", base, indent, dim, line+Reset)
			}
		}
	}

	return sb.String()
}

// FramedText wraps text in a Unicode box with an optional title.
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
				fmt.Fprintf(&sb, "%s▶ %s\n", indent, line)
			} else {
				fmt.Fprintf(&sb, "%s  %s\n", indent, line) // align with icon width
			}
		}
	}

	// Build the framed box
	lines := strings.Split(content, "\n")

	// Find max line width (rune count for correct alignment with multi-byte UTF-8)
	maxWidth := 0
	for _, line := range lines {
		if w := utf8.RuneCountInString(line); w > maxWidth {
			maxWidth = w
		}
	}

	// Top border
	fmt.Fprintf(&sb, "%s┌%s┐\n", indent, strings.Repeat("─", maxWidth+2))

	// Content lines
	for _, line := range lines {
		padding := strings.Repeat(" ", maxWidth-utf8.RuneCountInString(line))
		fmt.Fprintf(&sb, "%s│ %s%s │\n", indent, line, padding)
	}

	// Bottom border
	fmt.Fprintf(&sb, "%s└%s┘\n", indent, strings.Repeat("─", maxWidth+2))

	return sb.String()
}

// SidebarText renders content with a coloured vertical line flush to the left.
//
// Example output:
//
//	│ Title
//	│line one
//	│line two
func SidebarText(title string, content string, color string) string {
	var sb strings.Builder

	if title != "" {
		for _, line := range strings.Split(title, "\n") {
			fmt.Fprintf(&sb, "%s│ %s%s\n", color, Reset, line)
		}
	}

	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		fmt.Fprintf(&sb, "%s│%s%s\n", color, Reset, line)
	}

	return sb.String()
}
