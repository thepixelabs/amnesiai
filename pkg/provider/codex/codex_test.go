package codex_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thepixelabs/amensiai/pkg/provider/codex"
)

// populateFakeCodexDir creates a directory structure that mirrors ~/.codex/.
//
//	<base>/
//	  config.json          <-- must be included
//	  instructions.md      <-- must be included
//	  themes/
//	    monokai.json        <-- must be included
//	  ignored_dir/         <-- excluded: not in allowlist
//	    state.json
//	  auth_token.key        <-- excluded: ends with ".key"
//	  credentials.json      <-- excluded: contains "credential"
func populateFakeCodexDir(t *testing.T, base string) {
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

	mustWrite(filepath.Join(base, "config.json"), `{"model":"gpt-4"}`)
	mustWrite(filepath.Join(base, "instructions.md"), "# Codex instructions")
	mustWrite(filepath.Join(base, "themes", "monokai.json"), `{"fg":"#f8f8f2"}`)
	mustWrite(filepath.Join(base, "ignored_dir", "state.json"), `{}`)
	mustWrite(filepath.Join(base, "auth_token.key"), "private key material")
	mustWrite(filepath.Join(base, "credentials.json"), `{"api_key":"secret"}`)
}

// TestCodexDiscover_NonexistentDirReturnsNilNil verifies that Discover on a
// directory that does not exist returns (nil, nil) and no error — the tool
// is simply not installed.
func TestCodexDiscover_NonexistentDirReturnsNilNil(t *testing.T) {
	base := filepath.Join(t.TempDir(), "does-not-exist")
	p := codex.NewWithBaseDir(base)

	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover on nonexistent dir: unexpected error: %v", err)
	}
	if paths != nil {
		t.Errorf("Discover on nonexistent dir: got paths %v, want nil", paths)
	}
}

// TestCodexDiscover_ReturnsAllowedFilesAndExcludesOthers verifies that only
// files in the allowlist tree are returned and excluded files are omitted.
func TestCodexDiscover_ReturnsAllowedFilesAndExcludesOthers(t *testing.T) {
	base := t.TempDir()
	populateFakeCodexDir(t, base)

	p := codex.NewWithBaseDir(base)
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

	want := []string{
		"config.json",
		"instructions.md",
		filepath.Join("themes", "monokai.json"),
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

// TestCodexDiscover_ExcludesKeyAndCredentialFiles verifies the specific
// exclusion rules for .key files and files containing "credential".
func TestCodexDiscover_ExcludesKeyAndCredentialFiles(t *testing.T) {
	base := t.TempDir()
	populateFakeCodexDir(t, base)

	p := codex.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, path := range paths {
		name := filepath.Base(path)
		if filepath.Ext(name) == ".key" {
			t.Errorf("Discover returned a .key file: %s", path)
		}
		// "credentials.json" is in the top-level dir but not in the allowlist
		// ("credentials" != "config.json" | "instructions.md" | "themes").
		// The exclusion happens via the allowlist, not the name filter here,
		// but we still assert it never appears.
		if name == "credentials.json" {
			t.Errorf("Discover returned credentials.json, which must be excluded")
		}
	}
}

// TestDiff_StatusCases verifies that the codex provider's Diff method correctly
// classifies files as unchanged, modified, added, or deleted.
func TestDiff_StatusCases(t *testing.T) {
	t.Run("Unchanged", func(t *testing.T) {
		base := t.TempDir()
		populateFakeCodexDir(t, base)
		p := codex.NewWithBaseDir(base)

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
		populateFakeCodexDir(t, base)
		p := codex.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		target := filepath.Join(base, "config.json")
		if err := os.WriteFile(target, []byte(`{"model":"gpt-4o"}`), 0600); err != nil {
			t.Fatalf("overwrite config.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "config.json" {
				found = true
				if e.Status != "modified" {
					t.Errorf("config.json: got status %q, want %q", e.Status, "modified")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for config.json")
		}
	})

	t.Run("Added", func(t *testing.T) {
		base := t.TempDir()
		populateFakeCodexDir(t, base)
		p := codex.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		// themes/ is in the allowlist; a new .json inside it will be discovered.
		newFile := filepath.Join(base, "themes", "dracula.json")
		if err := os.WriteFile(newFile, []byte(`{"bg":"#282a36"}`), 0600); err != nil {
			t.Fatalf("write dracula.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == filepath.Join("themes", "dracula.json") {
				found = true
				if e.Status != "added" {
					t.Errorf("dracula.json: got status %q, want %q", e.Status, "added")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for newly added themes/dracula.json")
		}
	})

	t.Run("Deleted", func(t *testing.T) {
		base := t.TempDir()
		populateFakeCodexDir(t, base)
		p := codex.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		target := filepath.Join(base, "config.json")
		if err := os.Remove(target); err != nil {
			t.Fatalf("remove config.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "config.json" {
				found = true
				if e.Status != "deleted" {
					t.Errorf("config.json: got status %q, want %q", e.Status, "deleted")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for deleted config.json")
		}
	})
}

// TestCodexRoundTrip_ReadThenRestoreProducesIdenticalFiles verifies the
// read-then-restore round trip produces byte-for-byte identical output in a
// different directory.
func TestCodexRoundTrip_ReadThenRestoreProducesIdenticalFiles(t *testing.T) {
	srcDir := t.TempDir()
	populateFakeCodexDir(t, srcDir)

	src := codex.NewWithBaseDir(srcDir)
	snapshot, err := src.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(snapshot) == 0 {
		t.Fatal("Read returned empty snapshot, expected at least one file")
	}

	dstDir := t.TempDir()
	dst := codex.NewWithBaseDir(dstDir)

	if err := dst.Restore(snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for rel, wantData := range snapshot {
		gotPath := filepath.Join(dstDir, rel)
		gotData, err := os.ReadFile(gotPath)
		if err != nil {
			t.Fatalf("read restored file %s: %v", rel, err)
		}
		if string(gotData) != string(wantData) {
			t.Errorf("%s: content mismatch\n  got:  %q\n  want: %q", rel, gotData, wantData)
		}
	}
}
