package tui

import "fmt"

// HelpBar renders the bottom help line.
type HelpBar struct {
	width int
}

// SetWidth updates the bar width.
func (h *HelpBar) SetWidth(w int) {
	h.width = w
}

// View renders the help bar.
func (h HelpBar) View() string {
	keys := []struct{ key, desc string }{
		{"q", "quit"},
		{"j/k", "scroll"},
		{"G", "bottom"},
		{"g", "top"},
		{"/", "search"},
	}

	var content string
	for i, k := range keys {
		if i > 0 {
			content += "  "
		}
		content += fmt.Sprintf("%s:%s", helpKeyStyle.Render(k.key), k.desc)
	}

	return helpBarStyle.Width(h.width).Render(content)
}
