package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/byronellis/ragtime/internal/protocol"
)

// Model is the top-level Bubble Tea model for the TUI dashboard.
type Model struct {
	client        *Client
	statusBar     StatusBar
	eventFeed     EventFeed
	helpBar       HelpBar
	connected     bool
	disconnectErr error
	sessions      map[string]bool // track unique "agent:session" keys
	width         int
	height        int
}

// NewModel creates the TUI model from an established client connection.
func NewModel(client *Client, info *protocol.SubscribeResponse) Model {
	sb := NewStatusBar(info.DaemonInfo)
	sb.SetSessions(len(info.Sessions))
	return Model{
		client:    client,
		statusBar: sb,
		eventFeed: NewEventFeed(),
		connected: true,
		sessions:  make(map[string]bool),
	}
}

// Init starts the event read loop.
func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		return nil
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.statusBar.SetWidth(msg.Width)
		m.helpBar.SetWidth(msg.Width)
		m.eventFeed.SetSize(msg.Width, m.feedHeight())

	case EventMsg:
		m.eventFeed.Push(msg.Event)
		m.trackSession(msg.Event)

	case DisconnectedMsg:
		m.connected = false
		m.disconnectErr = msg.Err
	}

	return m, nil
}

// trackSession updates session count and project from incoming events.
func (m *Model) trackSession(event protocol.StreamEvent) {
	if event.Event == nil {
		return
	}
	e := event.Event
	if e.SessionID != "" {
		key := e.Agent + ":" + e.SessionID
		if !m.sessions[key] {
			m.sessions[key] = true
			m.statusBar.SetSessions(len(m.sessions))
		}
	}
	if e.CWD != "" {
		m.statusBar.SetProject(e.CWD)
	}
}

// View renders the full TUI.
func (m Model) View() string {
	var status string
	if m.connected {
		status = m.statusBar.View()
	} else {
		status = DisconnectedStatusBar(m.width)
	}

	feed := m.eventFeed.View()
	help := m.helpBar.View()

	return status + "\n" + feed + "\n" + help
}

// feedHeight returns the available height for the event feed.
func (m Model) feedHeight() int {
	// Total height minus status bar (1) and help bar (1) and newlines (2)
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}
