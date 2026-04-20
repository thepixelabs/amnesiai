// Package cmd — TUI entry point for amnesiai.
//
// Visual style mirrors the altergo Python library:
//   - figlet ASCII-art banner with a left-to-right ocean gradient
//   - time-of-day greeting keyed to hour windows (ported from altergo_greetings.py)
//   - arrow-key navigation (↑↓) through menu items
//   - BBS-style shine-sweep footer that animates every ~80 ms
//   - Unicode box-drawing borders (╭ ╮ ╰ ╯ ─ │)
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	figure "github.com/common-nighthawk/go-figure"
	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/core"
	providerregistry "github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
	amnesiaitui "github.com/thepixelabs/amnesiai/internal/tui"
	"github.com/thepixelabs/amnesiai/internal/version"
)

// ─── Ocean palette — matches altergo's "ocean" theme ─────────────────────────
//
// altergo uses curses color 51 (electric cyan, #00d7ff) → 39 (slate blue, #0087d7)
// for the banner gradient and color 105 (indigo, #8787ff) for accent / brand.

const (
	tuiColorCyan   = "#00d7ff" // altergo grad stop 0 — electric cyan
	tuiColorBlue   = "#005fd7" // altergo grad stop 1 — slate blue
	tuiColorIndigo = "#8787ff" // altergo brand/accent — indigo
	tuiColorGreen  = "#5faf5f" // success
	tuiColorAmber  = "#ffaf00" // warning
	tuiColorRed    = "#ff5f5f" // error
	tuiColorDim    = "#585858" // muted
	tuiColorWhite  = "#d0d0d0" // normal text
)

// ─── Lipgloss styles ──────────────────────────────────────────────────────────

var (
	tuiAccentStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tuiColorCyan))
	tuiIndigoStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tuiColorIndigo))
	tuiSuccessStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tuiColorGreen))
	tuiWarnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tuiColorAmber))
	tuiErrorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tuiColorRed))
	tuiMutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(tuiColorDim))
	tuiNormalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(tuiColorWhite))

	// tuiSelectedStyle highlights the focused menu item.
	tuiSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(tuiColorCyan))

	// tuiBorderStyle draws a Unicode box with ocean-blue borders.
	tuiBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.Border{
			Top:         "─",
			Bottom:      "─",
			Left:        "│",
			Right:       "│",
			TopLeft:     "╭",
			TopRight:    "╮",
			BottomLeft:  "╰",
			BottomRight: "╯",
		}).
		BorderForeground(lipgloss.Color(tuiColorBlue)).
		Padding(0, 1)
)

// ─── Gradient helpers ─────────────────────────────────────────────────────────

func hexToRGB(hex string) (r, g, b int) {
	hex = strings.TrimPrefix(hex, "#")
	v, _ := strconv.ParseInt(hex, 16, 32)
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)
}

// lerpColor interpolates between two hex stops at position t ∈ [0,1].
func lerpColor(from, to string, t float64) string {
	t = math.Max(0, math.Min(1, t))
	r1, g1, b1 := hexToRGB(from)
	r2, g2, b2 := hexToRGB(to)
	r := int(float64(r1) + (float64(r2)-float64(r1))*t)
	g := int(float64(g1) + (float64(g2)-float64(g1))*t)
	b := int(float64(b1) + (float64(b2)-float64(b1))*t)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// gradientText renders each rune in text with a left-to-right ocean gradient.
func gradientText(text string) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}
	var sb strings.Builder
	for i, ch := range runes {
		t := float64(i) / math.Max(float64(n-1), 1)
		col := lerpColor(tuiColorCyan, tuiColorBlue, t)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(col)).Render(string(ch)))
	}
	return sb.String()
}

// ─── Banner ───────────────────────────────────────────────────────────────────

