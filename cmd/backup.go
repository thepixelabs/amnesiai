package cmd

import (
	"fmt"
	"os"
	"strings"

	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/core"
	"github.com/thepixelabs/amnesiai/internal/remote"
	"github.com/thepixelabs/amnesiai/internal/storage"
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
	backupCmd.Flags().Bool("no-push", false, "skip automatic git push for git-remote mode")

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
	noPush, _ := cmd.Flags().GetBool("no-push")
	// AutoPush=false in config also disables push; honour both.
	if !cfg.AutoPush {
		noPush = true
	}

	// Resolve a scoped token for the bound account so that git push uses the
	// correct credentials for multi-account setups instead of the ambient token.
	var tokenEnv []string
	if cfg.StorageMode == "git-remote" && cfg.GitRemote.URL != "" {
		st, stErr := config.LoadState()
		if stErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load state for token resolution: %v\n", stErr)
		} else if binding, ok := st.LookupBinding(cfg.GitRemote.URL); ok {
			switch binding.Host {
			case "github":
				if tok, tokErr := remote.GHToken(binding.Account); tokErr == nil && tok != "" {
					tokenEnv = []string{"GH_TOKEN=" + tok}
				} else if tokErr != nil {
					fmt.Fprintf(os.Stderr, "warning: could not fetch GH token for %s: %v\n", binding.Account, tokErr)
				}
			case "gitlab":
				if tok, tokErr := remote.GLabToken(binding.Account); tokErr == nil && tok != "" {
					tokenEnv = []string{"GITLAB_TOKEN=" + tok}
				} else if tokErr != nil {
					fmt.Fprintf(os.Stderr, "warning: could not fetch GitLab token for %s: %v\n", binding.Account, tokErr)
				}
			}
		}
	}

	store, err := storage.NewWithOptions(cfg.StorageMode, cfg.BackupDir, noPush, tokenEnv)
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
	noEncrypt := getNoEncrypt(cmd)
	forceNoEncrypt := getForceNoEncrypt(cmd)

	opts := core.BackupOptions{
		Providers:      providers,
		ProjectPaths:   cfg.ProjectPaths,
		Passphrase:     getPassphrase(cmd),
		Labels:         labels,
		Message:        message,
		NoEncrypt:      noEncrypt,
		ForceNoEncrypt: forceNoEncrypt,
	}

	result, err := core.Backup(store, opts)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Backup complete: %s\n", result.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "Providers: %s\n", strings.Join(result.Providers, ", "))
	fmt.Fprintf(cmd.OutOrStdout(), "Timestamp: %s\n", result.Timestamp.Format("2006-01-02 15:04:05 UTC"))

	encrypted := opts.Passphrase != ""
	isTTY := xterm.IsTerminal(os.Stdout.Fd())

	for provName, findings := range result.Findings {
		if len(findings) == 0 {
			continue
		}
		// Summary line — always printed.
		if encrypted {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"WARNING: %d secret(s) found in %s (encrypted in archive)",
				len(findings), provName)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"WARNING: %d secret(s) REDACTED in %s (archive is UNENCRYPTED)",
				len(findings), provName)
		}

		// Only show the detail hint and per-rule breakdown when stdout is a TTY.
		// When piped/redirected we must not leak file paths + rule IDs (S1/Q3).
		if isTTY {
			fmt.Fprintf(cmd.ErrOrStderr(), " [pass -d for details]\n")
			// Group findings by rule ID for a compact display.
			ruleCount := make(map[string]int)
			for _, f := range findings {
				ruleCount[f.Type]++
			}
			for ruleID, count := range ruleCount {
				fmt.Fprintf(cmd.ErrOrStderr(), "  %dx %s\n", count, ruleID)
			}
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "\n")
		}
	}

	incrementBackupCount()

	return nil
}
