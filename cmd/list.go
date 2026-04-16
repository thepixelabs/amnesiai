package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available backups",
	Long:  `Lists all backups in storage, sorted newest first.`,
	RunE:  runList,
}

func init() {
	listCmd.Flags().Bool("json", false, "output as JSON")
	listCmd.Flags().Int("limit", 20, "maximum number of backups to show")

	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	store, err := getStorage()
	if err != nil {
		return err
	}

	entries, err := store.List()
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}

	limit, _ := cmd.Flags().GetInt("limit")
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No backups found.")
		return nil
	}

	for _, e := range entries {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  [%s]\n",
			e.ID,
			e.Timestamp.Format("2006-01-02 15:04:05"),
			strings.Join(e.Providers, ", "),
		)
	}

	return nil
}
