package components

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
			PaddingRight(1)

	refStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
)

// StatusBar renders a top status bar with title and reference info.
func StatusBar(width int, title, reference string) string {
	left := titleStyle.Render(title)
	right := refStyle.Render(reference)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := lipgloss.NewStyle().
		Width(width).
		Render(left + lipgloss.NewStyle().Width(gap).Render("") + right)

	return bar
}
