package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/core"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up AI assistant configurations",
	Long: `Discovers configuration files from selected providers, scans for secrets,
archives them into a compressed tarball, optionally encrypts with age,
and saves to the configured storage backend.`,
	RunE: runBackup,
}

func init() {
	backupCmd.Flags().StringSlice("providers", nil, "providers to back up (default: all configured)")
	backupCmd.Flags().StringArray("label", nil, "labels as key=value pairs (repeatable)")
	backupCmd.Flags().String("message", "", "commit message override (default: auto-generated)")

	rootCmd.AddCommand(backupCmd)
}

// incrementBackupCount loads config, bumps BackupCount, clears FirstRun once the
// first backup has completed, and saves.  Errors are logged but do not fail the
// backup — the data is already on disk.
func incrementBackupCount() {
	updatedCfg := cfg
	updatedCfg.BackupCount++
	if updatedCfg.FirstRun && updatedCfg.BackupCount >= 1 {
		updatedCfg.FirstRun = false
	}
	if err := config.Save(updatedCfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update backup_count: %v\n", err)
	}
	cfg = updatedCfg
}

func runBackup(cmd *cobra.Command, args []string) error {
	store, err := getStorage()
	if err != nil {
		return err
	}

	providers, _ := cmd.Flags().GetStringSlice("providers")
	if len(providers) == 0 {
		providers = cfg.Providers
	}

	labelSlice, _ := cmd.Flags().GetStringArray("label")
	labels := make(map[string]string)
	for _, l := range labelSlice {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			labels[parts[0]] = parts[1]
		}
	}

	message, _ := cmd.Flags().GetString("message")

	opts := core.BackupOptions{
		Providers:  providers,
		Passphrase: getPassphrase(cmd),
		Labels:     labels,
		Message:    message,
	}

	result, err := core.Backup(store, opts)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Backup complete: %s\n", result.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "Providers: %s\n", strings.Join(result.Providers, ", "))
	fmt.Fprintf(cmd.OutOrStdout(), "Timestamp: %s\n", result.Timestamp.Format("2006-01-02 15:04:05 UTC"))

	for provName, findings := range result.Findings {
		if len(findings) > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: %d secret(s) redacted in %s\n", len(findings), provName)
		}
	}

	incrementBackupCount()

	return nil
}
