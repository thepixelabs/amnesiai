package claude_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thepixelabs/amnesiai/pkg/provider/claude"
)

// mustWrite is a test helper that creates parent directories and writes content.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// populateFakeClaudeDir creates a ~/.claude/-like directory for testing.
//
//	<base>/
//	  CLAUDE.md                   <-- backed up (allowlisted)
//	  settings.json               <-- backed up (allowlisted)
//	  keybindings.json            <-- backed up (allowlisted)
//	  settings.local.json         <-- excluded (not in allowlist)
//	  unknown_file.json           <-- excluded (not in allowlist)
//	  todos/                      <-- excluded (directories are never walked)
//	    todo1.md
//	  ide/                        <-- excluded (directories are never walked)
//	    ide_state.json
//	  .credentials.json           <-- excluded (not in allowlist)
//	  projects/                   <-- excluded (directories are never walked)
//	    conversation.json
//	  statsig/                    <-- excluded (directories are never walked)
//	    state.json
func populateFakeClaudeDir(t *testing.T, base string) {
	t.Helper()
	mustWrite(t, filepath.Join(base, "CLAUDE.md"), "# CLAUDE\nSystem prompt here.")
	mustWrite(t, filepath.Join(base, "settings.json"), `{"theme":"dark"}`)
	mustWrite(t, filepath.Join(base, "keybindings.json"), `[]`)
	mustWrite(t, filepath.Join(base, "settings.local.json"), `{"localOverride":true}`)
	mustWrite(t, filepath.Join(base, "unknown_file.json"), `{}`)
	mustWrite(t, filepath.Join(base, "todos", "todo1.md"), "- [ ] task one")
	mustWrite(t, filepath.Join(base, "ide", "ide_state.json"), `{}`)
	mustWrite(t, filepath.Join(base, ".credentials.json"), `{"token":"secret"}`)
	mustWrite(t, filepath.Join(base, "projects", "conversation.json"), `{"messages":[]}`)
	mustWrite(t, filepath.Join(base, "statsig", "state.json"), `{}`)
}

// TestDiscover_GlobalAllowlistOnly verifies that Discover returns exactly the
// three allowlisted global files and nothing else.
func TestDiscover_GlobalAllowlistOnly(t *testing.T) {
	base := t.TempDir()
	populateFakeClaudeDir(t, base)

	p := claude.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	rel := make([]string, 0, len(paths))
	for _, abs := range paths {
		r, _ := filepath.Rel(base, abs)
		rel = append(rel, r)
	}
	sort.Strings(rel)

	want := []string{"CLAUDE.md", "keybindings.json", "settings.json"}
	sort.Strings(want)

	if len(rel) != len(want) {
		t.Fatalf("Discover returned %d paths, want %d:\n  got:  %v\n  want: %v", len(rel), len(want), rel, want)
	}
	for i := range want {
		if rel[i] != want[i] {
			t.Errorf("paths[%d]: got %q, want %q", i, rel[i], want[i])
		}
	}
}

// TestDiscover_ExcludesCredentialsJSON verifies explicitly that .credentials.json
// never appears in Discover output regardless of what else is present.
func TestDiscover_ExcludesCredentialsJSON(t *testing.T) {
	base := t.TempDir()
	populateFakeClaudeDir(t, base)

	p := claude.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, path := range paths {
		if filepath.Base(path) == ".credentials.json" {
			t.Errorf("Discover returned .credentials.json but it must always be excluded: %s", path)
		}
	}
}

// TestDiscover_ExcludesAllExcludedDirs verifies that files under projects/,
// statsig/, todos/, and ide/ are never discovered, and that settings.local.json
// is also excluded.
func TestDiscover_ExcludesAllExcludedDirs(t *testing.T) {
	base := t.TempDir()
	populateFakeClaudeDir(t, base)

	p := claude.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	excluded := map[string]bool{
		"projects": true,
		"statsig":  true,
		"todos":    true,
		"ide":      true,
	}

	for _, path := range paths {
		rel, _ := filepath.Rel(base, path)
		top := rel
		for {
			parent := filepath.Dir(top)
			if parent == "." {
				break
			}
			top = parent
		}
		if excluded[top] {
			t.Errorf("Discover returned a file inside excluded directory %q: %s", top, path)
		}
		if filepath.Base(rel) == "settings.local.json" {
			t.Errorf("Discover returned settings.local.json but it must always be excluded: %s", path)
		}
	}
}

