package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thepixelabs/amnesiai/internal/crypto"
	"github.com/thepixelabs/amnesiai/internal/provider"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// redactedMarker is the prefix written by scan.Scan for every redacted secret.
// We look for this literal string in restored file bytes to warn the user.
const redactedMarker = "<REDACTED:"

// RestoreOptions controls the restore operation.
type RestoreOptions struct {
	BackupID     string                               // backup to restore (empty = latest)
	Providers    []string                             // subset of providers to restore (empty = all from backup)
	ProjectPaths []string                             // per-project directories forwarded to provider constructors
	Overrides    map[string]provider.ProviderOverride // per-provider allowlist tweaks
	Passphrase   string                               // decryption passphrase
	DryRun       bool                                 // if true, report what would change without writing
	OutDir       string                               // if set, extract to <OutDir>/... instead of real destinations
	Force        bool                                 // for OutDir: allow non-empty target dir
	// Files is an optional allowlist of archive paths (e.g. "claude/agents/foo.md")
	// to restore. Empty = restore every file in each selected provider. When set,
	// only entries whose ArchPath appears in this list are written. Unknown entries
	// (path not present in the backup) are silently skipped — callers should
	// pre-validate via the dry-run peek if they want strict behaviour.
	Files []string
}

// RestoreResult holds the outcome of a restore operation.
type RestoreResult struct {
	BackupID          string
	Providers         []string
	DryRun            bool
	OutDir            string
	Files             int      // number of files restored
	RestoredPaths     []string // destination paths of files actually written (live restore only)
	UnencryptedBackup bool     // true if the source backup was not encrypted
	PlaceholderFiles  []string // archive paths of files that contain <REDACTED: markers
	UnknownFiles      []string // archive paths requested via opts.Files that were not found in the backup
}

// loadAndExtract loads the backup identified by backupID, decrypts it with
// passphrase, and extracts the archive.  It is the shared prelude used by
// Restore and PeekArchive; diff.go performs the same steps inline and should
// be refactored to call this helper in a future hardening pass.
func loadAndExtract(store storage.Storage, backupID, passphrase string) (storage.Metadata, map[string][]byte, []ManifestEntry, error) {
	meta, payload, err := store.Load(backupID)
	if err != nil {
		return storage.Metadata{}, nil, nil, fmt.Errorf("load backup %s: %w", backupID, err)
	}

	// payload is not zeroed after decryption — see Restore for the same policy;
	// a hardening pass should update both.
	payload, err = crypto.Decrypt(passphrase, payload)
	if err != nil {
		return storage.Metadata{}, nil, nil, fmt.Errorf("decrypt backup %s: %w", backupID, err)
	}

	archiveFiles, manifest, err := ExtractArchive(payload)
	if err != nil {
		return storage.Metadata{}, nil, nil, fmt.Errorf("extract backup %s: %w", backupID, err)
	}

	return meta, archiveFiles, manifest, nil
}

