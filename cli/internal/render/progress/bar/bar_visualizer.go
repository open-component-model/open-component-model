package bar

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"ocm.software/open-component-model/cli/internal/render/progress"
)

// barVisualizer renders progress as a scrolling log with a progress bar.
type barVisualizer[T any] struct {
	mu             sync.Mutex
	out            io.Writer
	total          int
	events         []progress.Event[T]
	done           chan struct{}
	maxLogs        int
	header         string
	spinnerFrame   int
	dotFrame       int
	errorFormatter func(T, error) string
	logBuffer      *progress.SyncBuffer
}

// NewVisualizer is a [progress.VisualizerFactory] that creates an animated
// progress bar visualizer. For simple operations (total=0), only a spinner
// header is shown with no progress bar.
func NewVisualizer[T any](out io.Writer, total int) progress.Visualizer[T] {
	return &barVisualizer[T]{
		out:            out,
		total:          total,
		errorFormatter: func(_ T, err error) string { return TreeErrorFormatter(err) },
	}
}

// SetErrorFormatter implements [progress.ErrorFormatterSetter].
func (v *barVisualizer[T]) SetErrorFormatter(f func(T, error) string) {
	v.errorFormatter = f
}

// SetLogBuffer sets the shared slog buffer from the tracker.
func (v *barVisualizer[T]) SetLogBuffer(buf *progress.SyncBuffer) {
	v.logBuffer = buf
}

// Begin starts the animation.
func (v *barVisualizer[T]) Begin(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.header = name
	v.events = nil
	v.done = make(chan struct{})
	v.spinnerFrame = 0
	v.dotFrame = 0
	v.maxLogs = min(4, v.total)

	v.reserveSpace()

	RunAnimation(v.done, func(spinFrame, dotFrame int) {
		v.mu.Lock()
		v.dotFrame = dotFrame
		v.spinnerFrame = spinFrame
		v.renderLocked()
		v.mu.Unlock()
	})
}

// HandleEvent receives a progress update and refreshes the display.
func (v *barVisualizer[T]) HandleEvent(event progress.Event[T]) {
	v.mu.Lock()
	defer v.mu.Unlock()

	for i, e := range v.events {
		if e.ID == event.ID {
			if e.State == progress.Completed || e.State == progress.Failed {
				return
			}
			v.events[i] = event
			v.renderLocked()
			return
		}
	}
	v.events = append(v.events, event)
	v.renderLocked()
}

// End stops the animation and renders the final status.
func (v *barVisualizer[T]) End(err error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	select {
	case <-v.done:
	default:
		close(v.done)
	}

	v.clearLines()
	v.drainLogBuffer()

	hasFailures := false
	for _, event := range v.events {
		if event.State == progress.Failed {
			hasFailures = true
			break
		}
	}

	if err != nil || hasFailures {
		WriteFailedLine(v.out, v.header)
	} else {
		WriteCompletedLine(v.out, v.header)
	}

	v.renderEvents()
	if v.total > 0 {
		v.renderBar()
	}

	for _, event := range v.events {
		if event.State == progress.Failed {
			v.renderFailureSummary()
			return
		}
	}
}

// --- rendering ---

func (v *barVisualizer[T]) reserveSpace() {
	for i := 0; i < v.fixedLines(); i++ {
		fmt.Fprintln(v.out)
	}
}

func (v *barVisualizer[T]) fixedLines() int {
	lines := v.maxLogs
	if v.total > 0 {
		lines++ // bar
	}
	if v.header != "" {
		lines++ // header
	}
	return lines
}

func (v *barVisualizer[T]) clearLines() {
	for i := 0; i < v.fixedLines(); i++ {
		fmt.Fprint(v.out, CursorUp+ClearLine)
	}
}

func (v *barVisualizer[T]) renderLocked() {
	select {
	case <-v.done:
		return
	default:
	}

	v.clearLines()
	v.drainLogBuffer()
	v.renderHeader()
	v.renderEvents()
	if v.total > 0 {
		v.renderBar()
	}
}

func (v *barVisualizer[T]) drainLogBuffer() {
	if v.logBuffer == nil || v.logBuffer.Len() == 0 {
		return
	}
	raw := v.logBuffer.DrainString()
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if line != "" {
			fmt.Fprintf(v.out, "%s%s\n", line, Reset)
		}
	}
}

func (v *barVisualizer[T]) renderHeader() {
	if v.header != "" {
		WriteRunningLine(v.out, v.header, v.spinnerFrame, v.dotFrame)
	}
}

func (v *barVisualizer[T]) renderEvents() {
	start := 0
	if len(v.events) > v.maxLogs {
		start = len(v.events) - v.maxLogs
	}
	visible := v.events[start:]

	for _, event := range visible {
		fmt.Fprintln(v.out, v.formatItem(event))
	}

	for i := 0; i < v.maxLogs-len(visible); i++ {
		fmt.Fprintln(v.out)
	}
}

func (v *barVisualizer[T]) renderBar() {
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

	pct := 0
	if v.total > 0 {
		pct = completed * 100 / v.total
	}
	filled := barWidth * completed / max(v.total, 1)
	empty := barWidth - filled

	status := fmt.Sprintf("%s%d/%d%s", Bold+white, completed, v.total, Reset)
	if failed > 0 {
		status += fmt.Sprintf(" %s(%d failed)%s", Red, failed, Reset)
	}
	if cancelled > 0 {
		status += fmt.Sprintf(" %s(%d cancelled)%s", DarkGray, cancelled, Reset)
	}

	fmt.Fprintf(v.out, "  %s[%s%s%s%s%s]%s %s%3d%%%s %s\n",
		Bold+DarkGray, white, strings.Repeat("█", filled),
		DarkGray, strings.Repeat("░", empty), Bold+DarkGray,
		Reset, Bold+white, pct, Reset, status)
}

func (v *barVisualizer[T]) formatItem(item progress.Event[T]) string {
	var symbol, color string
	switch item.State {
	case progress.Running:
		symbol, color = SpinnerIcon(v.spinnerFrame), DarkGray
	case progress.Completed:
		symbol, color = "✓", Blue
	case progress.Failed:
		symbol, color = "✗", Red
	case progress.Cancelled:
		symbol, color = "⊘", DarkGray
	default:
		symbol, color = "?", DarkGray
	}

	displayName := item.Name
	if displayName == "" {
		displayName = item.ID
	}
	return fmt.Sprintf("    %s%s%s %s", color, symbol, Reset, displayName)
}

func (v *barVisualizer[T]) renderFailureSummary() {
	fmt.Fprint(v.out, "\nErrors:\n")
	for _, event := range v.events {
		if event.State != progress.Failed {
			continue
		}
		fmt.Fprintf(v.out, "  %s✗%s %s%s%s\n", Red, Reset, underline+white, event.ID, Reset)
		if event.Err != nil && v.errorFormatter != nil {
			if formatted := v.errorFormatter(event.Data, event.Err); formatted != "" {
				fmt.Fprintf(v.out, "%s\n", formatted)
			}
		}
	}
}