// buildBanner renders the figlet ASCII art of "amnesiai" with the ocean gradient
// spread left-to-right across the full banner width (same approach as altergo).
func buildBanner() string {
	f := figure.NewFigure("amnesiai", "smslant", true)
	raw := strings.TrimRight(f.String(), "\n")
	lines := strings.Split(raw, "\n")

	var sb strings.Builder
	for _, line := range lines {
		runes := []rune(line)
		for i, ch := range runes {
			t := float64(i) / math.Max(float64(len(runes)-1), 1)
			col := lerpColor(tuiColorCyan, tuiColorBlue, t)
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(col)).Render(string(ch)))
		}
		sb.WriteRune('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ─── Greeting system — ported from altergo_greetings.py ─────────────────────

type tuiGreeting struct {
	icon string
	text string
}

var greetingWindows = []struct {
	id    string
	start int
	end   int
}{
	{"dead_of_night", 0, 2},
	{"late_night", 3, 5},
	{"early_morning", 6, 8},
	{"morning", 9, 11},
	{"midday", 12, 13},
	{"afternoon", 14, 16},
	{"evening", 17, 19},
	{"night", 20, 23},
}

// greetingBank is keyed by window id with developer-themed quips for a
// backup/restore tool. Same structure and hour windows as altergo_greetings.py.
var greetingBank = map[string][]tuiGreeting{
	"dead_of_night": {
		{"🌑", "Midnight configs still need saving."},
		{"💾", "Past midnight. Your dotfiles are not tired."},
		{"🔐", "Dark hours call for encrypted backups."},
		{"📜", "The git log ends here. The backup does not."},
		{"🤖", "The backup daemon has no opinion on your sleep schedule."},
	},
	"late_night": {
		{"❌", "Three AM backups — bold move."},
		{"😴", "CI is asleep. Your configs are not."},
		{"⚠️", "Nothing good was ever committed at this hour."},
		{"💀", "Late enough that the restore test matters even more."},
		{"🌀", "Deep night, deep backup. Respect."},
	},
	"early_morning": {
		{"⚡", "First snapshot of the day. The record is clean."},
		{"☕", "The coffee hasn't lied to you yet today."},
		{"🐦", "Dawn and a fresh backup — similarly refreshing."},
		{"✅", "Early start. Your configs thank you."},
		{"🌅", "Up before the sun. Making sure configs survive the day."},
	},
	"morning": {
		{"📌", "Morning. Good time for a backup."},
		{"☕", "Two cups of coffee from full productivity. Configs: ready."},
		{"📆", "The workday has opinions. Your dotfiles do not."},
		{"🔄", "Fresh session. Fresh snapshot."},
		{"🏃", "The day is young. Back it up while it's clean."},
	},
	"midday": {
		{"😅", "Noon. The morning got away from you. The config didn't."},
		{"💡", "Pre-lunch backup: use it before context evaporates."},
		{"🧠", "Late enough to have changed something, early enough to save it."},
		{"⚖️", "Halfway through the day. Half your configs are safe."},
		{"🎬", "The morning was a rehearsal. This backup is real."},
	},
	"afternoon": {
		{"🌫️", "Post-lunch fog is optional. The backup is not."},
		{"📝", "The afternoon is long. So is your config list."},
		{"🔬", "The feature exists in theory. The backup will verify."},
		{"🦆", "Somewhere a rubber duck is solving someone's config problem."},
		{"☕", "Three PM. The caffeine wore off. The backup remains."},
	},
	"evening": {
		{"🌙", "After hours. Your configs are still on the clock."},
		{"🚢", "Ship it or stash it — same question for configs."},
		{"✅", "The build passed. Back it up before it stops."},
		{"📖", "Whatever didn't ship today, at least it's saved."},
		{"🎯", "Evening: the line between work and hobby blurs. Backup anyway."},
	},
	"night": {
		{"🤡", "A reasonable time to start a config audit. Said no one."},
		{"🔥", "You and the dotfiles, alone again. This is fine."},
		{"⌨️", "The keyboard has been patient with you all day."},
		{"🔮", "Ten PM. The backup is close. It has always been close."},
		{"🌃", "Night shift, asked for or not. Configs secured."},
	},
}

func windowForHour(hour int) string {
	for _, w := range greetingWindows {
		if hour >= w.start && hour <= w.end {
			return w.id
		}
	}
	return "morning"
}

// pickGreeting returns the greeting for the current time, stable per-minute
// (same seeding strategy as altergo_greetings.py: seed = int(time.time()//60)).
func pickGreeting() tuiGreeting {
	now := time.Now()
	window := windowForHour(now.Hour())
	bank := greetingBank[window]
	if len(bank) == 0 {
		bank = greetingBank["morning"]
	}
	h := fnv.New32a()
	h.Write([]byte(window))
	seed := int(now.Unix()/60) ^ int(h.Sum32())
	idx := ((seed % len(bank)) + len(bank)) % len(bank)
	return bank[idx]
}

// ─── Menu ─────────────────────────────────────────────────────────────────────

// menuAction is the result returned when the user selects a menu item.
type menuAction int

const (
	actionNone       menuAction = iota
	actionBackup                // run backup flow
	actionRestore               // run restore flow
	actionDiff                  // run diff flow
	actionList                  // run list flow
	actionCompletion            // show completion help
	actionSettings              // open settings menu (hotkey: s)
	actionQuit                  // exit
)

var menuLabels = []string{
	"Backup providers",
	"Restore a backup",
	"Diff against a backup",
	"List backups",
	"Completion help",
	"Settings",
	"Quit",
}

// ─── Bubbletea model ──────────────────────────────────────────────────────────

// tickMsg is sent every ~80 ms to drive the footer animation.
type tickMsg struct{}

// selectedMsg carries the chosen action when the user presses Enter.
type selectedMsg struct{ action menuAction }

type tuiModel struct {
	cursor   int
	phase    int        // footer shine-sweep phase counter
	selected menuAction // set when user commits a choice; causes quit
	greeting tuiGreeting
	width    int
	height   int
}

// footerText is the full footer bar content — matches altergo's nav line style.
const footerText = " ↑↓ navigate · Enter select · s settings · q quit · amnesiai by pixelabs"

func newTUIModel() tuiModel {
	return tuiModel{
		cursor:   0,
		greeting: pickGreeting(),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tickEvery80ms()
}

func tickEvery80ms() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.phase++
		return m, tickEvery80ms()

	case selectedMsg:
		m.selected = msg.action
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(menuLabels)-1 {
				m.cursor++
			}
		case "enter", " ":
			action := menuAction(m.cursor + 1) // +1 because actionNone=0
			return m, func() tea.Msg { return selectedMsg{action: action} }
		case "s":
			// Shortcut: jump directly to settings without navigating the cursor.
			return m, func() tea.Msg { return selectedMsg{action: actionSettings} }
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m tuiModel) View() string {
	var sb strings.Builder

	// Banner + version + greeting inside a Unicode box.
	banner := buildBanner()
	ver := ""
	if version.Version != "dev" {
		ver = " " + version.Version
	}
	verLine := tuiMutedStyle.Render("  amnesiai" + ver + " — back up AI assistant configs")
	greetLine := tuiIndigoStyle.Render("  "+m.greeting.icon+"  ") + gradientText(m.greeting.text)

	bannerBox := tuiBorderStyle.Render(banner + "\n" + verLine + "\n" + greetLine)
	sb.WriteString(bannerBox)
	sb.WriteString("\n\n")

	// Menu items — selected item gets a ▸ cursor and accent color.
	for i, label := range menuLabels {
		if i == m.cursor {
			sb.WriteString("  " + tuiSelectedStyle.Render("▸ "+label))
		} else {
			sb.WriteString("  " + tuiNormalStyle.Render("  "+label))
		}
		sb.WriteRune('\n')
	}
	sb.WriteRune('\n')

	// Animated footer.
	sb.WriteString(m.renderFooter())

	return sb.String()
}

// renderFooter renders the footer nav bar with a BBS-style left-to-right shine
// sweep, exactly mirroring altergo's _draw_animated_nav logic:
//
//   - cycle_len = len(text) + 24  (gap creates a pause between sweeps)
//   - shine_pos = phase % cycle_len
//   - dist = |i - shine_pos|
//   - dist ≤ 1  → peak bright (electric cyan)
//   - dist ≤ 3  → mid  (slate blue)
//   - '·' separators twinkle on a staggered per-position cycle
//   - "pixelabs" is rendered in brand indigo
func (m tuiModel) renderFooter() string {
	text := footerText
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}

	cycleLen := n + 24
	shinePos := m.phase % cycleLen

	lower := strings.ToLower(text)
	pixStart := strings.Index(lower, "pixelabs")
	pixEnd := -1
	if pixStart >= 0 {
		pixEnd = pixStart + len("pixelabs")
	}

	var sb strings.Builder
	for i, ch := range runes {
		// Base style.
		var style lipgloss.Style
		if pixStart >= 0 && i >= pixStart && i < pixEnd {
			style = tuiIndigoStyle
		} else {
			style = tuiMutedStyle
		}

		// Shine sweep overlay.
		dist := i - shinePos
		if dist < 0 {
			dist = -dist
		}
		switch {
		case dist <= 1:
			style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tuiColorCyan))
		case dist <= 3:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color(tuiColorBlue))
		case dist <= 5:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color(tuiColorBlue)).Faint(true)
		}

		// Dot twinkle (independent per-position phase, matching altergo).
		if ch == '·' {
			twinkle := (m.phase*2 + i*7) % 48
			switch {
			case twinkle < 2:
				style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tuiColorCyan))
			case twinkle < 5:
				style = lipgloss.NewStyle().Foreground(lipgloss.Color(tuiColorBlue))
			}
		}

		sb.WriteString(style.Render(string(ch)))
	}
	return sb.String() + "\n"
}

