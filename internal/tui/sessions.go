package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/byronellis/ragtime/internal/protocol"
)

// SessionsPanel displays active sessions in a compact table.
type SessionsPanel struct {
	sessions map[string]*sessionEntry // keyed by "agent:sessionID"
	width    int
	height   int
}

type sessionEntry struct {
	info      protocol.SessionInfo
	project   string
	lastEvent time.Time
}

// NewSessionsPanel creates a new sessions panel.
func NewSessionsPanel() SessionsPanel {
	return SessionsPanel{
		sessions: make(map[string]*sessionEntry),
	}
}

// SetSize updates the panel dimensions.
func (p *SessionsPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// InitSessions loads the initial session snapshot.
func (p *SessionsPanel) InitSessions(infos []protocol.SessionInfo) {
	for _, info := range infos {
		key := info.Agent + ":" + info.SessionID
		p.sessions[key] = &sessionEntry{
			info:      info,
			lastEvent: info.LastEvent,
		}
	}
}

// Update processes an incoming event to update session state.
func (p *SessionsPanel) Update(event protocol.StreamEvent) {
	if event.Session != nil {
		key := event.Session.Agent + ":" + event.Session.SessionID
		entry, ok := p.sessions[key]
		if !ok {
			entry = &sessionEntry{}
			p.sessions[key] = entry
		}
		entry.info = *event.Session
		entry.lastEvent = event.Session.LastEvent
		return
	}

	if event.Event == nil {
		return
	}
	e := event.Event
	if e.SessionID == "" {
		return
	}

	key := e.Agent + ":" + e.SessionID
	entry, ok := p.sessions[key]
	if !ok {
		entry = &sessionEntry{
			info: protocol.SessionInfo{
				Agent:     e.Agent,
				SessionID: e.SessionID,
				StartedAt: event.Timestamp,
			},
		}
		p.sessions[key] = entry
	}
	entry.info.EventCount++
	entry.lastEvent = event.Timestamp
	entry.info.LastEvent = event.Timestamp
	if e.CWD != "" {
		entry.project = projectName(e.CWD)
	}
}

// Count returns the number of tracked sessions.
func (p *SessionsPanel) Count() int {
	return len(p.sessions)
}

// View renders the sessions panel.
func (p SessionsPanel) View() string {
	if len(p.sessions) == 0 {
		empty := lipgloss.NewStyle().Foreground(colorDim).Render("No active sessions")
		return sessionPanelStyle.Width(p.width).Height(p.height).Render(empty)
	}

	// Sort sessions by last activity (most recent first)
	entries := make([]*sessionEntry, 0, len(p.sessions))
	for _, e := range p.sessions {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastEvent.After(entries[j].lastEvent)
	})

	// Header
	header := sessionHeaderStyle.Render(
		fmt.Sprintf("%-8s %-14s %-16s %6s  %s",
			"AGENT", "SESSION", "PROJECT", "EVENTS", "LAST ACTIVE"))

	lines := []string{header}

	// Limit rows to available height (minus header)
	maxRows := p.height - 1
	if maxRows < 0 {
		maxRows = 0
	}

	for i, e := range entries {
		if i >= maxRows {
			break
		}
		lines = append(lines, p.renderSession(e))
	}

	// Pad to height
	for len(lines) < p.height {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")
	return sessionPanelStyle.Width(p.width).Render(content)
}

func (p SessionsPanel) renderSession(e *sessionEntry) string {
	agent := sessionAgentStyle.Render(fmt.Sprintf("%-8s", e.info.Agent))

	sid := e.info.SessionID
	if len(sid) > 12 {
		sid = sid[:12] + ".."
	}
	session := sessionIDStyle.Render(fmt.Sprintf("%-14s", sid))

	proj := e.project
	if proj == "" {
		proj = "-"
	}
	if len(proj) > 16 {
		proj = proj[:16]
	}
	project := sessionProjectStyle.Render(fmt.Sprintf("%-16s", proj))

	events := fmt.Sprintf("%6d", e.info.EventCount)

	ago := formatAgo(time.Since(e.lastEvent))
	last := sessionAgoStyle.Render(ago)

	return fmt.Sprintf("%s %s %s %s  %s", agent, session, project, events, last)
}

// formatAgo returns a compact "time ago" string.
func formatAgo(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Second {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// projectName extracts the last path component from a CWD.
func projectName(cwd string) string {
	if cwd == "" {
		return ""
	}
	// Simple: last path component
	parts := strings.Split(cwd, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return cwd
}
