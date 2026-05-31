package core_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thepixelabs/amnesiai/internal/core"
	"github.com/thepixelabs/amnesiai/internal/storage"
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

// ----------------------------------------------------------------------------
// Files filter tests
// ----------------------------------------------------------------------------

// threeFileSnapshot returns a snapshot with three distinct files for filter tests.
func threeFileSnapshot() map[string][]byte {
	return map[string][]byte{
		"config/settings.json":    []byte(`{"theme":"dark"}`),
		"config/keybindings.json": []byte(`{"copy":"ctrl+c"}`),
		"agents/foo.md":           []byte("# Agent foo"),
	}
}

// TestRestore_FilesFilterRestoresOnlySelected verifies that when Files is set,
// only the matching archive entry is written and the result count reflects it.
func TestRestore_FilesFilterRestoresOnlySelected(t *testing.T) {
	store := newStore(t)
	snap := threeFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	// Pick one specific file by its archive path.
	wantArchPath := "testprovider/agents/foo.md"
	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: backupResult.ID,
		Files:    []string{wantArchPath},
	})
	if err != nil {
		t.Fatalf("Restore with Files filter: %v", err)
	}

	if r.Files != 1 {
		t.Errorf("RestoreResult.Files = %d, want 1", r.Files)
	}
	if len(r.RestoredPaths) != 1 {
		t.Fatalf("RestoredPaths = %v, want 1 entry", r.RestoredPaths)
	}

	// The single restored path must match the original path for the selected entry.
	if r.RestoredPaths[0] != "agents/foo.md" {
		t.Errorf("RestoredPaths[0] = %q, want %q", r.RestoredPaths[0], "agents/foo.md")
	}

	// The mock's restored map must contain exactly one file.
	if len(testMock.restored) != 1 {
		t.Errorf("mock restored %d files, want 1", len(testMock.restored))
	}
	if _, ok := testMock.restored["agents/foo.md"]; !ok {
		t.Errorf("mock.restored missing key %q; got keys: %v", "agents/foo.md", keys(testMock.restored))
	}
}

// TestRestore_FilesFilterEmptyMeansAll confirms that when Files is nil/empty,
// every file in the backup is restored (existing behaviour preserved).
func TestRestore_FilesFilterEmptyMeansAll(t *testing.T) {
	store := newStore(t)
	snap := threeFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: backupResult.ID,
	})
	if err != nil {
		t.Fatalf("Restore (no filter): %v", err)
	}

	if r.Files != len(snap) {
		t.Errorf("RestoreResult.Files = %d, want %d", r.Files, len(snap))
	}
}

// TestRestore_FilesFilterCombinesWithProviders verifies that the Files filter
// operates within the intersection of the chosen Providers. When both Providers
// and Files are set, only entries matching both constraints are written.
func TestRestore_FilesFilterCombinesWithProviders(t *testing.T) {
	store := newStore(t)

	// Populate two separate providers.
	alphaSnap := map[string][]byte{
		"alpha-config.json": []byte(`{"alpha":true}`),
	}
	betaSnap := map[string][]byte{
		"beta-config.json": []byte(`{"beta":true}`),
	}

	alphaMock.snapshot = alphaSnap
	betaMock.snapshot = betaSnap
	alphaMock.restored = nil
	betaMock.restored = nil

	opts := core.BackupOptions{
		Providers: []string{"alpha", "beta"},
	}
	result, err := core.Backup(store, opts)
	if err != nil {
		t.Fatalf("Backup two providers: %v", err)
	}

	alphaMock.restored = nil
	betaMock.restored = nil

	// Restore only alpha's file — even though beta is also selected.
	r, restoreErr := core.Restore(store, core.RestoreOptions{
		BackupID:  result.ID,
		Providers: []string{"alpha", "beta"},
		Files:     []string{"alpha/alpha-config.json"},
	})
	if restoreErr != nil {
		t.Fatalf("Restore with provider+file filter: %v", restoreErr)
	}

	if r.Files != 1 {
		t.Errorf("RestoreResult.Files = %d, want 1", r.Files)
	}
	// Alpha must have received the file.
	if len(alphaMock.restored) != 1 {
		t.Errorf("alphaMock.restored has %d entry/entries, want 1", len(alphaMock.restored))
	}
	// Beta must not have received anything.
	if len(betaMock.restored) != 0 {
		t.Errorf("betaMock.restored has %d entry/entries, want 0", len(betaMock.restored))
	}
}

