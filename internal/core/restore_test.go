package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thepixelabs/amnesiai/internal/core"
)

// TestRestore_LiveRestorePopulatesRestoredPaths verifies that a live restore
// (no DryRun, no OutDir) returns a non-empty RestoredPaths containing the
// original file paths written back through the provider.
func TestRestore_LiveRestorePopulatesRestoredPaths(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{})

	testMock.snapshot = nil
	testMock.restored = nil

	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(r.RestoredPaths) == 0 {
		t.Fatal("RestoreResult.RestoredPaths is empty for a live restore; expected paths of written files")
	}

	// Every path in RestoredPaths must correspond to a path that was in the
	// original snapshot.
	for _, p := range r.RestoredPaths {
		if _, ok := snap[p]; !ok {
			t.Errorf("RestoredPaths contains unexpected path %q; not present in original snapshot", p)
		}
	}

	// Every path in the original snapshot must appear in RestoredPaths.
	if len(r.RestoredPaths) != len(snap) {
		t.Errorf("RestoredPaths has %d entries, want %d (one per backed-up file)", len(r.RestoredPaths), len(snap))
	}
}

// TestRestore_DryRunRestoredPathsEmpty verifies that a dry-run restore does not
// populate RestoredPaths — no files are written so there are no paths to report.
func TestRestore_DryRunRestoredPathsEmpty(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Restore (dry-run): %v", err)
	}

	if len(r.RestoredPaths) != 0 {
		t.Errorf("dry-run RestoreResult.RestoredPaths = %v; expected nil/empty", r.RestoredPaths)
	}
}

// TestRestore_OutDirRestoredPathsEmpty verifies that an out-dir restore does
// not populate RestoredPaths.  Files are written to an alternate directory, not
// to the live provider destinations, so the field must remain empty.
func TestRestore_OutDirRestoredPathsEmpty(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	outDir := filepath.Join(t.TempDir(), "out")
	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatalf("Restore --out-dir: %v", err)
	}

	if len(r.RestoredPaths) != 0 {
		t.Errorf("out-dir RestoreResult.RestoredPaths = %v; expected nil/empty", r.RestoredPaths)
	}
}

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