// TestDiscover_NonexistentDirReturnsNil verifies that Discover on a
// nonexistent base directory returns (nil, nil) rather than an error.
func TestDiscover_NonexistentDirReturnsNil(t *testing.T) {
	base := filepath.Join(t.TempDir(), "does-not-exist")
	p := claude.NewWithBaseDir(base)

	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover on nonexistent dir: unexpected error: %v", err)
	}
	if paths != nil {
		t.Errorf("Discover on nonexistent dir: got paths %v, want nil", paths)
	}
}

// TestDiscover_PerProject_FindsCLAUDEmdAndSettings verifies that per-project
// paths are discovered when ProjectPaths is set.
func TestDiscover_PerProject_FindsCLAUDEmdAndSettings(t *testing.T) {
	// Set up global ~/.claude/ dir.
	globalBase := t.TempDir()
	mustWrite(t, filepath.Join(globalBase, "CLAUDE.md"), "# global")
	mustWrite(t, filepath.Join(globalBase, "settings.json"), `{}`)

	// Set up a project dir with both per-project files.
	proj := t.TempDir()
	mustWrite(t, filepath.Join(proj, "CLAUDE.md"), "# per-project instructions")
	mustWrite(t, filepath.Join(proj, ".claude", "settings.json"), `{"projectSetting":true}`)
	// settings.local.json should NOT be picked up.
	mustWrite(t, filepath.Join(proj, ".claude", "settings.local.json"), `{"machine":true}`)

	p := claude.NewWithProjects(globalBase, []string{proj})
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Build a set of discovered absolute paths.
	found := make(map[string]bool, len(paths))
	for _, abs := range paths {
		found[abs] = true
	}

	wantPresent := []string{
		filepath.Join(globalBase, "CLAUDE.md"),
		filepath.Join(globalBase, "settings.json"),
		filepath.Join(proj, "CLAUDE.md"),
		filepath.Join(proj, ".claude", "settings.json"),
	}
	for _, w := range wantPresent {
		if !found[w] {
			t.Errorf("Discover missing expected path: %s", w)
		}
	}

	// settings.local.json must never appear.
	localJSON := filepath.Join(proj, ".claude", "settings.local.json")
	if found[localJSON] {
		t.Errorf("Discover returned settings.local.json but it must be excluded: %s", localJSON)
	}
}

// TestDiscover_PerProject_EmptyPathsLogsAndSkips verifies that an empty
// ProjectPaths does not cause an error and simply skips per-project scanning.
func TestDiscover_PerProject_EmptyPathsLogsAndSkips(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, filepath.Join(base, "CLAUDE.md"), "# global")

	p := claude.NewWithProjects(base, nil)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover with empty ProjectPaths: unexpected error: %v", err)
	}
	// Only the global CLAUDE.md should appear.
	if len(paths) != 1 {
		t.Errorf("expected 1 path (global CLAUDE.md), got %d: %v", len(paths), paths)
	}
}

// TestDiscover_PerProject_NonexistentProjectSkipped verifies that a project
// path that does not exist is silently skipped.
func TestDiscover_PerProject_NonexistentProjectSkipped(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, filepath.Join(base, "CLAUDE.md"), "# global")

	missing := filepath.Join(t.TempDir(), "no-such-project")
	p := claude.NewWithProjects(base, []string{missing})
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover with missing project: unexpected error: %v", err)
	}
	// Only global CLAUDE.md; missing project contributes nothing.
	for _, path := range paths {
		if filepath.HasPrefix(path, missing) {
			t.Errorf("Discover returned path from missing project: %s", path)
		}
	}
}

