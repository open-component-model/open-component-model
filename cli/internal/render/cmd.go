package render

import "strings"

const (
	CursorUp  = "\x1b[A" // Move cursor up one line
	EraseLine = "\x1b[K" // Erase the current line
)

func EraseNLines(n int) string {
	if n <= 0 {
		return ""
	}
	b := strings.Builder{}
	for range n {
		b.WriteString(CursorUp)
		b.WriteString(EraseLine)
	}
	return b.String()
}
