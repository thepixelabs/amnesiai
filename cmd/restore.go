package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/core"
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore AI assistant configurations from a backup",
	Long: `Loads a backup from storage, decrypts if needed, and restores
configuration files through the appropriate providers.`,
	RunE: runRestore,
}

func init() {
	restoreCmd.Flags().String("id", "", "backup ID to restore (default: latest)")
	restoreCmd.Flags().StringSlice("providers", nil, "subset of providers to restore")
	restoreCmd.Flags().Bool("dry-run", false, "show what would be restored without writing")

	rootCmd.AddCommand(restoreCmd)
}

func runRestore(cmd *cobra.Command, args []string) error {
	store, err := getStorage()
	if err != nil {
		return err
	}

	backupID, _ := cmd.Flags().GetString("id")
	providers, _ := cmd.Flags().GetStringSlice("providers")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Resolve passphrase once; interactive prompt only fires here.
	passphrase := getPassphrase(cmd)

	restoreOpts := core.RestoreOptions{
		BackupID:   backupID,
		Providers:  providers,
		Passphrase: passphrase,
		DryRun:     dryRun,
	}

	// Always do a metadata peek first (dry-run = true) so we can warn about
	// unencrypted archives before writing any files.
	peek, err := core.Restore(store, core.RestoreOptions{
		BackupID:   backupID,
		Providers:  providers,
		Passphrase: passphrase,
		DryRun:     true,
	})
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	// C3: warn prominently before restoring an unencrypted archive.
	if peek.UnencryptedBackup {
		fmt.Fprintln(cmd.ErrOrStderr(), "")
		fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: This backup is UNENCRYPTED. File contents may contain")
		fmt.Fprintln(cmd.ErrOrStderr(), "  <REDACTED:...> placeholders that will overwrite your real values.")
		fmt.Fprintln(cmd.ErrOrStderr(), "")

		if !dryRun {
			// Interactive confirmation — only when stdin is a terminal.
			if xterm.IsTerminal(os.Stdin.Fd()) {
				fmt.Fprint(cmd.ErrOrStderr(), "Continue? [y/N]: ")
				scanner := bufio.NewScanner(os.Stdin)
				scanner.Scan()
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(cmd.OutOrStdout(), "Restore cancelled.")
					return nil
				}
			}
			// Non-interactive: proceed; the warning was already printed above.
		}
	}

	// Dry-run: report using the peek result (already complete).
	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would restore %d file(s) from backup %s\n", peek.Files, peek.BackupID)
		fmt.Fprintf(cmd.OutOrStdout(), "Providers: %s\n", strings.Join(peek.Providers, ", "))
		if peek.UnencryptedBackup && len(peek.PlaceholderFiles) > 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: the following files contain <REDACTED:...> placeholders:")
			for _, f := range peek.PlaceholderFiles {
				fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", f)
			}
		}
		return nil
	}

	// Live restore.
	result, err := core.Restore(store, restoreOpts)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Restored %d file(s) from backup %s\n", result.Files, result.BackupID)
	fmt.Fprintf(cmd.OutOrStdout(), "Providers: %s\n", strings.Join(result.Providers, ", "))

	// Post-restore: warn about files that still contain <REDACTED: placeholders.
	if len(result.PlaceholderFiles) > 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: the following restored files contain <REDACTED:...> placeholders.")
		fmt.Fprintln(cmd.ErrOrStderr(), "  These files were backed up WITHOUT encryption; secrets were redacted.")
		fmt.Fprintln(cmd.ErrOrStderr(), "  You will need to restore the original values manually.")
		for _, f := range result.PlaceholderFiles {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", f)
		}
	}

	return nil
}
