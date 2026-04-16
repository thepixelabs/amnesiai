package copilot_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/thepixelabs/amensiai/pkg/provider/copilot"
)

// populateFakeCopilotDir creates a directory structure representing a Copilot
// config directory.
//
//	<base>/
//	  hosts.json              <-- must be included
//	  settings.json           <-- must be included
//	  token.json              <-- excluded: name contains "token"
//	  github_token            <-- excluded: name contains "token"
//	  secret_store.json       <-- excluded: name contains "secret"
//	  api_key.json            <-- excluded: name contains "key"
//	  auth.json               <-- excluded: name contains "auth"
//	  oauth_token.json        <-- excluded: name contains "token"
func populateFakeCopilotDir(t *testing.T, base string) {
	t.Helper()
	mustWrite := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustWrite(filepath.Join(base, "hosts.json"), `{"github.com":{}}`)
	mustWrite(filepath.Join(base, "settings.json"), `{"editor":"vscode"}`)
	mustWrite(filepath.Join(base, "token.json"), `{"access_token":"secret"}`)
	mustWrite(filepath.Join(base, "github_token"), "ghp_xxxxxx")
	mustWrite(filepath.Join(base, "secret_store.json"), `{"secret":"value"}`)
	mustWrite(filepath.Join(base, "api_key.json"), `{"key":"12345"}`)
	mustWrite(filepath.Join(base, "auth.json"), `{"oauth":"token"}`)
	mustWrite(filepath.Join(base, "oauth_token.json"), `{"token":"xxxxx"}`)
}

// TestCopilotDiscover_ExcludesFilesWithSensitiveNames verifies that files
// whose names contain "token", "secret", "key", or "auth" are never returned
// by Discover.
func TestCopilotDiscover_ExcludesFilesWithSensitiveNames(t *testing.T) {
	base := t.TempDir()
	populateFakeCopilotDir(t, base)

	p := copilot.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	sensitiveTerms := []string{"token", "secret", "key", "auth"}
	for _, path := range paths {
		nameLower := strings.ToLower(filepath.Base(path))
		for _, term := range sensitiveTerms {
			if strings.Contains(nameLower, term) {
				t.Errorf("Discover returned file %q which contains sensitive term %q", filepath.Base(path), term)
			}
		}
	}
}

// TestCopilotDiscover_IncludesHostsJSON verifies that hosts.json is discovered.
// The file holds hostname settings, not credentials — actual tokens are in the
// OS keychain.
func TestCopilotDiscover_IncludesHostsJSON(t *testing.T) {
	base := t.TempDir()
	populateFakeCopilotDir(t, base)

	p := copilot.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, path := range paths {
		if filepath.Base(path) == "hosts.json" {
			return // found — test passes
		}
	}
	t.Error("Discover did not return hosts.json, but it must be included")
}

// TestCopilotDiscover_ReturnsOnlyNonSensitiveJSONFiles verifies the complete
// set of discovered files against expected inclusions and exclusions.
func TestCopilotDiscover_ReturnsOnlyNonSensitiveJSONFiles(t *testing.T) {
	base := t.TempDir()
	populateFakeCopilotDir(t, base)

	p := copilot.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)

	// Only hosts.json and settings.json should survive.
	want := []string{"hosts.json", "settings.json"}

	if len(names) != len(want) {
		t.Fatalf("Discover returned %d files, want %d:\n  got:  %v\n  want: %v", len(names), len(want), names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("files[%d]: got %q, want %q", i, names[i], want[i])
		}
	}
}

// TestCopilotDiscover_NonexistentDirReturnsNilNil verifies that Discover on a
// nonexistent base directory returns (nil, nil) — the tool is not installed.
func TestCopilotDiscover_NonexistentDirReturnsNilNil(t *testing.T) {
	base := filepath.Join(t.TempDir(), "not-there")
	p := copilot.NewWithBaseDir(base)

	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover on nonexistent dir: unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("Discover on nonexistent dir: got paths %v, want empty", paths)
	}
}

// TestDiff_StatusCases verifies that the copilot provider's Diff method
// correctly classifies files as unchanged, modified, added, or deleted.
func TestDiff_StatusCases(t *testing.T) {
	t.Run("Unchanged", func(t *testing.T) {
		base := t.TempDir()
		populateFakeCopilotDir(t, base)
		p := copilot.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(snapshot) == 0 {
			t.Fatal("Read returned empty snapshot; cannot test Diff")
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		for _, e := range entries {
			if e.Status != "unchanged" {
				t.Errorf("entry %q: got status %q, want %q", e.Path, e.Status, "unchanged")
			}
		}
	})

	t.Run("Modified", func(t *testing.T) {
		base := t.TempDir()
		populateFakeCopilotDir(t, base)
		p := copilot.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		target := filepath.Join(base, "settings.json")
		if err := os.WriteFile(target, []byte(`{"editor":"neovim"}`), 0600); err != nil {
			t.Fatalf("overwrite settings.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "settings.json" {
				found = true
				if e.Status != "modified" {
					t.Errorf("settings.json: got status %q, want %q", e.Status, "modified")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for settings.json")
		}
	})

	t.Run("Added", func(t *testing.T) {
		base := t.TempDir()
		populateFakeCopilotDir(t, base)
		p := copilot.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		// Write a new non-sensitive JSON file that passes the allowlist.
		newFile := filepath.Join(base, "profiles.json")
		if err := os.WriteFile(newFile, []byte(`{"default":"work"}`), 0600); err != nil {
			t.Fatalf("write profiles.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "profiles.json" {
				found = true
				if e.Status != "added" {
					t.Errorf("profiles.json: got status %q, want %q", e.Status, "added")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for newly added profiles.json")
		}
	})

	t.Run("Deleted", func(t *testing.T) {
		base := t.TempDir()
		populateFakeCopilotDir(t, base)
		p := copilot.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		target := filepath.Join(base, "settings.json")
		if err := os.Remove(target); err != nil {
			t.Fatalf("remove settings.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "settings.json" {
				found = true
				if e.Status != "deleted" {
					t.Errorf("settings.json: got status %q, want %q", e.Status, "deleted")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for deleted settings.json")
		}
	})
}
