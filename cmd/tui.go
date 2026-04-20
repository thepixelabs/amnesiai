// Package cmd — TUI entry point for amnesiai.
//
// Visual style mirrors the altergo Python library:
//   - Pre-rendered thin figlet ASCII-art banner with a left-to-right ocean gradient.
//   - Time-of-day greeting keyed to hour windows.
//   - Arrow-key navigation (↑↓) through menu items with single-letter hotkeys.
//   - Middle-dot (·, U+00B7) as selection marker in provider picker and passphrase mask.
//   - Unicode box-drawing borders (╭ ╮ ╰ ╯ ─ │).
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/core"
	providerregistry "github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
	internaltui "github.com/thepixelabs/amnesiai/internal/tui"
	"github.com/thepixelabs/amnesiai/internal/version"
)

// Re-export style aliases so sub-flow helpers below can use short names.
var (
	tuiAccentStyle  = internaltui.AccentStyle
	tuiIndigoStyle  = internaltui.IndigoStyle
	tuiSuccessStyle = internaltui.SuccessStyle
	tuiWarnStyle    = internaltui.WarnStyle
	tuiErrorStyle   = internaltui.ErrorStyle
	tuiMutedStyle   = internaltui.MutedStyle
)

// ─── Cobra wiring ─────────────────────────────────────────────────────────────

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive terminal UI",
	RunE:  runTUI,
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

// runRoot is called when amnesiai is invoked with no subcommand. It launches
// the TUI when stdout is a TTY, otherwise prints help.
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
// On the first iteration (when config.FirstRun is true) the onboarding wizard
// is run before the main menu appears.
func tuiLoop(cmd *cobra.Command) error {
	if cfg.FirstRun {
		if err := runOnboardingFlow(); err != nil {
			return err
		}
	}

	for {
		model := internaltui.NewMenuModel(version.Version)
		p := tea.NewProgram(model, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			return fmt.Errorf("tui: %w", err)
		}

		m, ok := finalModel.(internaltui.MenuModel)
		if !ok {
			return nil
		}

		ui := &legacyUI{cmd: cmd}

		switch m.Selected {
		case internaltui.ActionBackup:
			_ = ui.backupFlow()
		case internaltui.ActionRestore:
			_ = ui.restoreFlow()
		case internaltui.ActionDiff:
			_ = ui.diffFlow()
		case internaltui.ActionList:
			_ = ui.listFlow()
		case internaltui.ActionCompletion:
			ui.completionHelp()
		case internaltui.ActionSettings:
			if err := runSettingsFlow(); err != nil {
				tuiPrintError(err)
			}
		case internaltui.ActionQuit, internaltui.ActionNone:
			return nil
		}
	}
}

// ─── Onboarding and settings flows ───────────────────────────────────────────

// runOnboardingFlow runs the onboarding wizard and persists the result.
//
// Skip rules:
//   - If the user aborts (ctrl+c), FirstRun stays true so the wizard triggers again.
//   - If the wizard completed but the user didn't run a backup, FirstRun stays
//     true. Only a completed backup (via incrementBackupCount) flips FirstRun.
func runOnboardingFlow() error {
	result, err := internaltui.RunOnboarding()
	if err != nil {
		return fmt.Errorf("onboarding: %w", err)
	}

	if !result.Completed {
		return nil
	}

	cfg.StorageMode = result.StorageMode
	cfg.Telemetry = result.Telemetry

	st, _ := config.LoadState()
	if st != nil {
		st.OnboardingLastSeenVersion = version.Version
		_ = st.Save()
	}

	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save config after onboarding: %v\n", err)
	}

	return nil
}

// runSettingsFlow opens the settings Bubbletea menu and applies any changes.
func runSettingsFlow() error {
	for {
		st, _ := config.LoadState()
		result, updatedCfg, err := internaltui.RunSettings(cfg, st)
		if err != nil {
			return err
		}

		if cfg.VerboseHelp != updatedCfg.VerboseHelp || cfg.Telemetry != updatedCfg.Telemetry {
			cfg.VerboseHelp = updatedCfg.VerboseHelp
			cfg.Telemetry = updatedCfg.Telemetry
			if saveErr := config.Save(cfg); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save settings: %v\n", saveErr)
			}
		}

		tuiClearScreen()

		switch result.Action {
		case internaltui.SettingsActionRerunOnboard:
			if err := runOnboardingFlow(); err != nil {
				tuiPrintError(err)
			}

		case internaltui.SettingsActionViewConfig:
			tuiPrintSubHeader("Config path")
			fmt.Print(internaltui.FormatConfigPath())
			r := bufio.NewReader(os.Stdin)
			tuiPause(r)

		case internaltui.SettingsActionViewBindings:
			tuiPrintSubHeader("Remote bindings")
			fmt.Print(internaltui.FormatRemoteBindings(st))
			r := bufio.NewReader(os.Stdin)
			tuiPause(r)

		case internaltui.SettingsActionBack, internaltui.SettingsActionNone:
			return nil
		}
	}
}

// hasTTY returns true when stdout is a character device (TTY).
func hasTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

var isTTYFn = hasTTY

