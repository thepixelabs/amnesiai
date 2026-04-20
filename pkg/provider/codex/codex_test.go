package codex_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thepixelabs/amnesiai/pkg/provider/codex"
)

// populateFakeCodexDir creates a directory structure that mirrors ~/.codex/.
//
//	<base>/
//	  agents/
//	    coder.toml           <-- must be included
//	    reviewer.toml        <-- must be included
//	    not_a_toml.json      <-- excluded: not .toml
//	  rules/
//	    default.rules        <-- must be included
//	    custom.rules         <-- excluded: not in allowedRulesFiles
//	  other_dir/             <-- excluded: not in allowlist
//	    state.json
//	  auth_token.key         <-- excluded: ends with ".key"
//	  credentials.json       <-- excluded: not in allowlist
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

	mustWrite(filepath.Join(base, "agents", "coder.toml"), `[agent]\nmodel="o3"`)
	mustWrite(filepath.Join(base, "agents", "reviewer.toml"), `[agent]\nmodel="o4-mini"`)
	mustWrite(filepath.Join(base, "agents", "not_a_toml.json"), `{}`)
	mustWrite(filepath.Join(base, "rules", "default.rules"), "# default rules")
	mustWrite(filepath.Join(base, "rules", "custom.rules"), "# custom rules")
	mustWrite(filepath.Join(base, "other_dir", "state.json"), `{}`)
	mustWrite(filepath.Join(base, "auth_token.key"), "private key material")
	mustWrite(filepath.Join(base, "credentials.json"), `{"api_key":"secret"}`)
}

// TestCodexDiscover_NonexistentDirReturnsNilNil verifies that Discover on a
// directory that does not exist returns (nil, nil) — the tool is not installed.
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
		filepath.Join("agents", "coder.toml"),
		filepath.Join("agents", "reviewer.toml"),
		filepath.Join("rules", "default.rules"),
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

// TestCodexDiscover_ExcludesKeyAndCredentialFiles verifies the exclusion rules
// for .key files and non-allowlisted paths (credentials.json, other_dir/).
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
		if name == "credentials.json" {
			t.Errorf("Discover returned credentials.json, which must be excluded")
		}
		if name == "not_a_toml.json" {
			t.Errorf("Discover returned non-toml file from agents/: %s", path)
		}
		if name == "custom.rules" {
			t.Errorf("Discover returned custom.rules, which is not in the rules allowlist")
		}
	}
}

// TestCodexDiscover_AgentsOnlyToml verifies that only .toml files are
// returned from the agents/ directory.
func TestCodexDiscover_AgentsOnlyToml(t *testing.T) {
	base := t.TempDir()
	populateFakeCodexDir(t, base)

	p := codex.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, path := range paths {
		rel, _ := filepath.Rel(base, path)
		// Check that agents/ entries are .toml.
		dir := filepath.Dir(rel)
		if dir == "agents" && filepath.Ext(filepath.Base(rel)) != ".toml" {
			t.Errorf("Discover returned non-.toml file from agents/: %s", rel)
		}
	}
}

// TestCodexDiscover_RulesOnlyDefaultRules verifies that only default.rules is
// returned from the rules/ directory.
func TestCodexDiscover_RulesOnlyDefaultRules(t *testing.T) {
	base := t.TempDir()
	populateFakeCodexDir(t, base)

	p := codex.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, path := range paths {
		rel, _ := filepath.Rel(base, path)
		if filepath.Dir(rel) == "rules" && filepath.Base(rel) != "default.rules" {
			t.Errorf("Discover returned unexpected rules file: %s", rel)
		}
	}

	// default.rules must appear.
	found := false
	for _, path := range paths {
		rel, _ := filepath.Rel(base, path)
		if rel == filepath.Join("rules", "default.rules") {
			found = true
		}
	}
	if !found {
		t.Error("Discover did not return rules/default.rules")
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

		target := filepath.Join(base, "agents", "coder.toml")
		if err := os.WriteFile(target, []byte(`[agent]\nmodel="o4"`), 0600); err != nil {
			t.Fatalf("overwrite coder.toml: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == filepath.Join("agents", "coder.toml") {
				found = true
				if e.Status != "modified" {
					t.Errorf("coder.toml: got status %q, want %q", e.Status, "modified")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for modified agents/coder.toml")
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

		// agents/ is in the allowlist; a new .toml inside it will be discovered.
		newFile := filepath.Join(base, "agents", "planner.toml")
		if err := os.WriteFile(newFile, []byte(`[agent]\nrole="planner"`), 0600); err != nil {
			t.Fatalf("write planner.toml: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == filepath.Join("agents", "planner.toml") {
				found = true
				if e.Status != "added" {
					t.Errorf("planner.toml: got status %q, want %q", e.Status, "added")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for newly added agents/planner.toml")
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

		target := filepath.Join(base, "rules", "default.rules")
		if err := os.Remove(target); err != nil {
			t.Fatalf("remove default.rules: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == filepath.Join("rules", "default.rules") {
				found = true
				if e.Status != "deleted" {
					t.Errorf("default.rules: got status %q, want %q", e.Status, "deleted")
				}
			}
		}
		if !found {
			t.Error("Diff did not return an entry for deleted rules/default.rules")
		}
	})
}

// TestCodexRoundTrip_ReadThenRestoreProducesIdenticalFiles verifies the
// read-then-restore round trip produces byte-for-byte identical output.
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

// TestCodexRestore_RejectsNonAllowlistedPaths verifies that Restore silently
// skips paths not in the allowlist (including top-level traversal attempts).
func TestCodexRestore_RejectsNonAllowlistedPaths(t *testing.T) {
	base := t.TempDir()
	p := codex.NewWithBaseDir(base)

	snapshot := map[string][]byte{
		filepath.Join("agents", "ok.toml"):       []byte(`[agent]`),
		filepath.Join("rules", "default.rules"):   []byte("# ok"),
		filepath.Join("rules", "custom.rules"):    []byte("# should be skipped"),
		"credentials.json":                        []byte(`{"key":"val"}`),
		filepath.Join("other_dir", "state.json"):  []byte(`{}`),
	}

	if err := p.Restore(snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// These must exist.
	mustExist := []string{
		filepath.Join(base, "agents", "ok.toml"),
		filepath.Join(base, "rules", "default.rules"),
	}
	for _, path := range mustExist {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Restore should have written %s but didn't: %v", path, err)
		}
	}

	// These must NOT exist.
	mustNotExist := []string{
		filepath.Join(base, "rules", "custom.rules"),
		filepath.Join(base, "credentials.json"),
		filepath.Join(base, "other_dir", "state.json"),
	}
	for _, path := range mustNotExist {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("Restore wrote %s but it must be skipped", path)
		}
	}
}