// Restore loads a backup from storage, decrypts it, extracts the archive,
// and writes files back through the appropriate providers.
func Restore(store storage.Storage, opts RestoreOptions) (*RestoreResult, error) {
	backupID := opts.BackupID
	if backupID == "" {
		latest, err := store.Latest()
		if err != nil {
			return nil, fmt.Errorf("find latest backup: %w", err)
		}
		backupID = latest
	}

	meta, archiveFiles, manifest, err := loadAndExtract(store, backupID, opts.Passphrase)
	if err != nil {
		return nil, err
	}

	restoreProviders := opts.Providers
	if len(restoreProviders) == 0 {
		restoreProviders = meta.Providers
	}

	// Build an O(1) lookup set for the file allowlist when one is provided.
	// matchedFiles tracks which requested paths were actually found so we can
	// surface unknowns to the caller.
	var filesAllowlist map[string]bool
	var matchedFiles map[string]bool
	if len(opts.Files) > 0 {
		filesAllowlist = make(map[string]bool, len(opts.Files))
		matchedFiles = make(map[string]bool, len(opts.Files))
		for _, f := range opts.Files {
			filesAllowlist[f] = true
		}
	}

	providerSnapshots := make(map[string]map[string][]byte)
	for _, entry := range manifest {
		if !containsString(restoreProviders, entry.Provider) {
			continue
		}
		// Allowlist check comes before the provider-snapshot map init so we
		// never allocate a snapshot map for a provider that contributes zero
		// matching files (readability; behaviour is unchanged).
		if filesAllowlist != nil {
			if !filesAllowlist[entry.ArchPath] {
				continue
			}
			matchedFiles[entry.ArchPath] = true
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

	// Compute unknown paths: requested but not present in the backup after
	// provider filtering.
	var unknownFiles []string
	for _, f := range opts.Files {
		if !matchedFiles[f] {
			unknownFiles = append(unknownFiles, f)
		}
	}

	// When an explicit file list was given and zero entries matched, the caller
	// almost certainly made a mistake (e.g. wrong provider scope). Return an
	// error rather than silently "succeeding" with zero files restored.
	if len(opts.Files) > 0 && len(matchedFiles) == 0 {
		return nil, fmt.Errorf("no files matched the --files filter; requested: %v", opts.Files)
	}

	// Scan all files for <REDACTED: markers so we can warn the user post-
	// restore. Done before the dry-run branch so it applies to dry-run too.
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
			OutDir:            opts.OutDir,
			Files:             fileCount,
			UnencryptedBackup: !meta.Encrypted,
			PlaceholderFiles:  placeholderFiles,
			UnknownFiles:      unknownFiles,
		}, nil
	}

	// Out-dir mode: extract only, no live destinations touched.
	if opts.OutDir != "" {
		resolvedOut, err := validateOutDir(opts.OutDir, restoreProviders, opts.ProjectPaths, opts.Force)
		if err != nil {
			return nil, err
		}
		totalFiles := 0
		for provName, snapshot := range providerSnapshots {
			p, err := provider.Get(provName, provider.ProviderOpts{
				ProjectPaths: opts.ProjectPaths,
				Overrides:    opts.Overrides,
			})
			if err != nil {
				return nil, fmt.Errorf("get provider %s for restore: %w", provName, err)
			}
			if err := p.RestoreTo(resolvedOut, snapshot); err != nil {
				return nil, fmt.Errorf("restore provider %s to %s: %w", provName, resolvedOut, err)
			}
			totalFiles += len(snapshot)
		}
		return &RestoreResult{
			BackupID:          backupID,
			Providers:         restoreProviders,
			OutDir:            resolvedOut,
			Files:             totalFiles,
			UnencryptedBackup: !meta.Encrypted,
			PlaceholderFiles:  placeholderFiles,
			UnknownFiles:      unknownFiles,
		}, nil
	}

	// Live restore.
	totalFiles := 0
	var restoredProviders []string
	var restoredPaths []string
	for provName, snapshot := range providerSnapshots {
		p, err := provider.Get(provName, provider.ProviderOpts{
			ProjectPaths: opts.ProjectPaths,
		})
		if err != nil {
			return nil, fmt.Errorf("get provider %s for restore: %w", provName, err)
		}
		if err := p.RestoreTo("", snapshot); err != nil {
			return nil, fmt.Errorf("restore provider %s: %w", provName, err)
		}
		restoredProviders = append(restoredProviders, provName)
		totalFiles += len(snapshot)
		for origPath := range snapshot {
			restoredPaths = append(restoredPaths, origPath)
		}
	}
	sort.Strings(restoredPaths)

	return &RestoreResult{
		BackupID:          backupID,
		Providers:         restoredProviders,
		DryRun:            false,
		Files:             totalFiles,
		RestoredPaths:     restoredPaths,
		UnencryptedBackup: !meta.Encrypted,
		PlaceholderFiles:  placeholderFiles,
		UnknownFiles:      unknownFiles,
	}, nil
}

