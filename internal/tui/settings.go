// Package tui — Settings menu Bubbletea model.
//
// The settings menu is accessible from the main menu (hotkey "s") or via
// `amnesiai --settings`.  It provides:
//   - Re-run onboarding wizard
//   - View config file path
//   - Toggle verbose help (config.VerboseHelp)
//   - Toggle telemetry  (config.Telemetry, default OFF)
//   - View state.json remote bindings
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thepixelabs/amnesiai/internal/config"
)

// ─── Actions ──────────────────────────────────────────────────────────────────

// SettingsAction is the result code returned when the user selects a settings item.
type SettingsAction int

const (
	SettingsActionNone         SettingsAction = iota
	SettingsActionRerunOnboard                // re-run the onboarding wizard
	SettingsActionViewConfig                  // display config file path
	SettingsActionToggleVerbose               // flip config.VerboseHelp
	SettingsActionToggleTelemetry             // flip config.Telemetry
	SettingsActionViewBindings                // display state.json remote_bindings
	SettingsActionBack                        // return to main menu
)

// SettingsResult is returned by the settings menu when it exits.
type SettingsResult struct {
	Action SettingsAction
	// ToggleVerboseHelp / ToggleTelemetry carry the new desired value.
	NewVerboseHelp bool
	NewTelemetry   bool
}

// ─── Messages ─────────────────────────────────────────────────────────────────

// settingsSelectedMsg is emitted when the user commits a choice.
type settingsSelectedMsg struct{ action SettingsAction }

// ─── Model ────────────────────────────────────────────────────────────────────

// SettingsModel is the Bubbletea model for the settings menu.
type SettingsModel struct {
	cursor      int
	cfg         config.Config
	state       *config.State
	result      SettingsResult
	width       int
	infoMessage string // transient line shown after a toggle
}

// settingsEntry pairs the label shown in the UI with its action code.
type settingsEntry struct {
	label  string
	action SettingsAction
}

// buildEntries constructs the settings menu entries, incorporating live toggle state.
func buildEntries(cfg config.Config) []settingsEntry {
	verboseLabel := "Verbose help: OFF"
	if cfg.VerboseHelp {
		verboseLabel = "Verbose help: ON"
	}
	telemetryLabel := "Telemetry: OFF (recommended)"
	if cfg.Telemetry {
		telemetryLabel = "Telemetry: ON"
	}
	return []settingsEntry{
		{"Re-run onboarding wizard", SettingsActionRerunOnboard},
		{"View config file path", SettingsActionViewConfig},
		{verboseLabel, SettingsActionToggleVerbose},
		{telemetryLabel, SettingsActionToggleTelemetry},
		{"View remote bindings (state.json)", SettingsActionViewBindings},
		{"Back to main menu", SettingsActionBack},
	}
}

// NewSettingsModel creates a settings model seeded with current config and state.
func NewSettingsModel(cfg config.Config, st *config.State) SettingsModel {
	if st == nil {
		st, _ = config.LoadState()
		if st == nil {
			empty := &config.State{}
			st = empty
		}
	}
	return SettingsModel{cfg: cfg, state: st}
}

func (m SettingsModel) Init() tea.Cmd { return nil }

func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	entries := buildEntries(m.cfg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case settingsSelectedMsg:
		m.result.Action = msg.action
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.result.Action = SettingsActionBack
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.infoMessage = ""

		case "down", "j":
			if m.cursor < len(entries)-1 {
				m.cursor++
			}
			m.infoMessage = ""

		case "enter", " ":
			action := entries[m.cursor].action
			switch action {
			case SettingsActionToggleVerbose:
				m.cfg.VerboseHelp = !m.cfg.VerboseHelp
				m.result.NewVerboseHelp = m.cfg.VerboseHelp
				if m.cfg.VerboseHelp {
					m.infoMessage = "Verbose help enabled."
				} else {
					m.infoMessage = "Verbose help disabled."
				}
				// Don't quit — let the menu re-render with updated label.
				return m, nil

			case SettingsActionToggleTelemetry:
				m.cfg.Telemetry = !m.cfg.Telemetry
				m.result.NewTelemetry = m.cfg.Telemetry
				if m.cfg.Telemetry {
					m.infoMessage = "Telemetry enabled."
				} else {
					m.infoMessage = "Telemetry disabled."
				}
				return m, nil

			default:
				return m, func() tea.Msg { return settingsSelectedMsg{action: action} }
			}
		}
	}
	return m, nil
}

