package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thepixelabs/amensiai/internal/core"
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

	opts := core.RestoreOptions{
		BackupID:   backupID,
		Providers:  providers,
		Passphrase: getPassphrase(cmd),
		DryRun:     dryRun,
	}

	result, err := core.Restore(store, opts)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	if result.DryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would restore %d file(s) from backup %s\n", result.Files, result.BackupID)
		fmt.Fprintf(cmd.OutOrStdout(), "Providers: %s\n", strings.Join(result.Providers, ", "))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Restored %d file(s) from backup %s\n", result.Files, result.BackupID)
		fmt.Fprintf(cmd.OutOrStdout(), "Providers: %s\n", strings.Join(result.Providers, ", "))
	}

	return nil
}
