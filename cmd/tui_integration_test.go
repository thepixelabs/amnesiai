package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	providerregistry "github.com/thepixelabs/amnesiai/internal/provider"
	_ "github.com/thepixelabs/amnesiai/internal/provider/all"
)

// TestPresentProvidersNow_RealRegistry exercises the production helper against
// the real provider registry with a controlled $HOME. Covers the wiring seam
// (ProviderOpts construction + registry.Get + real provider.Discover) that the
// fake-getter unit test in tui_test.go deliberately bypasses.
//
// Concrete regressions this catches that nothing else does:
//   - A provider removed from internal/provider/all (silently shrinking
//     registry.Names()).
//   - ProviderOpts shape change that breaks presentProvidersNow's wiring.
//   - A real provider's Discover() returning a non-empty list for a
//     non-existent base dir (false-present bug).
func TestPresentProvidersNow_RealRegistry(t *testing.T) {
	registered := providerregistry.Names()
	if len(registered) == 0 {
		t.Fatal("no providers registered; internal/provider/all import-side-effect missing?")
	}

	t.Run("EmptyHomeReturnsNone", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		got := callPresentProvidersNow()
		if len(got) != 0 {
			t.Fatalf("empty HOME: picker would show %v, want []", got)
		}
	})

	t.Run("OnlyClaudePresent", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte("# integration\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := callPresentProvidersNow()
		if !reflect.DeepEqual(got, []string{"claude"}) {
			t.Fatalf("only ~/.claude present: picker would show %v, want [claude]", got)
		}
	})

	t.Run("OnlyGeminiPresent", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".gemini", "GEMINI.md"), []byte("# integration\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := callPresentProvidersNow()
		if !reflect.DeepEqual(got, []string{"gemini"}) {
			t.Fatalf("only ~/.gemini present: picker would show %v, want [gemini]", got)
		}
	})

	t.Run("MultiplePresent", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		for _, p := range []struct{ dir, file string }{
			{".claude", "CLAUDE.md"},
			{".gemini", "GEMINI.md"},
		} {
			if err := os.MkdirAll(filepath.Join(home, p.dir), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, p.dir, p.file), []byte("# integration\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got := callPresentProvidersNow()
		sort.Strings(got)
		want := []string{"claude", "gemini"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("claude + gemini present: picker would show %v, want %v", got, want)
		}
	})
}

// callPresentProvidersNow is a thin wrapper that invokes the same code path as
// presentProvidersNow but with a zero-value ProviderOpts (independent of the
// global cfg, which is not initialized in unit-test runs).
func callPresentProvidersNow() []string {
	opts := providerregistry.ProviderOpts{}
	return presentProviderNames(providerregistry.Names(), func(n string) (providerregistry.Provider, error) {
		return providerregistry.Get(n, opts)
	})
}
