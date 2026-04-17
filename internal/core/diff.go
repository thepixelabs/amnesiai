package core

import (
	"fmt"

	"github.com/thepixelabs/amnesiai/internal/crypto"
	"github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// DiffEntry is re-exported from provider for convenience.
type DiffEntry = provider.DiffEntry

// DiffOptions controls the diff operation.
type DiffOptions struct {
	BackupID   string   // compare against this backup (empty = latest)
	Providers  []string // subset of providers to diff
	Passphrase string   // decryption passphrase for the backup
}

// DiffResult holds the diff output for all requested providers.
type DiffResult struct {
	BackupID string
	Entries  map[string][]provider.DiffEntry // keyed by provider name
}

// Diff compares the current on-disk state against a stored backup snapshot.
func Diff(store storage.Storage, opts DiffOptions) (*DiffResult, error) {
	// Determine which backup to compare against.
	backupID := opts.BackupID
	if backupID == "" {
		latest, err := store.Latest()
		if err != nil {
			return nil, fmt.Errorf("find latest backup: %w", err)
		}
		backupID = latest
	}

	// Load and decrypt the backup.
	_, payload, err := store.Load(backupID)
	if err != nil {
		return nil, fmt.Errorf("load backup %s: %w", backupID, err)
	}

	payload, err = crypto.Decrypt(opts.Passphrase, payload)
	if err != nil {
		return nil, fmt.Errorf("decrypt backup %s: %w", backupID, err)
	}

	// Extract the archive.
	archiveFiles, manifest, err := ExtractArchive(payload)
	if err != nil {
		return nil, fmt.Errorf("extract backup %s: %w", backupID, err)
	}

	// Build per-provider snapshots.
	providerSnapshots := make(map[string]map[string][]byte)
	for _, entry := range manifest {
		if len(opts.Providers) > 0 && !containsString(opts.Providers, entry.Provider) {
			continue
		}
		if _, ok := providerSnapshots[entry.Provider]; !ok {
			providerSnapshots[entry.Provider] = make(map[string][]byte)
		}
		data, ok := archiveFiles[entry.ArchPath]
		if !ok {
			continue
		}
		providerSnapshots[entry.Provider][entry.OrigPath] = data
	}

	// Diff each provider.
	result := &DiffResult{
		BackupID: backupID,
		Entries:  make(map[string][]provider.DiffEntry),
	}

	providerNames := opts.Providers
	if len(providerNames) == 0 {
		for name := range providerSnapshots {
			providerNames = append(providerNames, name)
		}
	}

	for _, name := range providerNames {
		p, err := provider.Get(name)
		if err != nil {
			return nil, fmt.Errorf("get provider %s: %w", name, err)
		}

		snapshot := providerSnapshots[name]
		if snapshot == nil {
			snapshot = make(map[string][]byte)
		}

		diffs, err := p.Diff(snapshot)
		if err != nil {
			return nil, fmt.Errorf("diff provider %s: %w", name, err)
		}
		result.Entries[name] = diffs
	}

	return result, nil
}
