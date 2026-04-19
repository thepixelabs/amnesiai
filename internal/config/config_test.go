package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/thepixelabs/amnesiai/internal/config"
)

// newViperFromFile creates a viper instance pointed at the given TOML file.
func newViperFromFile(t *testing.T, path string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	// It is valid for the file not to exist — Load handles that case via defaults.
	_ = v.ReadInConfig()
	return v
}

// TestLoad_ValidTOMLReturnsExpectedConfig verifies that Load correctly
// unmarshals a valid TOML file into the Config struct.
func TestLoad_ValidTOMLReturnsExpectedConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	tomlContent := `
storage_mode = "git-local"
backup_dir   = "/tmp/my-backups"
providers    = ["claude", "gemini"]
auto_commit  = false
auto_push    = true

[git_remote]
url    = "https://github.com/example/backup.git"
branch = "main"
`
	if err := os.WriteFile(cfgPath, []byte(tomlContent), 0600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	v := newViperFromFile(t, cfgPath)
	cfg, err := config.Load(v)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.StorageMode != "git-local" {
		t.Errorf("StorageMode: got %q, want %q", cfg.StorageMode, "git-local")
	}
	if cfg.BackupDir != "/tmp/my-backups" {
		t.Errorf("BackupDir: got %q, want %q", cfg.BackupDir, "/tmp/my-backups")
	}
	if len(cfg.Providers) != 2 || cfg.Providers[0] != "claude" || cfg.Providers[1] != "gemini" {
		t.Errorf("Providers: got %v, want [claude gemini]", cfg.Providers)
	}
	if cfg.AutoCommit {
		t.Error("AutoCommit: got true, want false")
	}
	if !cfg.AutoPush {
		t.Error("AutoPush: got false, want true")
	}
	if cfg.GitRemote.URL != "https://github.com/example/backup.git" {
		t.Errorf("GitRemote.URL: got %q, want %q", cfg.GitRemote.URL, "https://github.com/example/backup.git")
	}
}

// TestLoad_MissingFileReturnsSensibleDefaults verifies that Load returns
// valid defaults when no config file exists — it must not return an error.
func TestLoad_MissingFileReturnsSensibleDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nonexistent.toml")

	v := newViperFromFile(t, cfgPath)
	cfg, err := config.Load(v)
	if err != nil {
		t.Fatalf("Load with missing file: unexpected error: %v", err)
	}

	if cfg.StorageMode == "" {
		t.Error("StorageMode should have a default, got empty string")
	}
	if cfg.BackupDir == "" {
		t.Error("BackupDir should have a default, got empty string")
	}
	if len(cfg.Providers) == 0 {
		t.Error("Providers should have defaults, got empty slice")
	}
}

// TestLoad_EnvVarOverridesTomlStorageMode verifies that the
// AMNESIAI_STORAGE_MODE environment variable takes precedence over the
// value set in the TOML config file.
func TestLoad_EnvVarOverridesTomlStorageMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	tomlContent := `storage_mode = "local"` + "\n"
	if err := os.WriteFile(cfgPath, []byte(tomlContent), 0600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	t.Setenv("AMNESIAI_STORAGE_MODE", "git-remote")

	v := newViperFromFile(t, cfgPath)
	// Bind the env var so viper picks it up.
	v.SetEnvPrefix("AMNESIAI")
	v.AutomaticEnv()
	if err := v.BindEnv("storage_mode", "AMNESIAI_STORAGE_MODE"); err != nil {
		t.Fatalf("BindEnv: %v", err)
	}

	cfg, err := config.Load(v)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.StorageMode != "git-remote" {
		t.Errorf("StorageMode: expected env override %q, got %q", "git-remote", cfg.StorageMode)
	}
}

// TestValidate_AcceptsValidConfig verifies that a well-formed Config passes
// validation.
func TestValidate_AcceptsValidConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if err := config.Validate(cfg); err != nil {
		t.Errorf("Validate(DefaultConfig): unexpected error: %v", err)
	}
}

// TestValidate_RejectsUnknownStorageMode verifies that an invalid storage_mode
// is rejected.
func TestValidate_RejectsUnknownStorageMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.StorageMode = "s3"
	if err := config.Validate(cfg); err == nil {
		t.Error("expected error for unknown storage_mode, got nil")
	}
}

// TestValidate_RequiresGitRemoteURL verifies that git-remote mode without a
// URL is rejected.
func TestValidate_RequiresGitRemoteURL(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.StorageMode = "git-remote"
	cfg.GitRemote.URL = ""
	if err := config.Validate(cfg); err == nil {
		t.Error("expected error for git-remote mode without URL, got nil")
	}
}

