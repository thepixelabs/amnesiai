// Package cmd implements the TUI entry point for amnesiai.
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
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
		model := internaltui.NewMenuModel(version.Version, cfg.VerboseHelp)
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
//   - If the wizard completes, FirstRun is flipped to false so the wizard does
//     not re-trigger on next launch.
//
// When the user chose git-remote, this function calls storage.InitGitRemote to
// wire up the remote repository.  On failure it prints a warning and degrades
// to git-local so the user is never left in a broken half-configured state.
// FirstRun is set to false regardless of the InitGitRemote outcome so the
// wizard does not re-run on the next launch.
func runOnboardingFlow() error {
	result, err := internaltui.RunOnboarding()
	if err != nil {
		return fmt.Errorf("onboarding: %w", err)
	}

	if !result.Completed {
		return nil
	}

	cfg.StorageMode = result.StorageMode
	cfg.FirstRun = false

	switch result.StorageMode {
	case "git-local":
		if initErr := storage.InitGitLocal(cfg.BackupDir); initErr != nil {
			fmt.Fprintf(os.Stderr, "warning: git-local setup failed: %v\n", initErr)
			fmt.Fprintf(os.Stderr, "         Falling back to local; run `amnesiai init --mode git-local` later.\n")
			cfg.StorageMode = "local"
		}

	case "git-remote":
		// The wizard may have collected a user-chosen backup directory. Apply it
		// to cfg.BackupDir (with ~/ expansion + absolute-path validation) before
		// initialising the remote so the clone lives where the user asked.
		if result.BackupDir != "" {
			chosen := result.BackupDir
			if strings.HasPrefix(chosen, "~/") {
				if home, herr := os.UserHomeDir(); herr == nil {
					chosen = filepath.Join(home, chosen[2:])
				}
			}
			if !filepath.IsAbs(chosen) {
				fmt.Fprintf(os.Stderr, "warning: chosen backup dir %q is not absolute — using default %s\n", result.BackupDir, cfg.BackupDir)
			} else {
				cfg.BackupDir = chosen
			}
		}
		url, initErr := storage.InitGitRemote(storage.InitGitRemoteOptions{
			Dir:        cfg.BackupDir,
			RepoURL:    result.RemoteURL,
			CreateRepo: result.CreateRepo,
			RepoName:   result.RepoName,
		})
		if initErr != nil {
			fmt.Fprintf(os.Stderr, "warning: git-remote setup failed: %v\n", initErr)
			fmt.Fprintf(os.Stderr, "         Falling back to git-local; run `amnesiai init --mode git-remote` later.\n")
			cfg.StorageMode = "git-local"
			if localErr := storage.InitGitLocal(cfg.BackupDir); localErr != nil {
				fmt.Fprintf(os.Stderr, "warning: git-local fallback also failed: %v\n", localErr)
				cfg.StorageMode = "local"
			}
		} else {
			cfg.GitRemote.URL = url
		}
	}

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

		if cfg.VerboseHelp != updatedCfg.VerboseHelp ||
			cfg.BackupShowFiles != updatedCfg.BackupShowFiles ||
			cfg.Retention.AutoPrune != updatedCfg.Retention.AutoPrune {
			cfg.VerboseHelp = updatedCfg.VerboseHelp
			cfg.BackupShowFiles = updatedCfg.BackupShowFiles
			cfg.Retention.AutoPrune = updatedCfg.Retention.AutoPrune
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

		case internaltui.SettingsActionPickProviders:
			tuiPrintSubHeader("Default providers")
			selected, pickErr := tuiPickProviders(cfg.Providers)
			if pickErr != nil {
				if !errors.Is(pickErr, internaltui.ErrCancelled) {
					tuiPrintError(pickErr)
				}
				continue
			}
			cfg.Providers = selected
			if saveErr := config.Save(cfg); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save default providers: %v\n", saveErr)
			} else {
				fmt.Println(tuiSuccessStyle.Render("  Default providers updated: ") + strings.Join(selected, ", "))
				r := bufio.NewReader(os.Stdin)
				tuiPause(r)
			}

		case internaltui.SettingsActionEditRetention:
			editorResult, edErr := internaltui.RunRetentionEditor(cfg.Retention.KeepLast, cfg.Retention.MaxAgeDays)
			if edErr != nil {
				tuiPrintError(edErr)
				continue
			}
			if !editorResult.Saved {
				continue
			}
			cfg.Retention.KeepLast = editorResult.KeepLast
			cfg.Retention.MaxAgeDays = editorResult.MaxAgeDays
			if saveErr := config.Save(cfg); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save retention settings: %v\n", saveErr)
			} else {
				fmt.Printf("%s keep_last=%d, max_age_days=%d\n",
					tuiSuccessStyle.Render("  Retention saved:"),
					cfg.Retention.KeepLast, cfg.Retention.MaxAgeDays)
				r := bufio.NewReader(os.Stdin)
				tuiPause(r)
			}

		case internaltui.SettingsActionPruneNow:
			if cfg.Retention.KeepLast == 0 && cfg.Retention.MaxAgeDays == 0 {
				fmt.Println(tuiWarnStyle.Render("  Retention is disabled (both keep_last and max_age_days are 0)."))
				fmt.Println(tuiMutedStyle.Render("  Use the Retention setting above to configure limits first."))
				r := bufio.NewReader(os.Stdin)
				tuiPause(r)
				continue
			}
			store, storeErr := getStorage()
			if storeErr != nil {
				tuiPrintError(storeErr)
				continue
			}
			preview, previewErr := core.Prune(store, cfg.Retention, true)
			if previewErr != nil {
				tuiPrintError(fmt.Errorf("prune preview: %w", previewErr))
				continue
			}
			if len(preview.Deleted) == 0 {
				fmt.Println(tuiSuccessStyle.Render("  No backups outside the retention policy. Nothing to delete."))
				r := bufio.NewReader(os.Stdin)
				tuiPause(r)
				continue
			}
			r := bufio.NewReader(os.Stdin)
			fmt.Printf("  Would delete %d backup(s), keep %d.\n", len(preview.Deleted), len(preview.Kept))
			for _, id := range preview.Deleted {
				fmt.Println("    " + tuiMutedStyle.Render(id))
			}
			fmt.Println()
			if !tuiConfirm("Delete these backups", false, r) {
				continue
			}
			pruneResult, pruneErr := core.Prune(store, cfg.Retention, false)
			if pruneErr != nil {
				tuiPrintError(fmt.Errorf("prune: %w", pruneErr))
				continue
			}
			fmt.Printf("%s %d backup(s) deleted.\n", tuiSuccessStyle.Render("  Pruned:"), len(pruneResult.Deleted))
			tuiPause(r)

		case internaltui.SettingsActionChangeBackupDir:
			tuiPrintSubHeader("Backup location")
			fmt.Printf("  Current: %s\n\n", tuiMutedStyle.Render(cfg.BackupDir))
			newPath, promptErr := internaltui.RunDirPicker(cfg.BackupDir)
			if errors.Is(promptErr, internaltui.ErrCancelled) {
				continue
			}
			if promptErr != nil {
				tuiPrintError(promptErr)
				continue
			}
			// Expand a leading ~/ to the user's home directory.
			if strings.HasPrefix(newPath, "~/") {
				home, homeErr := os.UserHomeDir()
				if homeErr != nil {
					tuiPrintError(fmt.Errorf("cannot determine home directory: %w", homeErr))
					continue
				}
				newPath = filepath.Join(home, newPath[2:])
			}
			// Reject clearly relative paths (but allow absolute paths with no tilde).
			if !filepath.IsAbs(newPath) {
				fmt.Println(tuiWarnStyle.Render("  Path must be absolute (start with / or ~/). Cancelled."))
				r2 := bufio.NewReader(os.Stdin)
				tuiPause(r2)
				continue
			}
			// Check whether the directory already exists; offer to create it if not.
			if _, statErr := os.Stat(newPath); os.IsNotExist(statErr) {
				fmt.Printf("\n  %s does not exist.\n", tuiMutedStyle.Render(newPath))
				r2 := bufio.NewReader(os.Stdin)
				if !tuiConfirm("Create it", false, r2) {
					continue
				}
				if mkErr := os.MkdirAll(newPath, 0700); mkErr != nil {
					tuiPrintError(fmt.Errorf("create directory: %w", mkErr))
					continue
				}
			}
			oldDir := cfg.BackupDir
			if oldDir != newPath {
				// Only migrate when the directory actually exists and is non-empty.
				entries, readErr := os.ReadDir(oldDir)
				if readErr != nil && !os.IsNotExist(readErr) {
					tuiPrintError(fmt.Errorf("read backup directory: %w", readErr))
					r3 := bufio.NewReader(os.Stdin)
					tuiPause(r3)
					continue
				}
				if len(entries) > 0 {
					fmt.Printf("\n%s\n", tuiMutedStyle.Render(fmt.Sprintf("  Moving backups from %s to %s...", oldDir, newPath)))
					succeeded := 0
					failed := 0
					crossDevice := false
					for _, entry := range entries {
						src := filepath.Join(oldDir, entry.Name())
						dst := filepath.Join(newPath, entry.Name())
						renameErr := os.Rename(src, dst)
						if renameErr == nil {
							succeeded++
							continue
						}
						// Cross-device: fall back to recursive copy for the whole dir.
						if errors.Is(renameErr, syscall.EXDEV) {
							crossDevice = true
							break
						}
						fmt.Printf("%s\n", tuiWarnStyle.Render(fmt.Sprintf("  Warning: could not move %s: %v", entry.Name(), renameErr)))
						failed++
					}
					if crossDevice {
						fmt.Printf("%s\n", tuiMutedStyle.Render("  Cross-device move detected — copying files..."))
						if copyErr := moveDir(oldDir, newPath); copyErr != nil {
							tuiPrintError(fmt.Errorf("migrate backups: %w", copyErr))
							r3 := bufio.NewReader(os.Stdin)
							tuiPause(r3)
							continue
						}
						fmt.Printf("%s\n", tuiSuccessStyle.Render(fmt.Sprintf("  ✓ Moved all items to %s", newPath)))
					} else {
						// Best-effort removal of the now-empty source dir.
						_ = os.Remove(oldDir)
						if failed == 0 {
							fmt.Printf("%s\n", tuiSuccessStyle.Render(fmt.Sprintf("  ✓ Moved %d item(s) to %s", succeeded, newPath)))
						} else {
							fmt.Printf("%s\n", tuiWarnStyle.Render(fmt.Sprintf("  Moved %d item(s); %d item(s) could not be moved — check above for details", succeeded, failed)))
						}
					}
				}
			}
			cfg.BackupDir = newPath
			if saveErr := config.Save(cfg); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save backup location: %v\n", saveErr)
			} else {
				fmt.Println(tuiSuccessStyle.Render("  Backup location updated: ") + newPath)
			}
			r3 := bufio.NewReader(os.Stdin)
			tuiPause(r3)

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

