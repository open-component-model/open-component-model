package bar

// ANSI escape codes for terminal rendering.
// Centralised here so that theme changes only need one edit.
const (
	Reset    = "\033[0m"
	Bold     = "\033[1m"
	DarkGray = "\033[38;5;238m"
	Blue     = "\033[38;2;37;125;220m" // OCM brand blue (#257ddc)
	Red      = "\033[38;5;196m"

	ClearLine = "\033[2K"
	CursorUp  = "\033[1A"

	underline = "\033[4m"
	dim       = "\033[2m"
	white     = "\033[38;5;252m" // soft white for filled bar

	barWidth = 40 // terminal width for the progress bar
)

// SpinnerFrames contains braille spinner characters for progress animation.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// DotFrames contains the cycling dot animation frames for headers.
var DotFrames = []string{".", "..", "..."}

// ShimmerGradient defines brightness levels for the shimmer wave effect.
var ShimmerGradient = []string{
	"\033[38;5;15m", // pure bright white (peak)
	"\033[38;5;255m",
	"\033[38;5;254m",
}

// ShimmerBase is the default color for non-highlighted shimmer characters.
const ShimmerBase = "\033[38;5;252m"
