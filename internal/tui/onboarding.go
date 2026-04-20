// Package tui provides Bubbletea models for the amnesiai terminal UI.
//
// Onboarding wizard — displayed on first launch (config.FirstRun == true)
// or when the user passes --settings / chooses "Re-run onboarding" from the
// Settings menu.
//
// The wizard is a simple linear step machine.  Each step renders a prompt and
// accepts one or more key presses.  It intentionally does NOT run inside
// tea.WithAltScreen so that the user can scroll back to read earlier prompts.
// When the wizard exits it returns a WizardResult the caller uses to persist
// choices to config and state.
package tui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Palette reused from the main TUI (defined here so this package compiles
// standalone — callers in cmd/ wire the colour constants via lipgloss directly).

const (
	wCyan      = "#00d7ff"
	wBlue      = "#005fd7"
	wIndigoHex = "#8787ff"
	wGreen     = "#5faf5f"
	wAmber     = "#ffaf00"
	wDim       = "#585858"
	wWhite     = "#d0d0d0"
)

var (
	wAccent  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wCyan))
	wIndigo  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wIndigoHex))
	wSuccess = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wGreen))
	wWarn    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wAmber))
	wMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color(wDim))
	wNormal  = lipgloss.NewStyle().Foreground(lipgloss.Color(wWhite))
	wPrompt  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(wCyan))
)

// ─── Result ───────────────────────────────────────────────────────────────────

// WizardResult holds all choices the user made during the wizard.
// Fields are only valid when Completed == true.
type WizardResult struct {
	// Completed is true when the user stepped through every wizard screen.
	// False means ctrl+c was pressed mid-wizard.
	Completed bool

	// StorageMode is one of "local", "git-local", "git-remote".
	StorageMode string

	// RemoteHost is "github" or "gitlab" (only set when StorageMode == "git-remote").
	RemoteHost string

	// RemoteAccount is the authenticated account chosen for git-remote (may be "").
	RemoteAccount string

	// Telemetry is false by default (our policy: off is the privacy-respecting default).
	Telemetry bool

	// RunBackupNow is true when the user chose to run a first backup immediately.
	RunBackupNow bool
}

// ─── Wizard steps ─────────────────────────────────────────────────────────────

type wizardStep int

const (
	stepWelcome       wizardStep = iota // static info screen
	stepStorageMode                     // pick local / git-local / git-remote
	stepRemoteHost                      // pick github / gitlab  (git-remote only)
	stepRemoteAccount                   // pick from discovered accounts
	stepPassphraseNote                  // advisory — no choice needed
	stepTelemetry                       // toggle on/off
	stepFirstBackup                     // offer to run now
	stepDone                            // sentinel
)

// ─── Model ────────────────────────────────────────────────────────────────────

// OnboardingModel is the bubbletea model for the wizard.
type OnboardingModel struct {
	step    wizardStep
	result  WizardResult
	cursor  int    // index into the current choice list
	width   int
	aborted bool

	// Discovered CLI accounts, populated lazily on stepRemoteHost.
	ghAccounts   []string
	glabAccounts []string
}

// NewOnboardingModel creates a fresh wizard model.
func NewOnboardingModel() OnboardingModel {
	return OnboardingModel{
		step: stepWelcome,
	}
}

func (m OnboardingModel) Init() tea.Cmd {
	return nil
}

func (m OnboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.aborted = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			choiceCount := m.currentChoiceCount()
			if m.cursor < choiceCount-1 {
				m.cursor++
			}

		case "enter", " ":
			return m.commit()
		}
	}
	return m, nil
}

// commit records the selection for the current step and advances.
func (m OnboardingModel) commit() (OnboardingModel, tea.Cmd) {
	switch m.step {
	case stepWelcome:
		m.step = stepStorageMode
		m.cursor = 0

	case stepStorageMode:
		modes := []string{"local", "git-local", "git-remote"}
		m.result.StorageMode = modes[m.cursor]
		if m.result.StorageMode == "git-remote" {
			m.step = stepRemoteHost
			// Discover accounts now so the next screen can render them.
			m.ghAccounts = discoverGHAccounts()
			m.glabAccounts = discoverGlabAccounts()
		} else {
			m.step = stepPassphraseNote
		}
		m.cursor = 0

	case stepRemoteHost:
		hosts := []string{"github", "gitlab"}
		m.result.RemoteHost = hosts[m.cursor]
		// Populate account list for the chosen host.
		var accounts []string
		if m.result.RemoteHost == "github" {
			accounts = m.ghAccounts
		} else {
			accounts = m.glabAccounts
		}
		if len(accounts) > 0 {
			m.step = stepRemoteAccount
		} else {
			// No accounts found — skip account selection, user will configure manually.
			m.result.RemoteAccount = ""
			m.step = stepPassphraseNote
		}
		m.cursor = 0

	case stepRemoteAccount:
		var accounts []string
		if m.result.RemoteHost == "github" {
			accounts = m.ghAccounts
		} else {
			accounts = m.glabAccounts
		}
		if m.cursor < len(accounts) {
			m.result.RemoteAccount = accounts[m.cursor]
		}
		m.step = stepPassphraseNote
		m.cursor = 0

	case stepPassphraseNote:
		m.step = stepTelemetry
		m.cursor = 0 // cursor 0 = OFF (privacy-respecting default)

	case stepTelemetry:
		// cursor 0 = OFF, cursor 1 = ON
		m.result.Telemetry = m.cursor == 1
		m.step = stepFirstBackup
		m.cursor = 0 // cursor 0 = "Yes, backup now"

	case stepFirstBackup:
		m.result.RunBackupNow = m.cursor == 0
		m.result.Completed = true
		m.step = stepDone
		return m, tea.Quit
	}

	return m, nil
}

