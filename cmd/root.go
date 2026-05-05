// Package cmd defines the CLI commands for amnesiai using cobra.
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/thepixelabs/amnesiai/internal/config"
	providerregistry2 "github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

var (
	cfgFile string
	cfg     config.Config
	v       = viper.New()
)

// rootCmd is the base command for amnesiai.
var rootCmd = &cobra.Command{
	Use:   "amnesiai",
	Short: "Back up and restore AI coding assistant configurations",
	Long: `amnesiai is an open-source CLI that backs up and restores
configuration files for AI coding assistants including
Claude Code, Gemini CLI, GitHub Copilot, and Codex CLI.

It supports multiple storage modes (local, git-local, git-remote),
age encryption, secret scanning, and intelligent git commit messages.`,
	Example: `  amnesiai
  amnesiai backup --providers claude,gemini
  amnesiai restore --id 20240416T143022`,
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
	// Suppress Cobra's auto-generated `completion` subcommand; shell completion
	// is intentionally not a supported feature.
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.amnesiai/config.toml)")
	rootCmd.PersistentFlags().String("storage-mode", "", "storage mode: local, git-local, git-remote")
	rootCmd.PersistentFlags().String("backup-dir", "", "backup directory path")
	// --passphrase-fd reads the passphrase from a file descriptor (fd 0 = stdin).
	// Prefer AMNESIAI_PASSPHRASE env var or interactive prompt over this flag.
	// The old --passphrase flag has been removed to prevent secrets appearing in
	// argv / shell history / process listings.
	rootCmd.PersistentFlags().Int("passphrase-fd", -1, "read encryption passphrase from this file descriptor (e.g. 3)")
	rootCmd.PersistentFlags().Bool("no-encrypt", false, "skip encryption even if passphrase is available")
	rootCmd.PersistentFlags().Bool("force-no-encrypt", false, "allow unencrypted backup even when secrets are detected (requires --no-encrypt)")
	// --settings opens the settings/onboarding menu instead of the main TUI.
	rootCmd.Flags().Bool("settings", false, "open the settings menu (re-run onboarding, toggle options)")

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
		configDir := filepath.Join(home, ".amnesiai")
		v.AddConfigPath(configDir)
		v.SetConfigName("config")
		v.SetConfigType("toml")
	}

	// Environment variable overrides.
	v.SetEnvPrefix("AMNESIAI")
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
// Priority: AMNESIAI_PASSPHRASE env > --passphrase-fd > interactive terminal prompt.
// Returns empty string if --no-encrypt is set.
func getPassphrase(cmd *cobra.Command) string {
	// --no-encrypt is a PersistentFlag on rootCmd; use InheritedFlags so
	// subcommands can see it (cmd.Flags() only returns locally-defined flags).
	noEncrypt, _ := cmd.InheritedFlags().GetBool("no-encrypt")
	if noEncrypt {
		return ""
	}

	// 1. Environment variable — highest priority, never visible in argv.
	if envVal := os.Getenv("AMNESIAI_PASSPHRASE"); envVal != "" {
		return envVal
	}

	// 2. --passphrase-fd: read from a specific file descriptor.
	fd, fdErr := cmd.InheritedFlags().GetInt("passphrase-fd")
	if fdErr == nil && fd >= 0 {
		f := os.NewFile(uintptr(fd), "passphrase-fd")
		if f != nil {
			raw, err := io.ReadAll(f)
			_ = f.Close()
			if err == nil && len(raw) > 0 {
				// Strip trailing newline, which is common when piping echo output.
				pp := string(raw)
				for len(pp) > 0 && (pp[len(pp)-1] == '\n' || pp[len(pp)-1] == '\r') {
					pp = pp[:len(pp)-1]
				}
				if pp != "" {
					return pp
				}
			}
		}
	}

	// 3. Interactive prompt — turn off echo so the passphrase is not visible.
	stdinFd := os.Stdin.Fd()
	if xterm.IsTerminal(stdinFd) {
		fmt.Fprint(os.Stderr, "Encryption passphrase (leave blank to skip): ")
		raw, err := xterm.ReadPassword(stdinFd)
		fmt.Fprintln(os.Stderr) // newline after hidden input
		if err == nil && len(raw) > 0 {
			return string(raw)
		}
	}

	return ""
}

// getForceNoEncrypt returns true if --force-no-encrypt was passed.
func getForceNoEncrypt(cmd *cobra.Command) bool {
	v, _ := cmd.InheritedFlags().GetBool("force-no-encrypt")
	return v
}

// getNoEncrypt returns true if --no-encrypt was passed.
func getNoEncrypt(cmd *cobra.Command) bool {
	v, _ := cmd.InheritedFlags().GetBool("no-encrypt")
	return v
}

// getStorage creates a Storage backend from the current configuration.
//
// Defensively auto-inits a git-local repo when the configured mode is git-local
// or git-remote but the directory hasn't been initialised yet (e.g. user
// hand-edited config.toml, or onboarding ran before the init bugfix).
// InitGitLocal is idempotent — it returns nil immediately if the dir is already
// a git repo. For git-remote we only init the local repo here; attaching a
// remote remains a deliberate `amnesiai init` action.
func getStorage() (storage.Storage, error) {
	if err := ensureGitInitIfNeeded(); err != nil {
		return nil, err
	}
	return storage.New(cfg.StorageMode, cfg.BackupDir)
}

// buildProviderOverrides converts user-facing config.ProviderOverride entries
// into the internal provider.ProviderOverride type expected by the registry,
// emitting a stderr warning (but no error) for any provider name in the
// config that the registry doesn't know about. Stale config should not brick
// the binary — we just tell the user we ignored the entry.
func buildProviderOverrides() map[string]providerregistry2.ProviderOverride {
	if len(cfg.ProviderOverrides) == 0 {
		return nil
	}
	known := make(map[string]bool, 8)
	for _, name := range providerregistry2.Names() {
		known[name] = true
	}
	out := make(map[string]providerregistry2.ProviderOverride, len(cfg.ProviderOverrides))
	for name, ov := range cfg.ProviderOverrides {
		if !known[name] {
			fmt.Fprintf(os.Stderr,
				"warning: provider_overrides entry for unknown provider %q ignored\n", name)
			continue
		}
		out[name] = providerregistry2.ProviderOverride{
			ExtraFiles:   append([]string(nil), ov.ExtraFiles...),
			ExcludeFiles: append([]string(nil), ov.ExcludeFiles...),
		}
	}
	return out
}

// ensureGitInitIfNeeded auto-inits the local repo when the storage mode is
// git-y but the dir isn't a git repo yet.
func ensureGitInitIfNeeded() error {
	if cfg.StorageMode != "git-local" && cfg.StorageMode != "git-remote" {
		return nil
	}
	if err := storage.InitGitLocal(cfg.BackupDir); err != nil {
		return fmt.Errorf("auto-init backup repo: %w", err)
	}
	return nil
}
