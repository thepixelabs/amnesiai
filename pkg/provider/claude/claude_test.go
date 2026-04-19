package claude_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thepixelabs/amnesiai/pkg/provider/claude"
)

// populateFakeClaudeDir creates a directory structure that mirrors ~/.claude/
// with files that should be discovered, files that should be excluded, and
// excluded subtrees.
//
//	<base>/
//	  CLAUDE.md                   <-- backed up
//	  settings.json               <-- backed up
//	  settings.local.json         <-- excluded (machine-local)
//	  todos/                      <-- entire subtree excluded
//	    todo1.md
//	  ide/                        <-- entire subtree excluded
//	    keybindings.json
//	  .credentials.json           <-- excluded (credential file)
//	  projects/                   <-- entire subtree excluded
//	    conversation.json
//	  statsig/                    <-- entire subtree excluded
//	    state.json
func populateFakeClaudeDir(t *testing.T, base string) {
	t.Helper()
	mustWrite := func(path string, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustWrite(filepath.Join(base, "CLAUDE.md"), "# CLAUDE\nSystem prompt here.")
	mustWrite(filepath.Join(base, "settings.json"), `{"theme":"dark"}`)
	mustWrite(filepath.Join(base, "settings.local.json"), `{"localOverride":true}`)
	mustWrite(filepath.Join(base, "todos", "todo1.md"), "- [ ] task one")
	mustWrite(filepath.Join(base, "ide", "keybindings.json"), `[]`)
	mustWrite(filepath.Join(base, ".credentials.json"), `{"token":"secret"}`)
	mustWrite(filepath.Join(base, "projects", "conversation.json"), `{"messages":[]}`)
	mustWrite(filepath.Join(base, "statsig", "state.json"), `{}`)
}

// TestDiscover_ReturnsExpectedPathsAndExcludesPrivateDirs verifies that
// Discover includes only the portable config files and excludes projects/,
// statsig/, todos/, ide/, .credentials.json, and settings.local.json.
func TestDiscover_ReturnsExpectedPathsAndExcludesPrivateDirs(t *testing.T) {
	base := t.TempDir()
	populateFakeClaudeDir(t, base)

	p := claude.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Convert to relative paths for stable assertions.
	rel := make([]string, 0, len(paths))
	for _, abs := range paths {
		r, _ := filepath.Rel(base, abs)
		rel = append(rel, r)
	}
	sort.Strings(rel)

	// Only the portable, non-machine-local files survive.
	want := []string{
		"CLAUDE.md",
		"settings.json",
	}
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

		// Check excluded directories.
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

		// Check excluded files.
		if filepath.Base(rel) == "settings.local.json" {
			t.Errorf("Discover returned settings.local.json but it must always be excluded: %s", path)
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

// TestRestore_WritesCorrectContent verifies that Restore produces files with
// exactly the content from the snapshot.
func TestRestore_WritesCorrectContent(t *testing.T) {
	base := t.TempDir()
	p := claude.NewWithBaseDir(base)

	snapshot := map[string][]byte{
		"CLAUDE.md":     []byte("# My custom prompt"),
		"settings.json": []byte(`{"fontSize":16}`),
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

// TestRestore_SilentlySkipsCredentialsJSON verifies that even if
// .credentials.json appears in a snapshot, Restore never writes it.
func TestRestore_SilentlySkipsCredentialsJSON(t *testing.T) {
	base := t.TempDir()
	p := claude.NewWithBaseDir(base)

	snapshot := map[string][]byte{
		"settings.json":     []byte(`{}`),
		".credentials.json": []byte(`{"token":"should-never-land"}`),
	}

	if err := p.Restore(snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	credPath := filepath.Join(base, ".credentials.json")
	if _, err := os.Stat(credPath); err == nil {
		t.Error("Restore wrote .credentials.json, but it must always be skipped")
	}
}

// TestDiscover_NonexistentDirReturnsNilNil verifies that Discover on a
// nonexistent base directory returns (nil, nil) rather than an error —
// meaning the tool is simply not installed.
func TestDiscover_NonexistentDirReturnsNilNil(t *testing.T) {
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

// TestDiff_StatusCases verifies that the claude provider's Diff method correctly
// classifies files as unchanged, modified, added, or deleted relative to the
// snapshot produced by Read.
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

		// Overwrite a known file with different content.
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
		base := t.TempDir()
		populateFakeClaudeDir(t, base)
		p := claude.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		// Write a new eligible file at the top level — not in an excluded dir.
		newFile := filepath.Join(base, "extra.md")
		if err := os.WriteFile(newFile, []byte("# extra notes"), 0600); err != nil {
			t.Fatalf("write extra.md: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "extra.md" {
				found = true
				if e.Status != "added" {
					t.Errorf("extra.md: got status %q, want %q", e.Status, "added")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for the newly added extra.md")
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

		// Delete a file that was in the snapshot.
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