// ─── Cobra wiring ─────────────────────────────────────────────────────────────

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive terminal UI",
	RunE:  runTUI,
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

// runRoot is called when amnesiai is invoked with no subcommand. It launches the
// TUI when stdout is a TTY, otherwise prints help (matching altergo's pattern of
// checking only sys.stdout.isatty()).
func runRoot(cmd *cobra.Command, args []string) error {
	if !isTTYFn() {
		return cmd.Help()
	}
	// --settings bypasses the main menu and goes directly to the settings flow.
	if openSettings, _ := cmd.Flags().GetBool("settings"); openSettings {
		return runSettingsFlow()
	}
	return runTUI(cmd, args)
}

func runTUI(cmd *cobra.Command, args []string) error {
	if !isTTYFn() {
		return fmt.Errorf("interactive mode requires a terminal")
	}
	return tuiLoop(cmd)
}

// tuiLoop runs the Bubbletea main-menu and dispatches to sub-flows in a loop.
// When the user picks an action the program quits Bubbletea, runs the sub-flow
// (which reads/writes directly on the real terminal), then re-enters Bubbletea.
// This avoids the complexity of running readline-style prompts inside the event
// loop and matches the clean suspend/resume approach used by altergo's curses
// wrapper pattern.
//
// On the first iteration (when config.FirstRun is true) the onboarding wizard
// is run before the main menu appears.
func tuiLoop(cmd *cobra.Command) error {
	// First-run check: show onboarding wizard before the main menu.
	if cfg.FirstRun {
		if err := runOnboardingFlow(); err != nil {
			return err
		}
		// If FirstRun is still true after the wizard (user aborted or didn't
		// complete a backup), stay in the loop and show the main menu anyway.
	}

	for {
		model := newTUIModel()
		p := tea.NewProgram(model, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			return fmt.Errorf("tui: %w", err)
		}

		m, ok := finalModel.(tuiModel)
		if !ok {
			return nil
		}

		ui := &legacyUI{cmd: cmd}

		switch m.selected {
		case actionBackup:
			_ = ui.backupFlow()
		case actionRestore:
			_ = ui.restoreFlow()
		case actionDiff:
			_ = ui.diffFlow()
		case actionList:
			_ = ui.listFlow()
		case actionCompletion:
			ui.completionHelp()
		case actionSettings:
			if err := runSettingsFlow(); err != nil {
				tuiPrintError(err)
			}
		case actionQuit, actionNone:
			return nil
		}
		// After any sub-flow, loop back to show the TUI again.
	}
}

