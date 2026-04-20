package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/core"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show changes since last backup",
	Long: `Compares the current on-disk configuration state against a stored backup
and displays what has been added, modified, or deleted.`,
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().String("id", "", "backup ID to compare against (default: latest)")
	diffCmd.Flags().StringSlice("providers", nil, "subset of providers to diff")

	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	store, err := getStorage()
	if err != nil {
		return err
	}

	backupID, _ := cmd.Flags().GetString("id")
	providers, _ := cmd.Flags().GetStringSlice("providers")
	if len(providers) == 0 {
		providers = cfg.Providers
	}

	opts := core.DiffOptions{
		BackupID:     backupID,
		Providers:    providers,
		ProjectPaths: cfg.ProjectPaths,
		Passphrase:   getPassphrase(cmd),
	}

	result, err := core.Diff(store, opts)
	if err != nil {
		return fmt.Errorf("diff failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Comparing against backup: %s\n\n", result.BackupID)

	hasChanges := false
	for provName, diffs := range result.Entries {
		changed := filterChanged(diffs)
		if len(changed) == 0 {
			continue
		}
		hasChanges = true
		fmt.Fprintf(cmd.OutOrStdout(), "[%s]\n", provName)
		for _, d := range changed {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s\n", statusSymbol(d.Status), d.Path)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	if !hasChanges {
		fmt.Fprintln(cmd.OutOrStdout(), "No changes detected.")
	}

	// Print summary.
	counts := map[string]int{"added": 0, "modified": 0, "deleted": 0}
	for _, diffs := range result.Entries {
		for _, d := range diffs {
			if d.Status != "unchanged" {
				counts[d.Status]++
			}
		}
	}
	var parts []string
	for _, status := range []string{"added", "modified", "deleted"} {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[status], status))
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Summary: %s\n", strings.Join(parts, ", "))
	}

	return nil
}

func filterChanged(diffs []core.DiffEntry) []core.DiffEntry {
	var out []core.DiffEntry
	for _, d := range diffs {
		if d.Status != "unchanged" {
			out = append(out, d)
		}
	}
	return out
}

func statusSymbol(status string) string {
	switch status {
	case "added":
		return "+"
	case "modified":
		return "~"
	case "deleted":
		return "-"
	default:
		return "?"
	}
}
