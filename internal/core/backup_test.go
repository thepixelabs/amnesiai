package core_test

import (
	"strings"
	"testing"

	"github.com/thepixelabs/amnesiai/internal/core"
	"github.com/thepixelabs/amnesiai/internal/crypto"
	"github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// ----------------------------------------------------------------------------
// Test provider mock
// ----------------------------------------------------------------------------

// mockProvider is a minimal in-memory Provider used only in tests.
// It stores a fixed snapshot that Backup reads and Restore writes back into.
type mockProvider struct {
	name     string
	snapshot map[string][]byte
	restored map[string][]byte // last snapshot passed to Restore
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Discover() ([]string, error) {
	paths := make([]string, 0, len(m.snapshot))
	for p := range m.snapshot {
		paths = append(paths, p)
	}
	return paths, nil
}

func (m *mockProvider) Read() (map[string][]byte, error) {
	out := make(map[string][]byte, len(m.snapshot))
	for k, v := range m.snapshot {
		out[k] = v
	}
	return out, nil
}

func (m *mockProvider) Diff(snap map[string][]byte) ([]provider.DiffEntry, error) {
	// Build the union of paths from the current snapshot and the supplied snap.
	seen := make(map[string]bool)
	for k := range m.snapshot {
		seen[k] = true
	}
	for k := range snap {
		seen[k] = true
	}

	entries := make([]provider.DiffEntry, 0, len(seen))
	for rel := range seen {
		cur, inCurrent := m.snapshot[rel]
		old, inSnap := snap[rel]

		var status string
		switch {
		case inCurrent && !inSnap:
			status = "added"
		case !inCurrent && inSnap:
			status = "deleted"
		case string(cur) == string(old):
			status = "unchanged"
		default:
			status = "modified"
		}

		entries = append(entries, provider.DiffEntry{
			Path:   rel,
			Status: status,
			Before: old,
			After:  cur,
		})
	}
	return entries, nil
}

func (m *mockProvider) Restore(snap map[string][]byte) error {
	m.restored = snap
	return nil
}

// ----------------------------------------------------------------------------
// TestMain: register the shared mock provider exactly once for the whole
// test binary. The provider registry panics on duplicate registration, so
// we cannot call Register inside individual test functions.
// ----------------------------------------------------------------------------

// testMock is the single shared mock instance. Individual tests adjust its
// snapshot field before calling Backup.
var testMock = &mockProvider{name: "testprovider"}

func TestMain(m *testing.M) {
	provider.Register(testMock)
	m.Run()
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// newStore creates a localStorage backed by an isolated temp directory.
func newStore(t *testing.T) storage.Storage {
	t.Helper()
	s, err := storage.New("local", t.TempDir())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return s
}

// twoFileSnapshot returns a snapshot map with two distinct files.
func twoFileSnapshot() map[string][]byte {
	return map[string][]byte{
		"config/settings.json":    []byte(`{"theme":"dark"}`),
		"config/keybindings.json": []byte(`{"copy":"ctrl+c"}`),
	}
}

// runBackup runs a Backup with the testMock snapshot set to snap.
func runBackup(t *testing.T, store storage.Storage, snap map[string][]byte, opts core.BackupOptions) *core.BackupResult {
	t.Helper()
	testMock.snapshot = snap
	opts.Providers = []string{"testprovider"}
	result, err := core.Backup(store, opts)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	return result
}

// loadAndExtract loads a backup from storage and extracts its archive.
// It does NOT decrypt; use loadDecryptExtract for encrypted backups.
func loadAndExtract(t *testing.T, store storage.Storage, id string) (map[string][]byte, []core.ManifestEntry) {
	t.Helper()
	_, payload, err := store.Load(id)
	if err != nil {
		t.Fatalf("store.Load(%q): %v", id, err)
	}
	files, manifest, err := core.ExtractArchive(payload)
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	return files, manifest
}

// loadDecryptExtract loads, decrypts, and extracts a backup from storage.
func loadDecryptExtract(t *testing.T, store storage.Storage, id, passphrase string) (map[string][]byte, []core.ManifestEntry) {
	t.Helper()
	_, payload, err := store.Load(id)
	if err != nil {
		t.Fatalf("store.Load(%q): %v", id, err)
	}
	payload, err = crypto.Decrypt(passphrase, payload)
	if err != nil {
		t.Fatalf("crypto.Decrypt: %v", err)
	}
	files, manifest, err := core.ExtractArchive(payload)
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	return files, manifest
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// TestBackupExtractRoundTrip_NoEncryption verifies that Backup archives the
// provider's files and that ExtractArchive recovers both their content and
// the manifest mapping original paths to archive paths.
func TestBackupExtractRoundTrip_NoEncryption(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{})

	if result.ID == "" {
		t.Fatal("Backup returned an empty ID")
	}

	files, manifest := loadAndExtract(t, store, result.ID)

	// Both files must appear in the archive under testprovider/<path>.
	for origPath, wantContent := range snap {
		archPath := "testprovider/" + origPath
		gotContent, ok := files[archPath]
		if !ok {
			t.Errorf("archive missing entry %q", archPath)
			continue
		}
		if string(gotContent) != string(wantContent) {
			t.Errorf("content mismatch for %q: got %q, want %q", archPath, gotContent, wantContent)
		}
	}

	// Manifest must map every archive path back to its original path.
	if len(manifest) != len(snap) {
		t.Errorf("manifest has %d entries, want %d", len(manifest), len(snap))
	}
	for _, entry := range manifest {
		if entry.Provider != "testprovider" {
			t.Errorf("manifest entry Provider = %q, want %q", entry.Provider, "testprovider")
		}
		wantArchPath := "testprovider/" + entry.OrigPath
		if entry.ArchPath != wantArchPath {
			t.Errorf("manifest ArchPath = %q, want %q", entry.ArchPath, wantArchPath)
		}
	}
}

// TestBackupExtractRoundTrip_WithEncryption verifies that the backup encrypted
// with a passphrase can be decrypted and then extracted to recover original content.
func TestBackupExtractRoundTrip_WithEncryption(t *testing.T) {
	const passphrase = "test-passphrase"
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{Passphrase: passphrase})

	if result.ID == "" {
		t.Fatal("Backup returned an empty ID")
	}

	files, _ := loadDecryptExtract(t, store, result.ID, passphrase)

	for origPath, wantContent := range snap {
		archPath := "testprovider/" + origPath
		gotContent, ok := files[archPath]
		if !ok {
			t.Errorf("archive missing entry %q after decrypt+extract", archPath)
			continue
		}
		if string(gotContent) != string(wantContent) {
			t.Errorf("content mismatch for %q: got %q, want %q", archPath, gotContent, wantContent)
		}
	}
}

// TestBackupEncryption_WrongPassphraseReturnsError verifies that loading a
// passphrase-encrypted backup and decrypting with the wrong key fails loudly
// rather than returning corrupt data.
func TestBackupEncryption_WrongPassphraseReturnsError(t *testing.T) {
	store := newStore(t)

	result := runBackup(t, store, twoFileSnapshot(), core.BackupOptions{Passphrase: "correct"})

	_, payload, err := store.Load(result.ID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}

	_, err = crypto.Decrypt("wrong", payload)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong passphrase, got nil")
	}
}

// TestBackup_ScanRedactsSecrets verifies that a file containing a real secret
// pattern (AWS access key) is stored redacted in the archive, not verbatim.
func TestBackup_ScanRedactsSecrets(t *testing.T) {
	store := newStore(t)

	// AKIA1234567890ABCDEF is the pattern used in scan_test.go for the
	// aws-access-token gitleaks rule. It must not appear verbatim in the archive.
	snap := map[string][]byte{
		"config/env": []byte("ACCESS_KEY_ID=AKIA1234567890ABCDEF\nregion=us-east-1\n"),
	}

	result := runBackup(t, store, snap, core.BackupOptions{})

	files, _ := loadAndExtract(t, store, result.ID)

	archPath := "testprovider/config/env"
	content, ok := files[archPath]
	if !ok {
		t.Fatalf("archive missing entry %q", archPath)
	}

	if strings.Contains(string(content), "AKIA1234567890ABCDEF") {
		t.Errorf("archive still contains the raw AWS key; expected it to be redacted: %q", content)
	}

	if !strings.Contains(string(content), "<REDACTED:") {
		t.Errorf("expected <REDACTED:...> placeholder in archived content; got: %q", content)
	}

	// The non-secret part of the file must be preserved.
	if !strings.Contains(string(content), "region=us-east-1") {
		t.Errorf("non-secret content was lost during redaction; got: %q", content)
	}
}

// TestBackup_FindingsReportedForSecrets verifies that BackupResult.Findings
// lists the provider and secret type when a secret is detected.
func TestBackup_FindingsReportedForSecrets(t *testing.T) {
	store := newStore(t)
	testMock.snapshot = map[string][]byte{
		"config/env": []byte("ACCESS_KEY_ID=AKIA1234567890ABCDEF"),
	}

	result, err := core.Backup(store, core.BackupOptions{
		Providers: []string{"testprovider"},
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	findings, ok := result.Findings["testprovider"]
	if !ok || len(findings) == 0 {
		t.Fatalf("expected findings for testprovider, got: %v", result.Findings)
	}

	found := false
	for _, f := range findings {
		if f.Type == "aws-access-token" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding with Type %q; got: %+v", "aws-access-token", findings)
	}
}

// TestRestore_RoundTrip verifies that Restore writes the provider's files
// back through the provider using content identical to what was backed up.
func TestRestore_RoundTrip(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{})

	// Reset the mock's in-memory state so Restore has something fresh to write to.
	testMock.snapshot = nil
	testMock.restored = nil

	restoreResult, err := core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if restoreResult.Files != len(snap) {
		t.Errorf("Restore.Files = %d, want %d", restoreResult.Files, len(snap))
	}

	if restoreResult.DryRun {
		t.Error("Restore.DryRun should be false for a live restore")
	}

	if testMock.restored == nil {
		t.Fatal("provider.Restore was never called")
	}

	for origPath, wantContent := range snap {
		gotContent, ok := testMock.restored[origPath]
		if !ok {
			t.Errorf("restored snapshot missing path %q", origPath)
			continue
		}
		if string(gotContent) != string(wantContent) {
			t.Errorf("restored content mismatch for %q: got %q, want %q", origPath, gotContent, wantContent)
		}
	}
}

// TestRestore_WithEncryption verifies that an encrypted backup is correctly
// decrypted before being written back through the provider.
func TestRestore_WithEncryption(t *testing.T) {
	const passphrase = "restore-passphrase"
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{Passphrase: passphrase})

	testMock.snapshot = nil
	testMock.restored = nil

	_, err := core.Restore(store, core.RestoreOptions{
		BackupID:   result.ID,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("Restore (encrypted): %v", err)
	}

	for origPath, wantContent := range snap {
		gotContent, ok := testMock.restored[origPath]
		if !ok {
			t.Errorf("restored snapshot missing path %q", origPath)
			continue
		}
		if string(gotContent) != string(wantContent) {
			t.Errorf("restored content mismatch for %q: got %q, want %q", origPath, gotContent, wantContent)
		}
	}
}

// TestRestore_WrongPassphraseReturnsError verifies that Restore propagates a
// decryption failure rather than silently restoring garbage.
func TestRestore_WrongPassphraseReturnsError(t *testing.T) {
	store := newStore(t)

	result := runBackup(t, store, twoFileSnapshot(), core.BackupOptions{Passphrase: "correct"})

	_, err := core.Restore(store, core.RestoreOptions{
		BackupID:   result.ID,
		Passphrase: "wrong",
	})
	if err == nil {
		t.Fatal("expected error when restoring with wrong passphrase, got nil")
	}
}

// TestRestore_DryRunDoesNotCallProviderRestore verifies that a dry-run restore
// counts files but never calls provider.Restore.
func TestRestore_DryRunDoesNotCallProviderRestore(t *testing.T) {
	store := newStore(t)
	snap := twoFileSnapshot()

	result := runBackup(t, store, snap, core.BackupOptions{})

	testMock.restored = nil

	restoreResult, err := core.Restore(store, core.RestoreOptions{
		BackupID: result.ID,
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Restore (dry-run): %v", err)
	}

	if !restoreResult.DryRun {
		t.Error("RestoreResult.DryRun should be true")
	}

	if restoreResult.Files != len(snap) {
		t.Errorf("dry-run file count = %d, want %d", restoreResult.Files, len(snap))
	}

	if testMock.restored != nil {
		t.Error("provider.Restore was called during a dry-run; it must not be")
	}
}

// TestRestore_LatestBackupUsedWhenIDOmitted verifies that omitting BackupID
// causes Restore to select the most recent backup automatically.
func TestRestore_LatestBackupUsedWhenIDOmitted(t *testing.T) {
	store := newStore(t)
	snap := map[string][]byte{
		"config/settings.json": []byte(`{"version":2}`),
	}

	result := runBackup(t, store, snap, core.BackupOptions{})

	testMock.snapshot = nil
	testMock.restored = nil

	restoreResult, err := core.Restore(store, core.RestoreOptions{
		// BackupID intentionally omitted — should resolve to latest.
	})
	if err != nil {
		t.Fatalf("Restore without BackupID: %v", err)
	}

	if restoreResult.BackupID != result.ID {
		t.Errorf("resolved backup ID = %q, want %q", restoreResult.BackupID, result.ID)
	}
}

// TestBackup_MetadataProvidersMatchActualProviders verifies that the stored
// metadata.Providers list reflects the providers that actually ran, not the
// raw opts.Providers input.
func TestBackup_MetadataProvidersMatchActualProviders(t *testing.T) {
	store := newStore(t)

	result := runBackup(t, store, twoFileSnapshot(), core.BackupOptions{})

	entries, err := store.List()
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no backups found in storage after Backup")
	}

	meta, _, err := store.Load(result.ID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}

	if len(meta.Providers) == 0 {
		t.Error("stored metadata.Providers is empty; expected at least one provider name")
	}

	found := false
	for _, p := range meta.Providers {
		if p == "testprovider" {
			found = true
		}
	}
	if !found {
		t.Errorf("stored metadata.Providers = %v; expected to contain %q", meta.Providers, "testprovider")
	}
}

// TestBackup_EmptyProviderSnapshotProducesValidArchive verifies that a provider
// returning zero files still produces a valid (though empty) archive rather than
// an error.
func TestBackup_EmptyProviderSnapshotProducesValidArchive(t *testing.T) {
	store := newStore(t)

	result := runBackup(t, store, map[string][]byte{}, core.BackupOptions{})

	if result.ID == "" {
		t.Fatal("Backup with empty snapshot returned an empty ID")
	}

	files, manifest := loadAndExtract(t, store, result.ID)

	if len(files) != 0 {
		t.Errorf("expected empty archive for empty snapshot, got %d entries: %v", len(files), files)
	}
	if len(manifest) != 0 {
		t.Errorf("expected empty manifest for empty snapshot, got %d entries", len(manifest))
	}
}
