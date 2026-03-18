package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SearchRequestMsg is sent when the user submits a search query.
type SearchRequestMsg struct {
	Query string
}

// SearchResultMsg carries the daemon's search response back to the model.
type SearchResultMsg struct {
	Results []searchResult
	Error   string
}

type searchResult struct {
	Content string
	Source  string
	Score   float32
}

type searchStatus int

const (
	searchIdle    searchStatus = iota
	searchBusy                 // waiting for daemon
	searchDone                 // results ready
	searchErrored              // request failed
)

// SearchPanel is a modal overlay for semantic search.
type SearchPanel struct {
	input   textinput.Model
	status  searchStatus
	results []searchResult
	errMsg  string
	offset  int // scroll offset into results
	width   int
	height  int
}

// NewSearchPanel creates a search panel ready for input.
func NewSearchPanel(width, height int) SearchPanel {
	ti := textinput.New()
	ti.Placeholder = "search sessions..."
	ti.CharLimit = 200
	ti.Focus()

	return SearchPanel{
		input:  ti,
		width:  width,
		height: height,
	}
}

// Update handles key events for the search panel.
// Returns (panel, cmd, done) — done=true means the user pressed Escape.
func (s SearchPanel) Update(msg tea.Msg) (SearchPanel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "escape":
			return s, nil, true
		case "enter":
			if s.status == searchBusy {
				return s, nil, false
			}
			q := strings.TrimSpace(s.input.Value())
			if q == "" {
				return s, nil, false
			}
			s.status = searchBusy
			s.results = nil
			s.offset = 0
			return s, func() tea.Msg { return SearchRequestMsg{Query: q} }, false
		case "j", "down":
			if s.offset < len(s.results)-1 {
				s.offset++
			}
		case "k", "up":
			if s.offset > 0 {
				s.offset--
			}
		default:
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return s, cmd, false
		}

	case SearchResultMsg:
		if msg.Error != "" {
			s.status = searchErrored
			s.errMsg = msg.Error
		} else {
			s.status = searchDone
			s.results = msg.Results
		}
		s.offset = 0
	}

	return s, nil, false
}

// SetSize updates the panel dimensions.
func (s *SearchPanel) SetSize(w, h int) {
	s.width = w
	s.height = h
	s.input.Width = w - 10
}

// View renders the search panel as a full-screen overlay.
func (s SearchPanel) View() string {
	panelWidth := s.width * 4 / 5
	if panelWidth < 50 {
		panelWidth = 50
	}
	if panelWidth > s.width-4 {
		panelWidth = s.width - 4
	}
	innerWidth := panelWidth - 4 // padding

	title := searchTitleStyle.Render("Search Sessions")

	s.input.Width = innerWidth
	inputLine := "> " + s.input.View()

	var body string
	switch s.status {
	case searchIdle:
		body = searchDimStyle.Render("Type a query and press Enter")
	case searchBusy:
		body = searchDimStyle.Render("Searching…")
	case searchErrored:
		body = searchErrorStyle.Render("Error: " + s.errMsg)
	case searchDone:
		body = s.renderResults(innerWidth)
	}

	help := searchHelpStyle.Render("enter:search  j/k:scroll  esc:close")

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		inputLine,
		"",
		body,
		"",
		help,
	)

	box := searchBoxStyle.Width(panelWidth).Render(content)
	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, box)
}

func (s SearchPanel) renderResults(innerWidth int) string {
	if len(s.results) == 0 {
		return searchDimStyle.Render("No results found")
	}

	header := searchDimStyle.Render(fmt.Sprintf("%d result(s)", len(s.results)))

	var lines []string
	lines = append(lines, header, "")

	// How many results fit? Rough estimate: each result takes ~4 lines
	maxVisible := (s.height/2 - 6) / 4
	if maxVisible < 1 {
		maxVisible = 1
	}

	end := s.offset + maxVisible
	if end > len(s.results) {
		end = len(s.results)
	}

	for i, r := range s.results[s.offset:end] {
		idx := s.offset + i
		scoreStr := fmt.Sprintf("%.2f", r.Score)
		meta := searchMetaStyle.Render(fmt.Sprintf("[%d] score:%s  %s", idx+1, scoreStr, shortSource(r.Source)))
		lines = append(lines, meta)

		// Truncate content to a couple of lines
		content := r.Content
		if len(content) > innerWidth*2 {
			content = content[:innerWidth*2] + "…"
		}
		// Replace newlines with spaces for compact display
		content = strings.ReplaceAll(content, "\n", " ")
		if len(content) > innerWidth-2 {
			content = content[:innerWidth-2] + "…"
		}
		lines = append(lines, "  "+searchContentStyle.Render(content), "")
	}

	if len(s.results) > maxVisible {
		lines = append(lines, searchDimStyle.Render(fmt.Sprintf("  ↑↓ scroll (%d/%d)", s.offset+1, len(s.results))))
	}

	return strings.Join(lines, "\n")
}

func shortSource(source string) string {
	if source == "" {
		return ""
	}
	parts := strings.Split(source, "/")
	if len(parts) > 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return source
}

// parseSearchResults converts the daemon's CommandResponse.Data into typed results.
func parseSearchResults(data any) []searchResult {
	arr, ok := data.([]interface{})
	if !ok {
		return nil
	}
	var results []searchResult
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := m["content"].(string)
		source, _ := m["source"].(string)
		score, _ := m["score"].(float64)
		results = append(results, searchResult{
			Content: content,
			Source:  source,
			Score:   float32(score),
		})
	}
	return results
}

// searchCmd returns a tea.Cmd that sends a search command to the daemon.
func searchCmd(client *Client, query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.SendCommand("search", map[string]any{
			"query":      query,
			"collection": "sessions",
			"top_k":      float64(10),
		})
		if err != nil {
			return SearchResultMsg{Error: err.Error()}
		}
		if !resp.Success {
			return SearchResultMsg{Error: resp.Error}
		}
		return SearchResultMsg{Results: parseSearchResults(resp.Data)}
	}
}

