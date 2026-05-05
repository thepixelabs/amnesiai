// Package cmd — `amnesiai delete <id>` removes a single backup.
//
// Confirmation rules:
//   - In a TTY without --yes: prompt before deleting.
//   - In a non-TTY without --yes: refuse (protects loop-driven scripts).
//   - With --yes: delete unconditionally.
package cmd

import (
	"errors"
	"fmt"
	"os"

	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/storage"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a single backup by ID",
	Long: `Permanently removes one backup from storage. There is no
trash bin: in git modes the deletion is committed (and pushed if
auto_push is on), so the only recovery path is git history.`,
	Example: `  amnesiai delete 20260504T120000Z
  amnesiai delete 20260504T120000Z --yes`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().Bool("yes", false, "skip the confirmation prompt (required in non-TTY contexts)")

	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	yes, _ := cmd.Flags().GetBool("yes")

	store, err := getStorage()
	if err != nil {
		return err
	}

	// Look up metadata so the confirmation prompt can show timestamp/providers
	// — easy to delete the wrong one when ids are dense timestamps.
	meta, _, loadErr := store.Load(id)
	if loadErr != nil {
		return fmt.Errorf("backup %q not found: %w", id, loadErr)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Backup: %s\n", meta.ID)
	fmt.Fprintf(out, "Timestamp: %s\n", meta.Timestamp.Format("2006-01-02 15:04:05 UTC"))
	if len(meta.Providers) > 0 {
		fmt.Fprintf(out, "Providers: %v\n", meta.Providers)
	}

	isTTY := xterm.IsTerminal(os.Stdin.Fd())
	if !yes {
		if !isTTY {
			return fmt.Errorf("refusing to delete without --yes in a non-TTY context")
		}
		if !confirmPrompt(fmt.Sprintf("Delete backup %s from %s?",
			meta.ID, meta.Timestamp.Format("2006-01-02 15:04:05 UTC"))) {
			fmt.Fprintln(out, "Cancelled.")
			return nil
		}
	}

	if err := store.Delete(id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("backup %q not found", id)
		}
		return fmt.Errorf("delete: %w", err)
	}

	fmt.Fprintf(out, "Deleted backup %s.\n", id)
	return nil
}
