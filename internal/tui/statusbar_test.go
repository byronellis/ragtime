package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

func TestStatusBarView(t *testing.T) {
	info := protocol.DaemonInfo{
		PID:        12345,
		StartedAt:  time.Now().Add(-2 * time.Hour),
		SocketPath: "/tmp/test.sock",
		RuleCount:  5,
	}

	sb := NewStatusBar(info)
	sb.SetWidth(80)

	view := sb.View()

	if !strings.Contains(view, "ragtime") {
		t.Error("should contain 'ragtime'")
	}
	if !strings.Contains(view, "12345") {
		t.Error("should contain PID")
	}
	if !strings.Contains(view, "5") {
		t.Error("should contain rule count")
	}
}

func TestDisconnectedStatusBar(t *testing.T) {
	view := DisconnectedStatusBar(80)
	if !strings.Contains(view, "disconnected") {
		t.Error("should contain 'disconnected'")
	}
	if !strings.Contains(view, "ragtime") {
		t.Error("should contain 'ragtime'")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 15*time.Minute, "2h15m"},
		{0, "0s"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
