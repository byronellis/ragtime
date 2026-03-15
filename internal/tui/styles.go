package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorGreen  = lipgloss.Color("#00ff87")
	colorRed    = lipgloss.Color("#ff5f87")
	colorYellow = lipgloss.Color("#ffff87")
	colorBlue   = lipgloss.Color("#87afff")
	colorDim    = lipgloss.Color("#626262")
	colorWhite  = lipgloss.Color("#ffffff")

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#3c3836")).
			Foreground(colorWhite).
			Padding(0, 1)

	statusDotStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	statusLabelStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	// Help bar
	helpBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#3c3836")).
			Foreground(colorDim).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorBlue)

	// Event feed
	eventTimeStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	eventToolStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Width(8)

	eventDetailStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	eventDecisionAllow = lipgloss.NewStyle().
				Foreground(colorGreen)

	eventDecisionDeny = lipgloss.NewStyle().
				Foreground(colorRed)

	eventDecisionAsk = lipgloss.NewStyle().
				Foreground(colorYellow)

	// Title
	titleStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)
)
