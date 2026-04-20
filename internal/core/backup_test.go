package core_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
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

// ----------------------------------------------------------------------------
// Security model tests (Track B)
// ----------------------------------------------------------------------------

// TestBackupWithEncryption_SecretsAreLossless verifies that when encryption is on
// and gitleaks finds a secret, the raw bytes are stored verbatim in the archive
// (i.e. no <REDACTED:...> substitution). The archive is encrypted, so the secret
// is protected by the passphrase.
func TestBackupWithEncryption_SecretsAreLossless(t *testing.T) {
	const passphrase = "lossless-passphrase"
	const key = "AKIA1234567890ABCDEF"
	store := newStore(t)

	snap := map[string][]byte{
		"config/env": []byte("ACCESS_KEY_ID=" + key + "\nregion=us-east-1\n"),
	}
	testMock.snapshot = snap
	result, err := core.Backup(store, core.BackupOptions{
		Providers:  []string{"testprovider"},
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("Backup (encrypted): %v", err)
	}

	findings, ok := result.Findings["testprovider"]
	if !ok || len(findings) == 0 {
		t.Fatalf("expected findings for encrypted backup, got: %v", result.Findings)
	}

	files, _ := loadDecryptExtract(t, store, result.ID, passphrase)
	content, ok := files["testprovider/config/env"]
	if !ok {
		t.Fatal("archive missing testprovider/config/env after decrypt+extract")
	}
	if !strings.Contains(string(content), key) {
		t.Errorf("encrypted archive does NOT contain the raw key — lossless invariant broken; got: %q", content)
	}
	if strings.Contains(string(content), "<REDACTED:") {
		t.Errorf("encrypted archive contains <REDACTED:> placeholder — should be lossless; got: %q", content)
	}
}

// TestBackupNoEncryptForceNoEncrypt_RedactionPresent verifies that when
// --no-encrypt + --force-no-encrypt are both set, the backup succeeds with
// <REDACTED:...> placeholders in the archive and findings are reported.
func TestBackupNoEncryptForceNoEncrypt_RedactionPresent(t *testing.T) {
	const key = "AKIA1234567890ABCDEF"
	store := newStore(t)

	snap := map[string][]byte{
		"config/env": []byte("ACCESS_KEY_ID=" + key + "\nregion=us-east-1\n"),
	}
	testMock.snapshot = snap
	result, err := core.Backup(store, core.BackupOptions{
		Providers:      []string{"testprovider"},
		Passphrase:     "",
		NoEncrypt:      true,
		ForceNoEncrypt: true,
	})
	if err != nil {
		t.Fatalf("Backup (force-no-encrypt): %v", err)
	}

	files, _ := loadAndExtract(t, store, result.ID)
	content, ok := files["testprovider/config/env"]
	if !ok {
		t.Fatal("archive missing testprovider/config/env")
	}
	if strings.Contains(string(content), key) {
		t.Errorf("unencrypted archive still contains raw key: %q", content)
	}
	if !strings.Contains(string(content), "<REDACTED:") {
		t.Errorf("unencrypted archive missing <REDACTED:> placeholder: %q", content)
	}
}

// TestBackupNoEncrypt_SecretsFound_ReturnsError verifies that when --no-encrypt
// is set and secrets are found, Backup returns a non-nil error and no backup is
// written to storage (unless --force-no-encrypt is also set).
func TestBackupNoEncrypt_SecretsFound_ReturnsError(t *testing.T) {
	store := newStore(t)

	snap := map[string][]byte{
		"config/env": []byte("ACCESS_KEY_ID=AKIA1234567890ABCDEF"),
	}
	testMock.snapshot = snap
	_, err := core.Backup(store, core.BackupOptions{
		Providers:      []string{"testprovider"},
		Passphrase:     "",
		NoEncrypt:      true,
		ForceNoEncrypt: false,
	})
	if err == nil {
		t.Fatal("expected error when --no-encrypt and secrets found without --force-no-encrypt, got nil")
	}

	entries, listErr := store.List()
	if listErr != nil {
		t.Fatalf("store.List: %v", listErr)
	}
	if len(entries) != 0 {
		t.Errorf("expected no backups in storage after guard error; got %d entry/entries", len(entries))
	}
}

// TestBackupStoredFindings_PopulatedInMetadata verifies that after a backup,
// storage.Metadata.Findings contains a FindingSummary entry with the correct
// RuleID, File, and a non-empty SecretHash for every detected secret.
func TestBackupStoredFindings_PopulatedInMetadata(t *testing.T) {
	const key = "AKIA1234567890ABCDEF"
	store := newStore(t)

	snap := map[string][]byte{
		"config/env": []byte("ACCESS_KEY_ID=" + key),
	}
	testMock.snapshot = snap
	result, err := core.Backup(store, core.BackupOptions{
		Providers:  []string{"testprovider"},
		Passphrase: "meta-passphrase",
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	meta, _, err := store.Load(result.ID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}

	summaries, ok := meta.Findings["testprovider"]
	if !ok || len(summaries) == 0 {
		t.Fatalf("expected Findings in metadata for testprovider, got: %v", meta.Findings)
	}

	s := summaries[0]
	if s.RuleID != "aws-access-token" {
		t.Errorf("FindingSummary.RuleID = %q, want %q", s.RuleID, "aws-access-token")
	}
	if s.File != "config/env" {
		t.Errorf("FindingSummary.File = %q, want %q", s.File, "config/env")
	}
	if s.SecretHash == "" {
		t.Error("FindingSummary.SecretHash is empty; expected hex SHA-256 of the raw secret")
	}
	if len(s.SecretHash) != 64 {
		t.Errorf("SecretHash length = %d, want 64 (hex SHA-256)", len(s.SecretHash))
	}
}

// ─── Path-traversal tests (Track F) ───────────────────────────────────────────

// craftMaliciousTar builds a raw tar.gz archive containing an entry whose name
// is designed to escape the destination root (../../etc/passwd style).
func craftMaliciousTar(t *testing.T, entryName string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("pwned")
	hdr := &tar.Header{
		Name: entryName,
		Size: int64(len(content)),
		Mode: 0644,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("craftMaliciousTar WriteHeader: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("craftMaliciousTar Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("craftMaliciousTar tw.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("craftMaliciousTar gw.Close: %v", err)
	}
	return buf.Bytes()
}

// craftRawTar builds a tar.gz whose single entry header is written with raw
// bytes, bypassing Go's archive/tar.Writer validation.  This lets us craft
// entries with names that Go's writer rejects (null bytes, empty strings) but
// that a C tar implementation — or an attacker — could produce on disk.
//
// The POSIX ustar header layout (512 bytes):
//
//	[0:100]   name
//	[100:108] mode (octal ASCII, NUL-terminated)
//	[108:116] uid
//	[116:124] gid
//	[124:136] size
//	[136:148] mtime
//	[148:156] checksum
//	[156]     type flag ('0' = regular file)
//	[157:257] linkname
//	[257:263] magic "ustar\0"
//	[263:265] version "00"
//	... (rest zeroed)
func craftRawTar(t *testing.T, entryName string) []byte {
	t.Helper()

	content := []byte("pwned")

	// Build a 512-byte ustar header block.
	var hdr [512]byte
	// name (100 bytes) — copy raw bytes, including null bytes if present.
	copy(hdr[0:100], []byte(entryName))
	// mode
	copy(hdr[100:108], []byte("0000644\x00"))
	// uid, gid
	copy(hdr[108:116], []byte("0000000\x00"))
	copy(hdr[116:124], []byte("0000000\x00"))
	// size (octal, 11 digits + space)
	sizeFmt := []byte("00000000005 ") // len("pwned") == 5
	copy(hdr[124:136], sizeFmt)
	// mtime
	copy(hdr[136:148], []byte("00000000000 "))
	// type flag: '0' = regular file
	hdr[156] = '0'
	// magic + version
	copy(hdr[257:263], []byte("ustar\x00"))
	copy(hdr[263:265], []byte("00"))

	// Compute checksum: sum of all header bytes treating checksum field as spaces.
	for i := 148; i < 156; i++ {
		hdr[i] = ' '
	}
	var csum uint32
	for _, b := range hdr {
		csum += uint32(b)
	}
	_ = binary.BigEndian // satisfy import
	csumStr := []byte{'0', '0', '0', '0', '0', '0', ' ', '\x00'}
	// write 6-digit octal + space + NUL
	val := csum
	for i := 5; i >= 0; i-- {
		csumStr[i] = byte('0' + val%8)
		val /= 8
	}
	copy(hdr[148:156], csumStr)

	// Data block: pad content to 512-byte boundary.
	var dataBuf [512]byte
	copy(dataBuf[:], content)

	// Two 512-byte zero blocks = end-of-archive.
	var eoa [1024]byte

	// Combine into a raw tar stream and gzip it.
	var raw bytes.Buffer
	raw.Write(hdr[:])
	raw.Write(dataBuf[:])
	raw.Write(eoa[:])

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(raw.Bytes())
	if err := gw.Close(); err != nil {
		t.Fatalf("craftRawTar gzip.Close: %v", err)
	}
	return gz.Bytes()
}

// TestExtractArchive_PathTraversalRejected verifies that ExtractArchive refuses
// tar entries whose names would resolve outside the destination root.
func TestExtractArchive_PathTraversalRejected(t *testing.T) {
	cases := []struct {
		name      string
		entryName string
		rawTar    bool // use craftRawTar instead of craftMaliciousTar
	}{
		{"DotDotSlash", "../../etc/passwd", false},
		{"DotDotPrefix", "../secret", false},
		{"AbsolutePath", "/etc/passwd", false},
		{"DeepDotDot", "a/b/../../../../etc/shadow", false},
		{"WindowsBackslash", "..\\..\\etc\\passwd", false},
		{"LiteralDotDot", "..", false},
		// EmptyName must be crafted with raw bytes because Go's archive/tar.Writer
		// rejects an empty name during WriteHeader.
		{"EmptyName", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload []byte
			if tc.rawTar {
				payload = craftRawTar(t, tc.entryName)
			} else {
				payload = craftMaliciousTar(t, tc.entryName)
			}
			_, _, err := core.ExtractArchive(payload)
			if err == nil {
				t.Errorf("ExtractArchive(%q): expected path-traversal error, got nil", tc.entryName)
				return
			}
			if !strings.Contains(err.Error(), "illegal") {
				t.Errorf("ExtractArchive(%q): error %q does not mention 'illegal'", tc.entryName, err.Error())
			}
		})
	}
}

// TestExtractArchive_NullByteNameSanitizedByReader verifies that a tar entry
// whose raw header contains a null byte in the name field is sanitized by
// Go's archive/tar reader (null-terminates C strings), so the extracted name
// is the safe prefix before the null — not the traversal suffix after it.
// This documents that Go's tar reader is a first line of defense against this
// attack vector.  The strings.ContainsRune guard in ExtractArchive is
// defense-in-depth for future reader changes or PAX extensions.
func TestExtractArchive_NullByteNameSanitizedByReader(t *testing.T) {
	// "foo\x00../../etc/passwd" — Go's tar reader truncates at the null byte
	// and returns "foo", which is a safe path.
	payload := craftRawTar(t, "foo\x00../../etc/passwd")
	files, _, err := core.ExtractArchive(payload)
	if err != nil {
		// If the reader or our guard rejects it, that is also acceptable.
		return
	}
	// If accepted, the path must be the safe prefix, not the traversal component.
	for name := range files {
		if strings.Contains(name, "..") || strings.Contains(name, "etc") {
			t.Errorf("null-byte traversal component leaked into extracted path: %q", name)
		}
	}
}

// TestExtractArchive_PathTraversalRejected_Extra ensures the craftRawTar helper
// compiles and is reachable, so the build does not fail with an unused symbol.
func TestExtractArchive_PathTraversalRejected_Extra(t *testing.T) {
	// craftRawTar is used by TestExtractArchive_NullByteNameSanitizedByReader;
	// this test is a compile-time smoke test only.
	_ = craftRawTar
}

// TestExtractArchive_LegitimatePathAccepted verifies that normal relative paths
// are not rejected by the path-traversal check.
func TestExtractArchive_LegitimatePathAccepted(t *testing.T) {
	payload := craftMaliciousTar(t, "claude/config/settings.json")
	files, _, err := core.ExtractArchive(payload)
	if err != nil {
		t.Fatalf("ExtractArchive(legit path): unexpected error: %v", err)
	}
	if _, ok := files["claude/config/settings.json"]; !ok {
		t.Errorf("expected entry 'claude/config/settings.json' in extracted files, got: %v", files)
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
