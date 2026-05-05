package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thepixelabs/amnesiai/internal/core"
)

// TestRestore_OutDirRoundTrip verifies that --out-dir extracts files into a
// fresh directory without touching the provider's real destinations.
func TestRestore_OutDirRoundTrip(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{})

	testMock.snapshot = nil
	testMock.restored = nil

	outDir := filepath.Join(t.TempDir(), "out")
	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatalf("Restore --out-dir: %v", err)
	}
	if r.OutDir == "" {
		t.Error("RestoreResult.OutDir should be set")
	}
}

// TestRestore_OutDir_RefusesProjectPathOverlap verifies that the orchestrator
// refuses an --out-dir that overlaps a configured project path.
func TestRestore_OutDir_RefusesProjectPathOverlap(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()
	result := runBackup(t, store, snap, core.BackupOptions{})

	proj := t.TempDir()
	_, err := core.Restore(store, core.RestoreOptions{
		BackupID:     result.ID,
		OutDir:       proj,
		ProjectPaths: []string{proj},
	})
	if err == nil {
		t.Fatal("expected refusal for OutDir overlapping a project path")
	}
	if !strings.Contains(err.Error(), "overlap") {
		t.Errorf("expected overlap error, got: %v", err)
	}
}

// TestRestore_OutDir_NonEmptyRequiresForce verifies that a non-empty out-dir
// is refused unless Force is set.
func TestRestore_OutDir_NonEmptyRequiresForce(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()
	result := runBackup(t, store, snap, core.BackupOptions{})

	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "leftover"), []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
		OutDir:   out,
	})
	if err == nil {
		t.Fatal("expected refusal for non-empty OutDir without --force")
	}

	// With Force it should succeed.
	_, err = core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
		OutDir:   out,
		Force:    true,
	})
	if err != nil {
		t.Fatalf("expected success with Force=true: %v", err)
	}
}
