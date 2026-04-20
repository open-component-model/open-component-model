package bar

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// SpinnerIcon returns the spinner character for the given frame.
func SpinnerIcon(frame int) string {
	return SpinnerFrames[frame%len(SpinnerFrames)]
}

// RenderShimmer returns the text with a brightness wave sweeping across it.
// A highlight window moves left-to-right based on the frame counter, making
// characters brighter as it passes.
func RenderShimmer(text string, frame int) string {
	runes := []rune(text)
	textLen := len(runes)
	windowSize := len(ShimmerGradient)
	pos := frame % (textLen + windowSize)

	var result strings.Builder
	for i, ch := range runes {
		dist := pos - i
		if dist >= 0 && dist < windowSize {
			result.WriteString(ShimmerGradient[dist])
		} else {
			result.WriteString(ShimmerBase)
		}
		result.WriteRune(ch)
	}
	return result.String()
}

// RunAnimation starts a dual-ticker animation loop in a new goroutine.
// The spinner callback fires every 80ms; the dots callback fires every 400ms.
// Both stop when done is closed.
func RunAnimation(done <-chan struct{}, onSpin func(spinFrame, dotFrame int)) {
	go func() {
		spinTicker := time.NewTicker(80 * time.Millisecond)
		dotTicker := time.NewTicker(400 * time.Millisecond)
		defer spinTicker.Stop()
		defer dotTicker.Stop()

		spinFrame, dotFrame := 1, 0
		for {
			select {
			case <-done:
				return
			case <-dotTicker.C:
				dotFrame++
			case <-spinTicker.C:
				onSpin(spinFrame, dotFrame)
				spinFrame++
			}
		}
	}()
}

// WriteRunningLine writes an animated header line with spinner, shimmer, and dots.
func WriteRunningLine(out io.Writer, text string, spinFrame, dotFrame int) {
	dots := DotFrames[dotFrame%len(DotFrames)]
	fullText := text + dots
	shimmer := RenderShimmer(fullText, spinFrame)
	fmt.Fprintf(out, "%s%s%s %s%s%s\n",
		DarkGray, SpinnerIcon(spinFrame), Reset, Bold, shimmer, Reset)
}

// WriteCompletedLine writes a completed status line: "✓ text..."
func WriteCompletedLine(out io.Writer, text string) {
	fmt.Fprintf(out, "%s✓%s %s...\n", Blue, Reset, text)
}

// WriteFailedLine writes a failed status line: "✗ text..."
func WriteFailedLine(out io.Writer, text string) {
	fmt.Fprintf(out, "%s✗%s %s...\n", Red, Reset, text)
}
