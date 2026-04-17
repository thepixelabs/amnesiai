package core_test

import (
	"testing"

	"github.com/thepixelabs/amnesiai/internal/core"
)

// TestDiff_NoChanges_AllUnchanged verifies that when the current provider
// snapshot matches the stored backup exactly, every diff entry is "unchanged".
func TestDiff_NoChanges_AllUnchanged(t *testing.T) {
	store := newStore(t)
	snap := map[string][]byte{
		"config/settings.json": []byte(`{"theme":"dark"}`),
	}
	testMock.snapshot = snap
	result := runBackup(t, store, snap, core.BackupOptions{})

	diffResult, err := core.Diff(store, core.DiffOptions{
		BackupID:  result.ID,
		Providers: []string{"testprovider"},
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	entries := diffResult.Entries["testprovider"]
	if len(entries) == 0 {
		t.Fatal("Diff returned no entries; expected at least one")
	}
	for _, e := range entries {
		if e.Status != "unchanged" {
			t.Errorf("entry %q: got status %q, want %q", e.Path, e.Status, "unchanged")
		}
	}
}

// TestDiff_ModifiedFile_ShowsModified verifies that a file whose content
// changes between the backup and the current state is reported as "modified".
func TestDiff_ModifiedFile_ShowsModified(t *testing.T) {
	store := newStore(t)
	original := map[string][]byte{
		"config/settings.json": []byte(`{"theme":"dark"}`),
	}
	result := runBackup(t, store, original, core.BackupOptions{})

	// Simulate a modification: same key, different content.
	testMock.snapshot = map[string][]byte{
		"config/settings.json": []byte(`{"theme":"light"}`),
	}

	diffResult, err := core.Diff(store, core.DiffOptions{
		BackupID:  result.ID,
		Providers: []string{"testprovider"},
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	entries := diffResult.Entries["testprovider"]
	found := false
	for _, e := range entries {
		if e.Path == "config/settings.json" {
			found = true
			if e.Status != "modified" {
				t.Errorf("entry %q: got status %q, want %q", e.Path, e.Status, "modified")
			}
		}
	}
	if !found {
		t.Error("Diff did not return an entry for config/settings.json")
	}
}

// TestDiff_AddedFile_ShowsAdded verifies that a file present on disk but
// absent from the stored backup is reported as "added".
func TestDiff_AddedFile_ShowsAdded(t *testing.T) {
	store := newStore(t)
	original := map[string][]byte{
		"config/settings.json": []byte(`{"theme":"dark"}`),
	}
	result := runBackup(t, store, original, core.BackupOptions{})

	// Add a second file to the current snapshot.
	testMock.snapshot = map[string][]byte{
		"config/settings.json":    []byte(`{"theme":"dark"}`),
		"config/keybindings.json": []byte(`{"copy":"ctrl+c"}`),
	}

	diffResult, err := core.Diff(store, core.DiffOptions{
		BackupID:  result.ID,
		Providers: []string{"testprovider"},
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	entries := diffResult.Entries["testprovider"]
	found := false
	for _, e := range entries {
		if e.Path == "config/keybindings.json" {
			found = true
			if e.Status != "added" {
				t.Errorf("new file %q: got status %q, want %q", e.Path, e.Status, "added")
			}
		}
	}
	if !found {
		t.Error("Diff did not return an entry for the newly added config/keybindings.json")
	}
}

// TestDiff_DeletedFile_ShowsDeleted verifies that a file present in the stored
// backup but absent from the current state is reported as "deleted".
func TestDiff_DeletedFile_ShowsDeleted(t *testing.T) {
	store := newStore(t)
	original := map[string][]byte{
		"config/settings.json":    []byte(`{"theme":"dark"}`),
		"config/keybindings.json": []byte(`{"copy":"ctrl+c"}`),
	}
	result := runBackup(t, store, original, core.BackupOptions{})

	// Remove one file from the current state.
	testMock.snapshot = map[string][]byte{
		"config/settings.json": []byte(`{"theme":"dark"}`),
	}

	diffResult, err := core.Diff(store, core.DiffOptions{
		BackupID:  result.ID,
		Providers: []string{"testprovider"},
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	entries := diffResult.Entries["testprovider"]
	found := false
	for _, e := range entries {
		if e.Path == "config/keybindings.json" {
			found = true
			if e.Status != "deleted" {
				t.Errorf("deleted file %q: got status %q, want %q", e.Path, e.Status, "deleted")
			}
		}
	}
	if !found {
		t.Error("Diff did not return an entry for the deleted config/keybindings.json")
	}
}

// TestDiff_WrongPassphrase_ReturnsError verifies that Diff fails loudly when
// the decryption passphrase does not match the backup's encryption passphrase.
func TestDiff_WrongPassphrase_ReturnsError(t *testing.T) {
	store := newStore(t)
	snap := map[string][]byte{
		"config/settings.json": []byte(`{"theme":"dark"}`),
	}
	result := runBackup(t, store, snap, core.BackupOptions{Passphrase: "correct"})

	testMock.snapshot = snap

	_, err := core.Diff(store, core.DiffOptions{
		BackupID:   result.ID,
		Providers:  []string{"testprovider"},
		Passphrase: "wrong",
	})
	if err == nil {
		t.Fatal("Diff with wrong passphrase: expected error, got nil")
	}
}

// TestDiff_NoIDUsesLatest verifies that when BackupID is empty, Diff selects
// the most recent backup automatically and reports its ID in the result.
func TestDiff_NoIDUsesLatest(t *testing.T) {
	store := newStore(t)
	snap := map[string][]byte{
		"config/settings.json": []byte(`{"version":1}`),
	}

	// Save a first backup.
	runBackup(t, store, snap, core.BackupOptions{})

	// Save a second (newer) backup with the same snapshot.
	// We sleep 0 ms — but the ID is based on the wall clock second, so we
	// override via the metadata rather than sleeping.  The simplest approach is
	// to call runBackup twice and assert the result ID matches the latest entry.
	secondResult := runBackup(t, store, snap, core.BackupOptions{})

	testMock.snapshot = snap

	diffResult, err := core.Diff(store, core.DiffOptions{
		Providers: []string{"testprovider"},
		// BackupID deliberately omitted to trigger latest-backup resolution.
	})
	if err != nil {
		t.Fatalf("Diff without BackupID: %v", err)
	}

	// The resolved backup ID must be the one returned by the second backup.
	if diffResult.BackupID != secondResult.ID {
		t.Errorf("Diff resolved BackupID = %q, want latest %q", diffResult.BackupID, secondResult.ID)
	}
}

// TestDiff_EmptyStore_ReturnsError verifies that Diff on a fresh empty store
// returns a non-nil error rather than panicking or returning an empty result.
func TestDiff_EmptyStore_ReturnsError(t *testing.T) {
	store := newStore(t)

	_, err := core.Diff(store, core.DiffOptions{
		Providers: []string{"testprovider"},
		// BackupID is empty; store has no backups.
	})
	if err == nil {
		t.Fatal("Diff on empty store: expected error, got nil")
	}
}