// ─── Legacy readline sub-flows ────────────────────────────────────────────────

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

	providers, err := tuiPickProviders(cfg.Providers)
	if err != nil {
		return tuiHandleInputErr(err)
	}

	labels, err := internaltui.PromptLabels()
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
		noEncrypt, _ := ui.cmd.InheritedFlags().GetBool("no-encrypt")
		if !noEncrypt {
			pp, ppErr := internaltui.ReadPassphrase("Passphrase", true)
			if ppErr != nil {
				if errors.Is(ppErr, internaltui.ErrPassphraseMismatch) {
					tuiPrintError(ppErr)
					return nil
				}
				return tuiHandleInputErr(ppErr)
			}
			passphrase = pp
		}
	}

	if passphrase == "" {
		fmt.Println(tuiMutedStyle.Render("Encryption: disabled"))
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
	if spinErr := tuiWithSpinner("Creating backup", func() error {
		var opErr error
		result, opErr = core.Backup(store, opts)
		return opErr
	}); spinErr != nil {
		tuiPrintError(fmt.Errorf("backup failed: %w", spinErr))
		return nil
	}

	incrementBackupCount()

	tuiClearScreen()
	tuiPrintSubHeader("Backup complete")
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("ID:"), result.ID)
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Providers:"), strings.Join(result.Providers, ", "))
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Timestamp:"), result.Timestamp.Format("2006-01-02 15:04:05 UTC"))

	// Enriched findings display.
	encrypted := passphrase != ""
	findingEntries := internaltui.BuildFindingEntries(result.Findings, encrypted)
	internaltui.PrintFindings(findingEntries, isTTYFn())

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

	providers, err := tuiPickProviders(entry.Providers)
	if err != nil {
		return tuiHandleInputErr(err)
	}

	dryRun := tuiConfirm("Dry run", false, ui.reader())
	if !dryRun && !tuiConfirm("Restore files to disk", false, ui.reader()) {
		return nil
	}

	passphrase := getPassphrase(ui.cmd)
	if passphrase == "" {
		noEncrypt, _ := ui.cmd.InheritedFlags().GetBool("no-encrypt")
		if !noEncrypt {
			pp, ppErr := internaltui.ReadPassphrase("Decryption passphrase", false)
			if ppErr != nil {
				return tuiHandleInputErr(ppErr)
			}
			passphrase = pp
		}
	}

	var result *core.RestoreResult
	if spinErr := tuiWithSpinner("Restoring backup", func() error {
		var opErr error
		result, opErr = core.Restore(store, core.RestoreOptions{
			BackupID:   entry.ID,
			Providers:  providers,
			Passphrase: passphrase,
			DryRun:     dryRun,
		})
		return opErr
	}); spinErr != nil {
		tuiPrintError(fmt.Errorf("restore failed: %w", spinErr))
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

	providers, err := tuiPickProviders(entry.Providers)
	if err != nil {
		return tuiHandleInputErr(err)
	}

	passphrase := getPassphrase(ui.cmd)
	if passphrase == "" {
		noEncrypt, _ := ui.cmd.InheritedFlags().GetBool("no-encrypt")
		if !noEncrypt {
			pp, ppErr := internaltui.ReadPassphrase("Decryption passphrase", false)
			if ppErr != nil {
				return tuiHandleInputErr(ppErr)
			}
			passphrase = pp
		}
	}

	var result *core.DiffResult
	if spinErr := tuiWithSpinner("Calculating diff", func() error {
		var opErr error
		result, opErr = core.Diff(store, core.DiffOptions{
			BackupID:   entry.ID,
			Providers:  providers,
			Passphrase: passphrase,
		})
		return opErr
	}); spinErr != nil {
		tuiPrintError(fmt.Errorf("diff failed: %w", spinErr))
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

// ─── Provider picker (Bubbletea sub-program) ──────────────────────────────────

func tuiPickProviders(defaults []string) ([]string, error) {
	available := providerregistry.Names()
	if len(available) == 0 {
		return nil, fmt.Errorf("no providers are registered")
	}

	filteredDefaults := filterProviders(defaults, available)
	if len(filteredDefaults) == 0 {
		filteredDefaults = available
	}

	pickerModel := internaltui.NewProviderPickerModel(available, filteredDefaults)
	p := tea.NewProgram(pickerModel)
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("provider picker: %w", err)
	}

	picker, ok := finalModel.(internaltui.ProviderPickerModel)
	if !ok {
		return nil, fmt.Errorf("provider picker: unexpected model type")
	}

	sel := picker.SelectedProviders()
	if len(sel) == 0 {
		return nil, fmt.Errorf("no providers selected")
	}
	return sel, nil
}

// ─── Sub-flow helpers ─────────────────────────────────────────────────────────

func tuiClearScreen() {
	fmt.Print("\033[H\033[2J")
}

func tuiPrintSubHeader(subtitle string) {
	title := "amnesiai"
	if version.Version != "dev" {
		title += " " + version.Version
	}
	box := internaltui.BorderStyle.Render(
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
	for _, part := range splitCSVLocal(input) {
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
	return internaltui.ParseLabels(input)
}

func splitCSVLocal(input string) []string {
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
