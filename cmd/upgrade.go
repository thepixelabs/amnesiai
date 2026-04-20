package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the storage mode while preserving existing backup history",
	Long: `Upgrades the backup directory to a richer storage mode without losing
any existing backups.

Supported upgrade paths:
  local → git-local   Git-initialises the backup directory and commits all
                      existing backups as a single "upgrade" commit.
  git-local → git-remote
                      Wires an existing git-local repo to a remote repository
                      and optionally pushes the history.

The config file is updated automatically after a successful upgrade.`,
	Example: `  amnesiai upgrade --mode git-local
  amnesiai upgrade --mode git-remote --remote-url https://github.com/you/amnesiai-backups
  amnesiai upgrade --mode git-remote --create-repo --repo-name amnesiai-backups`,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().String("mode", "", "target storage mode: git-local, git-remote (required)")
	upgradeCmd.Flags().Bool("create-repo", false, "create the remote repository automatically via gh/glab (git-remote only)")
	upgradeCmd.Flags().String("repo-name", "", "repository name for --create-repo (git-remote only)")
	upgradeCmd.Flags().String("remote-url", "", "existing remote repository URL (git-remote only)")

	_ = upgradeCmd.MarkFlagRequired("mode")

	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	targetMode, _ := cmd.Flags().GetString("mode")
	currentMode := cfg.StorageMode
	backupDir := cfg.BackupDir

	switch {
	case currentMode == "local" && targetMode == "git-local":
		return upgradeLocalToGitLocal(cmd, backupDir)

	case currentMode == "git-local" && targetMode == "git-remote":
		return upgradeGitLocalToRemote(cmd, backupDir)

	case currentMode == "local" && targetMode == "git-remote":
		// Two-step: local → git-local → git-remote.
		if err := upgradeLocalToGitLocal(cmd, backupDir); err != nil {
			return err
		}
		return upgradeGitLocalToRemote(cmd, backupDir)

	case currentMode == targetMode:
		return fmt.Errorf("already using storage mode %q — no upgrade needed", currentMode)

	default:
		return fmt.Errorf("unsupported upgrade path: %s → %s", currentMode, targetMode)
	}
}

func upgradeLocalToGitLocal(cmd *cobra.Command, dir string) error {
	if err := storage.UpgradeLocalToGitLocal(dir); err != nil {
		return fmt.Errorf("upgrade local → git-local: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Upgraded backup directory to git-local: %s\n", dir)

	cfg.StorageMode = "git-local"
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: could not save config: %v\n", err)
	}
	return nil
}

func upgradeGitLocalToRemote(cmd *cobra.Command, dir string) error {
	createRepo, _ := cmd.Flags().GetBool("create-repo")
	repoName, _ := cmd.Flags().GetString("repo-name")
	remoteURL, _ := cmd.Flags().GetString("remote-url")

	if remoteURL == "" {
		remoteURL = cfg.GitRemote.URL
	}
	if !createRepo && remoteURL == "" {
		return fmt.Errorf("git-remote upgrade requires either --remote-url or --create-repo")
	}

	url, err := storage.UpgradeGitLocalToRemote(storage.InitGitRemoteOptions{
		Dir:        dir,
		RepoURL:    remoteURL,
		CreateRepo: createRepo,
		RepoName:   repoName,
	})
	if err != nil {
		return fmt.Errorf("upgrade git-local → git-remote: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Upgraded backup repository to git-remote\n")
	fmt.Fprintf(cmd.OutOrStdout(), "Remote: %s\n", url)

	cfg.StorageMode = "git-remote"
	cfg.GitRemote.URL = url
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: could not save config: %v\n", err)
	}
	return nil
}