// moveDir recursively copies all contents of src into dst, then removes src.
// It is used as a cross-device fallback when os.Rename fails with EXDEV.
// Directories are created with 0700; files are created with 0600.
func moveDir(src, dst string) error {
	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		return copyFile(path, target)
	})
	if walkErr != nil {
		return walkErr
	}
	return os.RemoveAll(src)
}

// copyFile copies a single regular file from src to dst, creating dst if it
// does not exist. Permissions on the destination are always 0600.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

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

	labels, err := tuiPromptLabelsWithSuggestions()
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
		Providers:    providers,
		ProjectPaths: cfg.ProjectPaths,
		Overrides:    buildProviderOverrides(),
		Passphrase:   passphrase,
		Labels:       labels,
		Message:      message,
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
	runAutoPruneIfEnabled(ui.cmd, cfg)

	tuiClearScreen()
	tuiPrintSubHeader("Backup complete")
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("ID:"), result.ID)
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Providers:"), strings.Join(result.Providers, ", "))
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Timestamp:"), result.Timestamp.Format("2006-01-02 15:04:05 UTC"))

	tuiPrintBackupContents(result)

	// Enriched findings display.
	encrypted := passphrase != ""
	findingEntries := internaltui.BuildFindingEntries(result.Findings, encrypted)
	internaltui.PrintFindings(findingEntries, isTTYFn())

	tuiPause(ui.reader())
	return nil
}

