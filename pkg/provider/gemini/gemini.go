// Package gemini implements the amnesiai Provider for Gemini CLI configuration.
//
// Backed-up paths under ~/.gemini/:
//   - settings.json
//   - GEMINI.md
//   - themes/  (all files, recursively)
//
// Excluded:
//   - Any file whose base name matches *.key (glob) or starts with "auth"
//     (case-insensitive).  These patterns cover credential / OAuth token files
//     that Gemini CLI may create.
package gemini

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/thepixelabs/amnesiai/internal/provider"
)

// allowedTopLevel is the set of file names and directory names directly under
// ~/.gemini/ that are in scope.  Any other top-level entries are ignored.
var allowedTopLevel = map[string]bool{
	"settings.json": true,
	"GEMINI.md":     true,
	"themes":        true,
}

// isCredentialFile reports whether a file base name looks like a credential
// file that must never be backed up.
func isCredentialFile(name string) bool {
	lower := strings.ToLower(name)
	// *.key files
	if strings.HasSuffix(lower, ".key") {
		return true
	}
	// auth* files (auth.json, auth_token, etc.)
	if strings.HasPrefix(lower, "auth") {
		return true
	}
	return false
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for Gemini CLI.
type Provider struct {
	baseDir string // absolute path to ~/.gemini/
}

func init() {
	provider.RegisterFactory("gemini", func(_ provider.ProviderOpts) (provider.Provider, error) {
		return New()
	})
}

// New returns a new Gemini Provider targeting ~/.gemini/.
func New() (*Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("gemini: cannot determine home directory: %w", err)
	}
	return &Provider{baseDir: filepath.Join(home, ".gemini")}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory.
// Intended for testing; production code should use New().
func NewWithBaseDir(baseDir string) *Provider {
	return &Provider{baseDir: baseDir}
}

// Name returns "gemini".
func (p *Provider) Name() string { return "gemini" }

// Discover returns the absolute paths of all files managed by this provider.
// Returns (nil, nil) if ~/.gemini/ does not exist.
func (p *Provider) Discover() ([]string, error) {
	if _, err := os.Lstat(p.baseDir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("gemini: stat base dir: %w", err)
	}

	var paths []string
	err := filepath.WalkDir(p.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("gemini: discover: skipping %s: %v", path, err)
			return nil
		}

		// Never follow symlinks.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			log.Printf("gemini: discover: lstat %s: %v", path, statErr)
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if path == p.baseDir {
			return nil // descend into root
		}

		// Enforce allowlist at the first level below base.
		rel, relErr := filepath.Rel(p.baseDir, path)
		if relErr != nil {
			return nil
		}
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if !allowedTopLevel[topLevel] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil // descend into allowed subdirs (e.g. themes/)
		}

		// Skip credential files.
		if isCredentialFile(d.Name()) {
			return nil
		}

		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: walk: %w", err)
	}
	return paths, nil
}

// Read returns the current on-disk contents keyed by relative path from
// ~/.gemini/.  Unreadable files are skipped with a warning.
func (p *Provider) Read() (map[string][]byte, error) {
	absPaths, err := p.Discover()
	if err != nil {
		return nil, err
	}

	snapshot := make(map[string][]byte, len(absPaths))
	for _, abs := range absPaths {
		rel, err := filepath.Rel(p.baseDir, abs)
		if err != nil {
			log.Printf("gemini: read: rel path for %s: %v", abs, err)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			log.Printf("gemini: read: skipping %s: %v", abs, err)
			continue
		}
		snapshot[rel] = data
	}
	return snapshot, nil
}

// Diff compares snapshot against the current on-disk state.
func (p *Provider) Diff(snapshot map[string][]byte) ([]provider.DiffEntry, error) {
	current, err := p.Read()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	for k := range current {
		seen[k] = true
	}
	for k := range snapshot {
		seen[k] = true
	}

	entries := make([]provider.DiffEntry, 0, len(seen))
	for rel := range seen {
		cur, inCurrent := current[rel]
		snap, inSnapshot := snapshot[rel]

		var status string
		switch {
		case inCurrent && !inSnapshot:
			status = "added"
		case !inCurrent && inSnapshot:
			status = "deleted"
		case bytes.Equal(cur, snap):
			status = "unchanged"
		default:
			status = "modified"
		}

		entries = append(entries, provider.DiffEntry{
			Path:   rel,
			Status: status,
			Before: snap,
			After:  cur,
		})
	}
	return entries, nil
}

// Restore writes snapshot files back to ~/.gemini/, skipping credential files.
func (p *Provider) Restore(snapshot map[string][]byte) error {
	for rel, data := range snapshot {
		base := filepath.Base(rel)
		if isCredentialFile(base) {
			log.Printf("gemini: restore: skipping credential file %s", rel)
			continue
		}
		// Only restore files whose top-level component is in the allowlist.
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if !allowedTopLevel[topLevel] && rel != topLevel {
			continue
		}
		// Simpler check: the file is in the allowlist tree.
		// We already guard credential files above; allow anything else in the
		// snapshot (the snapshot was produced by Read(), which only includes
		// allowed paths).
		dest := filepath.Join(p.baseDir, rel)
		// Guard against path traversal: resolved dest must stay inside baseDir.
		if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator),
			filepath.Clean(p.baseDir)+string(filepath.Separator)) {
			log.Printf("gemini: restore: rejecting path traversal attempt: %s", rel)
			continue
		}
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("gemini: restore %s: %w", rel, err)
		}
	}
	return nil
}

// atomicWrite creates parent directories then writes data to dest atomically.
func atomicWrite(dest string, data []byte) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp := dest + ".amnesiai.tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dest, err)
	}
	return nil
}
