package explorer

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the key bindings for the explorer view.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Expand   key.Binding
	Collapse key.Binding
	PageUp   key.Binding
	PageDown key.Binding
}

// DefaultKeyMap returns the default explorer key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("k/up", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("j/down", "down"),
		),
		Expand: key.NewBinding(
			key.WithKeys("enter", "right", "l"),
			key.WithHelp("enter", "expand"),
		),
		Collapse: key.NewBinding(
			key.WithKeys("esc", "left", "h"),
			key.WithHelp("esc", "collapse"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdown", "page down"),
		),
	}
}