// tuiPrintBackupContents renders the per-provider file list captured in the
// BackupResult plus a loud warning when zero files were archived (the silent
// "empty backup" footgun: providers had nothing to back up because their dirs
// are empty and no project_paths are configured).
func tuiPrintBackupContents(result *core.BackupResult) {
	fmt.Println()
	fmt.Println(tuiAccentStyle.Render("Files backed up"))

	total := 0
	provNames := make([]string, 0, len(result.Files))
	for name := range result.Files {
		provNames = append(provNames, name)
	}
	sort.Strings(provNames)

	for _, name := range provNames {
		paths := result.Files[name]
		total += len(paths)
		fmt.Printf("  %s %s %s\n",
			tuiAccentStyle.Render("["+name+"]"),
			tuiMutedStyle.Render(fmt.Sprintf("(%d %s)", len(paths), pluralFile(len(paths)))),
			"",
		)
		if !cfg.BackupShowFiles {
			continue
		}
		if len(paths) == 0 {
			fmt.Println("    " + tuiMutedStyle.Render("(none)"))
			continue
		}
		for _, p := range paths {
			fmt.Println("    " + tuiMutedStyle.Render(p))
		}
	}

	if total == 0 {
		fmt.Println()
		fmt.Println(tuiWarnStyle.Render("WARNING: 0 files were backed up."))
		fmt.Println(tuiMutedStyle.Render("  Likely causes:"))
		fmt.Println(tuiMutedStyle.Render("  - Provider directories are empty or absent (~/.claude, ~/.codex, ~/.gemini, ~/.copilot)."))
		fmt.Println(tuiMutedStyle.Render("  - The files in those directories are not in amnesiai's allowlist."))
		fmt.Println(tuiMutedStyle.Render("    Add basenames via [provider_overrides.<name>] extra_files in config.toml."))
		fmt.Println(tuiMutedStyle.Render("  - No per-project paths configured. Edit ~/.amnesiai/config.toml to add them."))
	}
}

