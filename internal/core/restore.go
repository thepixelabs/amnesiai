package core

import (
	"fmt"

	"github.com/thepixelabs/amnesiai/internal/crypto"
	"github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// RestoreOptions controls the restore operation.
type RestoreOptions struct {
	BackupID   string   // backup to restore (empty = latest)
	Providers  []string // subset of providers to restore (empty = all from backup)
	Passphrase string   // decryption passphrase
	DryRun     bool     // if true, report what would change without writing
}

// RestoreResult holds the outcome of a restore operation.
type RestoreResult struct {
	BackupID  string
	Providers []string
	DryRun    bool
	Files     int // number of files restored
}

// Restore loads a backup from storage, decrypts it, extracts the archive,
// and writes files back through the appropriate providers.
func Restore(store storage.Storage, opts RestoreOptions) (*RestoreResult, error) {
	// Determine which backup to load.
	backupID := opts.BackupID
	if backupID == "" {
		latest, err := store.Latest()
		if err != nil {
			return nil, fmt.Errorf("find latest backup: %w", err)
		}
		backupID = latest
	}

	// Load from storage.
	meta, payload, err := store.Load(backupID)
	if err != nil {
		return nil, fmt.Errorf("load backup %s: %w", backupID, err)
	}

	// Decrypt.
	payload, err = crypto.Decrypt(opts.Passphrase, payload)
	if err != nil {
		return nil, fmt.Errorf("decrypt backup %s: %w", backupID, err)
	}

	// Extract archive.
	archiveFiles, manifest, err := ExtractArchive(payload)
	if err != nil {
		return nil, fmt.Errorf("extract backup %s: %w", backupID, err)
	}

	// Determine which providers to restore.
	restoreProviders := opts.Providers
	if len(restoreProviders) == 0 {
		restoreProviders = meta.Providers
	}

	// Build per-provider snapshots using the manifest.
	providerSnapshots := make(map[string]map[string][]byte)
	for _, entry := range manifest {
		if !containsString(restoreProviders, entry.Provider) {
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

	if opts.DryRun {
		fileCount := 0
		for _, snap := range providerSnapshots {
			fileCount += len(snap)
		}
		return &RestoreResult{
			BackupID:  backupID,
			Providers: restoreProviders,
			DryRun:    true,
			Files:     fileCount,
		}, nil
	}

	// Restore through each provider.
	totalFiles := 0
	for provName, snapshot := range providerSnapshots {
		p, err := provider.Get(provName)
		if err != nil {
			return nil, fmt.Errorf("get provider %s for restore: %w", provName, err)
		}
		if err := p.Restore(snapshot); err != nil {
			return nil, fmt.Errorf("restore provider %s: %w", provName, err)
		}
		totalFiles += len(snapshot)
	}

	return &RestoreResult{
		BackupID:  backupID,
		Providers: restoreProviders,
		DryRun:    false,
		Files:     totalFiles,
	}, nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
