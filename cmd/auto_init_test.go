package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thepixelabs/amnesiai/internal/config"
)

// TestGetStorage_AutoInitsGitLocalRepo verifies that calling getStorage()
// against a fresh git-local config + empty backup dir transparently runs
// `git init` and the backup dir becomes a git repo (.git/ exists).
//
// This catches the failure mode where a user hand-edits config.toml or where a
// pre-fix install completed onboarding without running InitGitLocal.
func TestGetStorage_AutoInitsGitLocalRepo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backups")

	// Mutate the package-level cfg the helpers read from. Save+restore around
	// the test so we don't leak state across tests.
	prev := cfg
	t.Cleanup(func() { cfg = prev })

	cfg = config.Config{
		StorageMode: "git-local",
		BackupDir:   dir,
	}

	store, err := getStorage()
	if err != nil {
		t.Fatalf("getStorage: %v", err)
	}
	if store == nil {
		t.Fatal("getStorage returned nil store with no error")
	}

	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		t.Fatalf("expected .git/ to exist after auto-init, got: %v", err)
	}

	// Idempotency: a second call must succeed without error.
	if _, err := getStorage(); err != nil {
		t.Fatalf("second getStorage() should be idempotent: %v", err)
	}
}

// TestGetStorage_LocalModeDoesNotCreateGitRepo verifies that the auto-init
// guard only runs for git-y modes.
func TestGetStorage_LocalModeDoesNotCreateGitRepo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backups")

	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = config.Config{
		StorageMode: "local",
		BackupDir:   dir,
	}

	if _, err := getStorage(); err != nil {
		t.Fatalf("getStorage(local): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Fatal("local mode should not create a .git directory")
	}
}