func pluralFile(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}

// tuiWarnUnencryptedBackup prints the unencrypted-backup warning and asks the
// user to confirm.  Returns true if the restore should proceed, false if the
// user declined.  Only prompts when mode is live (not dry-run, not out-dir).
func tuiWarnUnencryptedBackup(result *core.RestoreResult, r *bufio.Reader) bool {
	if !result.UnencryptedBackup {
		return true
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "WARNING: This backup is UNENCRYPTED. File contents may contain")
	fmt.Fprintln(os.Stderr, "  <REDACTED:...> placeholders that will overwrite your real values.")
	fmt.Fprintln(os.Stderr, "")
	if result.DryRun || result.OutDir != "" {
		// No confirmation needed for non-live modes.
		return true
	}
	return tuiConfirm("Continue with live restore", false, r)
}

// tuiPrintRestoreResult prints the outcome of a restore operation.
func tuiPrintRestoreResult(result *core.RestoreResult) {
	switch {
	case result.DryRun:
		fmt.Printf("%s Would restore %d file(s) from %s\n", tuiSuccessStyle.Render("Dry run:"), result.Files, result.BackupID)
	case result.OutDir != "":
		fmt.Printf("%s Extracted %d file(s) from %s into %s\n",
			tuiSuccessStyle.Render("Inspect:"), result.Files, result.BackupID, result.OutDir)
		fmt.Println(tuiMutedStyle.Render("(no real destinations were touched)"))
	default:
		fmt.Printf("%s Restored %d file(s) from %s\n", tuiSuccessStyle.Render("Applied:"), result.Files, result.BackupID)
		if len(result.RestoredPaths) > 0 {
			fmt.Println()
			fmt.Println(tuiAccentStyle.Render("Files restored"))
			for _, p := range result.RestoredPaths {
				fmt.Println("  " + tuiMutedStyle.Render(p))
			}
		}
	}
	fmt.Printf("%s %s\n", tuiSuccessStyle.Render("Providers:"), strings.Join(result.Providers, ", "))
	if len(result.UnknownFiles) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d file(s) requested by --files were not found: %v\n",
			len(result.UnknownFiles), result.UnknownFiles)
	}
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

	providers, err := tuiPickProvidersFrom(filterProviders(entry.Providers, providerregistry.Names()), entry.Providers)
	if err != nil {
		return tuiHandleInputErr(err)
	}

	// Ask whether the user wants to cherry-pick individual files.
	fmt.Println()
	fmt.Println(tuiAccentStyle.Render("File selection"))
	fmt.Println("  [a] Restore all files (default)")
	fmt.Println("  [c] Cherry-pick individual files")
	cherryChoice, choiceErr := tuiPrompt("Choose [a/c]", ui.reader())
	if choiceErr != nil {
		return tuiHandleInputErr(choiceErr)
	}

	var selectedFiles []string
	if strings.ToLower(strings.TrimSpace(cherryChoice)) == "c" {
		// Cherry-pick: collect passphrase early so PeekArchive can decrypt.
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

		var manifest []core.ManifestEntry
		if spinErr := tuiWithSpinner("Loading backup manifest", func() error {
			var peekErr error
			manifest, _, peekErr = core.PeekArchive(store, entry.ID, passphrase)
			return peekErr
		}); spinErr != nil {
			tuiPrintError(fmt.Errorf("peek failed: %w", spinErr))
			return nil
		}

		// Filter manifest to only the selected providers.
		var filtered []core.ManifestEntry
		for _, me := range manifest {
			if contains(providers, me.Provider) {
				filtered = append(filtered, me)
			}
		}
		if len(filtered) == 0 {
			tuiPrintError(fmt.Errorf("no files found in backup for selected providers"))
			return nil
		}

		picker := internaltui.NewFilePickerModel(filtered, nil)
		p := tea.NewProgram(picker, tea.WithAltScreen())
		finalModel, runErr := p.Run()
		if runErr != nil {
			return tuiHandleInputErr(runErr)
		}
		fp, ok := finalModel.(internaltui.FilePickerModel)
		if !ok {
			return nil
		}
		if fp.Cancelled() {
			return nil
		}
		selectedFiles = fp.SelectedArchPaths()
		if len(selectedFiles) == 0 {
			tuiPrintError(fmt.Errorf("no files selected"))
			return nil
		}

		// Now proceed with the rest of the flow, but passphrase is already known.
		mode, outDir, modeErr := tuiPickRestoreMode(ui.reader())
		if modeErr != nil {
			return tuiHandleInputErr(modeErr)
		}
		if mode == restoreModeCancel {
			return nil
		}

		// Dry-run peek to detect unencrypted backup before committing to a live restore.
		var peek *core.RestoreResult
		if spinErr := tuiWithSpinner("Checking backup", func() error {
			var peekErr error
			peek, peekErr = core.Restore(store, core.RestoreOptions{
				BackupID:     entry.ID,
				Providers:    providers,
				ProjectPaths: cfg.ProjectPaths,
				Overrides:    buildProviderOverrides(),
				Passphrase:   passphrase,
				DryRun:       true,
				Files:        selectedFiles,
			})
			return peekErr
		}); spinErr != nil {
			tuiPrintError(fmt.Errorf("restore failed: %w", spinErr))
			return nil
		}
		if !tuiWarnUnencryptedBackup(peek, ui.reader()) {
			fmt.Fprintln(os.Stdout, "Restore cancelled.")
			return nil
		}

		opts := core.RestoreOptions{
			BackupID:     entry.ID,
			Providers:    providers,
			ProjectPaths: cfg.ProjectPaths,
			Overrides:    buildProviderOverrides(),
			Passphrase:   passphrase,
			DryRun:       mode == restoreModeDryRun,
			OutDir:       outDir,
			Files:        selectedFiles,
		}

		var result *core.RestoreResult
		if spinErr := tuiWithSpinner("Restoring backup", func() error {
			var opErr error
			result, opErr = core.Restore(store, opts)
			return opErr
		}); spinErr != nil {
			tuiPrintError(fmt.Errorf("restore failed: %w", spinErr))
			return nil
		}

		tuiClearScreen()
		tuiPrintSubHeader("Restore result")
		tuiPrintRestoreResult(result)
		tuiPause(ui.reader())
		return nil
	}

	// Standard (all-files) path — original flow continues below.

	mode, outDir, err := tuiPickRestoreMode(ui.reader())
	if err != nil {
		return tuiHandleInputErr(err)
	}
	if mode == restoreModeCancel {
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

	// Dry-run peek to detect unencrypted backup before committing to a live restore.
	var allPeek *core.RestoreResult
	if spinErr := tuiWithSpinner("Checking backup", func() error {
		var peekErr error
		allPeek, peekErr = core.Restore(store, core.RestoreOptions{
			BackupID:     entry.ID,
			Providers:    providers,
			ProjectPaths: cfg.ProjectPaths,
			Overrides:    buildProviderOverrides(),
			Passphrase:   passphrase,
			DryRun:       true,
		})
		return peekErr
	}); spinErr != nil {
		tuiPrintError(fmt.Errorf("restore failed: %w", spinErr))
		return nil
	}
	if !tuiWarnUnencryptedBackup(allPeek, ui.reader()) {
		fmt.Fprintln(os.Stdout, "Restore cancelled.")
		return nil
	}

	opts := core.RestoreOptions{
		BackupID:     entry.ID,
		Providers:    providers,
		ProjectPaths: cfg.ProjectPaths,
		Overrides:    buildProviderOverrides(),
		Passphrase:   passphrase,
		DryRun:       mode == restoreModeDryRun,
		OutDir:       outDir,
		Files:        selectedFiles, // nil = all files
	}

	var result *core.RestoreResult
	if spinErr := tuiWithSpinner("Restoring backup", func() error {
		var opErr error
		result, opErr = core.Restore(store, opts)
		return opErr
	}); spinErr != nil {
		tuiPrintError(fmt.Errorf("restore failed: %w", spinErr))
		return nil
	}

	tuiClearScreen()
	tuiPrintSubHeader("Restore result")
	tuiPrintRestoreResult(result)
	tuiPause(ui.reader())
	return nil
}

