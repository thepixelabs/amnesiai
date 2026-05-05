package core_test

import (
	"sort"
	"testing"
	"time"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/core"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// fixedNow is the reference "now" used by the table tests; using a literal
// value keeps the assertions independent of wall-clock drift.
var fixedNow = time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

// makeEntries returns BackupEntries with the given ages-in-days, newest-first.
// IDs follow the format "id-<n>" where n is the index, so assertions can
// reference them without computing timestamps.
func makeEntries(daysAgo []int) []storage.BackupEntry {
	entries := make([]storage.BackupEntry, len(daysAgo))
	for i, d := range daysAgo {
		entries[i] = storage.BackupEntry{
			ID:        idForIndex(i),
			Timestamp: fixedNow.Add(-time.Duration(d) * 24 * time.Hour),
		}
	}
	// Caller is expected to pass newest-first; sort defensively so a swapped
	// test case still behaves like real storage.List output.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
	return entries
}

func idForIndex(i int) string {
	// Two-digit zero-padded so string sort matches numeric sort up to id-99.
	return "id-" + twoDigit(i)
}

func twoDigit(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	tens := i / 10
	ones := i % 10
	return string(rune('0'+tens)) + string(rune('0'+ones))
}

func TestSelectForDeletion(t *testing.T) {
	tests := []struct {
		name     string
		entries  []storage.BackupEntry
		policy   config.Retention
		wantDel  []string // expected IDs returned by SelectForDeletion
		wantNone bool     // true when we expect nil (no deletions)
	}{
		{
			name:     "EmptyInput",
			entries:  nil,
			policy:   config.Retention{KeepLast: 5, MaxAgeDays: 30},
			wantNone: true,
		},
		{
			name:     "BothWindowsZero_DisablesPolicy",
			entries:  makeEntries([]int{0, 10, 100, 1000}),
			policy:   config.Retention{KeepLast: 0, MaxAgeDays: 0},
			wantNone: true,
		},
		{
			name:    "KeepLastOnly_DeletesOlder",
			entries: makeEntries([]int{0, 1, 2, 3, 4}),
			policy:  config.Retention{KeepLast: 2},
			wantDel: []string{"id-02", "id-03", "id-04"},
		},
		{
			name:     "KeepLastGreaterThanCount_DeletesNothing",
			entries:  makeEntries([]int{0, 1, 2}),
			policy:   config.Retention{KeepLast: 10},
			wantNone: true,
		},
		{
			name:    "MaxAgeDaysOnly_DeletesOldEntries",
			entries: makeEntries([]int{0, 5, 31, 100}),
			policy:  config.Retention{MaxAgeDays: 30},
			wantDel: []string{"id-02", "id-03"},
		},
		{
			name:     "MaxAgeDaysWithinWindow_DeletesNothing",
			entries:  makeEntries([]int{0, 1, 2}),
			policy:   config.Retention{MaxAgeDays: 30},
			wantNone: true,
		},
		{
			name:    "BothWindows_MorePermissiveWins_KeepLastSaves",
			entries: makeEntries([]int{0, 10, 60, 90, 120}),
			// MaxAgeDays=30 alone would delete indices 2,3,4. KeepLast=4 saves
			// the first 4 (indices 0-3), so only index 4 (120 days old) is deleted.
			policy:  config.Retention{KeepLast: 4, MaxAgeDays: 30},
			wantDel: []string{"id-04"},
		},
		{
			name: "BothWindows_MorePermissiveWins_AgeSaves",
			// 5 backups, all very recent (within the last 7 days). KeepLast=2
			// alone would delete 3, but MaxAgeDays=30 saves them all.
			entries:  makeEntries([]int{0, 1, 2, 3, 4}),
			policy:   config.Retention{KeepLast: 2, MaxAgeDays: 30},
			wantNone: true,
		},
		{
			name: "BothWindows_DeletesOnlyOutsideBoth",
			// 6 entries: ages 0, 5, 20, 50, 100, 200.
			// KeepLast=2 keeps idx 0,1. MaxAgeDays=30 keeps idx 0,1,2.
			// Union of "kept" = idx 0,1,2. Deleted = idx 3,4,5.
			entries: makeEntries([]int{0, 5, 20, 50, 100, 200}),
			policy:  config.Retention{KeepLast: 2, MaxAgeDays: 30},
			wantDel: []string{"id-03", "id-04", "id-05"},
		},
		{
			name:     "SingleEntry_AlwaysKeptByKeepLast1",
			entries:  makeEntries([]int{500}),
			policy:   config.Retention{KeepLast: 1, MaxAgeDays: 30},
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := core.SelectForDeletion(tc.entries, tc.policy, fixedNow)

			if tc.wantNone {
				if len(got) != 0 {
					t.Fatalf("expected no deletions, got %v", got)
				}
				return
			}

			if len(got) != len(tc.wantDel) {
				t.Fatalf("got %v, want %v", got, tc.wantDel)
			}
			for i := range tc.wantDel {
				if got[i] != tc.wantDel[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tc.wantDel[i])
				}
			}
		})
	}
}
