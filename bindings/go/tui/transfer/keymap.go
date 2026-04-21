package transfer

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the key bindings for the transfer wizard.
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Submit key.Binding
}

// DefaultKeyMap returns the default transfer wizard key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("j", "down"),
		),
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
	}
}