// TestRestore_FilesFilterUnknownPathSkipped verifies that when ALL requested
// paths are unknown (not in the backup), Restore returns an error rather than
// silently succeeding with zero files restored.
func TestRestore_FilesFilterUnknownPathSkipped(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	_, err := core.Restore(store, core.RestoreOptions{
		BackupID: backupResult.ID,
		Files:    []string{"testprovider/does/not/exist.md"},
	})
	if err == nil {
		t.Fatal("expected error when all requested files are unknown; got nil")
	}
	if !strings.Contains(err.Error(), "no files matched") {
		t.Errorf("error %q does not mention 'no files matched'", err.Error())
	}
}

// TestRestore_FilesFilterPartialSkip verifies the partial-miss case: when some
// requested paths match and some do not, the restore succeeds for the matching
// paths and the missing ones are reported in UnknownFiles.
func TestRestore_FilesFilterPartialSkip(t *testing.T) {
	store := newStore(t)
	snap := threeFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: backupResult.ID,
		Files: []string{
			"testprovider/agents/foo.md",     // exists
			"testprovider/does/not/exist.md", // does not exist
		},
	})
	if err != nil {
		t.Fatalf("partial-skip Restore: %v", err)
	}
	if r.Files != 1 {
		t.Errorf("RestoreResult.Files = %d, want 1 (only the matching file)", r.Files)
	}
	if len(r.UnknownFiles) != 1 {
		t.Fatalf("UnknownFiles = %v, want 1 entry", r.UnknownFiles)
	}
	if r.UnknownFiles[0] != "testprovider/does/not/exist.md" {
		t.Errorf("UnknownFiles[0] = %q, want %q", r.UnknownFiles[0], "testprovider/does/not/exist.md")
	}
}

// TestPeekArchive_ReturnsManifestEntries round-trips a 2-file backup, calls
// PeekArchive, and asserts both entries are returned with correct fields.
func TestPeekArchive_ReturnsManifestEntries(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{})

	manifest, unencrypted, err := core.PeekArchive(store, backupResult.ID, "")
	if err != nil {
		t.Fatalf("PeekArchive: %v", err)
	}

	if !unencrypted {
		t.Error("PeekArchive: unencrypted should be true for unencrypted backup")
	}

	if len(manifest) != len(snap) {
		t.Errorf("PeekArchive returned %d entries, want %d", len(manifest), len(snap))
	}

	// Build a quick lookup from ArchPath.
	byArch := make(map[string]core.ManifestEntry, len(manifest))
	for _, me := range manifest {
		byArch[me.ArchPath] = me
	}

	for origPath := range snap {
		wantArch := "testprovider/" + origPath
		me, ok := byArch[wantArch]
		if !ok {
			t.Errorf("PeekArchive missing entry for %q", wantArch)
			continue
		}
		if me.Provider != "testprovider" {
			t.Errorf("entry.Provider = %q, want %q", me.Provider, "testprovider")
		}
		if me.OrigPath != origPath {
			t.Errorf("entry.OrigPath = %q, want %q", me.OrigPath, origPath)
		}
	}
}

// TestPeekArchive_EncryptedBackup verifies that PeekArchive decrypts correctly
// when a passphrase is provided.
func TestPeekArchive_EncryptedBackup(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{Passphrase: "secret"})

	manifest, unencrypted, err := core.PeekArchive(store, backupResult.ID, "secret")
	if err != nil {
		t.Fatalf("PeekArchive encrypted: %v", err)
	}
	if unencrypted {
		t.Error("PeekArchive: unencrypted should be false for encrypted backup")
	}
	if len(manifest) != len(snap) {
		t.Errorf("PeekArchive returned %d entries, want %d", len(manifest), len(snap))
	}
}

// TestRestore_FilesFilter_DryRunCount verifies that DryRun respects the Files
// filter and returns an accurate file count (not the full backup count).
func TestRestore_FilesFilter_DryRunCount(t *testing.T) {
	store := newStore(t)
	snap := threeFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{})

	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: backupResult.ID,
		DryRun:   true,
		Files:    []string{"testprovider/agents/foo.md"},
	})
	if err != nil {
		t.Fatalf("Restore dry-run with Files filter: %v", err)
	}
	if !r.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if r.Files != 1 {
		t.Errorf("dry-run Files count = %d, want 1", r.Files)
	}
}

// TestRestore_FilesFilterCombinesWithOutDir confirms that the Files filter
// applies in out-dir mode: only the requested file is included in the restore
// and the result count reflects that.
func TestRestore_FilesFilterCombinesWithOutDir(t *testing.T) {
	store := newStore(t)
	snap := threeFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	outDir := filepath.Join(t.TempDir(), "inspect")
	r, err := core.Restore(store, core.RestoreOptions{
		BackupID: backupResult.ID,
		OutDir:   outDir,
		Files:    []string{"testprovider/agents/foo.md"},
	})
	if err != nil {
		t.Fatalf("Restore --out-dir with Files filter: %v", err)
	}
	if r.Files != 1 {
		t.Errorf("RestoreResult.Files = %d, want 1", r.Files)
	}
	// The mock captures what RestoreTo received; only the one filtered file
	// should appear.
	if len(testMock.restored) != 1 {
		t.Errorf("mock.restored has %d entries, want 1: %v", len(testMock.restored), testMock.restored)
	}
	if _, ok := testMock.restored["agents/foo.md"]; !ok {
		t.Errorf("mock.restored missing 'agents/foo.md'; got: %v", keys(testMock.restored))
	}
}

