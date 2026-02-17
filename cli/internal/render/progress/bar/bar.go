// Package bar provides a terminal progress bar visualizer.
// It shows a scrolling log of recent items above a progress bar that updates in place.
//
// Terminal layout:
//
//	Header (optional)
//	  ⏳ item-1        <- scrolling log (most recent items)
//	  ✓ item-2
//	  ✗ item-3
//	  [████░░░░] 50%   <- progress bar (fixed at bottom)
//
// After completion, failed items are listed with their errors.
package bar

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"ocm.software/open-component-model/cli/internal/render/progress"
)

// Terminal width for the progress bar
const barWidth = 40

// ANSI escape codes for cursor control
const (
	clearLine = "\033[2K" // clear entire line
	cursorUp  = "\033[1A" // move cursor up one line
)

// ANSI escape codes for text styling
const (
	reset     = "\033[0m"
	bold      = "\033[1m"
	underline = "\033[4m"
	dim       = "\033[2m"
	darkGray  = "\033[38;5;238m"
	white     = "\033[38;5;252m"        // soft white for filled bar
	blue      = "\033[38;2;37;125;220m" // OCM brand blue (#257ddc)
	red       = "\033[38;5;196m"        // error red
)

// barVisualizer renders progress as a scrolling log with a progress bar.
// It tracks items by ID and displays the most recent ones above the bar.
type barVisualizer[T any] struct {
	out            io.Writer           // where to write output
	total          int                 // expected number of items
	events         []progress.Event[T] // events in order (oldest first)
	done           chan struct{}       // closed when finished
	maxLogs        int                 // how many log lines to show
	header         string              // text shown above the log
	formatter      Formatter[T]        // converts Data to display name
	errorFormatter ErrorFormatter[T]   // formats errors in summary
	logBuffer      *bytes.Buffer       // shared slog buffer from tracker
}

// NewBarVisualizer creates a factory for the progress bar visualizer.
// Use options like WithHeader, WithNameFormatter to customize behavior.
func NewBarVisualizer[T any](opts ...VisualiserOption[T]) progress.VisualizerFactory[T] {
	return func(out io.Writer, total int) progress.Visualizer[T] {
		v := &barVisualizer[T]{
			out:            out,
			total:          total,
			events:         make([]progress.Event[T], 0, total),
			done:           make(chan struct{}),
			maxLogs:        4,
			errorFormatter: func(_ T, err error) string { return TreeErrorFormatter(err) },
		}
		for _, opt := range opts {
			opt(v)
		}
		if total < v.maxLogs {
			v.maxLogs = total
		}
		v.reserveSpace()
		return v
	}
}

// SetLogBuffer sets the shared slog buffer from the tracker.
func (v *barVisualizer[T]) SetLogBuffer(buf *bytes.Buffer) {
	v.logBuffer = buf
}

// reserveSpace prints header and empty lines to claim terminal space.
func (v *barVisualizer[T]) reserveSpace() {
	for i := 0; i < v.fixedLines(); i++ {
		fmt.Fprintln(v.out)
	}
}

// fixedLines returns the number of lines used by header + logs + bar.
func (v *barVisualizer[T]) fixedLines() int {
	lines := v.maxLogs + 1 // logs + bar
	if v.header != "" {
		lines += 2 // header line + blank line before it
	}
	return lines
}

// renderHeader prints the header line if set.
func (v *barVisualizer[T]) renderHeader() {
	if v.header != "" {
		fmt.Fprintf(v.out, "\n%s%s%s\n", bold+white, v.header, reset)
	}
}

// HandleEvent receives a progress update and refreshes the display.
// Items in final states (Completed, Failed) are not updated again.
func (v *barVisualizer[T]) HandleEvent(event progress.Event[T]) {
	// Find existing event by ID
	for i, e := range v.events {
		if e.ID == event.ID {
			if e.State == progress.Completed || e.State == progress.Failed {
				return // Already in final state
			}
			v.events[i] = event
			v.render()
			return
		}
	}
	// New event - append
	v.events = append(v.events, event)
	v.render()
}

// clearLines moves cursor up and clears the fixed area (header + logs + bar).
func (v *barVisualizer[T]) clearLines() {
	for i := 0; i < v.fixedLines(); i++ {
		fmt.Fprint(v.out, cursorUp+clearLine)
	}
}

