package copilot_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/thepixelabs/amnesiai/pkg/provider/copilot"
)

// populateFakeCopilotDir creates a directory structure representing the
// modern GitHub Copilot CLI config directory (~/.copilot/).
//
//	<base>/
//	  settings.json                            <-- must be included
//	  config.json                              <-- must be included
//	  mcp-config.json                          <-- must be included
//	  lsp-config.json                          <-- must be included
//	  agents/foo.agent.md                      <-- must be included (markdown agent def)
//	  agents/notes.txt                         <-- excluded: not markdown
//	  command-history-state.json               <-- excluded: not in allowlist
//	  logs/some.log                            <-- excluded: logs/ not allowed
//	  token.json                               <-- excluded: name contains "token"
//	  github_token                             <-- excluded: name contains "token"
//	  secret_store.json                        <-- excluded: name contains "secret"
//	  api_key.json                             <-- excluded: name contains "key"
//	  auth.json                                <-- excluded: name contains "auth"
//	  oauth_token.json                         <-- excluded: name contains "token"
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

	mustWrite(filepath.Join(base, "settings.json"), `{"editor":"vscode"}`)
	mustWrite(filepath.Join(base, "config.json"), `{"trustedFolders":[]}`)
	mustWrite(filepath.Join(base, "mcp-config.json"), `{"mcpServers":{}}`)
	mustWrite(filepath.Join(base, "lsp-config.json"), `{"servers":{}}`)
	mustWrite(filepath.Join(base, "agents", "foo.agent.md"), "# foo agent")
	mustWrite(filepath.Join(base, "agents", "notes.txt"), "not a markdown file")
	mustWrite(filepath.Join(base, "command-history-state.json"), `{"items":[]}`)
	mustWrite(filepath.Join(base, "logs", "some.log"), "log line")
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

// TestCopilotDiscover_IncludesAllowlistedFiles verifies the four canonical
// top-level config files are returned by Discover.
func TestCopilotDiscover_IncludesAllowlistedFiles(t *testing.T) {
	base := t.TempDir()
	populateFakeCopilotDir(t, base)

	p := copilot.NewWithBaseDir(base)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	got := make(map[string]bool, len(paths))
	for _, p := range paths {
		got[filepath.Base(p)] = true
	}
	for _, want := range []string{"settings.json", "config.json", "mcp-config.json", "lsp-config.json"} {
		if !got[want] {
			t.Errorf("Discover did not return %q", want)
		}
	}
}

// TestCopilotDiscover_ReturnsAllowlistedFilesAndAgents verifies the complete
// expected set of discovered files (top-level allowlist + agents/*.agent.md)
// and that nothing else slips in.
func TestCopilotDiscover_ReturnsAllowlistedFilesAndAgents(t *testing.T) {
	base := t.TempDir()
	populateFakeCopilotDir(t, base)

	p := copilot.NewWithBaseDir(base)
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
		filepath.Join("agents", "foo.agent.md"),
		"config.json",
		"lsp-config.json",
		"mcp-config.json",
		"settings.json",
	}
	sort.Strings(want)

	if len(rel) != len(want) {
		t.Fatalf("Discover returned %d files, want %d:\n  got:  %v\n  want: %v", len(rel), len(want), rel, want)
	}
	for i := range want {
		if rel[i] != want[i] {
			t.Errorf("files[%d]: got %q, want %q", i, rel[i], want[i])
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

// TestCopilotDiscover_PerProject_FindsCopilotInstructions verifies that
// <project>/.github/copilot-instructions.md is discovered when ProjectPaths is
// configured and the file exists.
func TestCopilotDiscover_PerProject_FindsCopilotInstructions(t *testing.T) {
	globalBase := t.TempDir()

	// One project with the file, one without.
	projWith := t.TempDir()
	projWithout := t.TempDir()

	instrPath := filepath.Join(projWith, ".github", "copilot-instructions.md")
	if err := os.MkdirAll(filepath.Dir(instrPath), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(instrPath, []byte("# Copilot rules"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := copilot.NewWithProjects(globalBase, []string{projWith, projWithout})
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	found := false
	for _, path := range paths {
		if path == instrPath {
			found = true
		}
	}
	if !found {
		t.Errorf("Discover did not return copilot-instructions.md at %s", instrPath)
	}

	// projWithout should contribute nothing.
	for _, path := range paths {
		if strings.HasPrefix(path, projWithout) {
			t.Errorf("Discover returned path from project without instructions file: %s", path)
		}
	}
}

// TestCopilotDiscover_PerProject_EmptyPathsSkips verifies that an empty
// ProjectPaths does not cause an error and simply skips per-project scanning.
func TestCopilotDiscover_PerProject_EmptyPathsSkips(t *testing.T) {
	base := t.TempDir()
	populateFakeCopilotDir(t, base)

	p := copilot.NewWithProjects(base, nil)
	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover with empty ProjectPaths: unexpected error: %v", err)
	}
	// All returned paths should be inside the global base dir.
	for _, path := range paths {
		if !strings.HasPrefix(path, base) {
			t.Errorf("expected all paths under %s, got: %s", base, path)
		}
	}
}

// TestCopilotRestore_PerProject_WritesInstructions verifies that a per-project
// copilot-instructions.md key (absolute path) is restored correctly.
func TestCopilotRestore_PerProject_WritesInstructions(t *testing.T) {
	globalBase := t.TempDir()
	proj := t.TempDir()

	instrPath := filepath.Join(proj, ".github", "copilot-instructions.md")
	content := []byte("# restored instructions")

	p := copilot.NewWithProjects(globalBase, []string{proj})
	snapshot := map[string][]byte{
		instrPath: content,
	}

	if err := p.Restore(snapshot); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, err := os.ReadFile(instrPath)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
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

		// Write a new agent definition file that passes the allowlist.
		newFile := filepath.Join(base, "agents", "bar.agent.md")
		if err := os.WriteFile(newFile, []byte("# bar agent"), 0600); err != nil {
			t.Fatalf("write bar.agent.md: %v", err)
		}

		entries, err := p.Diff(snapshot)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		found := false
		want := filepath.Join("agents", "bar.agent.md")
		for _, e := range entries {
			if e.Path == want {
				found = true
				if e.Status != "added" {
					t.Errorf("%s: got status %q, want %q", want, e.Status, "added")
				}
			}
		}
		if !found {
			t.Errorf("Diff did not return an entry for newly added %s", want)
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