// validateOutDir ensures outDir is a safe inspection target:
//   - resolves to an absolute path
//   - is not equal to or inside any provider's baseDir
//   - is not equal to or inside any configured project path
//   - is empty, OR force=true (we still never delete pre-existing files)
func validateOutDir(outDir string, providerNames []string, projectPaths []string, force bool) (string, error) {
	if outDir == "" {
		return "", errors.New("validateOutDir: empty path")
	}
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return "", fmt.Errorf("resolve out-dir: %w", err)
	}
	// Resolve symlinks if the path exists; otherwise use the abs form so a
	// not-yet-created dir is still acceptable.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}

	if err := refuseIfClashesProviders(abs, providerNames); err != nil {
		return "", err
	}
	for _, proj := range projectPaths {
		expanded := expandHomePath(proj)
		expandedAbs, err := filepath.Abs(expanded)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(expandedAbs); err == nil {
			expandedAbs = real
		}
		if pathOverlaps(abs, expandedAbs) {
			return "", fmt.Errorf("--out-dir %s overlaps configured project path %s; choose a different directory", abs, expandedAbs)
		}
	}

	info, err := os.Stat(abs)
	switch {
	case os.IsNotExist(err):
		if mkErr := os.MkdirAll(abs, 0700); mkErr != nil {
			return "", fmt.Errorf("create out-dir: %w", mkErr)
		}
	case err != nil:
		return "", fmt.Errorf("stat out-dir: %w", err)
	default:
		if !info.IsDir() {
			return "", fmt.Errorf("--out-dir %s is not a directory", abs)
		}
		entries, _ := os.ReadDir(abs)
		if len(entries) > 0 && !force {
			return "", fmt.Errorf("--out-dir %s is not empty; pass --force to allow writing into it (existing files are never deleted)", abs)
		}
	}
	return abs, nil
}

// refuseIfClashesProviders returns an error if outDir equals or contains any
// provider's known base directory.
func refuseIfClashesProviders(outDir string, providerNames []string) error {
	if len(providerNames) == 0 {
		providerNames = provider.Names()
	}
	for _, name := range providerNames {
		p, err := provider.Get(name, provider.ProviderOpts{})
		if err != nil {
			continue
		}
		base := providerBaseDir(p)
		if base == "" {
			continue
		}
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(baseAbs); err == nil {
			baseAbs = real
		}
		if pathOverlaps(outDir, baseAbs) {
			return fmt.Errorf("--out-dir %s overlaps provider %s's base directory %s; choose a different directory", outDir, name, baseAbs)
		}
	}
	return nil
}

// pathOverlaps returns true when a == b, a contains b, or b contains a.
func pathOverlaps(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if ca == cb {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(ca+sep, cb+sep) || strings.HasPrefix(cb+sep, ca+sep)
}

// providerBaseDir extracts a provider's base directory using a duck-typed
// interface. Providers expose their base via baseDir()/base()/Base() in
// various concrete types; rather than refactor every provider to surface this
// uniformly, we use reflection-free type assertions on a tiny interface.
func providerBaseDir(p provider.Provider) string {
	type baseDirReporter interface{ BaseDir() string }
	if b, ok := p.(baseDirReporter); ok {
		return b.BaseDir()
	}
	return ""
}

// expandHomePath expands a leading "~/" into the user's home directory.
func expandHomePath(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

// PeekArchive loads a backup and returns its manifest entries (archive path,
// provider, orig path) without writing any files. Used by the TUI to populate
// the cherry-pick file picker. Decryption errors surface to the caller.
// The second return value is true when the backup was stored unencrypted.
func PeekArchive(store storage.Storage, backupID, passphrase string) ([]ManifestEntry, bool, error) {
	meta, _, manifest, err := loadAndExtract(store, backupID, passphrase)
	if err != nil {
		return nil, false, err
	}
	return manifest, !meta.Encrypted, nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
