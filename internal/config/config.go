// Package config handles loading and saving of amnesiai configuration.
// Configuration is stored at ~/.amnesiai/config.toml and can be overridden
// by environment variables prefixed with AMNESIAI_ or by CLI flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// GitRemote holds configuration for git-remote storage mode.
type GitRemote struct {
	URL    string `mapstructure:"url"`
	Branch string `mapstructure:"branch"`
}

// Config holds the top-level amnesiai configuration.
type Config struct {
	StorageMode  string    `mapstructure:"storage_mode"`  // "local" | "git-local" | "git-remote"
	BackupDir    string    `mapstructure:"backup_dir"`    // absolute path for backups
	Providers    []string  `mapstructure:"providers"`     // ["claude","gemini","copilot","codex"] or subset
	GitRemote    GitRemote `mapstructure:"git_remote"`
	AutoCommit   bool      `mapstructure:"auto_commit"`   // true=commit automatically
	AutoPush     bool      `mapstructure:"auto_push"`     // true=push automatically (git-remote only)
	BackupCount  int       `mapstructure:"backup_count"`  // total successful backups taken
	FirstRun     bool      `mapstructure:"first_run"`     // true until first successful backup
	VerboseHelp  bool      `mapstructure:"verbose_help"`  // show extended help text
	Telemetry    bool      `mapstructure:"telemetry"`     // opt-in usage telemetry
	ProjectPaths []string  `mapstructure:"project_paths"` // per-project dirs to scan for CLAUDE.md, copilot-instructions.md
}

// DefaultProviders returns the full list of supported provider names.
func DefaultProviders() []string {
	return []string{"claude", "gemini", "copilot", "codex"}
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		StorageMode: "local",
		BackupDir:   filepath.Join(home, ".amnesiai", "backups"),
		Providers:   DefaultProviders(),
		GitRemote: GitRemote{
			Branch: "main",
		},
		AutoCommit:  true,
		AutoPush:    false,
		BackupCount: 0,
		FirstRun:    true,
		VerboseHelp: false,
		Telemetry:   false,
	}
}

// ConfigDir returns the path to the amnesiai configuration directory (~/.amnesiai).
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".amnesiai"), nil
}

// ConfigFilePath returns the path to the default config file.
func ConfigFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads the configuration from the viper instance, applying defaults.
// Call this after viper has been configured with config paths and env bindings.
func Load(v *viper.Viper) (Config, error) {
	defaults := DefaultConfig()

	// Register defaults with viper so they are returned when no config file
	// or env var provides a value.  These must be registered before Unmarshal
	// is called; they do not affect higher-priority sources (file, env vars).
	v.SetDefault("storage_mode", defaults.StorageMode)
	v.SetDefault("backup_dir", defaults.BackupDir)
	v.SetDefault("providers", defaults.Providers)
	v.SetDefault("git_remote.branch", defaults.GitRemote.Branch)
	v.SetDefault("auto_commit", defaults.AutoCommit)
	v.SetDefault("auto_push", defaults.AutoPush)
	v.SetDefault("backup_count", 0)
	v.SetDefault("first_run", true)
	v.SetDefault("verbose_help", false)
	v.SetDefault("telemetry", false)
	v.SetDefault("project_paths", []string{})

	// Unmarshal into a zero-value struct so that viper's effective value for
	// each key (file > env > default) is written without interference from
	// pre-populated Go values.  Pre-populating a struct before Unmarshal
	// causes viper to skip slice values from the config file when the
	// pre-populated value differs from the file value.
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// SaveTo writes the configuration to the specified file path.
// The parent directory must already exist.  The file is written with 0600
// permissions to prevent other users from reading sensitive values.
func SaveTo(cfg Config, cfgPath string) error {
	v := viper.New()
	v.Set("storage_mode", cfg.StorageMode)
	v.Set("backup_dir", cfg.BackupDir)
	v.Set("providers", cfg.Providers)
	v.Set("git_remote.url", cfg.GitRemote.URL)
	v.Set("git_remote.branch", cfg.GitRemote.Branch)
	v.Set("auto_commit", cfg.AutoCommit)
	v.Set("auto_push", cfg.AutoPush)
	v.Set("backup_count", cfg.BackupCount)
	v.Set("first_run", cfg.FirstRun)
	v.Set("verbose_help", cfg.VerboseHelp)
	v.Set("telemetry", cfg.Telemetry)
	v.Set("project_paths", cfg.ProjectPaths)

	if err := v.WriteConfigAs(cfgPath); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	// Ensure config file has restrictive permissions.
	if err := os.Chmod(cfgPath, 0600); err != nil {
		return fmt.Errorf("failed to set config permissions: %w", err)
	}
	return nil
}

// Save writes the configuration to ~/.amnesiai/config.toml.
func Save(cfg Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}
	return SaveTo(cfg, filepath.Join(dir, "config.toml"))
}

// Validate checks that the config values are coherent.
func Validate(cfg Config) error {
	switch cfg.StorageMode {
	case "local", "git-local", "git-remote":
		// ok
	default:
		return fmt.Errorf("invalid storage_mode %q: must be local, git-local, or git-remote", cfg.StorageMode)
	}

	if cfg.BackupDir == "" {
		return fmt.Errorf("backup_dir must not be empty")
	}

	if cfg.StorageMode == "git-remote" && cfg.GitRemote.URL == "" {
		return fmt.Errorf("git_remote.url is required when storage_mode is git-remote")
	}

	for _, p := range cfg.Providers {
		switch p {
		case "claude", "gemini", "copilot", "codex":
			// ok
		default:
			return fmt.Errorf("unknown provider %q", p)
		}
	}

	for _, p := range cfg.ProjectPaths {
		if p == "/" {
			return fmt.Errorf("project_paths: %q is not allowed", p)
		}
		if !filepath.IsAbs(p) && !strings.HasPrefix(p, "~") {
			return fmt.Errorf("project_paths: %q must be an absolute path or start with ~", p)
		}
	}

	return nil
}