type restoreMode int

const (
	restoreModeCancel restoreMode = iota
	restoreModeLive
	restoreModeDryRun
	restoreModeOutDir
)

// tuiPickRestoreMode prompts the user to choose between live restore, dry run,
// or extract-to-directory inspection.
func tuiPickRestoreMode(r *bufio.Reader) (restoreMode, string, error) {
	fmt.Println()
	fmt.Println(tuiAccentStyle.Render("Restore mode"))
	fmt.Println("  [1] Restore to disk")
	fmt.Println("  [2] Dry run")
	fmt.Println("  [3] Inspect (extract to directory)")
	choice, err := tuiPrompt("Choose [1/2/3]", r)
	if err != nil {
		return restoreModeCancel, "", err
	}
	switch strings.TrimSpace(choice) {
	case "1", "":
		if !tuiConfirm("Restore files to disk", false, r) {
			return restoreModeCancel, "", nil
		}
		return restoreModeLive, "", nil
	case "2":
		return restoreModeDryRun, "", nil
	case "3":
		dir, perr := tuiPrompt("Output directory", r)
		if perr != nil {
			return restoreModeCancel, "", perr
		}
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return restoreModeCancel, "", fmt.Errorf("output directory cannot be empty")
		}
		return restoreModeOutDir, dir, nil
	default:
		return restoreModeCancel, "", fmt.Errorf("unknown choice %q", choice)
	}
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

	providers, err := tuiPickProvidersFrom(filterProviders(entry.Providers, providerregistry.Names()), entry.Providers)
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
			BackupID:     entry.ID,
			Providers:    providers,
			ProjectPaths: cfg.ProjectPaths,
			Overrides:    buildProviderOverrides(),
			Passphrase:   passphrase,
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
	// Loop so a deletion refreshes the list without bouncing back to the main menu.
	for {
		store, entries, err := tuiLoadEntries()
		if err != nil {
			tuiPrintError(err)
			return nil
		}

		result, runErr := internaltui.RunListView(entries)
		if runErr != nil {
			tuiPrintError(runErr)
			return nil
		}

		switch result.Action {
		case internaltui.ListActionDeleteRequest:
			if delErr := store.Delete(result.TargetID); delErr != nil {
				tuiPrintError(fmt.Errorf("delete %s: %w", result.TargetID, delErr))
				return nil
			}
			tuiClearScreen()
			fmt.Println(tuiSuccessStyle.Render("Deleted backup ") + result.TargetID)
			// Loop and re-render the list with the deletion applied.
			continue

		case internaltui.ListActionQuit:
			return nil
		}
	}
}