// TestValidate_EmptyBackupDirReturnsError verifies that an empty backup_dir
// is rejected regardless of other settings.
func TestValidate_EmptyBackupDirReturnsError(t *testing.T) {
	cfg := config.Config{
		BackupDir:   "",
		StorageMode: "local",
		Providers:   []string{"claude"},
	}
	if err := config.Validate(cfg); err == nil {
		t.Error("expected error for empty backup_dir, got nil")
	}
}

// TestValidate_UnknownProviderReturnsError verifies that a provider name not
// in the supported set is rejected.
func TestValidate_UnknownProviderReturnsError(t *testing.T) {
	cfg := config.Config{
		BackupDir:   "/tmp",
		StorageMode: "local",
		Providers:   []string{"claude", "notreal"},
	}
	if err := config.Validate(cfg); err == nil {
		t.Errorf("expected error for unknown provider %q, got nil", "notreal")
	}
}

// TestValidate_AllFourProvidersValid verifies that supplying all four known
// provider names passes validation without error.
func TestValidate_AllFourProvidersValid(t *testing.T) {
	cfg := config.Config{
		BackupDir:   "/tmp",
		StorageMode: "local",
		Providers:   []string{"claude", "gemini", "copilot", "codex"},
	}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("expected no error for all four providers, got: %v", err)
	}
}

// TestSaveLoad_RoundTrip verifies that every Config field survives a Save/Load
// cycle.  This guards against the "silently dropped field" class of bug where a
// new field is added to the struct but forgotten in Save().
func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Patch config dir by writing to a temp path and loading via viper directly.
	// We call Save (which writes to ~/.amnesiai/config.toml) and then redirect
	// the Load viper to that same file.  To avoid touching the real home dir we
	// swap the config path via the cfgFile mechanism used by the CLI.
	cfgPath := filepath.Join(dir, "config.toml")

	// Build a Config with non-zero/non-default values for every field.
	want := config.Config{
		StorageMode: "git-local",
		BackupDir:   filepath.Join(dir, "backups"),
		Providers:   []string{"claude", "codex"},
		GitRemote: config.GitRemote{
			URL:    "https://github.com/example/cfg.git",
			Branch: "backup",
		},
		AutoCommit:  false,
		AutoPush:    true,
		BackupCount: 42,
		FirstRun:    false,
		VerboseHelp: true,
		Telemetry:   true,
	}

	// Save using a viper instance pointed at our temp path.
	if err := config.SaveTo(want, cfgPath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// Load back.
	v := newViperFromFile(t, cfgPath)
	got, err := config.Load(v)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Assert field by field so failures identify the exact culprit.
	if got.StorageMode != want.StorageMode {
		t.Errorf("StorageMode: got %q, want %q", got.StorageMode, want.StorageMode)
	}
	if got.BackupDir != want.BackupDir {
		t.Errorf("BackupDir: got %q, want %q", got.BackupDir, want.BackupDir)
	}
	if len(got.Providers) != len(want.Providers) {
		t.Errorf("Providers length: got %d, want %d (%v)", len(got.Providers), len(want.Providers), got.Providers)
	}
	if got.GitRemote.URL != want.GitRemote.URL {
		t.Errorf("GitRemote.URL: got %q, want %q", got.GitRemote.URL, want.GitRemote.URL)
	}
	if got.GitRemote.Branch != want.GitRemote.Branch {
		t.Errorf("GitRemote.Branch: got %q, want %q", got.GitRemote.Branch, want.GitRemote.Branch)
	}
	if got.AutoCommit != want.AutoCommit {
		t.Errorf("AutoCommit: got %v, want %v", got.AutoCommit, want.AutoCommit)
	}
	if got.AutoPush != want.AutoPush {
		t.Errorf("AutoPush: got %v, want %v", got.AutoPush, want.AutoPush)
	}
	if got.BackupCount != want.BackupCount {
		t.Errorf("BackupCount: got %d, want %d", got.BackupCount, want.BackupCount)
	}
	if got.FirstRun != want.FirstRun {
		t.Errorf("FirstRun: got %v, want %v", got.FirstRun, want.FirstRun)
	}
	if got.VerboseHelp != want.VerboseHelp {
		t.Errorf("VerboseHelp: got %v, want %v", got.VerboseHelp, want.VerboseHelp)
	}
	if got.Telemetry != want.Telemetry {
		t.Errorf("Telemetry: got %v, want %v", got.Telemetry, want.Telemetry)
	}
}
