package core

import (
	"bytes"
	"fmt"

	"github.com/thepixelabs/amnesiai/internal/crypto"
	"github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// redactedMarker is the prefix written by scan.Scan for every redacted secret.
// We look for this literal string in restored file bytes to warn the user.
const redactedMarker = "<REDACTED:"

// RestoreOptions controls the restore operation.
type RestoreOptions struct {
	BackupID     string   // backup to restore (empty = latest)
	Providers    []string // subset of providers to restore (empty = all from backup)
	ProjectPaths []string // per-project directories forwarded to provider constructors
	Passphrase   string   // decryption passphrase
	DryRun       bool     // if true, report what would change without writing
}

// RestoreResult holds the outcome of a restore operation.
type RestoreResult struct {
	BackupID          string
	Providers         []string
	DryRun            bool
	Files             int      // number of files restored
	UnencryptedBackup bool     // true if the source backup was not encrypted
	PlaceholderFiles  []string // archive paths of files that contain <REDACTED: markers
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

	// Scan all files-to-restore for <REDACTED: markers so we can warn the user
	// post-restore. Do this before the dry-run branch so it also works there.
	var placeholderFiles []string
	marker := []byte(redactedMarker)
	for archPath, data := range archiveFiles {
		if bytes.Contains(data, marker) {
			placeholderFiles = append(placeholderFiles, archPath)
		}
	}

	if opts.DryRun {
		fileCount := 0
		for _, snap := range providerSnapshots {
			fileCount += len(snap)
		}
		return &RestoreResult{
			BackupID:          backupID,
			Providers:         restoreProviders,
			DryRun:            true,
			Files:             fileCount,
			UnencryptedBackup: !meta.Encrypted,
			PlaceholderFiles:  placeholderFiles,
		}, nil
	}

	// Restore through each provider.
	totalFiles := 0
	for provName, snapshot := range providerSnapshots {
		p, err := provider.Get(provName, provider.ProviderOpts{
			ProjectPaths: opts.ProjectPaths,
		})
		if err != nil {
			return nil, fmt.Errorf("get provider %s for restore: %w", provName, err)
		}
		if err := p.Restore(snapshot); err != nil {
			return nil, fmt.Errorf("restore provider %s: %w", provName, err)
		}
		totalFiles += len(snapshot)
	}

	return &RestoreResult{
		BackupID:          backupID,
		Providers:         restoreProviders,
		DryRun:            false,
		Files:             totalFiles,
		UnencryptedBackup: !meta.Encrypted,
		PlaceholderFiles:  placeholderFiles,
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
