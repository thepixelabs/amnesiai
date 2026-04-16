package storage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/thepixelabs/amensiai/internal/storage"
)

// newLocalStorage creates a localStorage instance backed by a temp directory.
func newLocalStorage(t *testing.T) storage.Storage {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.New("local", dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return s
}

// TestLocalStorage_SaveAndLoadRoundTrip verifies that a saved payload can be
// loaded back intact along with its metadata.
func TestLocalStorage_SaveAndLoadRoundTrip(t *testing.T) {
	s := newLocalStorage(t)

	meta := storage.Metadata{
		ID:        "test-backup-001",
		Timestamp: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		Providers: []string{"claude", "gemini"},
	}
	payload := []byte("compressed-and-encrypted-backup-content")

	id, err := s.Save("backup", meta, payload)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id != meta.ID {
		t.Errorf("Save returned ID %q, want %q", id, meta.ID)
	}

	gotMeta, gotPayload, err := s.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if gotMeta.ID != meta.ID {
		t.Errorf("metadata ID: got %q, want %q", gotMeta.ID, meta.ID)
	}
	if !gotMeta.Timestamp.Equal(meta.Timestamp) {
		t.Errorf("metadata Timestamp: got %v, want %v", gotMeta.Timestamp, meta.Timestamp)
	}
	if len(gotMeta.Providers) != len(meta.Providers) {
		t.Errorf("metadata Providers: got %v, want %v", gotMeta.Providers, meta.Providers)
	}
	if string(gotPayload) != string(payload) {
		t.Errorf("payload mismatch: got %q, want %q", gotPayload, payload)
	}
}

// TestLocalStorage_ListReturnsNewestFirst verifies that List returns backups
// sorted with the most recent timestamp first.
func TestLocalStorage_ListReturnsNewestFirst(t *testing.T) {
	s := newLocalStorage(t)

	// Save three backups with known timestamps in non-chronological order.
	backups := []storage.Metadata{
		{
			ID:        "backup-middle",
			Timestamp: time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC),
			Providers: []string{"claude"},
		},
		{
			ID:        "backup-oldest",
			Timestamp: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			Providers: []string{"claude"},
		},
		{
			ID:        "backup-newest",
			Timestamp: time.Date(2024, 12, 1, 10, 0, 0, 0, time.UTC),
			Providers: []string{"claude"},
		},
	}

	for _, meta := range backups {
		if _, err := s.Save("backup", meta, []byte("data")); err != nil {
			t.Fatalf("Save %s: %v", meta.ID, err)
		}
	}

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("List returned %d entries, want 3", len(entries))
	}

	wantOrder := []string{"backup-newest", "backup-middle", "backup-oldest"}
	for i, entry := range entries {
		if entry.ID != wantOrder[i] {
			t.Errorf("entries[%d].ID = %q, want %q", i, entry.ID, wantOrder[i])
		}
	}
}

// TestLocalStorage_LatestOnEmptyStoreReturnsErrNoBackups verifies that Latest
// returns ErrNoBackups when no backups have been saved yet.
func TestLocalStorage_LatestOnEmptyStoreReturnsErrNoBackups(t *testing.T) {
	s := newLocalStorage(t)

	_, err := s.Latest()
	if !errors.Is(err, storage.ErrNoBackups) {
		t.Errorf("Latest on empty store: got error %v, want ErrNoBackups", err)
	}
}

// TestLocalStorage_LatestReturnsNewestBackupID verifies that Latest returns
// the ID of the most recently timestamped backup.
func TestLocalStorage_LatestReturnsNewestBackupID(t *testing.T) {
	s := newLocalStorage(t)

	older := storage.Metadata{
		ID:        "old-backup",
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Providers: []string{"claude"},
	}
	newer := storage.Metadata{
		ID:        "new-backup",
		Timestamp: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Providers: []string{"claude"},
	}

	if _, err := s.Save("backup", older, []byte("old")); err != nil {
		t.Fatalf("Save older: %v", err)
	}
	if _, err := s.Save("backup", newer, []byte("new")); err != nil {
		t.Fatalf("Save newer: %v", err)
	}

	id, err := s.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if id != "new-backup" {
		t.Errorf("Latest: got %q, want %q", id, "new-backup")
	}
}
