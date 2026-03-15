package tui

import (
	"strings"
	"testing"
)

func TestHelpBarView(t *testing.T) {
	h := HelpBar{}
	h.SetWidth(80)

	view := h.View()

	for _, key := range []string{"q", "quit", "j/k", "scroll"} {
		if !strings.Contains(view, key) {
			t.Errorf("help bar should contain %q, got: %s", key, view)
		}
	}
}
