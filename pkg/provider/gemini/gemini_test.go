package gemini_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thepixelabs/amensiai/pkg/provider/gemini"
)

// populateFakeGeminiDir creates a directory structure that mirrors ~/.gemini/.
//
//	<base>/
//	  settings.json
//	  GEMINI.md
//	  themes/
//	    dark.json
//	    light.json
//	  auth_token.json    <-- excluded: starts with "auth"
//	  private.key        <-- excluded: ends with ".key"
//	  unrelated.txt      <-- excluded: not in allowlist
func populateFakeGeminiDir(t *testing.T, base string) {
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

	mustWrite(filepath.Join(base, "settings.json"), `{"theme":"dark"}`)
	mustWrite(filepath.Join(base, "GEMINI.md"), "# Gemini instructions")
	mustWrite(filepath.Join(base, "themes", "dark.json"), `{"bg":"#000"}`)
	mustWrite(filepath.Join(base, "themes", "light.json"), `{"bg":"#fff"}`)
	mustWrite(filepath.Join(base, "auth_token.json"), `{"token":"secret"}`)
	mustWrite(filepath.Join(base, "private.key"), "private key material")
	mustWrite(filepath.Join(base, "unrelated.txt"), "some unrelated file")
}

// TestGeminiDiscover_NonexistentDirReturnsEmptySliceNilError verifies that
// Discover on a path that doesn't exist returns (nil, nil) — not an error.
func TestGeminiDiscover_NonexistentDirReturnsEmptySliceNilError(t *testing.T) {
	base := filepath.Join(t.TempDir(), "does-not-exist")
	p := gemini.NewWithBaseDir(base)

	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover on nonexistent dir: unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("Discover on nonexistent dir: got paths %v, want empty", paths)
	}
}

// TestGeminiDiscover_ReturnsAllowedFilesOnly verifies that only files in the
// allowlist are returned and that credential/auth/key files are excluded.
func TestGeminiDiscover_ReturnsAllowedFilesOnly(t *testing.T) {
	base := t.TempDir()
	populateFakeGeminiDir(t, base)

	p := gemini.NewWithBaseDir(base)
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
		"GEMINI.md",
		"settings.json",
		filepath.Join("themes", "dark.json"),
		filepath.Join("themes", "light.json"),
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

// TestDiff_StatusCases verifies that the gemini provider's Diff method correctly
// classifies files as unchanged, modified, added, or deleted relative to the
// snapshot produced by Read.
func TestDiff_StatusCases(t *testing.T) {
	t.Run("Unchanged", func(t *testing.T) {
		base := t.TempDir()
		populateFakeGeminiDir(t, base)
		p := gemini.NewWithBaseDir(base)

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
		populateFakeGeminiDir(t, base)
		p := gemini.NewWithBaseDir(base)

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
		base := t.TempDir()
		populateFakeGeminiDir(t, base)
		p := gemini.NewWithBaseDir(base)

		snapshot, err := p.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}

		// themes/ is in the allowlist; a new .json inside it will be discovered.
		newFile := filepath.Join(base, "themes", "solarized.json")
		if err := os.WriteFile(newFile, []byte(`{"bg":"#002b36"}`), 0600); err != nil {
			t.Fatalf("write solarized.json: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == filepath.Join("themes", "solarized.json") {
				found = true
				if e.Status != "added" {
					t.Errorf("solarized.json: got status %q, want %q", e.Status, "added")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for the newly added themes/solarized.json")
		}
	})

	t.Run("Deleted", func(t *testing.T) {
		base := t.TempDir()
		populateFakeGeminiDir(t, base)
		p := gemini.NewWithBaseDir(base)

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

// TestGeminiRoundTrip_ReadThenRestoreProducesIdenticalFiles verifies that
// reading from one directory and restoring to a different temp directory
// produces byte-for-byte identical files for all discovered paths.
func TestGeminiRoundTrip_ReadThenRestoreProducesIdenticalFiles(t *testing.T) {
	srcDir := t.TempDir()
	populateFakeGeminiDir(t, srcDir)

	src := gemini.NewWithBaseDir(srcDir)

	snapshot, err := src.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(snapshot) == 0 {
		t.Fatal("Read returned empty snapshot, expected files")
	}

	dstDir := t.TempDir()
	dst := gemini.NewWithBaseDir(dstDir)

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
