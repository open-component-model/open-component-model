package tui

import "github.com/charmbracelet/lipgloss"

// Styles holds all lipgloss styles used across the TUI.
type Styles struct {
	// Layout
	TreePane   lipgloss.Style
	DetailPane lipgloss.Style

	// Tree
	TreeCursor    lipgloss.Style
	TreeExpanded  lipgloss.Style
	TreeCollapsed lipgloss.Style
	TreeLeaf      lipgloss.Style
	TreeLoading   lipgloss.Style

	// Detail
	DetailKey   lipgloss.Style
	DetailValue lipgloss.Style

	// Status bar
	StatusBar   lipgloss.Style
	StatusTitle lipgloss.Style
	StatusRef   lipgloss.Style
	StatusHelp  lipgloss.Style

	// General
	Error lipgloss.Style
}

// DefaultStyles returns the default style configuration.
func DefaultStyles() Styles {
	subtle := lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}
	highlight := lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	errColor := lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"}

	border := lipgloss.NormalBorder()

	return Styles{
		TreePane: lipgloss.NewStyle().
			BorderStyle(border).
			BorderRight(true).
			BorderForeground(subtle),
		DetailPane: lipgloss.NewStyle().
			PaddingLeft(1),

		TreeCursor: lipgloss.NewStyle().
			Foreground(highlight).
			Bold(true),
		TreeExpanded: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#DDDDDD"}),
		TreeCollapsed: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#DDDDDD"}),
		TreeLeaf: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"}),
		TreeLoading: lipgloss.NewStyle().
			Foreground(subtle).
			Italic(true),

		DetailKey: lipgloss.NewStyle().
			Foreground(highlight).
			Bold(true),
		DetailValue: lipgloss.NewStyle(),

		StatusBar: lipgloss.NewStyle().
			Padding(0, 1),
		StatusTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(highlight),
		StatusRef: lipgloss.NewStyle().
			Foreground(subtle),
		StatusHelp: lipgloss.NewStyle().
			Foreground(subtle),

		Error: lipgloss.NewStyle().
			Foreground(errColor).
			Bold(true),
	}
}