// TestPeekArchive_WrongPassphraseError verifies that an encrypted backup with
// the wrong passphrase returns a decrypt error.
func TestPeekArchive_WrongPassphraseError(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()
	backupResult := runBackup(t, store, snap, core.BackupOptions{Passphrase: "correct"})

	_, _, err := core.PeekArchive(store, backupResult.ID, "wrong")
	if err == nil {
		t.Fatal("expected decrypt error with wrong passphrase; got nil")
	}
}

// TestPeekArchive_NonexistentBackupID verifies that PeekArchive surfaces the
// load error for an ID that does not exist in storage.
func TestPeekArchive_NonexistentBackupID(t *testing.T) {
	store := newStore(t)

	_, _, err := core.PeekArchive(store, "no-such-id", "")
	if err == nil {
		t.Fatal("expected load error for nonexistent backup ID; got nil")
	}
}

// TestExtractArchive_RejectsTraversalPaths: skipped — equivalent coverage
// already exists as TestExtractArchive_PathTraversalRejected in backup_test.go.

// ----------------------------------------------------------------------------
// Task 4 (stretch): PeekArchive with corrupt or missing manifest
// ----------------------------------------------------------------------------

// buildArchiveWithManifest creates a tar.gz payload that contains a single data
// file and a manifest.json whose content is supplied verbatim (may be invalid JSON).
// When manifestData is nil the manifest entry is omitted entirely.
func buildArchiveWithManifest(t *testing.T, manifestData []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	writeEntry := func(name string, data []byte) {
		t.Helper()
		hdr := &tar.Header{
			Name:    name,
			Size:    int64(len(data)),
			Mode:    0600,
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("buildArchiveWithManifest WriteHeader(%s): %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("buildArchiveWithManifest Write(%s): %v", name, err)
		}
	}

	// Always include a real data file so the archive is non-empty.
	writeEntry("testprovider/config.json", []byte(`{"key":"val"}`))

	if manifestData != nil {
		writeEntry("manifest.json", manifestData)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("buildArchiveWithManifest tw.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("buildArchiveWithManifest gw.Close: %v", err)
	}
	return buf.Bytes()
}

// TestPeekArchive_CorruptManifest verifies that PeekArchive returns a non-nil
// error when the archive contains a manifest.json with malformed JSON.
// The error must mention "manifest" so callers can surface a useful message.
func TestPeekArchive_CorruptManifest(t *testing.T) {
	store := newStore(t)

	payload := buildArchiveWithManifest(t, []byte("{invalid json"))
	meta := storage.Metadata{
		Timestamp: time.Now().UTC(),
		Providers: []string{"testprovider"},
		Encrypted: false,
	}
	backupID, err := store.Save("corrupt", meta, payload)
	if err != nil {
		t.Fatalf("store.Save: %v", err)
	}

	_, _, peekErr := core.PeekArchive(store, backupID, "")
	if peekErr == nil {
		t.Fatal("PeekArchive with corrupt manifest.json: expected non-nil error; got nil")
	}
	if !strings.Contains(peekErr.Error(), "manifest") {
		t.Errorf("PeekArchive corrupt manifest error %q does not mention 'manifest'", peekErr.Error())
	}
}

// TestPeekArchive_MissingManifest documents that an archive without a
// manifest.json is accepted by PeekArchive but returns an empty entry list.
// This is a deliberate design choice: the file data is still present; only
// the path-mapping metadata is absent.
func TestPeekArchive_MissingManifest(t *testing.T) {
	store := newStore(t)

	payload := buildArchiveWithManifest(t, nil) // nil = omit manifest.json
	meta := storage.Metadata{
		Timestamp: time.Now().UTC(),
		Providers: []string{"testprovider"},
		Encrypted: false,
	}
	backupID, err := store.Save("nomanifest", meta, payload)
	if err != nil {
		t.Fatalf("store.Save: %v", err)
	}

	entries, _, peekErr := core.PeekArchive(store, backupID, "")
	if peekErr != nil {
		t.Fatalf("PeekArchive with no manifest.json: unexpected error: %v", peekErr)
	}
	if len(entries) != 0 {
		t.Errorf("PeekArchive missing manifest: want empty entries, got %v", entries)
	}
}

// keys returns the map keys as a slice for error messages.
func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