// ─── Onboarding and settings flows ───────────────────────────────────────────

// runOnboardingFlow runs the onboarding wizard and persists the result.
//
// Skip rules (per spec):
//   - If the user aborts (ctrl+c), FirstRun stays true so the wizard triggers again.
//   - If the wizard completed but RunBackupNow is false, FirstRun stays true.
//   - Only a completed wizard where the user also ran (or triggered) a backup
//     sets FirstRun = false — that is handled by incrementBackupCount() in the
//     backup flow.
func runOnboardingFlow() error {
	result, err := amnesiaitui.RunOnboarding()
	if err != nil {
		return fmt.Errorf("onboarding: %w", err)
	}

	if !result.Completed {
		// User aborted — keep FirstRun true.
		return nil
	}

	// Apply wizard choices to config.
	cfg.StorageMode = result.StorageMode
	cfg.Telemetry = result.Telemetry

	// Load and update state.
	st, _ := config.LoadState()
	if st == nil {
		st, _ = config.LoadState() // second try after potential dir creation
	}
	if st != nil {
		st.OnboardingLastSeenVersion = version.Version
		_ = st.Save()
	}

	// Persist config.  FirstRun stays true until a backup actually completes.
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save config after onboarding: %v\n", err)
	}

	return nil
}

// runSettingsFlow opens the settings Bubbletea menu and applies any changes.
// It loops internally so that the user can make multiple changes before backing
// out to the main menu.
func runSettingsFlow() error {
	for {
		st, _ := config.LoadState()
		result, updatedCfg, err := amnesiaitui.RunSettings(cfg, st)
		if err != nil {
			return err
		}

		// Persist any toggle changes immediately.
		if cfg.VerboseHelp != updatedCfg.VerboseHelp || cfg.Telemetry != updatedCfg.Telemetry {
			cfg.VerboseHelp = updatedCfg.VerboseHelp
			cfg.Telemetry = updatedCfg.Telemetry
			if saveErr := config.Save(cfg); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save settings: %v\n", saveErr)
			}
		}

		tuiClearScreen()

		switch result.Action {
		case amnesiaitui.SettingsActionRerunOnboard:
			if err := runOnboardingFlow(); err != nil {
				tuiPrintError(err)
			}
			// Fall through to loop — show settings menu again.

		case amnesiaitui.SettingsActionViewConfig:
			tuiPrintSubHeader("Config path")
			fmt.Print(amnesiaitui.FormatConfigPath())
			r := bufio.NewReader(os.Stdin)
			tuiPause(r)

		case amnesiaitui.SettingsActionViewBindings:
			tuiPrintSubHeader("Remote bindings")
			fmt.Print(amnesiaitui.FormatRemoteBindings(st))
			r := bufio.NewReader(os.Stdin)
			tuiPause(r)

		case amnesiaitui.SettingsActionBack, amnesiaitui.SettingsActionNone:
			return nil
		}
		// Toggle actions (verbose, telemetry) don't reach here — they stay in the
		// Bubbletea model loop until the user backs out.
	}
}

