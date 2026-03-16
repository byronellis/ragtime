package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/byronellis/ragtime/internal/protocol"
)

// TestInteractor implements starlark.Interactor by launching an interactive
// Bubble Tea modal. Used by `rt hook --test --tui` so rule authors can see
// and interact with their prompts exactly as they appear in the dashboard.
type TestInteractor struct{}

// Prompt launches a fullscreen modal and blocks until the user responds or timeout.
func (ti *TestInteractor) Prompt(text string, interType protocol.InteractionType, defaultVal string, timeoutSec int) protocol.InteractionResponse {
	req := protocol.InteractionRequest{
		ID:         "test",
		Text:       text,
		Type:       interType,
		Default:    defaultVal,
		TimeoutSec: timeoutSec,
	}

	m := testModalModel{request: req}
	p := tea.NewProgram(m, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		return protocol.InteractionResponse{ID: req.ID, Value: defaultVal}
	}

	final := result.(testModalModel)
	return final.response
}

// testModalModel is a minimal tea.Model that shows a single interaction modal.
type testModalModel struct {
	request  protocol.InteractionRequest
	modal    InteractionModal
	response protocol.InteractionResponse
	ready    bool
	width    int
	height   int
}

func (m testModalModel) Init() tea.Cmd {
	return nil
}

func (m testModalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.modal = NewInteractionModal(m.request, m.width, m.height)
			m.ready = true
			return m, tickCmd()
		}
		m.modal.SetSize(m.width, m.height)
		return m, nil

	case tea.KeyMsg:
		if !m.ready {
			return m, nil
		}
		if msg.String() == "ctrl+c" {
			m.response = protocol.InteractionResponse{
				ID:    m.request.ID,
				Value: m.request.Default,
			}
			return m, tea.Quit
		}
		modal, cmd := m.modal.Update(msg)
		m.modal = modal
		return m, cmd

	case InteractionTickMsg:
		if !m.ready {
			return m, nil
		}
		modal, cmd := m.modal.Update(msg)
		m.modal = modal
		if cmd != nil {
			// Modal returned a dismiss command — let Bubble Tea dispatch it
			return m, cmd
		}
		// Still counting down — schedule next tick
		return m, tickCmd()

	case InteractionDismissMsg:
		m.response = msg.Response
		return m, tea.Quit
	}

	return m, nil
}

func (m testModalModel) View() string {
	if !m.ready {
		return ""
	}
	return m.modal.View()
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return InteractionTickMsg{}
	})
}
