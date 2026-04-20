package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thepixelabs/amnesiai/internal/config"
)

// withTempHome overrides os.UserHomeDir indirectly by pointing the HOME env
// var to a fresh temp directory for the duration of the test.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestLoadState_MissingFileReturnsDefault(t *testing.T) {
	withTempHome(t)

	s, err := config.LoadState()
	if err != nil {
		t.Fatalf("LoadState on missing file returned error: %v", err)
	}
	if s == nil {
		t.Fatal("LoadState returned nil state for missing file")
	}
	if s.RemoteBindings == nil {
		t.Error("RemoteBindings map should be non-nil on default state")
	}
	if len(s.RemoteBindings) != 0 {
		t.Errorf("expected empty RemoteBindings, got %d entries", len(s.RemoteBindings))
	}
}

func TestSaveAndLoadState_RoundTrip(t *testing.T) {
	home := withTempHome(t)

	s, err := config.LoadState()
	if err != nil {
		t.Fatalf("initial LoadState: %v", err)
	}

	if err := s.BindRemote("https://github.com/user/repo", "github", "user"); err != nil {
		t.Fatalf("BindRemote: %v", err)
	}
	s.OnboardingLastSeenVersion = "1.1.0"

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the file is written with 0600 permissions.
	statePath := filepath.Join(home, ".amnesiai", "state.json")
	fi, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat state.json: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("state.json permissions = %o, want 0600", fi.Mode().Perm())
	}

	// Load it back and verify round-trip.
	s2, err := config.LoadState()
	if err != nil {
		t.Fatalf("second LoadState: %v", err)
	}

	if s2.OnboardingLastSeenVersion != "1.1.0" {
		t.Errorf("OnboardingLastSeenVersion = %q, want 1.1.0", s2.OnboardingLastSeenVersion)
	}
	binding, ok := s2.RemoteBindings["https://github.com/user/repo"]
	if !ok {
		t.Fatal("expected remote binding for repo, not found")
	}
	if binding.Host != "github" {
		t.Errorf("binding.Host = %q, want github", binding.Host)
	}
	if binding.Account != "user" {
		t.Errorf("binding.Account = %q, want user", binding.Account)
	}
	if binding.LastBoundAt.IsZero() {
		t.Error("LastBoundAt should not be zero")
	}
}

func TestAtomicWrite_TmpFileCleanedUp(t *testing.T) {
	home := withTempHome(t)

	s, _ := config.LoadState()
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The .tmp file should not remain after a successful save.
	tmpPath := filepath.Join(home, ".amnesiai", ".tmp", "state.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file to be absent after save, stat err = %v", err)
	}

	// The final file must exist.
	finalPath := filepath.Join(home, ".amnesiai", "state.json")
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("expected state.json to exist after save, stat err = %v", err)
	}
}

func TestBindRemote_Validation(t *testing.T) {
	withTempHome(t)

	s, _ := config.LoadState()

	cases := []struct {
		name    string
		url     string
		host    string
		account string
		wantErr bool
	}{
		{"valid github", "https://github.com/u/r", "github", "u", false},
		{"valid gitlab", "https://gitlab.com/u/r", "gitlab", "u", false},
		{"empty url", "", "github", "u", true},
		{"bad host", "https://bitbucket.org/u/r", "bitbucket", "u", true},
		{"empty account", "https://github.com/u/r", "github", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.BindRemote(tc.url, tc.host, tc.account)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadState_SchemaVersionGuard(t *testing.T) {
	t.Run("newer schema rejected", func(t *testing.T) {
		home := withTempHome(t)
		dir := filepath.Join(home, ".amnesiai")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Write a state.json with a future schema version.
		data := []byte(`{"schema_version":999,"remote_bindings":{}}`)
		if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := config.LoadState()
		if err == nil {
			t.Fatal("expected error for schema version 999, got nil")
		}
		if !strings.Contains(err.Error(), "newer than supported") {
			t.Errorf("error message should mention 'newer than supported', got: %v", err)
		}
	})

	t.Run("zero schema treated as fresh default", func(t *testing.T) {
		home := withTempHome(t)
		dir := filepath.Join(home, ".amnesiai")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// schema_version omitted — JSON zero value is 0.
		data := []byte(`{"remote_bindings":{}}`)
		if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		s, err := config.LoadState()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s == nil {
			t.Fatal("expected non-nil state")
		}
		if s.RemoteBindings == nil {
			t.Error("RemoteBindings should be non-nil on default state")
		}
	})
}

func TestBindRemote_UpdatesExistingBinding(t *testing.T) {
	withTempHome(t)

	s, _ := config.LoadState()
	url := "https://github.com/user/repo"

	_ = s.BindRemote(url, "github", "alice")
	before := s.RemoteBindings[url].LastBoundAt

	// Small sleep to ensure the timestamp differs.
	time.Sleep(2 * time.Millisecond)

	_ = s.BindRemote(url, "github", "bob")
	after := s.RemoteBindings[url]

	if after.Account != "bob" {
		t.Errorf("account not updated: got %q, want bob", after.Account)
	}
	if !after.LastBoundAt.After(before) {
		t.Error("LastBoundAt should be updated on re-bind")
	}
	if len(s.RemoteBindings) != 1 {
		t.Errorf("expected 1 binding, got %d", len(s.RemoteBindings))
	}
}
