package render

import (
	"log/slog"
	"strings"
)

const (
	// The CursorUp ANSI escape code is not supported on all terminals.
	CursorUp = "\x1b[A" // Move cursor up one line
	// The EraseLine ANSI escape code is not supported on all terminals.
	EraseLine = "\x1b[K" // Erase the current line
)

// EraseNLines erases n lines from the terminal.
// It uses ANSI escape codes to move the CursorUp and EraseLine.
// ANSI escape codes are not supported on all terminals, so this function may
// not work as expected in all environments.
func EraseNLines(n int) string {
	if n <= 0 {
		return ""
	}
	b := strings.Builder{}
	for range n {
		b.WriteString(CursorUp + EraseLine)
	}
	slog.Debug("erasing lines", slog.Int("count", n))
	return b.String()
}
