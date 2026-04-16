// Package claude implements the amensiai Provider for Claude Code configuration.
//
// Backed-up paths under ~/.claude/:
//   - CLAUDE.md
//   - settings.json
//   - settings.local.json
//   - todos/   (all files, recursively)
//   - ide/     (all files, recursively)
//
// Explicitly excluded (never read, never restored):
//   - projects/          (conversation history / PII)
//   - statsig/           (internal telemetry state)
//   - .credentials.json  (credential file — never touch)
package claude

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/thepixelabs/amensiai/internal/provider"
)

// excludedDirs are subdirectory names under ~/.claude/ that must never be
// traversed.  Checked against the base name of each directory entry.
var excludedDirs = map[string]bool{
	"projects": true,
	"statsig":  true,
}

// credentialFiles are specific file names that must never be backed up or
// restored, regardless of how they appear in a snapshot.
var credentialFiles = map[string]bool{
	".credentials.json": true,
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for Claude Code.
type Provider struct {
	baseDir string // absolute path to ~/.claude/
}

func init() {
	provider.RegisterFactory("claude", func() (provider.Provider, error) {
		return New()
	})
}

// New returns a new Claude Provider targeting ~/.claude/.
func New() (*Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("claude: cannot determine home directory: %w", err)
	}
	return &Provider{baseDir: filepath.Join(home, ".claude")}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory.
// Intended for testing; production code should use New().
func NewWithBaseDir(baseDir string) *Provider {
	return &Provider{baseDir: baseDir}
}

// Name returns "claude".
func (p *Provider) Name() string { return "claude" }

// Discover returns the absolute paths of all files managed by this provider.
// If ~/.claude/ does not exist the method returns (nil, nil) — the tool is
// simply not installed on this machine.
func (p *Provider) Discover() ([]string, error) {
	if _, err := os.Lstat(p.baseDir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("claude: stat base dir: %w", err)
	}

	var paths []string
	err := filepath.WalkDir(p.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Log and skip entries we cannot stat (e.g. permission denied).
			log.Printf("claude: discover: skipping %s: %v", path, err)
			return nil
		}

		// Never follow symlinks.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			log.Printf("claude: discover: lstat %s: %v", path, statErr)
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		if d.IsDir() {
			if path == p.baseDir {
				return nil // continue into the base dir itself
			}
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// It's a regular file.
		rel, relErr := filepath.Rel(p.baseDir, path)
		if relErr != nil {
			return nil
		}
		// Skip credential files anywhere in the tree.
		if credentialFiles[filepath.Base(rel)] {
			return nil
		}

		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claude: walk: %w", err)
	}
	return paths, nil
}

// Read returns the current on-disk contents of all discovered files, keyed by
// path relative to ~/.claude/.  Files that cannot be read are skipped with a
// warning.
func (p *Provider) Read() (map[string][]byte, error) {
	absPaths, err := p.Discover()
	if err != nil {
		return nil, err
	}

	snapshot := make(map[string][]byte, len(absPaths))
	for _, abs := range absPaths {
		rel, err := filepath.Rel(p.baseDir, abs)
		if err != nil {
			log.Printf("claude: read: rel path for %s: %v", abs, err)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			log.Printf("claude: read: skipping %s: %v", abs, err)
			continue
		}
		snapshot[rel] = data
	}
	return snapshot, nil
}

// Diff compares the provided snapshot against the current on-disk state and
// returns one DiffEntry per file that appears in either side.
func (p *Provider) Diff(snapshot map[string][]byte) ([]provider.DiffEntry, error) {
	current, err := p.Read()
	if err != nil {
		return nil, err
	}

	// Collect the union of all relative paths.
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

// Restore writes every file in snapshot back under ~/.claude/, creating parent
// directories (mode 0700) as needed.  Each write is atomic: content is written
// to a .amensiai.tmp sibling then renamed into place.  Credential files are
// silently skipped even if present in the snapshot.
func (p *Provider) Restore(snapshot map[string][]byte) error {
	for rel, data := range snapshot {
		// Guard: never restore credential files.
		base := filepath.Base(rel)
		if credentialFiles[base] {
			log.Printf("claude: restore: skipping credential file %s", rel)
			continue
		}
		// Guard: never restore into excluded directories.
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 && excludedDirs[parts[0]] {
			log.Printf("claude: restore: skipping excluded dir entry %s", rel)
			continue
		}

		dest := filepath.Join(p.baseDir, rel)
		// Guard against path traversal: resolved dest must stay inside baseDir.
		if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator),
			filepath.Clean(p.baseDir)+string(filepath.Separator)) {
			log.Printf("claude: restore: rejecting path traversal attempt: %s", rel)
			continue
		}
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("claude: restore %s: %w", rel, err)
		}
	}
	return nil
}

// atomicWrite creates parent directories then writes data to dest via a
// temporary sibling file, finalising with an atomic rename.
func atomicWrite(dest string, data []byte) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmp := dest + ".amensiai.tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		// Best-effort cleanup of the orphaned tmp file.
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dest, err)
	}
	return nil
}