func (m SettingsModel) View() string {
	var sb strings.Builder
	entries := buildEntries(m.cfg)

	sb.WriteString("\n")
	sb.WriteString(wAccent.Render("  Settings") + "\n\n")

	for i, entry := range entries {
		if i == m.cursor {
			sb.WriteString("  " + wAccent.Render("▸ "+entry.label) + "\n")
		} else {
			sb.WriteString("  " + wNormal.Render("  "+entry.label) + "\n")
		}
	}

	if m.infoMessage != "" {
		sb.WriteString("\n  " + wSuccess.Render(m.infoMessage) + "\n")
	}

	sb.WriteString("\n" + wMuted.Render("  ↑↓ navigate · Enter select · q back") + "\n")
	return sb.String()
}

// ─── Result accessor ──────────────────────────────────────────────────────────

// SettingsResultFrom extracts the SettingsResult from the final Bubbletea model.
func SettingsResultFrom(m tea.Model) (SettingsResult, config.Config) {
	sm, ok := m.(SettingsModel)
	if !ok {
		return SettingsResult{Action: SettingsActionBack}, config.Config{}
	}
	return sm.result, sm.cfg
}

// ─── Content screens ─────────────────────────────────────────────────────────
//
// These are rendered as plain text AFTER Bubbletea exits (in the legacy
// readline pattern used throughout cmd/tui.go).

// FormatConfigPath returns a formatted string for the "View config path" screen.
func FormatConfigPath() string {
	path, err := config.ConfigFilePath()
	if err != nil {
		return wWarn.Render("  Could not determine config path: ") + err.Error() + "\n"
	}
	var sb strings.Builder
	sb.WriteString(wAccent.Render("  Config file path") + "\n\n")
	sb.WriteString("  " + wNormal.Render(path) + "\n")
	return sb.String()
}

// FormatRemoteBindings returns a formatted string for the "View remote bindings" screen.
func FormatRemoteBindings(st *config.State) string {
	var sb strings.Builder
	sb.WriteString(wAccent.Render("  Remote bindings (state.json)") + "\n\n")

	if st == nil || len(st.RemoteBindings) == 0 {
		sb.WriteString("  " + wMuted.Render("No remote bindings configured.") + "\n")
		return sb.String()
	}

	for url, b := range st.RemoteBindings {
		sb.WriteString(fmt.Sprintf("  %s\n", wAccent.Render(url)))
		sb.WriteString(fmt.Sprintf("    host:    %s\n", wNormal.Render(b.Host)))
		sb.WriteString(fmt.Sprintf("    account: %s\n", wNormal.Render(b.Account)))
		sb.WriteString(fmt.Sprintf("    bound:   %s\n", wMuted.Render(b.LastBoundAt.Format("2006-01-02 15:04:05 UTC"))))
		sb.WriteString("\n")
	}
	return sb.String()
}

// RunSettings runs the settings menu and returns the result and the (potentially
// mutated) config.  Callers must persist the config if toggles changed.
func RunSettings(cfg config.Config, st *config.State) (SettingsResult, config.Config, error) {
	model := NewSettingsModel(cfg, st)
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		return SettingsResult{}, cfg, fmt.Errorf("settings menu: %w", err)
	}
	result, updatedCfg := SettingsResultFrom(final)
	return result, updatedCfg, nil
}