// hasTTY returns true when stdout is a character device (TTY).
// Matches altergo's sys.stdout.isatty() — stdin is intentionally not checked.
func hasTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// isTTYFn is the TTY detection function used by runRoot and runTUI.
// It is a variable so that tests can override it without spawning a subprocess.
var isTTYFn = hasTTY

// ─── Legacy readline sub-flows ────────────────────────────────────────────────
//
// These handle the interactive prompts for backup/restore/diff/list.  They run
// after Bubbletea has exited the alt-screen and write directly to os.Stdout,
// which is the normal terminal at that point.

type legacyUI struct {
	cmd *cobra.Command
	in  *bufio.Reader
}

func (ui *legacyUI) reader() *bufio.Reader {
	if ui.in == nil {
		ui.in = bufio.NewReader(os.Stdin)
	}
	return ui.in
}

func (ui *legacyUI) backupFlow() error {
	tuiClearScreen()
	tuiPrintSubHeader("Backup")

	providers, err := tuiChooseProviders(cfg.Providers, ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}

	labelsInput, err := tuiPrompt("Labels (key=value,key=value or blank)", ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}
	labels, err := parseLabels(labelsInput)
	if err != nil {
		tuiPrintError(err)
		return nil
	}

	message, err := tuiPrompt("Message (optional)", ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}

	passphrase := getPassphrase(ui.cmd)
	if passphrase == "" {
		fmt.Println(tuiMutedStyle.Render("Encryption: disabled (set AMNESIAI_PASSPHRASE to enable)"))
	} else {
		fmt.Println(tuiSuccessStyle.Render("Encryption: enabled"))
	}
	fmt.Println()

	if !tuiConfirm("Create backup", true, ui.reader()) {
		return nil
	}

	store, err := getStorage()
	if err != nil {
		tuiPrintError(err)
		return nil
	}

	opts := core.BackupOptions{
		Providers:  providers,
		Passphrase: passphrase,
		Labels:     labels,
		Message:    message,
	}

	var result *core.BackupResult
	if err := tuiWithSpinner("Creating backup", func() error {
		var opErr error
		result, opErr = core.Backup(store, opts)
		return opErr
	}); err != nil {
		tuiPrintError(fmt.Errorf("backup failed: %w", err))
		return nil
	}

	incrementBackupCount()

	tuiClearScreen()
	tuiPrintSubHeader("Backup complete")
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("ID:"), result.ID)
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Providers:"), strings.Join(result.Providers, ", "))
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Timestamp:"), result.Timestamp.Format("2006-01-02 15:04:05 UTC"))
	encrypted := opts.Passphrase != ""
	for provName, findings := range result.Findings {
		if len(findings) == 0 {
			continue
		}
		if encrypted {
			fmt.Printf("%s %d secret(s) found in %s (encrypted in archive)\n",
				tuiWarnStyle.Render("Warning:"), len(findings), provName)
		} else {
			fmt.Printf("%s %d secret(s) REDACTED in %s (archive is UNENCRYPTED)\n",
				tuiWarnStyle.Render("Warning:"), len(findings), provName)
		}
	}
	tuiPause(ui.reader())
	return nil
}