// TestRestore_GlobalFiles_WritesCorrectContent verifies that Restore produces
// files with exactly the content from the snapshot for global keys.
func TestRestore_GlobalFiles_WritesCorrectContent(t *testing.T) {
	base := t.TempDir()
	p := claude.NewWithBaseDir(base)

	snapshot := map[string][]byte{
		"CLAUDE.md":        []byte("# My custom prompt"),
		"settings.json":    []byte(`{"fontSize":16}`),
		"keybindings.json": []byte(`[{"key":"ctrl+s"}]`),
	}

	if err := p.Restore(snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for rel, want := range snapshot {
		got, err := os.ReadFile(filepath.Join(base, rel))
		if err != nil {
			t.Fatalf("read restored file %s: %v", rel, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s: got %q, want %q", rel, got, want)
		}
	}
}

// TestRestore_WritesFilesWithCorrectPermissions verifies that Restore creates
// files with 0600 permissions.
func TestRestore_WritesFilesWithCorrectPermissions(t *testing.T) {
	base := t.TempDir()
	p := claude.NewWithBaseDir(base)

	snapshot := map[string][]byte{
		"settings.json": []byte(`{"theme":"dark"}`),
		"CLAUDE.md":     []byte("# My prompt"),
	}

	if err := p.Restore(snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for rel := range snapshot {
		path := filepath.Join(base, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Errorf("%s: got permissions %04o, want 0600", rel, perm)
		}
	}
}

// TestRestore_SilentlySkipsNonAllowlistedKeys verifies that keys not in the
// allowlist (including .credentials.json) are never written.
func TestRestore_SilentlySkipsNonAllowlistedKeys(t *testing.T) {
	base := t.TempDir()
	p := claude.NewWithBaseDir(base)

	snapshot := map[string][]byte{
		"settings.json":     []byte(`{}`),
		".credentials.json": []byte(`{"token":"should-never-land"}`),
		"settings.local.json": []byte(`{"machine":true}`),
		"unknown.json":      []byte(`{}`),
	}

	if err := p.Restore(snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	neverWrite := []string{".credentials.json", "settings.local.json", "unknown.json"}
	for _, name := range neverWrite {
		if _, err := os.Stat(filepath.Join(base, name)); err == nil {
			t.Errorf("Restore wrote %s but it must always be skipped", name)
		}
	}
}

// TestDiff_StatusCases verifies that the claude provider's Diff method correctly
// classifies files as unchanged, modified, added, or deleted.
func TestDiff_StatusCases(t *testing.T) {
	t.Run("Unchanged", func(t *testing.T) {
		base := t.TempDir()
		populateFakeClaudeDir(t, base)
		p := claude.NewWithBaseDir(base)

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
		populateFakeClaudeDir(t, base)
		p := claude.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		target := filepath.Join(base, "settings.json")
		if err := os.WriteFile(target, []byte(`{"theme":"light"}`), 0600); err != nil {
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
		// Start with only CLAUDE.md and settings.json; then add keybindings.json
		// (which is allowlisted) so it appears as "added" in the diff.
		base := t.TempDir()
		mustWrite(t, filepath.Join(base, "CLAUDE.md"), "# prompt")
		mustWrite(t, filepath.Join(base, "settings.json"), `{}`)
		p := claude.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		// Now write keybindings.json — it is allowlisted so Discover will pick it up.
		newFile := filepath.Join(base, "keybindings.json")
		if err := os.WriteFile(newFile, []byte(`[]`), 0600); err != nil {
			t.Fatalf("write keybindings.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "keybindings.json" {
				found = true
				if e.Status != "added" {
					t.Errorf("keybindings.json: got status %q, want %q", e.Status, "added")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for newly added keybindings.json")
		}
	})

	t.Run("Deleted", func(t *testing.T) {
		base := t.TempDir()
		populateFakeClaudeDir(t, base)
		p := claude.NewWithBaseDir(base)

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
