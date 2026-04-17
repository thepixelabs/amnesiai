// Package copilot implements the amnesiai Provider for GitHub Copilot
// configuration.
//
// Base directory varies by OS:
//   - macOS:   ~/Library/Application Support/GitHub Copilot/
//   - Linux:   ~/.config/github-copilot/
//   - Windows: %APPDATA%/GitHub Copilot/
//
// Included files:
//   - All *.json files, including hosts.json (hostname → settings, not tokens;
//     actual tokens are stored in the OS keychain).
//
// Excluded files:
//   - Any file whose base name contains "token", "secret", "key",
//     "credential", or "auth" (case-insensitive match on the file name).
package copilot

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/thepixelabs/amnesiai/internal/provider"
)

// sensitiveTerms is the set of substrings that, if found anywhere in a file's
// base name (case-insensitive), cause the file to be excluded.
var sensitiveTerms = []string{
	"token",
	"secret",
	"key",
	"credential",
	"auth",
}

// isSensitiveFile returns true if the file's base name contains any sensitive
// term.  hosts.json does NOT contain any of these terms and is therefore
// included.
func isSensitiveFile(name string) bool {
	lower := strings.ToLower(name)
	for _, term := range sensitiveTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// baseDir returns the OS-specific base directory for GitHub Copilot config.
func baseDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("copilot: home dir: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "GitHub Copilot"), nil
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("copilot: home dir: %w", err)
		}
		return filepath.Join(home, ".config", "github-copilot"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("copilot: %%APPDATA%% is not set")
		}
		return filepath.Join(appData, "GitHub Copilot"), nil
	default:
		return "", fmt.Errorf("copilot: unsupported OS %q", runtime.GOOS)
	}
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for GitHub Copilot.
type Provider struct {
	base string // OS-specific base directory (absolute)
}

func init() {
	provider.RegisterFactory("copilot", func() (provider.Provider, error) {
		return New()
	})
}

// New returns a new Copilot Provider.
func New() (*Provider, error) {
	dir, err := baseDir()
	if err != nil {
		return nil, err
	}
	return &Provider{base: dir}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory.
// Intended for testing; production code should use New().
func NewWithBaseDir(base string) *Provider {
	return &Provider{base: base}
}

// Name returns "copilot".
func (p *Provider) Name() string { return "copilot" }

// Discover returns absolute paths of all non-sensitive JSON files under the
// Copilot config directory.  Returns (nil, nil) if the directory does not
// exist.
func (p *Provider) Discover() ([]string, error) {
	if _, err := os.Lstat(p.base); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("copilot: stat base dir: %w", err)
	}

	var paths []string
	err := filepath.WalkDir(p.base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("copilot: discover: skipping %s: %v", path, err)
			return nil
		}

		// Never follow symlinks.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			log.Printf("copilot: discover: lstat %s: %v", path, statErr)
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil // descend into subdirectories
		}

		name := d.Name()

		// Skip files with sensitive names.
		if isSensitiveFile(name) {
			return nil
		}

		// Only back up JSON files.
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			return nil
		}

		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("copilot: walk: %w", err)
	}
	return paths, nil
}

// Read returns the current on-disk contents keyed by relative path from the
// Copilot base directory.
func (p *Provider) Read() (map[string][]byte, error) {
	absPaths, err := p.Discover()
	if err != nil {
		return nil, err
	}

	snapshot := make(map[string][]byte, len(absPaths))
	for _, abs := range absPaths {
		rel, err := filepath.Rel(p.base, abs)
		if err != nil {
			log.Printf("copilot: read: rel path for %s: %v", abs, err)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			log.Printf("copilot: read: skipping %s: %v", abs, err)
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

// Restore writes snapshot files back to the Copilot base directory, skipping
// any sensitive-named files.
func (p *Provider) Restore(snapshot map[string][]byte) error {
	for rel, data := range snapshot {
		base := filepath.Base(rel)
		if isSensitiveFile(base) {
			log.Printf("copilot: restore: skipping sensitive file %s", rel)
			continue
		}
		dest := filepath.Join(p.base, rel)
		// Guard against path traversal: resolved dest must stay inside base.
		if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator),
			filepath.Clean(p.base)+string(filepath.Separator)) {
			log.Printf("copilot: restore: rejecting path traversal attempt: %s", rel)
			continue
		}
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("copilot: restore %s: %w", rel, err)
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
