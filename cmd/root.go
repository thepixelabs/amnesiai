// Package cmd defines the CLI commands for amensiai using cobra.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/thepixelabs/amensiai/internal/config"
	"github.com/thepixelabs/amensiai/internal/crypto"
	"github.com/thepixelabs/amensiai/internal/storage"
)

var (
	cfgFile string
	cfg     config.Config
	v       = viper.New()
)

// rootCmd is the base command for amensiai.
var rootCmd = &cobra.Command{
	Use:   "amensiai",
	Short: "Back up and restore AI coding assistant configurations",
	Long: `amensiai is an open-source CLI that backs up and restores
configuration files for AI coding assistants including
Claude Code, Gemini CLI, GitHub Copilot, and Codex CLI.

It supports multiple storage modes (local, git-local, git-remote),
age encryption, secret scanning, and intelligent git commit messages.`,
	Example: `  amensiai
  amensiai backup --providers claude,gemini
  amensiai restore --id 20240416T143022
  amensiai completion zsh`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initConfig()
	},
	RunE: runRoot,
}

// Execute runs the root command. Called from main.go.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.amensiai/config.toml)")
	rootCmd.PersistentFlags().String("storage-mode", "", "storage mode: local, git-local, git-remote")
	rootCmd.PersistentFlags().String("backup-dir", "", "backup directory path")
	rootCmd.PersistentFlags().String("passphrase", "", "encryption passphrase (prefer AMENSIAI_PASSPHRASE env)")
	rootCmd.PersistentFlags().Bool("no-encrypt", false, "skip encryption even if passphrase is available")

	// Bind flags to viper.
	_ = v.BindPFlag("storage_mode", rootCmd.PersistentFlags().Lookup("storage-mode"))
	_ = v.BindPFlag("backup_dir", rootCmd.PersistentFlags().Lookup("backup-dir"))
}

func initConfig() error {
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory: %w", err)
		}
		configDir := filepath.Join(home, ".amensiai")
		v.AddConfigPath(configDir)
		v.SetConfigName("config")
		v.SetConfigType("toml")
	}

	// Environment variable overrides.
	v.SetEnvPrefix("AMENSIAI")
	v.AutomaticEnv()

	// Read config file (ignore "not found" -- defaults will be used).
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Only error on parse failures, not missing files.
			if !os.IsNotExist(err) {
				return fmt.Errorf("read config: %w", err)
			}
		}
	}

	var err error
	cfg, err = config.Load(v)
	if err != nil {
		return err
	}

	return nil
}

// getPassphrase resolves the passphrase from flags and env.
// Returns empty string if --no-encrypt is set.
func getPassphrase(cmd *cobra.Command) string {
	// --no-encrypt is a PersistentFlag on rootCmd; use InheritedFlags so
	// subcommands can see it (cmd.Flags() only returns locally-defined flags).
	noEncrypt, _ := cmd.InheritedFlags().GetBool("no-encrypt")
	if noEncrypt {
		return ""
	}
	flagVal, _ := cmd.InheritedFlags().GetString("passphrase")
	return crypto.PassphraseFromEnvOrFlag(flagVal)
}

// getStorage creates a Storage backend from the current configuration.
func getStorage() (storage.Storage, error) {
	return storage.New(cfg.StorageMode, cfg.BackupDir)
}
