package bar

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderShimmer(t *testing.T) {
	t.Run("output contains all characters", func(t *testing.T) {
		result := RenderShimmer("Hello", 0)
		stripped := stripANSI(result)
		assert.Equal(t, "Hello", stripped)
	})

	t.Run("different frames produce different output", func(t *testing.T) {
		r1 := RenderShimmer("Hello", 0)
		r2 := RenderShimmer("Hello", 1)
		assert.NotEqual(t, r1, r2, "different frames should produce different shimmer output")
	})

	t.Run("wraps around", func(t *testing.T) {
		text := "Hi"
		windowSize := len(ShimmerGradient)
		period := len([]rune(text)) + windowSize
		r1 := RenderShimmer(text, 0)
		r2 := RenderShimmer(text, period)
		assert.Equal(t, r1, r2, "shimmer should repeat after full cycle")
	})

	t.Run("empty text returns empty", func(t *testing.T) {
		result := RenderShimmer("", 0)
		assert.Empty(t, result)
	})
}

func TestSpinnerIcon(t *testing.T) {
	assert.Equal(t, SpinnerFrames[0], SpinnerIcon(0))
	assert.Equal(t, SpinnerFrames[1], SpinnerIcon(1))
	assert.Equal(t, SpinnerFrames[0], SpinnerIcon(len(SpinnerFrames)), "should wrap around")
}

func TestWriteRunningLine(t *testing.T) {
	var buf bytes.Buffer
	WriteRunningLine(&buf, "Loading", 0, 0)
	output := stripANSI(buf.String())
	assert.Contains(t, output, SpinnerFrames[0])
	assert.Contains(t, output, "Loading.")
}

func TestWriteCompletedLine(t *testing.T) {
	var buf bytes.Buffer
	WriteCompletedLine(&buf, "Loading")
	output := buf.String()
	assert.Contains(t, output, "✓")
	assert.Contains(t, output, "Loading...")
}

func TestWriteFailedLine(t *testing.T) {
	var buf bytes.Buffer
	WriteFailedLine(&buf, "Loading")
	output := buf.String()
	assert.Contains(t, output, "✗")
	assert.Contains(t, output, "Loading...")
}

func TestRunAnimation(t *testing.T) {
	done := make(chan struct{})
	called := make(chan struct{}, 1)
	RunAnimation(done, func(spin, dot int) {
		select {
		case called <- struct{}{}:
		default:
		}
	})
	<-called // wait for at least one callback
	close(done)
}

// stripANSI removes ANSI escape codes from a string for testing.
func stripANSI(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
