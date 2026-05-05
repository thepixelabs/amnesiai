// Package cmd — `amnesiai prune` applies the retention policy on demand.
//
// Flags let the user override the configured policy without touching
// config.toml (handy for one-off cleanups). --dry-run prints what would be
// deleted but touches nothing. In non-TTY contexts the command refuses to
// proceed without --yes, to protect scripts that loop over multiple repos.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/core"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Apply the retention policy and delete old backups",
	Long: `Removes backups that fall outside the retention policy
(retention.keep_last + retention.max_age_days from config).

The two policy windows are combined with OR: a backup is kept if it is in
EITHER window. Setting both to zero (the default) disables retention.`,
	Example: `  amnesiai prune --dry-run
  amnesiai prune --keep-last 20
  amnesiai prune --max-age-days 30 --yes`,
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().Bool("dry-run", false, "show what would be deleted without removing anything")
	pruneCmd.Flags().Int("keep-last", -1, "override retention.keep_last for this run (>=0; -1 means use config)")
	pruneCmd.Flags().Int("max-age-days", -1, "override retention.max_age_days for this run (>=0; -1 means use config)")
	pruneCmd.Flags().Bool("yes", false, "skip the confirmation prompt (required in non-TTY contexts)")

	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	keepLastFlag, _ := cmd.Flags().GetInt("keep-last")
	maxAgeFlag, _ := cmd.Flags().GetInt("max-age-days")

	policy := cfg.Retention
	if keepLastFlag >= 0 {
		policy.KeepLast = keepLastFlag
	}
	if maxAgeFlag >= 0 {
		policy.MaxAgeDays = maxAgeFlag
	}

	if policy.KeepLast == 0 && policy.MaxAgeDays == 0 {
		fmt.Fprintln(cmd.OutOrStdout(),
			"Retention is disabled (both keep_last and max_age_days are 0). Nothing to prune.")
		fmt.Fprintln(cmd.OutOrStdout(),
			"Configure [retention] in ~/.amnesiai/config.toml or pass --keep-last / --max-age-days.")
		return nil
	}

	store, err := getStorage()
	if err != nil {
		return err
	}

	// Always do a dry-run first so we can show the user (and check non-TTY safety)
	// without ever silently deleting.
	preview, err := core.Prune(store, policy, true)
	if err != nil {
		return err
	}

	if len(preview.Deleted) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No backups outside the retention policy. Nothing to delete.")
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Retention policy: keep_last=%d, max_age_days=%d\n",
		policy.KeepLast, policy.MaxAgeDays)
	fmt.Fprintf(out, "Would delete %d backup(s), keep %d:\n",
		len(preview.Deleted), len(preview.Kept))
	for _, id := range preview.Deleted {
		fmt.Fprintf(out, "  - %s\n", id)
	}

	if dryRun {
		fmt.Fprintln(out, "\nDry run — no backups were removed.")
		return nil
	}

	// Safety: in non-TTY contexts, require --yes. Looping scripts must be
	// explicit about their intent.
	isTTY := xterm.IsTerminal(os.Stdin.Fd())
	if !yes {
		if !isTTY {
			return fmt.Errorf("refusing to prune without --yes in a non-TTY context")
		}
		if !confirmPrompt(fmt.Sprintf("Delete %d backup(s)?", len(preview.Deleted))) {
			fmt.Fprintln(out, "Cancelled.")
			return nil
		}
	}

	result, err := core.Prune(store, policy, false)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Pruned %d backup(s).\n", len(result.Deleted))
	return nil
}

// confirmPrompt prints "<question> [y/N]: " and returns true only on an
// affirmative ("y" / "yes", case-insensitive) reply.  Empty input → false.
func confirmPrompt(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", question)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// runAutoPruneIfEnabled is invoked from the backup paths after a successful
// backup. It applies the policy when AutoPrune is on and prints a one-line
// summary. Errors are downgraded to warnings — the user's new backup already
// succeeded, and a prune failure should not turn that into a non-zero exit.
func runAutoPruneIfEnabled(out *cobra.Command, _ config.Config) {
	if !cfg.Retention.AutoPrune {
		return
	}
	if cfg.Retention.KeepLast == 0 && cfg.Retention.MaxAgeDays == 0 {
		return // policy disabled — nothing to do even with AutoPrune on
	}
	store, err := getStorage()
	if err != nil {
		fmt.Fprintf(out.ErrOrStderr(), "warning: auto-prune skipped (storage error): %v\n", err)
		return
	}
	result, err := core.Prune(store, cfg.Retention, false)
	if err != nil {
		fmt.Fprintf(out.ErrOrStderr(), "warning: auto-prune failed: %v\n", err)
		return
	}
	if len(result.Deleted) > 0 {
		fmt.Fprintf(out.OutOrStdout(), "Pruned %d old backup(s).\n", len(result.Deleted))
	}
}
