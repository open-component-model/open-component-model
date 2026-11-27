// Package terminal holds small helpers for detecting terminal output, shared by the
// renderers so the same *os.File check is not reimplemented per command.
package terminal

import (
	"io"
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether w is connected to a terminal. A writer that is not an
// *os.File (a buffer, a pipe) is never a terminal.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
