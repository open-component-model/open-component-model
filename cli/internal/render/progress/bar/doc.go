// Package bar provides an ANSI terminal implementation of [progress.Visualizer].
//
// [NewVisualizer] creates an animated progress visualizer. For operations with
// events (total > 0), it renders a scrolling item log above a progress bar.
// For simple operations (total = 0), only a spinner header is shown.
//
// Terminal layout for tracked phases:
//
//	⠋ Transferring component versions...
//	    ✓ item-1
//	    ⠋ item-2
//	  [████░░░░] 50%  2/4
package bar
