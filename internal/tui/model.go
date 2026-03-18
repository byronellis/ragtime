package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/byronellis/ragtime/internal/protocol"
)

// Model is the top-level Bubble Tea model for the TUI dashboard.
type Model struct {
	client        *Client
	statusBar     StatusBar
	sessionsPanel SessionsPanel
	eventFeed     EventFeed
	helpBar       HelpBar
	interaction   *InteractionModal
	connected     bool
	disconnectErr error
	width         int
	height        int
}

// NewModel creates the TUI model from an established client connection.
func NewModel(client *Client, info *protocol.SubscribeResponse) Model {
	sb := NewStatusBar(info.DaemonInfo)
	sp := NewSessionsPanel()
	sp.InitSessions(info.Sessions)
	sb.SetSessions(sp.Count())
	return Model{
		client:        client,
		statusBar:     sb,
		sessionsPanel: sp,
		eventFeed:     NewEventFeed(),
		connected:     true,
	}
}

// Init starts the event read loop.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return nil },
		uptimeTickCmd(),
	)
}

// interactionTickCmd returns a command that fires a tick after one second.
func interactionTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return InteractionTickMsg{}
	})
}

// UptimeTickMsg triggers a periodic redraw to keep the uptime timer current.
type UptimeTickMsg struct{}

func uptimeTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return UptimeTickMsg{}
	})
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// When modal is active, route all input to it
	if m.interaction != nil {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			modal, cmd := m.interaction.Update(msg)
			m.interaction = &modal
			return m, cmd

		case InteractionTickMsg:
			modal, cmd := m.interaction.Update(msg)
			m.interaction = &modal
			if cmd != nil {
				return m, cmd // dismiss command
			}
			return m, interactionTickCmd() // keep ticking

		case InteractionDismissMsg:
			m.client.SendInteractionResponse(msg.Response)
			m.interaction = nil
			return m, nil

		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			m.recalcLayout()
			m.interaction.SetSize(m.width, m.height)
			return m, nil

		case EventMsg:
			if msg.Event.Event != nil {
				m.eventFeed.Push(msg.Event)
			}
			m.sessionsPanel.Update(msg.Event)
			m.statusBar.SetSessions(m.sessionsPanel.Count())
			m.updateProject(msg.Event)
			m.recalcLayout()
			return m, nil

		case DisconnectedMsg:
			m.connected = false
			m.disconnectErr = msg.Err
			m.interaction = nil // dismiss modal on disconnect
			return m, nil
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.eventFeed.ScrollDown(1)
		case "k", "up":
			m.eventFeed.ScrollUp(1)
		case "G":
			m.eventFeed.ScrollToBottom()
		case "g":
			m.eventFeed.ScrollToTop()
		case "pgdown":
			m.eventFeed.ScrollDown(m.feedHeight())
		case "pgup":
			m.eventFeed.ScrollUp(m.feedHeight())
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()

	case EventMsg:
		if msg.Event.Event != nil {
			m.eventFeed.Push(msg.Event)
		}
		m.sessionsPanel.Update(msg.Event)
		m.statusBar.SetSessions(m.sessionsPanel.Count())
		m.updateProject(msg.Event)
		m.recalcLayout()

	case InteractionRequestMsg:
		modal := NewInteractionModal(msg.Request, m.width, m.height)
		m.interaction = &modal
		return m, interactionTickCmd()

	case UptimeTickMsg:
		return m, uptimeTickCmd()

	case DisconnectedMsg:
		m.connected = false
		m.disconnectErr = msg.Err
	}

	return m, nil
}

// updateProject sets the status bar project from an event's CWD.
func (m *Model) updateProject(event protocol.StreamEvent) {
	if event.Event != nil && event.Event.CWD != "" {
		m.statusBar.SetProject(event.Event.CWD)
	}
}

// recalcLayout distributes height between the sessions panel and event feed.
func (m *Model) recalcLayout() {
	m.statusBar.SetWidth(m.width)
	m.helpBar.SetWidth(m.width)

	sessHeight := m.sessionsPanelHeight()
	m.sessionsPanel.SetSize(m.width, sessHeight)

	feedHeight := m.height - 2 - sessHeight // 2 = status bar + help bar
	if sessHeight > 0 {
		feedHeight-- // border line between panels
	}
	if feedHeight < 1 {
		feedHeight = 1
	}
	m.eventFeed.SetSize(m.width, feedHeight)
}

// sessionsPanelHeight returns the height for the sessions panel.
// Grows with session count, capped to avoid dominating the screen.
func (m Model) sessionsPanelHeight() int {
	count := m.sessionsPanel.Count()
	if count == 0 {
		return 0
	}
	// 1 header + N session rows, max 25% of screen
	h := count + 1
	maxH := m.height / 4
	if maxH < 3 {
		maxH = 3
	}
	if h > maxH {
		h = maxH
	}
	return h
}

// feedHeight returns the available height for the event feed.
func (m Model) feedHeight() int {
	sessHeight := m.sessionsPanelHeight()
	h := m.height - 2 - sessHeight
	if sessHeight > 0 {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// View renders the full TUI.
func (m Model) View() string {
	// Modal overlay takes over the entire screen
	if m.interaction != nil {
		return m.interaction.View()
	}

	var status string
	if m.connected {
		status = m.statusBar.View()
	} else {
		status = DisconnectedStatusBar(m.width)
	}

	var sections []string
	sections = append(sections, status)

	if m.sessionsPanel.Count() > 0 {
		sections = append(sections, m.sessionsPanel.View())
	}

	sections = append(sections, m.eventFeed.View())
	sections = append(sections, m.helpBar.View())

	return joinSections(sections)
}

// joinSections joins view sections with newlines.
func joinSections(sections []string) string {
	result := ""
	for i, s := range sections {
		if i > 0 {
			result += "\n"
		}
		result += s
	}
	return result
}
