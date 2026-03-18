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

	// Event type tag — distinguish pre/post/prompt/stop/etc
	tag := eventTag(e.EventType)

	// Tool or event kind
	tool := e.ToolName
	if tool == "" {
		tool = ""
	}
	toolStr := eventToolStyle.Render(tool)

	// Detail content (strip newlines — events are single-line in the feed)
	detail := strings.ReplaceAll(eventDetail(e), "\n", " ")

	// Truncate detail to fit width
	// time(8) + tag(6) + tool(8) + spaces(4) = ~26 chars overhead
	maxDetail := f.width - 26
	if maxDetail < 20 {
		maxDetail = 20
	}
	if len(detail) > maxDetail {
		detail = detail[:maxDetail] + "..."
	}
	detailStr := eventDetailStyle.Render(detail)

	if tool == "" {
		return fmt.Sprintf("%s %s %s", ts, tag, detailStr)
	}
	return fmt.Sprintf("%s %s %s %s", ts, tag, toolStr, detailStr)
}

// eventTag returns a styled short label for the event type.
func eventTag(eventType string) string {
	switch eventType {
	case "pre-tool-use":
		return eventTagPreStyle.Render("PRE")
	case "post-tool-use":
		return eventTagPostStyle.Render("POST")
	case "user-prompt-submit":
		return eventTagPromptStyle.Render("USR")
	case "stop":
		return eventTagStopStyle.Render("STOP")
	case "notification":
		return eventTagNotifyStyle.Render("NOTE")
	case "session-start":
		return eventTagSessionStyle.Render("SESS")
	case "subagent-stop":
		return eventTagPostStyle.Render("SUB")
	default:
		return eventTagDimStyle.Render(strings.ToUpper(eventType))
	}
}

// eventDetail extracts a compact detail string from a hook event.
func eventDetail(e *protocol.HookEvent) string {
	switch e.EventType {
	case "user-prompt-submit":
		if e.Prompt != "" {
			p := e.Prompt
			if len(p) > 120 {
				p = p[:120] + "..."
			}
			return p
		}
		return ""
	case "stop":
		if e.Response != "" {
			r := e.Response
			// Show first line or first 120 chars of agent response
			if idx := strings.IndexByte(r, '\n'); idx > 0 && idx < 120 {
				r = r[:idx]
			} else if len(r) > 120 {
				r = r[:120] + "..."
			}
			return r
		}
		return "turn complete"
	case "notification":
		if e.Response != "" {
			r := e.Response
			if len(r) > 120 {
				r = r[:120] + "..."
			}
			return r
		}
		return ""
	case "session-start":
		return shortSessionID(e.SessionID)
	}

	// Tool events
	return shortToolDetail(e)
}

// shortToolDetail extracts a compact detail string from tool events.
func shortToolDetail(e *protocol.HookEvent) string {
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

func shortSessionID(id string) string {
	if len(id) > 12 {
		return id[:12] + "..."
	}
	return id
}
