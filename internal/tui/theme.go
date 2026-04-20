// Package tui — shared colour palette for amnesiai's terminal UI.
//
// All Bubbletea models in this package reference these constants so that
// the palette lives in one place.  cmd/tui.go keeps its own tuiColor*
// constants (it is in a different package and tracks A–D haven't merged yet).
package tui

import "github.com/charmbracelet/lipgloss"

// Hex colour stops — match the altergo "ocean" theme.
const (
	wCyan      = "#00d7ff" // electric cyan — gradient stop 0, prompt accent
	wBlue      = "#005fd7" // slate blue    — gradient stop 1
	wIndigoHex = "#8787ff" // brand indigo  — accent / brand
	wGreen     = "#5faf5f" // success
	wAmber     = "#ffaf00" // warning
	wDim       = "#585858" // muted / de-emphasised
	wWhite     = "#d0d0d0" // normal body text
)

// Pre-built lipgloss styles derived from the palette above.
var (
	wAccent  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wCyan))
	wIndigo  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wIndigoHex))
	wSuccess = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wGreen))
	wWarn    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wAmber))
	wMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color(wDim))
	wNormal  = lipgloss.NewStyle().Foreground(lipgloss.Color(wWhite))
	wPrompt  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wCyan))
)
