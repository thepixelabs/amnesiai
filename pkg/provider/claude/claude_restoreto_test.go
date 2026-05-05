package claude_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thepixelabs/amnesiai/pkg/provider/claude"
)

// TestRestoreTo_RootEmpty_WritesToRealDest verifies that RestoreTo("", snap) is
// equivalent to Restore(snap) — files land at their real destinations.
func TestRestoreTo_RootEmpty_WritesToRealDest(t *testing.T) {
	base := t.TempDir()
	p := claude.NewWithBaseDir(base)

	snap := map[string][]byte{
		"CLAUDE.md": []byte("# real"),
	}
	if err := p.RestoreTo("", snap); err != nil {
		t.Fatalf("RestoreTo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(base, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(got) != "# real" {
		t.Errorf("got %q, want %q", got, "# real")
	}
}

// TestRestoreTo_GlobalKey_RerootsUnderOutDir verifies that with a non-empty
// root, global keys land under <root>/<provider-base>/...
func TestRestoreTo_GlobalKey_RerootsUnderOutDir(t *testing.T) {
	base := t.TempDir()
	out := t.TempDir()
	p := claude.NewWithBaseDir(base)

	snap := map[string][]byte{
		"CLAUDE.md": []byte("# rerooted"),
	}
	if err := p.RestoreTo(out, snap); err != nil {
		t.Fatalf("RestoreTo: %v", err)
	}
	// The file must NOT exist at the real destination.
	if _, err := os.Stat(filepath.Join(base, "CLAUDE.md")); err == nil {
		t.Fatal("RestoreTo with out-dir wrote to real destination; should not")
	}
	// The file must exist under <out>/<base[1:]>/CLAUDE.md.
	expected := filepath.Join(out, base[1:], "CLAUDE.md")
	got, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("read rerooted file %s: %v", expected, err)
	}
	if string(got) != "# rerooted" {
		t.Errorf("got %q, want %q", got, "# rerooted")
	}
}

// TestRestore_PreservesExistingSymlink verifies that when the destination
// path is a symlink (e.g. CLAUDE.md is a chezmoi/dotbot link), Restore writes
// the new content THROUGH the link to the underlying target AND leaves the
// symlink itself intact. A naive tmp+rename implementation silently replaces
// the symlink with a regular file; this test guards against that regression.
func TestRestore_PreservesExistingSymlink(t *testing.T) {
	base := t.TempDir()

	// Set up a real target file outside ~/.claude/ and symlink CLAUDE.md to it.
	dotfilesDir := t.TempDir()
	target := filepath.Join(dotfilesDir, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("# original target"), 0600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	link := filepath.Join(base, "CLAUDE.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	p := claude.NewWithBaseDir(base)
	snap := map[string][]byte{"CLAUDE.md": []byte("# new content via restore")}
	if err := p.RestoreTo("", snap); err != nil {
		t.Fatalf("RestoreTo: %v", err)
	}

	// 1. The symlink itself must still exist (not replaced by a regular file).
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink at ~/.claude/CLAUDE.md was replaced by a regular file")
	}

	// 2. The target file must contain the new content.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "# new content via restore" {
		t.Errorf("target content: got %q, want %q", got, "# new content via restore")
	}

	// 3. Reading through the link should also see the new content.
	gotViaLink, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read via link: %v", err)
	}
	if string(gotViaLink) != "# new content via restore" {
		t.Errorf("link content: got %q, want %q", gotViaLink, "# new content via restore")
	}
}

// TestRestoreTo_AbsoluteKey_RerootsUnderOutDir verifies per-project absolute
// keys are stripped of the leading "/" and joined under root.
func TestRestoreTo_AbsoluteKey_RerootsUnderOutDir(t *testing.T) {
	base := t.TempDir()
	out := t.TempDir()
	proj := t.TempDir()
	p := claude.NewWithProjects(base, []string{proj})

	abs := filepath.Join(proj, "CLAUDE.md")
	snap := map[string][]byte{abs: []byte("# proj")}
	if err := p.RestoreTo(out, snap); err != nil {
		t.Fatalf("RestoreTo: %v", err)
	}
	expected := filepath.Join(out, abs[1:])
	got, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("read rerooted file %s: %v", expected, err)
	}
	if string(got) != "# proj" {
		t.Errorf("got %q, want %q", got, "# proj")
	}
}