// currentChoiceCount returns the number of selectable options on the current step.
func (m OnboardingModel) currentChoiceCount() int {
	switch m.step {
	case stepWelcome:
		return 1 // just "Continue"
	case stepStorageMode:
		return 3
	case stepRemoteHost:
		return 2
	case stepRemoteAccount:
		if m.result.RemoteHost == "github" {
			return len(m.ghAccounts)
		}
		return len(m.glabAccounts)
	case stepPassphraseNote:
		return 1
	case stepTelemetry:
		return 2
	case stepFirstBackup:
		return 2
	}
	return 1
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m OnboardingModel) View() string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(wAccent.Render("  amnesiai  ") + wMuted.Render("— setup wizard") + "\n\n")

	switch m.step {
	case stepWelcome:
		m.renderWelcome(&sb)
	case stepStorageMode:
		m.renderStorageMode(&sb)
	case stepRemoteHost:
		m.renderRemoteHost(&sb)
	case stepRemoteAccount:
		m.renderRemoteAccount(&sb)
	case stepPassphraseNote:
		m.renderPassphraseNote(&sb)
	case stepTelemetry:
		m.renderTelemetry(&sb)
	case stepFirstBackup:
		m.renderFirstBackup(&sb)
	case stepDone:
		sb.WriteString(wSuccess.Render("  Setup complete.") + "\n")
	}

	sb.WriteString("\n" + wMuted.Render("  ↑↓ navigate · Enter select · ctrl+c abort") + "\n")
	return sb.String()
}

func (m OnboardingModel) renderWelcome(sb *strings.Builder) {
	sb.WriteString(wNormal.Render("  amnesiai backs up your AI assistant configs (Claude, Copilot, Gemini, Codex)") + "\n")
	sb.WriteString(wNormal.Render("  using age encryption and optional git push to keep them safe.") + "\n")
	sb.WriteString(wNormal.Render("  This wizard takes about 60 seconds.") + "\n\n")
	m.renderChoice(sb, 0, "Continue")
}

func (m OnboardingModel) renderStorageMode(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  Where should backups be stored?") + "\n\n")

	choices := []struct{ label, desc string }{
		{"local", "keep backups only on this machine"},
		{"git-local", "commit to a local git repo (no push)"},
		{"git-remote", "commit and push to GitHub or GitLab"},
	}
	for i, c := range choices {
		m.renderChoiceWithDesc(sb, i, c.label, c.desc)
	}
}

func (m OnboardingModel) renderRemoteHost(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  Which git host?") + "\n\n")
	m.renderChoice(sb, 0, "GitHub")
	m.renderChoice(sb, 1, "GitLab")
}

func (m OnboardingModel) renderRemoteAccount(sb *strings.Builder) {
	var accounts []string
	if m.result.RemoteHost == "github" {
		accounts = m.ghAccounts
	} else {
		accounts = m.glabAccounts
	}

	sb.WriteString(wPrompt.Render("  Choose the authenticated account to use:") + "\n\n")
	for i, acc := range accounts {
		m.renderChoice(sb, i, acc)
	}
}

func (m OnboardingModel) renderPassphraseNote(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  Encryption passphrase") + "\n\n")
	sb.WriteString(wNormal.Render("  amnesiai encrypts backups with age when a passphrase is present.") + "\n")
	sb.WriteString(wNormal.Render("  Set the AMNESIAI_PASSPHRASE environment variable to avoid being") + "\n")
	sb.WriteString(wNormal.Render("  prompted on every run.  Leave it unset to enter interactively.") + "\n\n")
	sb.WriteString(wMuted.Render("  Tip: export AMNESIAI_PASSPHRASE=\"…\" in your shell profile.") + "\n\n")
	m.renderChoice(sb, 0, "Got it — continue")
}

