// Package tui — Settings menu Bubbletea model.
//
// The settings menu is accessible from the main menu (hotkey "s") or via
// `amnesiai --settings`.  It provides:
//   - Re-run onboarding wizard
//   - View config file path
//   - Toggle verbose help (config.VerboseHelp)
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
	SettingsActionNone              SettingsAction = iota
	SettingsActionRerunOnboard                     // re-run the onboarding wizard
	SettingsActionViewConfig                       // display config file path
	SettingsActionToggleVerbose                    // flip config.VerboseHelp
	SettingsActionPickProviders                    // change cfg.Providers (default backup set)
	SettingsActionToggleBackupFiles                // flip config.BackupShowFiles
	SettingsActionViewBindings                     // display state.json remote_bindings
	SettingsActionToggleAutoPrune                  // flip config.Retention.AutoPrune
	SettingsActionEditRetention                    // open the retention editor (keep_last + max_age_days)
	SettingsActionPruneNow                         // run prune against current policy
	SettingsActionChangeBackupDir                  // change the backup directory path
	SettingsActionBack                             // return to main menu
)

// SettingsResult is returned by the settings menu when it exits.
type SettingsResult struct {
	Action SettingsAction
	// NewVerboseHelp carries the new desired value for VerboseHelp.
	NewVerboseHelp bool
	// NewBackupShowFiles carries the new desired value for BackupShowFiles.
	NewBackupShowFiles bool
	// NewAutoPrune carries the new desired value for Retention.AutoPrune.
	NewAutoPrune bool
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
	verbose     bool   // when true, render each entry's description line
	infoMessage string // transient line shown after a toggle
}

// settingsEntry pairs the label shown in the UI with its action code and an
// optional description shown under the label when verbose mode is on.
type settingsEntry struct {
	label       string
	description string
	action      SettingsAction
}

// buildEntries constructs the settings menu entries, incorporating live toggle state.
func buildEntries(cfg config.Config) []settingsEntry {
	verboseLabel := "Verbose help: OFF"
	if cfg.VerboseHelp {
		verboseLabel = "Verbose help: ON"
	}

	providerLabel := "Default providers: (none — every backup will prompt)"
	if len(cfg.Providers) > 0 {
		providerLabel = "Default providers: " + strings.Join(cfg.Providers, ", ")
	}

	backupFilesLabel := "Backup output: counts only"
	if cfg.BackupShowFiles {
		backupFilesLabel = "Backup output: full file list"
	}

	autoPruneLabel := "Auto-prune: OFF"
	if cfg.Retention.AutoPrune {
		autoPruneLabel = "Auto-prune: ON"
	}

	retentionLabel := "Retention: disabled"
	if cfg.Retention.KeepLast > 0 || cfg.Retention.MaxAgeDays > 0 {
		retentionLabel = fmt.Sprintf("Retention: keep last %d, max age %d days",
			cfg.Retention.KeepLast, cfg.Retention.MaxAgeDays)
	}

	backupDirLabel := "Backup location: " + cfg.BackupDir

	return []settingsEntry{
		{"Re-run onboarding wizard", "Walk through the initial setup again to change storage mode or remote.", SettingsActionRerunOnboard},
		{"View config file path", "Print the location of ~/.amnesiai/config.toml.", SettingsActionViewConfig},
		{providerLabel, "Choose which providers are pre-selected when you run a backup.", SettingsActionPickProviders},
		{backupFilesLabel, "Toggle between showing a full per-file path list or just counts after a backup.", SettingsActionToggleBackupFiles},
		{verboseLabel, "Toggle description text under each menu item.", SettingsActionToggleVerbose},
		{autoPruneLabel, "Toggle whether old backups are pruned automatically after each new backup.", SettingsActionToggleAutoPrune},
		{retentionLabel, "Set how many backups to keep (keep_last) and/or a maximum age in days.", SettingsActionEditRetention},
		{"Prune now", "Apply the current retention policy immediately and delete old backups.", SettingsActionPruneNow},
		{"View remote bindings (state.json)", "Show the git-remote accounts bound to this installation.", SettingsActionViewBindings},
		{backupDirLabel, "Change the directory where backups (and the git repo) are stored.", SettingsActionChangeBackupDir},
		{"Back to main menu", "Return to the main menu.", SettingsActionBack},
	}
}

// NewSettingsModel creates a settings model seeded with current config and state.
// When verbose is true each entry's description line is shown beneath its label.
func NewSettingsModel(cfg config.Config, st *config.State, verbose bool) SettingsModel {
	if st == nil {
		st, _ = config.LoadState()
		if st == nil {
			empty := &config.State{}
			st = empty
		}
	}
	return SettingsModel{cfg: cfg, state: st, verbose: verbose}
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
				m.verbose = m.cfg.VerboseHelp
				m.result.NewVerboseHelp = m.cfg.VerboseHelp
				if m.cfg.VerboseHelp {
					m.infoMessage = "Verbose help enabled."
				} else {
					m.infoMessage = "Verbose help disabled."
				}
				// Don't quit — let the menu re-render with updated label.
				return m, nil

			case SettingsActionToggleBackupFiles:
				m.cfg.BackupShowFiles = !m.cfg.BackupShowFiles
				m.result.NewBackupShowFiles = m.cfg.BackupShowFiles
				if m.cfg.BackupShowFiles {
					m.infoMessage = "Backup output: full file list."
				} else {
					m.infoMessage = "Backup output: counts only."
				}
				return m, nil

			case SettingsActionToggleAutoPrune:
				m.cfg.Retention.AutoPrune = !m.cfg.Retention.AutoPrune
				m.result.NewAutoPrune = m.cfg.Retention.AutoPrune
				if m.cfg.Retention.AutoPrune {
					m.infoMessage = "Auto-prune: ON"
				} else {
					m.infoMessage = "Auto-prune: OFF"
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

	// Verbose: single description line below the footer, keyed to current cursor.
	if m.verbose {
		desc := entries[m.cursor].description
		if desc != "" {
			sb.WriteString(wMuted.Render("  ↳ "+desc) + "\n")
		}
	}

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
		fmt.Fprintf(&sb, "  %s\n", wAccent.Render(url))
		fmt.Fprintf(&sb, "    host:    %s\n", wNormal.Render(b.Host))
		fmt.Fprintf(&sb, "    account: %s\n", wNormal.Render(b.Account))
		fmt.Fprintf(&sb, "    bound:   %s\n", wMuted.Render(b.LastBoundAt.Format("2006-01-02 15:04:05 UTC")))
		sb.WriteString("\n")
	}
	return sb.String()
}

// RunSettings runs the settings menu and returns the result and the (potentially
// mutated) config.  Callers must persist the config if toggles changed.
// verbose mirrors config.VerboseHelp and controls whether description text is
// shown under each settings entry.
func RunSettings(cfg config.Config, st *config.State) (SettingsResult, config.Config, error) {
	model := NewSettingsModel(cfg, st, cfg.VerboseHelp)
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		return SettingsResult{}, cfg, fmt.Errorf("settings menu: %w", err)
	}
	result, updatedCfg := SettingsResultFrom(final)
	return result, updatedCfg, nil
}
