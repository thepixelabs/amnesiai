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
// The wizard offers "local", "git-local", and "git-remote".  When git-remote
// is chosen, additional sub-steps probe gh/glab for authenticated accounts,
// let the user pick a host+account, and then either enter an existing repo URL
// or provide a name for a new private repo to create.
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

	// StorageMode is one of "local", "git-local", "git-remote".
	StorageMode string

	// RemoteHost and RemoteAccount are the detected CLI host ("github"/"gitlab")
	// and the account username chosen during the git-remote sub-steps.
	RemoteHost    string
	RemoteAccount string

	// RemoteURL is the existing remote repository URL the user pasted in, or
	// the URL returned after repo creation.  Only set when StorageMode=="git-remote".
	RemoteURL string

	// CreateRepo is true when the user chose to create a new private repo rather
	// than supplying an existing URL.
	CreateRepo bool

	// RepoName is the repository name to pass to gh/glab for creation.
	// Only meaningful when CreateRepo==true.
	RepoName string
}

// ─── Wizard steps ─────────────────────────────────────────────────────────────

type wizardStep int

const (
	stepWelcome        wizardStep = iota // static info screen
	stepStorageMode                      // pick local / git-local / git-remote
	stepGRHost                           // pick github / gitlab (git-remote only)
	stepGRAccount                        // pick authenticated account (git-remote only)
	stepGRRepoChoice                     // use existing URL or create new repo
	stepGRRepoInput                      // text input: URL or repo name
	stepPassphraseNote                   // advisory — no choice needed
	stepDone                             // sentinel
)

// ─── Async account-discovery messages ────────────────────────────────────────

// accountsDiscoveredMsg carries the result of a background gh/glab probe.
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
	cursor  int // index into the current choice list
	width   int
	aborted bool

	// Discovered CLI accounts.
	ghAccounts   []string
	glabAccounts []string
	discovering  int // count of in-flight discovery commands (0 = done)

	// Text input state for the repo URL / repo name steps.
	textInput     string
	textInputHint string // placeholder hint rendered when empty
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
		if m.discovering > 0 {
			m.discovering--
		}
		if msg.err == nil {
			switch msg.host {
			case "github":
				m.ghAccounts = msg.accounts
			case "gitlab":
				m.glabAccounts = msg.accounts
			}
		}
		// If we are still on the host-pick step and discovery just finished,
		// check whether we can auto-advance.
		if m.step == stepGRHost && m.discovering == 0 {
			return m.maybeAutoAdvanceFromHost()
		}
		return m, nil

	case tea.KeyMsg:
		// Text-input steps consume most keys differently.
		if m.step == stepGRRepoInput {
			return m.updateTextInput(msg)
		}

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

