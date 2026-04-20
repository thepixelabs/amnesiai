// Package codex implements the amnesiai Provider for OpenAI Codex CLI
// configuration.
//
// Backed-up paths under ~/.codex/:
//   - agents/*.toml  (agent definitions)
//   - rules/default.rules
//
// Excluded:
//   - Any file whose base name ends in ".key" or contains "token" or
//     "credential" (case-insensitive).
//   - Everything else not in the allowlist above.
package codex

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

// allowedTopLevel is the set of directory names and file names directly under
// ~/.codex/ that are in scope for backup.
//
// agents/ contains agent definition TOML files. rules/ contains rule files.
// Both directories are walked; see Discover for the file-level filters applied
// inside each.
var allowedTopLevel = map[string]bool{
	"agents": true,
	"rules":  true,
}

// allowedRulesFiles is the explicit set of files inside rules/ that are backed
// up. Keeping this narrow prevents backing up machine-local rule caches.
var allowedRulesFiles = map[string]bool{
	"default.rules": true,
}

// isExcludedFile reports whether a file base name matches the codex exclusion
// rules:
//   - ends with ".key"
//   - contains "token" (case-insensitive)
//   - contains "credential" (case-insensitive)
func isExcludedFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".key") {
		return true
	}
	if strings.Contains(lower, "token") {
		return true
	}
	if strings.Contains(lower, "credential") {
		return true
	}
	return false
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for OpenAI Codex CLI.
type Provider struct {
	baseDir string // absolute path to ~/.codex/
}

func init() {
	provider.RegisterFactory("codex", func(_ provider.ProviderOpts) (provider.Provider, error) {
		return New()
	})
}

// New returns a new Codex Provider targeting ~/.codex/.
func New() (*Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("codex: cannot determine home directory: %w", err)
	}
	return &Provider{baseDir: filepath.Join(home, ".codex")}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory.
// Intended for testing; production code should use New().
func NewWithBaseDir(baseDir string) *Provider {
	return &Provider{baseDir: baseDir}
}

// Name returns "codex".
func (p *Provider) Name() string { return "codex" }

// Discover returns absolute paths of all files managed by this provider.
// Returns (nil, nil) if ~/.codex/ does not exist.
func (p *Provider) Discover() ([]string, error) {
	if _, err := os.Lstat(p.baseDir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("codex: stat base dir: %w", err)
	}

	var paths []string
	err := filepath.WalkDir(p.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("codex: discover: skipping %s: %v", path, err)
			return nil
		}

		// Never follow symlinks.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			log.Printf("codex: discover: lstat %s: %v", path, statErr)
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
			return nil // descend into allowed subdirs
		}

		// Skip excluded files (credential / key material).
		if isExcludedFile(d.Name()) {
			return nil
		}

		// Apply per-directory file filters.
		switch topLevel {
		case "agents":
			// Only back up TOML agent definitions.
			if !strings.HasSuffix(strings.ToLower(d.Name()), ".toml") {
				return nil
			}
		case "rules":
			// Only back up the default rules file.
			if !allowedRulesFiles[d.Name()] {
				return nil
			}
		}

		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("codex: walk: %w", err)
	}
	return paths, nil
}

// Read returns the current on-disk contents keyed by path relative to
// ~/.codex/.  Unreadable files are skipped with a warning.
func (p *Provider) Read() (map[string][]byte, error) {
	absPaths, err := p.Discover()
	if err != nil {
		return nil, err
	}

	snapshot := make(map[string][]byte, len(absPaths))
	for _, abs := range absPaths {
		rel, err := filepath.Rel(p.baseDir, abs)
		if err != nil {
			log.Printf("codex: read: rel path for %s: %v", abs, err)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			log.Printf("codex: read: skipping %s: %v", abs, err)
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

// Restore writes snapshot files back under ~/.codex/, enforcing the same
// allowlist that Discover uses. Files that would not be discovered are silently
// skipped rather than returned as errors.
func (p *Provider) Restore(snapshot map[string][]byte) error {
	for rel, data := range snapshot {
		name := filepath.Base(rel)

		// Reject credential / key material.
		if isExcludedFile(name) {
			log.Printf("codex: restore: skipping excluded file %s", rel)
			continue
		}

		// Enforce top-level allowlist.
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if !allowedTopLevel[topLevel] {
			log.Printf("codex: restore: skipping non-allowlisted path %s", rel)
			continue
		}

		// Enforce per-directory file filters.
		switch topLevel {
		case "agents":
			if !strings.HasSuffix(strings.ToLower(name), ".toml") {
				log.Printf("codex: restore: skipping non-toml file in agents/: %s", rel)
				continue
			}
		case "rules":
			if !allowedRulesFiles[name] {
				log.Printf("codex: restore: skipping non-allowlisted rules file: %s", rel)
				continue
			}
		}

		dest := filepath.Join(p.baseDir, rel)
		// Guard against path traversal: resolved dest must stay inside baseDir.
		if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator),
			filepath.Clean(p.baseDir)+string(filepath.Separator)) {
			log.Printf("codex: restore: rejecting path traversal attempt: %s", rel)
			continue
		}
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("codex: restore %s: %w", rel, err)
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