func (ui *legacyUI) restoreFlow() error {
	store, entries, err := tuiLoadEntries()
	if err != nil {
		tuiPrintError(err)
		return nil
	}

	tuiClearScreen()
	tuiPrintSubHeader("Restore")
	tuiPrintBackupTable(entries)

	entry, err := tuiChooseBackup(entries, ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}

	providers, err := tuiChooseProviders(entry.Providers, ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}

	dryRun := tuiConfirm("Dry run", false, ui.reader())
	if !dryRun && !tuiConfirm("Restore files to disk", false, ui.reader()) {
		return nil
	}

	var result *core.RestoreResult
	if err := tuiWithSpinner("Restoring backup", func() error {
		var opErr error
		result, opErr = core.Restore(store, core.RestoreOptions{
			BackupID:   entry.ID,
			Providers:  providers,
			Passphrase: getPassphrase(ui.cmd),
			DryRun:     dryRun,
		})
		return opErr
	}); err != nil {
		tuiPrintError(fmt.Errorf("restore failed: %w", err))
		return nil
	}

	tuiClearScreen()
	tuiPrintSubHeader("Restore result")
	if result.DryRun {
		fmt.Printf("%s Would restore %d file(s) from %s\n", tuiSuccessStyle.Render("Dry run:"), result.Files, result.BackupID)
	} else {
		fmt.Printf("%s Restored %d file(s) from %s\n", tuiSuccessStyle.Render("Applied:"), result.Files, result.BackupID)
	}
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Providers:"), strings.Join(result.Providers, ", "))
	tuiPause(ui.reader())
	return nil
}

func (ui *legacyUI) diffFlow() error {
	store, entries, err := tuiLoadEntries()
	if err != nil {
		tuiPrintError(err)
		return nil
	}

	tuiClearScreen()
	tuiPrintSubHeader("Diff")
	tuiPrintBackupTable(entries)

	entry, err := tuiChooseBackup(entries, ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}

	providers, err := tuiChooseProviders(entry.Providers, ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}

	var result *core.DiffResult
	if err := tuiWithSpinner("Calculating diff", func() error {
		var opErr error
		result, opErr = core.Diff(store, core.DiffOptions{
			BackupID:   entry.ID,
			Providers:  providers,
			Passphrase: getPassphrase(ui.cmd),
		})
		return opErr
	}); err != nil {
		tuiPrintError(fmt.Errorf("diff failed: %w", err))
		return nil
	}

	tuiClearScreen()
	tuiPrintSubHeader("Diff result")
	fmt.Printf("%s %s\n\n", tuiSuccessStyle.Render("Backup:"), result.BackupID)

	hasChanges := false
	counts := map[string]int{"added": 0, "modified": 0, "deleted": 0}
	for _, name := range providers {
		diffs := filterChanged(result.Entries[name])
		if len(diffs) == 0 {
			continue
		}
		hasChanges = true
		fmt.Println(tuiAccentStyle.Render("[" + name + "]"))
		for _, d := range diffs {
			fmt.Printf("  %s %s\n", tuiStatusSymbol(d.Status), d.Path)
			counts[d.Status]++
		}
		fmt.Println()
	}
	if !hasChanges {
		fmt.Println(tuiSuccessStyle.Render("No changes detected."))
	}

	var summary []string
	for _, status := range []string{"added", "modified", "deleted"} {
		if counts[status] > 0 {
			summary = append(summary, fmt.Sprintf("%d %s", counts[status], status))
		}
	}
	if len(summary) > 0 {
		fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Summary:"), strings.Join(summary, ", "))
	}
	tuiPause(ui.reader())
	return nil
}

