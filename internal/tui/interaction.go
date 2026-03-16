package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"

	"github.com/byronellis/ragtime/internal/protocol"
)

// InteractionRequestMsg is sent when the daemon pushes an interaction to the TUI.
type InteractionRequestMsg struct {
	Request protocol.InteractionRequest
}

// InteractionTickMsg fires every second to update the countdown.
type InteractionTickMsg struct{}

// InteractionDismissMsg is sent internally when the user makes a choice.
type InteractionDismissMsg struct {
	Response protocol.InteractionResponse
}

// InteractionModal renders a modal overlay for user prompts.
type InteractionModal struct {
	request      protocol.InteractionRequest
	countdown    int
	focused      int      // which button is focused
	buttons      []string // button labels
	input        textinput.Model
	renderedBody string // pre-rendered markdown
	width        int
	height       int
}

// NewInteractionModal creates a modal for the given request.
func NewInteractionModal(req protocol.InteractionRequest, width, height int) InteractionModal {
	var buttons []string
	switch req.Type {
	case protocol.InteractionOKCancel:
		buttons = []string{"OK", "Cancel"}
	case protocol.InteractionApproveDenyCancel:
		buttons = []string{"Approve", "Deny", "Cancel"}
	case protocol.InteractionFreeform:
		buttons = []string{"Submit", "Cancel"}
	default:
		buttons = []string{"OK", "Cancel"}
	}

	ti := textinput.New()
	ti.Placeholder = "Type your response..."
	ti.CharLimit = 500
	if req.Type == protocol.InteractionFreeform {
		ti.Focus()
	}

	m := InteractionModal{
		request:   req,
		countdown: req.TimeoutSec,
		focused:   0,
		buttons:   buttons,
		input:     ti,
		width:     width,
		height:    height,
	}
	m.renderBody()
	return m
}

// modalStyle returns a glamour style tuned for modal rendering:
// headers are bold colored text (no ## prefixes), code blocks are highlighted,
// and the document has no extra margins.
func modalStyle() ansi.StyleConfig {
	t := true
	blue := "#87afff"
	cyan := "#87ffff"
	white := "#ffffff"
	dim := "#626262"
	indent := uint(2)
	zero := uint(0)

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
			Margin:         &zero,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  &t,
				Color: &blue,
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  &t,
				Color: &blue,
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  &t,
				Color: &blue,
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  &t,
				Color: &cyan,
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  &t,
				Color: &cyan,
			},
		},
		Strong: ansi.StylePrimitive{
			Bold:  &t,
			Color: &white,
		},
		Emph: ansi.StylePrimitive{
			Italic: &t,
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:  &dim,
				Italic: &t,
			},
			Indent: &indent,
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: &cyan,
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				Margin: &zero,
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{Color: &white},
			},
		},
		List: ansi.StyleList{
			StyleBlock: ansi.StyleBlock{},
			LevelIndent: 2,
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "  • ",
		},
		Paragraph: ansi.StyleBlock{},
		Text: ansi.StylePrimitive{
			Color: &white,
		},
	}
}

// renderBody pre-renders the request text as markdown.
func (m *InteractionModal) renderBody() {
	contentWidth := m.contentWidth()
	if contentWidth < 10 {
		contentWidth = 40
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(modalStyle()),
		glamour.WithWordWrap(contentWidth),
	)
	if err != nil {
		m.renderedBody = modalTextStyle.Width(contentWidth).Render(m.request.Text)
		return
	}

	rendered, err := r.Render(m.request.Text)
	if err != nil {
		m.renderedBody = modalTextStyle.Width(contentWidth).Render(m.request.Text)
		return
	}

	// glamour adds trailing newlines — trim them
	m.renderedBody = strings.TrimRight(rendered, "\n")
}

func (m InteractionModal) contentWidth() int {
	modalWidth := m.width * 3 / 5
	if modalWidth < 40 {
		modalWidth = 40
	}
	if modalWidth > m.width-4 {
		modalWidth = m.width - 4
	}
	return modalWidth - 4 // padding
}

// Update handles key events when the modal is active.
func (m InteractionModal) Update(msg tea.Msg) (InteractionModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "escape":
			return m, m.dismiss("cancel", false)
		case "tab", "right":
			m.focused = (m.focused + 1) % len(m.buttons)
		case "shift+tab", "left":
			m.focused = (m.focused - 1 + len(m.buttons)) % len(m.buttons)
		case "enter":
			return m, m.selectFocused()
		default:
			if m.request.Type == protocol.InteractionFreeform {
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}

	case InteractionTickMsg:
		m.countdown--
		if m.countdown <= 0 {
			return m, m.dismiss(m.request.Default, true)
		}
	}

	return m, nil
}

func (m InteractionModal) selectFocused() tea.Cmd {
	label := strings.ToLower(m.buttons[m.focused])
	if m.request.Type == protocol.InteractionFreeform && label == "submit" {
		return m.dismiss(m.input.Value(), false)
	}
	return m.dismiss(label, false)
}

func (m InteractionModal) dismiss(value string, timedOut bool) tea.Cmd {
	return func() tea.Msg {
		return InteractionDismissMsg{
			Response: protocol.InteractionResponse{
				ID:       m.request.ID,
				Value:    value,
				TimedOut: timedOut,
			},
		}
	}
}

// View renders the modal overlay.
func (m InteractionModal) View() string {
	modalWidth := m.width * 3 / 5
	if modalWidth < 40 {
		modalWidth = 40
	}
	if modalWidth > m.width-4 {
		modalWidth = m.width - 4
	}
	contentWidth := modalWidth - 4 // padding

	// Title
	title := modalTitleStyle.Render("Interaction Required")

	// Body — pre-rendered markdown
	body := m.renderedBody

	// Input field (freeform only)
	var inputView string
	if m.request.Type == protocol.InteractionFreeform {
		m.input.Width = contentWidth
		inputView = "\n" + m.input.View() + "\n"
	}

	// Buttons
	var btnViews []string
	for i, label := range m.buttons {
		style := modalButtonStyle
		if i == m.focused {
			style = modalButtonActiveStyle
		}
		btnViews = append(btnViews, style.Render(" "+label+" "))
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, btnViews...)

	// Timer
	timer := modalTimerStyle.Render(fmt.Sprintf("Auto-responding in %ds", m.countdown))

	// Compose modal content
	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		body,
		inputView,
		"",
		buttons,
		"",
		timer,
	)

	// Modal box
	box := modalBoxStyle.Width(modalWidth).Render(content)

	// Center on screen
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// SetSize updates the modal dimensions and re-renders the body.
func (m *InteractionModal) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.renderBody()
}
