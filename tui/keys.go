package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap defines all keybindings for the TUI.
type keyMap struct {
	Submit key.Binding
	Quit   key.Binding
	Clear  key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Clear: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "clear"),
		),
	}
}