// render clears the fixed area, prints any new slog output, then redraws header + logs + bar.
func (v *barVisualizer[T]) render() {
	select {
	case <-v.done:
		return
	default:
	}

	v.clearLines()
	v.drainLogBuffer()
	v.renderHeader()
	v.renderLogs()
	v.renderBar()
}

// drainLogBuffer prints any new slog output above the fixed area.
func (v *barVisualizer[T]) drainLogBuffer() {
	if v.logBuffer == nil || v.logBuffer.Len() == 0 {
		return
	}
	raw := v.logBuffer.String()
	v.logBuffer.Reset()
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if line != "" {
			fmt.Fprintf(v.out, "%s%s\n", line, reset)
		}
	}
}

// renderLogs shows the most recent items (up to maxLogs) with status icons.
func (v *barVisualizer[T]) renderLogs() {
	// Determine which items to show (last maxLogs)
	start := 0
	if len(v.events) > v.maxLogs {
		start = len(v.events) - v.maxLogs
	}
	visible := v.events[start:]

	// Print items first (oldest to newest)
	for _, event := range visible {
		fmt.Fprintln(v.out, v.formatItem(event))
	}

	// Print empty lines after items (padding above progress bar)
	emptyLines := v.maxLogs - len(visible)
	for i := 0; i < emptyLines; i++ {
		fmt.Fprintln(v.out)
	}
}

// renderBar draws the progress bar showing completion percentage and counts.
func (v *barVisualizer[T]) renderBar() {
	// Count completed, failed, and cancelled
	completed, failed, cancelled := 0, 0, 0
	for _, event := range v.events {
		switch event.State {
		case progress.Completed:
			completed++
		case progress.Failed:
			failed++
		case progress.Cancelled:
			cancelled++
		}
	}

	// Progress calculation (only completed items count)
	pct := 0
	if v.total > 0 {
		pct = completed * 100 / v.total
	}
	filled := barWidth * completed / max(v.total, 1)
	empty := barWidth - filled

	// Bar color
	barColor := white

	// Status string
	status := fmt.Sprintf("%s%d/%d%s", bold+white, completed, v.total, reset)
	if failed > 0 {
		status += fmt.Sprintf(" %s(%d failed)%s", red, failed, reset)
	}
	if cancelled > 0 {
		status += fmt.Sprintf(" %s(%d cancelled)%s", darkGray, cancelled, reset)
	}

	// Print progress bar
	fmt.Fprintf(v.out, "  %s[%s%s%s%s%s]%s %s%3d%%%s %s\n",
		bold+darkGray, barColor, strings.Repeat("█", filled),
		darkGray, strings.Repeat("░", empty), bold+darkGray,
		reset, bold+white, pct, reset, status)
}

// formatItem returns a formatted line: "  ✓ display-name" with color.
// Uses the formatter if set, otherwise falls back to item.ID.
func (v *barVisualizer[T]) formatItem(item progress.Event[T]) string {
	var symbol, color string
	switch item.State {
	case progress.Running:
		symbol, color = "⏳", darkGray
	case progress.Completed:
		symbol, color = "✓", blue
	case progress.Failed:
		symbol, color = "✗", red
	case progress.Cancelled:
		symbol, color = "⊘", darkGray
	default:
		symbol, color = "?", darkGray
	}

	displayName := item.ID
	if v.formatter != nil {
		displayName = v.formatter(item.Data)
	}
	return fmt.Sprintf("  %s%s%s %s", color, symbol, reset, displayName)
}

// Summary is called after all events are processed.
// If any items failed, it prints detailed error information.
func (v *barVisualizer[T]) Summary(err error) {
	if v.out == nil {
		return
	}
	close(v.done)

	for _, event := range v.events {
		if event.State == progress.Failed {
			v.renderFailureSummary()
			return
		}
	}
	if err != nil {
		fmt.Fprint(v.out, "\nErrors:\n")
	}
}

// renderFailureSummary lists all failed items with their error details.
func (v *barVisualizer[T]) renderFailureSummary() {
	fmt.Fprint(v.out, "\nErrors:\n")
	for _, event := range v.events {
		if event.State != progress.Failed {
			continue
		}
		fmt.Fprintf(v.out, "  %s✗%s %s%s%s\n", red, reset, underline+white, event.ID, reset)
		if event.Err != nil {
			fmt.Fprintf(v.out, "%s\n", v.errorFormatter(event.Data, event.Err))
		}
	}
}