func (ui *legacyUI) listFlow() error {
	_, entries, err := tuiLoadEntries()
	if err != nil {
		tuiPrintError(err)
		return nil
	}

	tuiClearScreen()
	tuiPrintSubHeader("Backups")
	tuiPrintBackupTable(entries)
	tuiPause(ui.reader())
	return nil
}

func (ui *legacyUI) completionHelp() {
	tuiClearScreen()
	tuiPrintSubHeader("Completion")
	fmt.Println("This is a command, not a flag. It prints a shell completion script.")
	fmt.Println()
	fmt.Println(tuiAccentStyle.Render("Examples"))
	fmt.Println("  bash:  amnesiai completion bash > ~/.local/share/bash-completion/completions/amnesiai")
	fmt.Println("  zsh:   amnesiai completion zsh > ~/.zfunc/_amnesiai")
	fmt.Println("  fish:  amnesiai completion fish > ~/.config/fish/completions/amnesiai.fish")
	fmt.Println("  pwsh:  amnesiai completion powershell > amnesiai.ps1")
	fmt.Println()
	fmt.Println(tuiMutedStyle.Render("After writing the script, reload your shell config to enable tab completion."))
	tuiPause(ui.reader())
}

// ─── Sub-flow helpers (write to os.Stdout directly) ──────────────────────────

func tuiClearScreen() {
	fmt.Print("\033[H\033[2J")
}

func tuiPrintSubHeader(subtitle string) {
	title := "amnesiai"
	if version.Version != "dev" {
		title += " " + version.Version
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.Border{
			Top: "─", Bottom: "─", Left: "│", Right: "│",
			TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯",
		}).
		BorderForeground(lipgloss.Color(tuiColorBlue)).
		Padding(0, 1).
		Render(
			tuiAccentStyle.Render(title) + "\n" +
				tuiIndigoStyle.Render(subtitle),
		)
	fmt.Println(box)
	fmt.Println()
}

func tuiPrintError(err error) {
	tuiClearScreen()
	tuiPrintSubHeader("Error")
	fmt.Fprintln(os.Stderr, tuiErrorStyle.Render(err.Error()))
	// Brief pause so the user can read the error before the TUI reappears.
	r := bufio.NewReader(os.Stdin)
	tuiPause(r)
}

