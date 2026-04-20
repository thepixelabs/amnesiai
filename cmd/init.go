package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialise the amnesiai backup directory for the configured storage mode",
	Long: `Prepares the backup directory for use.

For "local" mode: creates the backup directory if it does not exist.
For "git-local" mode: creates the directory and initialises a git repository
with a .gitignore and an initial empty commit.
For "git-remote" mode: does everything git-local does, then creates or wires up
a remote repository via gh or glab, verifies it is private, and adds it as
the git remote named "origin".`,
	Example: `  amnesiai init --mode local
  amnesiai init --mode git-local
  amnesiai init --mode git-remote --remote-url https://github.com/you/amnesiai-backups
  amnesiai init --mode git-remote --create-repo --repo-name amnesiai-backups`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().String("mode", "", "storage mode to initialise: local, git-local, git-remote (overrides config)")
	initCmd.Flags().Bool("create-repo", false, "create the remote repository automatically via gh/glab (git-remote only)")
	initCmd.Flags().String("repo-name", "", "repository name for --create-repo (git-remote only)")
	initCmd.Flags().String("remote-url", "", "existing remote repository URL (git-remote only)")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, _ []string) error {
	mode, _ := cmd.Flags().GetString("mode")
	if mode == "" {
		mode = cfg.StorageMode
	}

	backupDir := cfg.BackupDir

	switch mode {
	case "local":
		if err := os.MkdirAll(backupDir, 0700); err != nil {
			return fmt.Errorf("create backup dir: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Initialised local backup directory: %s\n", backupDir)
		return nil

	case "git-local":
		if err := storage.InitGitLocal(backupDir); err != nil {
			return fmt.Errorf("init git-local: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Initialised git-local backup repository: %s\n", backupDir)
		cfg.StorageMode = "git-local"
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: could not save config: %v\n", err)
		}
		return nil

	case "git-remote":
		createRepo, _ := cmd.Flags().GetBool("create-repo")
		repoName, _ := cmd.Flags().GetString("repo-name")
		remoteURL, _ := cmd.Flags().GetString("remote-url")

		if remoteURL == "" {
			remoteURL = cfg.GitRemote.URL
		}
		if !createRepo && remoteURL == "" {
			return fmt.Errorf("git-remote mode requires either --remote-url or --create-repo")
		}

		url, err := storage.InitGitRemote(storage.InitGitRemoteOptions{
			Dir:        backupDir,
			RepoURL:    remoteURL,
			CreateRepo: createRepo,
			RepoName:   repoName,
		})
		if err != nil {
			return fmt.Errorf("init git-remote: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Initialised git-remote backup repository: %s\n", backupDir)
		fmt.Fprintf(cmd.OutOrStdout(), "Remote: %s\n", url)

		cfg.StorageMode = "git-remote"
		cfg.GitRemote.URL = url
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: could not save config: %v\n", err)
		}
		return nil

	default:
		return fmt.Errorf("unknown storage mode %q: must be local, git-local, or git-remote", mode)
	}
}