// updateTextInput handles key messages while on a text-input sub-step.
func (m OnboardingModel) updateTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.aborted = true
		return m, tea.Quit

	case "enter":
		// Accept whatever is in the buffer (may be empty — validated in commit).
		return m.commit()

	case "backspace", "ctrl+h":
		if len(m.textInput) > 0 {
			// Safe rune-aware trim.
			runes := []rune(m.textInput)
			m.textInput = string(runes[:len(runes)-1])
		}

	default:
		// Only accept printable, single-character keys.
		s := msg.String()
		if len(s) == 1 {
			m.textInput += s
		} else if s == "space" {
			m.textInput += " "
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
			// Fire both discovery commands concurrently and wait for both.
			m.discovering = 2
			m.step = stepGRHost
			m.cursor = 0
			return m, tea.Batch(
				discoverAccountsCmd("github"),
				discoverAccountsCmd("gitlab"),
			)
		}
		m.step = stepPassphraseNote
		m.cursor = 0

	case stepGRHost:
		// If discovery is still running we are in the "detecting" state — do nothing.
		if m.discovering > 0 {
			return m, nil
		}
		hosts := m.availableHosts()
		if len(hosts) == 0 {
			// No hosts — degrade to git-local with a note already shown in the view.
			m.result.StorageMode = "git-local"
			m.step = stepPassphraseNote
			m.cursor = 0
			return m, nil
		}
		m.result.RemoteHost = hosts[m.cursor]
		m.step = stepGRAccount
		m.cursor = 0
		// Auto-advance if only one account.
		if accounts := m.accountsForHost(m.result.RemoteHost); len(accounts) == 1 {
			m.result.RemoteAccount = accounts[0]
			m.step = stepGRRepoChoice
			m.cursor = 0
		}

	case stepGRAccount:
		accounts := m.accountsForHost(m.result.RemoteHost)
		if len(accounts) == 0 {
			// Shouldn't happen — but guard.
			m.result.RemoteAccount = ""
		} else {
			m.result.RemoteAccount = accounts[m.cursor]
		}
		m.step = stepGRRepoChoice
		m.cursor = 0

	case stepGRRepoChoice:
		// cursor 0 = existing URL, cursor 1 = create new repo
		m.result.CreateRepo = m.cursor == 1
		m.step = stepGRRepoInput
		m.textInput = ""
		if m.result.CreateRepo {
			m.textInputHint = "amnesiai-backups"
		} else {
			m.textInputHint = "https://github.com/you/amnesiai-backups"
		}

	case stepGRRepoInput:
		value := strings.TrimSpace(m.textInput)
		if value == "" {
			// Use the hint as the default.
			value = m.textInputHint
		}
		if m.result.CreateRepo {
			m.result.RepoName = value
		} else {
			m.result.RemoteURL = value
		}
		m.step = stepPassphraseNote
		m.cursor = 0

	case stepPassphraseNote:
		m.result.Completed = true
		m.step = stepDone
		return m, tea.Quit
	}

	return m, nil
}

// maybeAutoAdvanceFromHost is called after all discovery completes while we
// are still on stepGRHost.  If neither host has accounts we degrade; if
// exactly one host has accounts we skip the host-pick screen.
func (m OnboardingModel) maybeAutoAdvanceFromHost() (OnboardingModel, tea.Cmd) {
	hosts := m.availableHosts()
	switch len(hosts) {
	case 0:
		// No authenticated hosts found — degrade to git-local.
		m.result.StorageMode = "git-local"
		m.step = stepPassphraseNote
		m.cursor = 0
	case 1:
		// Only one host — skip the pick screen.
		m.result.RemoteHost = hosts[0]
		m.step = stepGRAccount
		m.cursor = 0
		// If that host also has exactly one account, skip account pick too.
		if accounts := m.accountsForHost(m.result.RemoteHost); len(accounts) == 1 {
			m.result.RemoteAccount = accounts[0]
			m.step = stepGRRepoChoice
		}
	}
	// If len(hosts) > 1 we stay on stepGRHost and let the user pick.
	return m, nil
}

// availableHosts returns the list of hosts that have at least one account.
func (m OnboardingModel) availableHosts() []string {
	var hosts []string
	if len(m.ghAccounts) > 0 {
		hosts = append(hosts, "github")
	}
	if len(m.glabAccounts) > 0 {
		hosts = append(hosts, "gitlab")
	}
	return hosts
}

// accountsForHost returns the discovered account list for the given host.
func (m OnboardingModel) accountsForHost(host string) []string {
	switch host {
	case "github":
		return m.ghAccounts
	case "gitlab":
		return m.glabAccounts
	}
	return nil
}

