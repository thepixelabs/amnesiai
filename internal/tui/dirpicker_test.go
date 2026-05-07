package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// ----------------------------------------------------------------------------
// splitDirPrefix
// ----------------------------------------------------------------------------

// TestSplitDirPrefix_EmptyString verifies that an empty input resolves to the
// filesystem root with no prefix filter — the only safe default when nothing
// has been typed yet.
func TestSplitDirPrefix_EmptyString(t *testing.T) {
	dir, prefix := splitDirPrefix("")
	if dir != "/" {
		t.Errorf("dir = %q, want %q", dir, "/")
	}
	if prefix != "" {
		t.Errorf("prefix = %q, want %q", prefix, "")
	}
}

// TestSplitDirPrefix_AbsolutePathWithBasename verifies that a clean absolute
// path splits into its parent directory and final component.
func TestSplitDirPrefix_AbsolutePathWithBasename(t *testing.T) {
	dir, prefix := splitDirPrefix("/foo/bar")
	if dir != "/foo" {
		t.Errorf("dir = %q, want %q", dir, "/foo")
	}
	if prefix != "bar" {
		t.Errorf("prefix = %q, want %q", prefix, "bar")
	}
}

// TestSplitDirPrefix_TrailingSlash verifies that an input ending with "/" is
// treated as a complete directory path: the directory is the input itself and
// the prefix is empty (all children should be listed).
func TestSplitDirPrefix_TrailingSlash(t *testing.T) {
	dir, prefix := splitDirPrefix("/foo/bar/")
	if dir != "/foo/bar/" {
		t.Errorf("dir = %q, want %q", dir, "/foo/bar/")
	}
	if prefix != "" {
		t.Errorf("prefix = %q, want %q", prefix, "")
	}
}

// TestSplitDirPrefix_TildeExpansion verifies that a "~/something" input is
// expanded to an absolute path under the home directory.  The parent of the
// expanded path is the home directory and the prefix is the basename.
func TestSplitDirPrefix_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home directory: %v", err)
	}

	dir, prefix := splitDirPrefix("~/something")

	wantDir := home
	wantPrefix := "something"

	if dir != wantDir {
		t.Errorf("dir = %q, want %q", dir, wantDir)
	}
	if prefix != wantPrefix {
		t.Errorf("prefix = %q, want %q", prefix, wantPrefix)
	}
}

// TestSplitDirPrefix_TildeAlone verifies that "~" alone (with no slash or
// trailing component) is treated as the home directory with an empty prefix.
func TestSplitDirPrefix_TildeAlone(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home directory: %v", err)
	}

	dir, prefix := splitDirPrefix("~")

	if dir != home {
		t.Errorf("dir = %q, want home %q", dir, home)
	}
	if prefix != "" {
		t.Errorf("prefix = %q, want %q", prefix, "")
	}
}

// ----------------------------------------------------------------------------
// DirPickerModel.refreshSuggestions
// ----------------------------------------------------------------------------

// TestRefreshSuggestions_ListsSubdirectoriesOnly verifies that when the input
// is a directory path with a trailing slash, refreshSuggestions returns only
// child directories and excludes files.
func TestRefreshSuggestions_ListsSubdirectoriesOnly(t *testing.T) {
	tmp := t.TempDir()

	// Create two subdirectories and one regular file.
	for _, sub := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(tmp, sub), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "gamma"), []byte("data"), 0600); err != nil {
		t.Fatalf("create file gamma: %v", err)
	}

	m := DirPickerModel{input: tmp + "/"}
	m.refreshSuggestions()

	wantAlpha := filepath.Join(tmp, "alpha")
	wantBeta := filepath.Join(tmp, "beta")

	if !containsPath(m.suggestions, wantAlpha) {
		t.Errorf("suggestions %v does not contain %q", m.suggestions, wantAlpha)
	}
	if !containsPath(m.suggestions, wantBeta) {
		t.Errorf("suggestions %v does not contain %q", m.suggestions, wantBeta)
	}

	// The regular file must not appear as a suggestion.
	for _, s := range m.suggestions {
		if filepath.Base(s) == "gamma" {
			t.Errorf("suggestions contains file %q; only directories should be listed", s)
		}
	}
}

// TestRefreshSuggestions_PrefixFiltersToMatchingSubdirs verifies that when the
// input is a directory + partial basename, only children whose names start with
// that prefix are suggested.
func TestRefreshSuggestions_PrefixFiltersToMatchingSubdirs(t *testing.T) {
	tmp := t.TempDir()

	for _, sub := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(tmp, sub), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	m := DirPickerModel{input: filepath.Join(tmp, "al")}
	m.refreshSuggestions()

	wantAlpha := filepath.Join(tmp, "alpha")

	if !containsPath(m.suggestions, wantAlpha) {
		t.Errorf("suggestions %v does not contain %q", m.suggestions, wantAlpha)
	}

	// "beta" must not appear — its name does not start with "al".
	for _, s := range m.suggestions {
		if filepath.Base(s) == "beta" {
			t.Errorf("suggestions contains %q, which does not match prefix %q", s, "al")
		}
	}
}

// ----------------------------------------------------------------------------
// Helper
// ----------------------------------------------------------------------------

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
