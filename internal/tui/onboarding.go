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
//
// # Storage mode
//
// The wizard offers only "local" and "git-local".  The "git-remote" mode
// requires creating a remote repository and authenticating via gh/glab, which
// is handled by `amnesiai init --mode git-remote` (Track F).  A hint line
// directs users there.
package tui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ─── Result ───────────────────────────────────────────────────────────────────

// WizardResult holds all choices the user made during the wizard.
// Fields are only valid when Completed == true.
type WizardResult struct {
	// Completed is true when the user stepped through every wizard screen.
	// False means ctrl+c was pressed mid-wizard.
	Completed bool

	// StorageMode is one of "local", "git-local".
	// "git-remote" is not offered here — use `amnesiai init --mode git-remote`.
	StorageMode string

	// RemoteHost and RemoteAccount are reserved for Track F; always empty from
	// the wizard in v1.1.x.
	RemoteHost    string
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
	stepStorageMode                     // pick local / git-local
	stepPassphraseNote                  // advisory — no choice needed
	stepTelemetry                       // toggle on/off
	stepFirstBackup                     // offer to run now
	stepDone                            // sentinel
)

// ─── Async account-discovery messages ────────────────────────────────────────

// accountsDiscoveredMsg carries the result of a background gh/glab probe.
// It is intentionally kept for potential future use when git-remote is
// re-introduced into the wizard (Track F).
type accountsDiscoveredMsg struct {
	host     string   // "github" or "gitlab"
	accounts []string // may be empty
	err      error    // non-nil if the CLI invocation failed
}

// discoverAccountsCmd returns a tea.Cmd that runs account discovery for the
// given host in a goroutine and sends an accountsDiscoveredMsg when done.
func discoverAccountsCmd(host string) tea.Cmd {
	return func() tea.Msg {
		var accounts []string
		var err error
		switch host {
		case "github":
			accounts, err = discoverGHAccounts()
		case "gitlab":
			accounts, err = discoverGlabAccounts()
		default:
			err = fmt.Errorf("unknown host %q", host)
		}
		return accountsDiscoveredMsg{host: host, accounts: accounts, err: err}
	}
}

// ─── Model ────────────────────────────────────────────────────────────────────

// OnboardingModel is the bubbletea model for the wizard.
type OnboardingModel struct {
	step    wizardStep
	result  WizardResult
	cursor  int    // index into the current choice list
	width   int
	aborted bool

	// Discovered CLI accounts (reserved for Track F / future git-remote support).
	ghAccounts   []string
	glabAccounts []string
	discovering  bool // true while a background discovery is in-flight
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

	// Handle async account-discovery results.
	case accountsDiscoveredMsg:
		m.discovering = false
		if msg.err == nil {
			switch msg.host {
			case "github":
				m.ghAccounts = msg.accounts
			case "gitlab":
				m.glabAccounts = msg.accounts
			}
		}
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
		// Only "local" and "git-local" are offered.  git-remote requires
		// `amnesiai init --mode git-remote` (Track F).
		modes := []string{"local", "git-local"}
		m.result.StorageMode = modes[m.cursor]
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
		return 2 // local, git-local
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
	}
	for i, c := range choices {
		m.renderChoiceWithDesc(sb, i, c.label, c.desc)
	}

	sb.WriteString("\n" + wMuted.Render("  For git-remote mode, run `amnesiai init --mode git-remote` after onboarding") + "\n")
	sb.WriteString(wMuted.Render("  (requires gh or glab CLI).") + "\n")
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

// WizardResultFrom extracts the WizardResult from the final model returned by p.Run().
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
// already-authenticated accounts.  Failures are silently ignored — the caller
// simply receives an empty slice.
//
// Both gh and glab write their output to stderr, so we use CombinedOutput()
// to capture both stdout and stderr in a single pass.

func discoverGHAccounts() ([]string, error) {
	cmd := exec.Command("gh", "auth", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	return parseGHAuthList(string(out)), nil
}

func discoverGlabAccounts() ([]string, error) {
	cmd := exec.Command("glab", "auth", "status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	return parseGlabAuthStatus(string(out)), nil
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
