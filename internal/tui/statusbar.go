package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/byronellis/ragtime/internal/protocol"
)

// StatusBar renders the top status line.
type StatusBar struct {
	info     protocol.DaemonInfo
	sessions int
	project  string // last seen CWD project name
	width    int
}

// NewStatusBar creates a status bar from daemon info.
func NewStatusBar(info protocol.DaemonInfo) StatusBar {
	return StatusBar{info: info}
}

// SetWidth updates the bar width.
func (s *StatusBar) SetWidth(w int) {
	s.width = w
}

// SetSessions updates the session count.
func (s *StatusBar) SetSessions(n int) {
	s.sessions = n
}

// SetProject updates the project name from a CWD path.
func (s *StatusBar) SetProject(cwd string) {
	if cwd == "" {
		return
	}
	s.project = filepath.Base(cwd)
}

// View renders the status bar.
func (s StatusBar) View() string {
	dot := statusDotStyle.Render("\u25cf")
	title := titleStyle.Render("ragtime")

	pid := statusLabelStyle.Render("pid:") + fmt.Sprintf("%d", s.info.PID)
	uptime := statusLabelStyle.Render("up:") + formatDuration(time.Since(s.info.StartedAt))
	rules := statusLabelStyle.Render("rules:") + fmt.Sprintf("%d", s.info.RuleCount)
	sess := statusLabelStyle.Render("sessions:") + fmt.Sprintf("%d", s.sessions)

	content := fmt.Sprintf("%s %s  %s  %s  %s  %s", title, dot, pid, uptime, rules, sess)

	if s.project != "" {
		proj := statusLabelStyle.Render("project:") + s.project
		content += "  " + proj
	}

	return statusBarStyle.Width(s.width).Render(content)
}

// formatDuration returns a compact duration string.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	var parts []string
	parts = append(parts, fmt.Sprintf("%dh", h))
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	return strings.Join(parts, "")
}

// DisconnectedStatusBar renders a status bar showing disconnected state.
func DisconnectedStatusBar(width int) string {
	dot := lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("\u25cf")
	title := titleStyle.Render("ragtime")
	status := lipgloss.NewStyle().Foreground(colorRed).Render("disconnected")
	content := fmt.Sprintf("%s %s  %s", title, dot, status)
	return statusBarStyle.Width(width).Render(content)
}