// currentChoiceCount returns the number of selectable options on the current step.
func (m OnboardingModel) currentChoiceCount() int {
	switch m.step {
	case stepWelcome:
		return 1 // just "Continue"
	case stepStorageMode:
		return 3 // local, git-local, git-remote
	case stepGRHost:
		if m.discovering > 0 {
			return 0 // busy
		}
		return len(m.availableHosts())
	case stepGRAccount:
		return len(m.accountsForHost(m.result.RemoteHost))
	case stepGRRepoChoice:
		return 2 // existing URL or create new
	case stepGRRepoInput:
		return 0 // text input — no arrow choices
	case stepPassphraseNote:
		return 1
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
	case stepGRHost:
		m.renderGRHost(&sb)
	case stepGRAccount:
		m.renderGRAccount(&sb)
	case stepGRRepoChoice:
		m.renderGRRepoChoice(&sb)
	case stepGRRepoInput:
		m.renderGRRepoInput(&sb)
	case stepPassphraseNote:
		m.renderPassphraseNote(&sb)
	case stepDone:
		sb.WriteString(wSuccess.Render("  Setup complete.") + "\n")
	}

	if m.step == stepGRRepoInput {
		sb.WriteString("\n" + wMuted.Render("  Enter confirm · ctrl+c abort") + "\n")
	} else {
		sb.WriteString("\n" + wMuted.Render("  ↑↓ navigate · Enter select · ctrl+c abort") + "\n")
	}
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
		{"git-remote", "commit and push to a remote repository"},
	}
	for i, c := range choices {
		m.renderChoiceWithDesc(sb, i, c.label, c.desc)
	}
}

func (m OnboardingModel) renderGRHost(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  Which remote host?") + "\n\n")

	if m.discovering > 0 {
		sb.WriteString(wMuted.Render("  Detecting authenticated accounts…") + "\n")
		return
	}

	hosts := m.availableHosts()
	if len(hosts) == 0 {
		sb.WriteString(wWarn.Render("  No authenticated gh or glab accounts found.") + "\n")
		sb.WriteString(wNormal.Render("  Install gh (GitHub CLI) or glab (GitLab CLI) and run `gh auth login`") + "\n")
		sb.WriteString(wNormal.Render("  or `glab auth login`, then re-run amnesiai to set up git-remote.") + "\n\n")
		sb.WriteString(wMuted.Render("  Falling back to git-local — press Enter to continue.") + "\n")
		m.renderChoice(sb, 0, "Continue (use git-local)")
		return
	}

	hostLabels := map[string]string{
		"github": "GitHub (via gh CLI)",
		"gitlab": "GitLab (via glab CLI)",
	}
	for i, h := range hosts {
		label := hostLabels[h]
		if label == "" {
			label = h
		}
		m.renderChoice(sb, i, label)
	}
}

func (m OnboardingModel) renderGRAccount(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  Which account?") + "\n\n")
	accounts := m.accountsForHost(m.result.RemoteHost)
	for i, a := range accounts {
		m.renderChoice(sb, i, a)
	}
}

func (m OnboardingModel) renderGRRepoChoice(sb *strings.Builder) {
	sb.WriteString(wPrompt.Render("  How should the remote repo be set up?") + "\n\n")

	hostLabel := m.result.RemoteHost
	if m.result.RemoteAccount != "" {
		hostLabel = m.result.RemoteAccount + " on " + m.result.RemoteHost
	}
	sb.WriteString(wMuted.Render("  Host: "+hostLabel) + "\n\n")

	m.renderChoiceWithDesc(sb, 0, "Use an existing repo", "paste the HTTPS or SSH URL on the next screen")
	m.renderChoiceWithDesc(sb, 1, "Create a new private repo", "amnesiai will run `gh repo create` / `glab repo create`")
}

func (m OnboardingModel) renderGRRepoInput(sb *strings.Builder) {
	if m.result.CreateRepo {
		sb.WriteString(wPrompt.Render("  New repository name") + "\n\n")
		sb.WriteString(wMuted.Render("  The repo will be created as a private repository under your account.") + "\n\n")
	} else {
		sb.WriteString(wPrompt.Render("  Repository URL") + "\n\n")
		sb.WriteString(wMuted.Render("  Paste the HTTPS or SSH URL of an existing private repository.") + "\n\n")
	}

	// Render the input field.
	display := m.textInput
	hint := ""
	if display == "" {
		hint = wMuted.Render(m.textInputHint)
	}
	cursor := wAccent.Render("█")

	if display == "" {
		sb.WriteString("  " + wNormal.Render("  ") + hint + cursor + "\n")
	} else {
		sb.WriteString("  " + wAccent.Render("▸ ") + wNormal.Render(display) + cursor + "\n")
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
