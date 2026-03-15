package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Run connects to the daemon and starts the TUI dashboard.
func Run(socketPath string) error {
	client, info, err := Connect(socketPath)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	model := NewModel(client, info)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start reading events in the background
	go client.ReadLoop(p)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	return nil
}