func (m OnboardingModel) renderTelemetry(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  Anonymous usage telemetry") + "\n\n")
	sb.WriteString(wNormal.Render("  Telemetry is OFF by default.  Enabling it sends anonymous usage") + "\n")
	sb.WriteString(wNormal.Render("  counts (no file paths, no config values) to help prioritise features.") + "\n\n")

	m.renderChoice(sb, 0, "Keep OFF (recommended)")
	m.renderChoice(sb, 1, "Enable telemetry")
}

func (m OnboardingModel) renderFirstBackup(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  Run first backup now?") + "\n\n")
	sb.WriteString(wNormal.Render("  A backup captures your current AI config files and encrypts them.") + "\n\n")
	m.renderChoice(sb, 0, "Yes — backup now")
	m.renderChoice(sb, 1, "No — I'll do it later")
}

// renderChoice renders a single selectable item.
func (m OnboardingModel) renderChoice(sb *strings.Builder, idx int, label string) {
	if idx == m.cursor {
		sb.WriteString("  " + wAccent.Render("▸ "+label) + "\n")
	} else {
		sb.WriteString("  " + wNormal.Render("  "+label) + "\n")
	}
}

// renderChoiceWithDesc renders a selectable item with a muted description.
func (m OnboardingModel) renderChoiceWithDesc(sb *strings.Builder, idx int, label, desc string) {
	if idx == m.cursor {
		sb.WriteString("  " + wAccent.Render("▸ "+label) + "  " + wMuted.Render(desc) + "\n")
	} else {
		sb.WriteString("  " + wNormal.Render("  "+label) + "  " + wMuted.Render(desc) + "\n")
	}
}

// ─── Result accessor ──────────────────────────────────────────────────────────

// Result extracts the WizardResult from the final model returned by p.Run().
// Returns (result, true) when the wizard completed normally; (zero, false)
// when the user aborted or the model type is unexpected.
func WizardResultFrom(m tea.Model) (WizardResult, bool) {
	wm, ok := m.(OnboardingModel)
	if !ok || wm.aborted || !wm.result.Completed {
		return WizardResult{}, false
	}
	return wm.result, true
}

// ─── CLI account discovery ────────────────────────────────────────────────────
//
// These functions shell out to `gh auth list` and `glab auth status` to find
// already-authenticated accounts.  Failures are silently ignored — the wizard
// simply skips the account-selection screen when no accounts are found.

func discoverGHAccounts() []string {
	out, err := exec.Command("gh", "auth", "list").Output()
	if err != nil {
		return nil
	}
	return parseGHAuthList(string(out))
}

func discoverGlabAccounts() []string {
	out, err := exec.Command("glab", "auth", "status").Output()
	if err != nil {
		return nil
	}
	return parseGlabAuthStatus(string(out))
}

// parseGHAuthList extracts usernames from `gh auth list` output.
// The format is:
//
//	github.com   alice (oauth)  ✓ Logged in to github.com as alice …
//	github.com   bob   (oauth)  …
//
// We look for lines containing "Logged in to" and extract the username.
func parseGHAuthList(output string) []string {
	var accounts []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "logged in") || strings.Contains(lower, "github.com") {
			// The username follows "as " in the line.
			if idx := strings.Index(strings.ToLower(line), " as "); idx >= 0 {
				rest := line[idx+4:]
				// Take the first word-token (ends at space, comma, or parens).
				rest = strings.Fields(rest)[0]
				rest = strings.Trim(rest, " ()")
				if rest != "" && !seen[rest] {
					seen[rest] = true
					accounts = append(accounts, rest)
				}
			} else {
				// Fallback: second space-delimited column is usually the username.
				fields := strings.Fields(line)
				if len(fields) >= 2 && !strings.HasPrefix(fields[1], "(") {
					name := strings.Trim(fields[1], "()")
					if name != "" && !seen[name] {
						seen[name] = true
						accounts = append(accounts, name)
					}
				}
			}
		}
	}
	return accounts
}

// parseGlabAuthStatus extracts usernames from `glab auth status` output.
// Typical format:
//
//	gitlab.com
//	  ✓ Logged in to gitlab.com as alice (…)
func parseGlabAuthStatus(output string) []string {
	var accounts []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		if idx := strings.Index(strings.ToLower(line), " as "); idx >= 0 {
			rest := line[idx+4:]
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				name := strings.Trim(fields[0], "()")
				if name != "" && !seen[name] {
					seen[name] = true
					accounts = append(accounts, name)
				}
			}
		}
	}
	return accounts
}

// RunOnboarding runs the onboarding wizard as a full-screen Bubbletea program
// and returns the result.  Callers are responsible for persisting the result.
func RunOnboarding() (WizardResult, error) {
	model := NewOnboardingModel()
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		return WizardResult{}, fmt.Errorf("onboarding wizard: %w", err)
	}
	result, ok := WizardResultFrom(final)
	if !ok {
		return WizardResult{}, nil // user aborted
	}
	return result, nil
}
