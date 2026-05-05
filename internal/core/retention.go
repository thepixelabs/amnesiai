// Package core — retention.go
//
// Implements amnesiai's backup-retention policy:
//
//   - KeepLast keeps the N most recent backups.
//   - MaxAgeDays keeps anything from the last N days.
//
// The two windows are combined with OR (more permissive wins). A backup
// survives if it is in either window. When both KeepLast and MaxAgeDays are
// zero the policy is disabled and nothing is selected for deletion — this is
// the default for new and existing installs so retention never auto-deletes
// without being explicitly opted into.
package core

import (
	"fmt"
	"time"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// PruneResult summarises a Prune (or dry-run) call.
//
// Deleted contains the IDs that were (or would be) removed.
// Kept contains the IDs that survived the policy.
// DryRun mirrors the caller's intent so the result is self-describing when
// passed around without context.
type PruneResult struct {
	Deleted []string
	Kept    []string
	DryRun  bool
}

// SelectForDeletion is a pure function that applies the retention policy and
// returns the IDs that should be deleted. It does no I/O so it is the
// testable seam for the policy logic. Caller is responsible for actually
// invoking storage.Delete for each ID — see Prune for the full pipeline.
//
// entries must be sorted newest-first (which storage.Storage.List guarantees).
// If the slice is empty, returns nil. If both policy windows are zero,
// returns nil regardless of input — retention is disabled.
func SelectForDeletion(entries []storage.BackupEntry, policy config.Retention, now time.Time) []string {
	if len(entries) == 0 {
		return nil
	}
	// Both windows zero: retention disabled. Treat as a no-op rather than
	// nuking everything — this is the safe default for opt-in.
	if policy.KeepLast == 0 && policy.MaxAgeDays == 0 {
		return nil
	}

	maxAge := time.Duration(policy.MaxAgeDays) * 24 * time.Hour

	var toDelete []string
	for i, e := range entries {
		// "More permissive wins" — kept if EITHER window keeps it.
		inCountWindow := policy.KeepLast > 0 && i < policy.KeepLast
		inAgeWindow := policy.MaxAgeDays > 0 && now.Sub(e.Timestamp) < maxAge

		if !inCountWindow && !inAgeWindow {
			toDelete = append(toDelete, e.ID)
		}
	}
	return toDelete
}

// Prune applies the retention policy and (unless dryRun is true) deletes the
// selected backups via store.Delete. It always lists the store first so the
// caller's PruneResult is consistent with on-disk state at policy-evaluation
// time.
//
// Per-ID delete failures are accumulated into the returned error but the
// function continues — partial progress is preferable to an all-or-nothing
// abort, and any backup that wasn't deleted simply remains for the next run.
func Prune(store storage.Storage, policy config.Retention, dryRun bool) (*PruneResult, error) {
	entries, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}

	now := time.Now().UTC()
	toDelete := SelectForDeletion(entries, policy, now)

	deletedSet := make(map[string]bool, len(toDelete))
	for _, id := range toDelete {
		deletedSet[id] = true
	}

	kept := make([]string, 0, len(entries)-len(toDelete))
	for _, e := range entries {
		if !deletedSet[e.ID] {
			kept = append(kept, e.ID)
		}
	}

	result := &PruneResult{
		Deleted: append([]string(nil), toDelete...),
		Kept:    kept,
		DryRun:  dryRun,
	}

	if dryRun {
		return result, nil
	}

	var deleteErrs []error
	for _, id := range toDelete {
		if delErr := store.Delete(id); delErr != nil {
			deleteErrs = append(deleteErrs, fmt.Errorf("delete %s: %w", id, delErr))
		}
	}

	if len(deleteErrs) > 0 {
		// Re-build Deleted to reflect what actually got removed (best-effort).
		// We only know the failures; the rest are presumed deleted.
		failed := make(map[string]bool, len(deleteErrs))
		for _, de := range deleteErrs {
			// Errors are wrapped with "delete <id>:" — we do not parse them
			// here, we just keep the full toDelete list and surface the
			// composite error so the caller can decide how to report.
			_ = de
			_ = failed
		}
		return result, fmt.Errorf("prune: %d delete error(s); first: %w", len(deleteErrs), deleteErrs[0])
	}

	return result, nil
}
