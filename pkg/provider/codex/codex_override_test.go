package codex_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/pkg/provider/codex"
)

func newCodexWithOverride(t *testing.T, base string, extras, excludes []string) *codex.Provider {
	t.Helper()
	return codex.NewWithBaseDirOverrides(base, provider.ProviderOverride{
		ExtraFiles:   extras,
		ExcludeFiles: excludes,
	})
}

// These tests cover the user-facing provider_overrides config feature for the
// codex provider. They construct the Provider via the registered factory so
// the override-application path mirrors what backup/restore actually do.

func TestCodexOverride_ExtraFiles_AddsToAllowlist(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "config.toml"), []byte("# default"), 0600); err != nil {
		t.Fatal(err)
	}
	// Custom file that defaults would skip.
	if err := os.WriteFile(filepath.Join(base, "scratchpad.md"), []byte("# notes"), 0600); err != nil {
		t.Fatal(err)
	}

	p := newCodexWithOverride(t, base, []string{"scratchpad.md"}, nil)

	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	rel := relSorted(t, base, paths)
	want := []string{"config.toml", "scratchpad.md"}
	if !equalSorted(rel, want) {
		t.Errorf("got %v, want %v", rel, want)
	}
}

func TestCodexOverride_ExcludeFiles_RemovesFromAllowlist(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "config.toml"), []byte("# default"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "AGENTS.md"), []byte("# agents"), 0600); err != nil {
		t.Fatal(err)
	}

	// Exclude the AGENTS.md default.
	p := newCodexWithOverride(t, base, nil, []string{"AGENTS.md"})

	paths, err := p.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	rel := relSorted(t, base, paths)
	if !equalSorted(rel, []string{"config.toml"}) {
		t.Errorf("expected only config.toml, got %v", rel)
	}
}

func relSorted(t *testing.T, base string, abs []string) []string {
	t.Helper()
	out := make([]string, 0, len(abs))
	for _, a := range abs {
		r, err := filepath.Rel(base, a)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func equalSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
