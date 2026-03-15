package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/byronellis/ragtime/internal/protocol"
)

const maxEvents = 500

// EventFeed is a scrollable list of hook events.
type EventFeed struct {
	events     []protocol.StreamEvent
	offset     int // scroll offset (0 = showing latest at bottom)
	width      int
	height     int
	autoScroll bool
}

// NewEventFeed creates a new event feed.
func NewEventFeed() EventFeed {
	return EventFeed{autoScroll: true}
}

// SetSize updates the viewport dimensions.
func (f *EventFeed) SetSize(w, h int) {
	f.width = w
	f.height = h
}

// Push adds an event to the feed.
func (f *EventFeed) Push(event protocol.StreamEvent) {
	f.events = append(f.events, event)
	if len(f.events) > maxEvents {
		f.events = f.events[len(f.events)-maxEvents:]
		// Adjust offset for trimmed events
		if f.offset > 0 {
			f.offset--
			if f.offset < 0 {
				f.offset = 0
			}
		}
	}
	if f.autoScroll {
		f.offset = 0
	}
}

// ScrollUp moves the view up.
func (f *EventFeed) ScrollUp(n int) {
	f.autoScroll = false
	maxOffset := len(f.events) - f.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	f.offset += n
	if f.offset > maxOffset {
		f.offset = maxOffset
	}
}

// ScrollDown moves the view down.
func (f *EventFeed) ScrollDown(n int) {
	f.offset -= n
	if f.offset <= 0 {
		f.offset = 0
		f.autoScroll = true
	}
}

// ScrollToBottom jumps to the latest events.
func (f *EventFeed) ScrollToBottom() {
	f.offset = 0
	f.autoScroll = true
}

// ScrollToTop jumps to the oldest events.
func (f *EventFeed) ScrollToTop() {
	f.autoScroll = false
	maxOffset := len(f.events) - f.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	f.offset = maxOffset
}

// View renders the event feed.
func (f EventFeed) View() string {
	if len(f.events) == 0 {
		msg := lipgloss.NewStyle().Foreground(colorDim).Render("Waiting for events...")
		return lipgloss.Place(f.width, f.height, lipgloss.Center, lipgloss.Center, msg)
	}

	// Calculate visible window (offset is from the bottom)
	end := len(f.events) - f.offset
	start := end - f.height
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}

	lines := make([]string, 0, f.height)
	for i := start; i < end; i++ {
		lines = append(lines, f.renderEvent(f.events[i]))
	}

	// Pad remaining height
	for len(lines) < f.height {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

func (f EventFeed) renderEvent(event protocol.StreamEvent) string {
	if event.Event == nil {
		return ""
	}
	e := event.Event

	ts := eventTimeStyle.Render(event.Timestamp.Local().Format("15:04:05"))

	tool := e.ToolName
	if tool == "" {
		tool = e.EventType
	}
	toolStr := eventToolStyle.Render(tool)

	detail := shortDetail(e)
	// Truncate detail to fit width (leave room for time, tool, decision, spacing)
	maxDetail := f.width - 8 - 8 - 8 - 4 // rough estimate
	if maxDetail < 0 {
		maxDetail = 20
	}
	if len(detail) > maxDetail {
		detail = detail[:maxDetail] + "..."
	}
	detailStr := eventDetailStyle.Render(detail)

	var decStr string
	switch e.EventType {
	case "tool_use":
		// No decision to show for pre-hook events
	default:
		// Show decision if any is present in the event
	}

	// For the event feed, we show the event type styling based on what happened
	line := fmt.Sprintf("%s %s %s", ts, toolStr, detailStr)
	if decStr != "" {
		line += " " + decStr
	}

	return line
}

// shortDetail extracts a compact detail string from a hook event.
func shortDetail(e *protocol.HookEvent) string {
	if e.Prompt != "" {
		p := e.Prompt
		if len(p) > 80 {
			p = p[:80] + "..."
		}
		return p
	}

	if len(e.ToolInput) == 0 {
		return ""
	}

	switch e.ToolName {
	case "Read", "Write", "Edit":
		return shortPath(strField(e.ToolInput, "file_path"))
	case "Bash":
		cmd := strField(e.ToolInput, "command")
		if len(cmd) > 80 {
			cmd = cmd[:80] + "..."
		}
		return cmd
	case "Grep":
		pattern := strField(e.ToolInput, "pattern")
		return fmt.Sprintf("%q", pattern)
	case "Glob":
		return strField(e.ToolInput, "pattern")
	case "Agent":
		return strField(e.ToolInput, "description")
	default:
		for _, key := range []string{"file_path", "path", "command", "query", "description"} {
			if v := strField(e.ToolInput, key); v != "" {
				return shortPath(v)
			}
		}
		return ""
	}
}

func strField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func shortPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) <= 3 {
		return path
	}
	return strings.Join(parts[len(parts)-3:], "/")
}