func tuiPrompt(label string, r *bufio.Reader) (string, error) {
	fmt.Printf("%s ", tuiAccentStyle.Render(label+":"))
	line, err := r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return strings.TrimSpace(line), io.EOF
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func tuiConfirm(label string, defaultYes bool, r *bufio.Reader) bool {
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}
	input, err := tuiPrompt(label+" "+suffix, r)
	if err != nil {
		return false
	}
	if input == "" {
		return defaultYes
	}
	switch strings.ToLower(input) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func tuiPause(r *bufio.Reader) {
	fmt.Println()
	fmt.Print(tuiMutedStyle.Render("Press Enter to continue..."))
	_, _ = r.ReadString('\n')
}

func tuiHandleInputErr(err error) error {
	if errors.Is(err, io.EOF) {
		fmt.Println()
		return io.EOF
	}
	tuiPrintError(err)
	return nil
}

func tuiChooseProviders(defaults []string, r *bufio.Reader) ([]string, error) {
	available := providerregistry.Names()
	if len(available) == 0 {
		return nil, fmt.Errorf("no providers are registered")
	}

	filteredDefaults := filterProviders(defaults, available)
	if len(filteredDefaults) == 0 {
		filteredDefaults = available
	}

	fmt.Println(tuiAccentStyle.Render("Providers"))
	for i, name := range available {
		marker := " "
		if contains(filteredDefaults, name) {
			marker = "x"
		}
		fmt.Printf("  %d. [%s] %s\n", i+1, marker, name)
	}
	fmt.Println()
	fmt.Printf("%s %s\n", tuiMutedStyle.Render("Default:"), strings.Join(filteredDefaults, ", "))
	input, err := tuiPrompt("Providers (Enter=default, all, 1,3 or names)", r)
	if err != nil {
		return nil, err
	}

	selection, err := resolveProviders(input, filteredDefaults, available)
	if err != nil {
		return nil, err
	}
	fmt.Println()
	return selection, nil
}

func tuiChooseBackup(entries []storage.BackupEntry, r *bufio.Reader) (storage.BackupEntry, error) {
	input, err := tuiPrompt("Backup (Enter=latest, number or exact ID)", r)
	if err != nil {
		return storage.BackupEntry{}, err
	}
	if input == "" {
		return entries[0], nil
	}

	if idx, err := strconv.Atoi(input); err == nil {
		if idx < 1 || idx > len(entries) {
			return storage.BackupEntry{}, fmt.Errorf("backup selection %d is out of range", idx)
		}
		return entries[idx-1], nil
	}

	for _, entry := range entries {
		if entry.ID == input {
			return entry, nil
		}
	}
	return storage.BackupEntry{}, fmt.Errorf("backup %q was not found", input)
}

func tuiLoadEntries() (storage.Storage, []storage.BackupEntry, error) {
	store, err := getStorage()
	if err != nil {
		return nil, nil, err
	}
	entries, err := store.List()
	if err != nil {
		return nil, nil, fmt.Errorf("list backups: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil, storage.ErrNoBackups
	}
	return store, entries, nil
}

func tuiPrintBackupTable(entries []storage.BackupEntry) {
	if len(entries) == 0 {
		fmt.Println(tuiMutedStyle.Render("No backups found."))
		fmt.Println()
		return
	}
	fmt.Println(tuiAccentStyle.Render("Available backups"))
	for i, entry := range entries {
		fmt.Printf("  %2d. %s  %s  [%s]\n",
			i+1,
			entry.ID,
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			strings.Join(entry.Providers, ", "),
		)
	}
	fmt.Println()
}

// tuiWithSpinner runs fn while printing an animated spinner.
// No artificial minimum delay — the spinner runs only as long as fn takes.
func tuiWithSpinner(label string, fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()

	frames := []string{"✦", "✧", "✶", "✸", "✺", "✸", "✶", "✧"}
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	frame := 0

	for {
		select {
		case err := <-done:
			if err != nil {
				fmt.Printf("\r%s %s\n", tuiErrorStyle.Render("✗"), label)
				return err
			}
			fmt.Printf("\r%s %s\n", tuiSuccessStyle.Render("✓"), label)
			return nil
		case <-ticker.C:
			fmt.Printf("\r%s %s", tuiAccentStyle.Render(frames[frame%len(frames)]), label)
			frame++
		}
	}
}

// tuiStatusSymbol returns a styled diff status symbol (used in the TUI diff view).
// The plain-text statusSymbol in diff.go is used by the non-TUI diff command.
func tuiStatusSymbol(status string) string {
	switch status {
	case "added":
		return tuiSuccessStyle.Render("+")
	case "deleted":
		return tuiErrorStyle.Render("-")
	case "modified":
		return tuiWarnStyle.Render("~")
	default:
		return tuiMutedStyle.Render("?")
	}
}

// ─── Pure utility helpers ─────────────────────────────────────────────────────

func resolveProviders(input string, defaults []string, available []string) ([]string, error) {
	if input == "" {
		return append([]string(nil), defaults...), nil
	}
	if strings.EqualFold(input, "all") {
		return append([]string(nil), available...), nil
	}

	selected := make([]string, 0, len(available))
	seen := make(map[string]bool, len(available))
	for _, part := range splitCSV(input) {
		if idx, err := strconv.Atoi(part); err == nil {
			if idx < 1 || idx > len(available) {
				return nil, fmt.Errorf("provider selection %d is out of range", idx)
			}
			name := available[idx-1]
			if !seen[name] {
				selected = append(selected, name)
				seen[name] = true
			}
			continue
		}

		name := strings.ToLower(part)
		if !contains(available, name) {
			return nil, fmt.Errorf("unknown provider %q", part)
		}
		if !seen[name] {
			selected = append(selected, name)
			seen[name] = true
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no providers selected")
	}
	return selected, nil
}

func parseLabels(input string) (map[string]string, error) {
	labels := make(map[string]string)
	if strings.TrimSpace(input) == "" {
		return labels, nil
	}
	for _, part := range splitCSV(input) {
		pieces := strings.SplitN(part, "=", 2)
		if len(pieces) != 2 || pieces[0] == "" {
			return nil, fmt.Errorf("invalid label %q; expected key=value", part)
		}
		labels[pieces[0]] = pieces[1]
	}
	return labels, nil
}

func splitCSV(input string) []string {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func filterProviders(values []string, available []string) []string {
	filtered := make([]string, 0, len(values))
	for _, v := range values {
		if contains(available, v) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