// ─── Provider picker (Bubbletea sub-program) ──────────────────────────────────

func tuiPickProviders(defaults []string) ([]string, error) {
	return tuiPickProvidersFrom(providerregistry.Names(), defaults)
}

// tuiPickProvidersFrom runs the provider picker with an explicit available set.
// Used by restore/diff flows to restrict choices to what the backup actually contains.
func tuiPickProvidersFrom(available, defaults []string) ([]string, error) {
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

	if picker.Cancelled() {
		return nil, internaltui.ErrCancelled
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
	if errors.Is(err, internaltui.ErrCancelled) {
		return nil
	}
	tuiPrintError(err)
	return nil
}

func tuiChooseBackup(entries []storage.BackupEntry, r *bufio.Reader) (storage.BackupEntry, error) {
	input, err := tuiPrompt("Backup (Enter=latest, number, exact ID, or q to cancel)", r)
	if err != nil {
		return storage.BackupEntry{}, err
	}
	if input == "" {
		return entries[0], nil
	}
	switch strings.ToLower(input) {
	case "q", "quit", "back", "esc":
		return storage.BackupEntry{}, internaltui.ErrCancelled
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
		if meta := formatBackupMeta(entry); meta != "" {
			fmt.Println("      " + tuiMutedStyle.Render(meta))
		}
	}
	fmt.Println()
}

// formatBackupMeta renders the labels and/or message line shown under each
// backup row.  Returns "" when both are empty so the table stays compact.
func formatBackupMeta(entry storage.BackupEntry) string {
	var parts []string
	if len(entry.Labels) > 0 {
		keys := make([]string, 0, len(entry.Labels))
		for k := range entry.Labels {
			if strings.HasPrefix(k, "_") {
				continue // internal labels (e.g. _filecount_*)
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, k+"="+entry.Labels[k])
		}
		if len(pairs) > 0 {
			parts = append(parts, strings.Join(pairs, ", "))
		}
	}
	if entry.Message != "" {
		parts = append(parts, `"`+entry.Message+`"`)
	}
	return strings.Join(parts, " · ")
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

// tuiPromptLabelsWithSuggestions shows a multi-select picker of recent labels
// (gathered from prior backups) and then a free-form prompt for additional new
// labels.  Returns the merged map.  When no recent labels exist it falls
// straight through to the free-form prompt — no empty picker is shown.
//
// q / esc on the picker means "skip suggestions, just type new ones" — not
// "abort the whole label step" — since it's reasonable to want fresh labels
// without sifting through history.
func tuiPromptLabelsWithSuggestions() (map[string]string, error) {
	recent := recentLabelStrings(20)

	picked := map[string]string{}
	if len(recent) > 0 {
		model := internaltui.NewLabelPickerModel(recent)
		p := tea.NewProgram(model)
		final, err := p.Run()
		if err != nil {
			return nil, fmt.Errorf("label picker: %w", err)
		}
		if pm, ok := final.(internaltui.LabelPickerModel); ok && !pm.Cancelled() {
			for _, kv := range pm.SelectedLabels() {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					picked[parts[0]] = parts[1]
				}
			}
		}
	}

	typed, err := internaltui.PromptLabels()
	if err != nil {
		return nil, err
	}

	// Typed labels win on key collision — the user just typed them.
	for k, v := range typed {
		picked[k] = v
	}
	return picked, nil
}

// recentLabelStrings walks the existing backups newest-first and returns up to
// max distinct "key=value" strings.  Newest backup's labels appear first; later
// duplicates are skipped.  Storage errors are swallowed — label suggestions are
// purely a convenience and must never block a backup.
func recentLabelStrings(max int) []string {
	store, err := getStorage()
	if err != nil {
		return nil
	}
	entries, err := store.List()
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	out := make([]string, 0, max)
	for _, e := range entries {
		for k, v := range e.Labels {
			if strings.HasPrefix(k, "_") {
				continue // internal labels (e.g. _filecount_*)
			}
			kv := k + "=" + v
			if seen[kv] {
				continue
			}
			seen[kv] = true
			out = append(out, kv)
			if len(out) >= max {
				return out
			}
		}
	}
	return out
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
