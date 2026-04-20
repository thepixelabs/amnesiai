// Package copilot implements the amnesiai Provider for GitHub Copilot
// configuration.
//
// Base directory varies by OS:
//   - macOS:   ~/Library/Application Support/GitHub Copilot/
//   - Linux:   ~/.config/github-copilot/
//   - Windows: %APPDATA%/GitHub Copilot/
//
// Global included files:
//   - All *.json files, including hosts.json (hostname → settings, not tokens;
//     actual tokens are stored in the OS keychain).
//
// Global excluded files:
//   - Any file whose base name contains "token", "secret", "key",
//     "credential", or "auth" (case-insensitive match on the file name).
//
// Per-project files (backed up for each path in ProjectPaths):
//   - <project>/.github/copilot-instructions.md
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
	base         string   // OS-specific base directory (absolute)
	projectPaths []string // per-project directories to scan for copilot-instructions.md
}

func init() {
	provider.RegisterFactory("copilot", func(o provider.ProviderOpts) (provider.Provider, error) {
		dir, err := baseDir()
		if err != nil {
			return nil, err
		}
		if len(o.ProjectPaths) == 0 {
			log.Printf("copilot: no project paths configured; skipping per-project backup. " +
				"Add via `amnesiai config set project_paths ~/code/foo,~/code/bar`")
		}
		return &Provider{base: dir, projectPaths: o.ProjectPaths}, nil
	})
}

// New returns a new Copilot Provider with no project paths.
func New() (*Provider, error) {
	dir, err := baseDir()
	if err != nil {
		return nil, err
	}
	return &Provider{base: dir}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory with
// no project paths. Intended for testing; production code should use New().
func NewWithBaseDir(base string) *Provider {
	return &Provider{base: base}
}

// NewWithProjects returns a Provider targeting an explicit base directory and
// the given project paths. Intended for testing; production code should use
// New() and set ProjectPaths via config.
func NewWithProjects(base string, projectPaths []string) *Provider {
	return &Provider{base: base, projectPaths: projectPaths}
}

// Name returns "copilot".
func (p *Provider) Name() string { return "copilot" }

// Discover returns absolute paths of all non-sensitive JSON files under the
// Copilot config directory plus any per-project copilot-instructions.md files.
// Returns (nil, nil) for the global section if the directory does not exist.
func (p *Provider) Discover() ([]string, error) {
	var paths []string

	// Global Copilot config dir.
	globalPaths, err := p.discoverGlobal()
	if err != nil {
		return nil, err
	}
	paths = append(paths, globalPaths...)

	// Per-project copilot-instructions.md.
	for _, proj := range p.projectPaths {
		projPaths, err := p.discoverProject(proj)
		if err != nil {
			log.Printf("copilot: discover project %s: %v", proj, err)
			continue
		}
		paths = append(paths, projPaths...)
	}

	return paths, nil
}

// discoverGlobal returns non-sensitive JSON files from the Copilot config dir.
func (p *Provider) discoverGlobal() ([]string, error) {
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

		// Only back up JSON files from the global config dir.
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

// discoverProject returns the copilot-instructions.md path for a project if it
// exists.
func (p *Provider) discoverProject(proj string) ([]string, error) {
	proj = expandHome(proj)

	info, err := os.Lstat(proj)
	if os.IsNotExist(err) {
		log.Printf("copilot: project path does not exist, skipping: %s", proj)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat project dir %s: %w", proj, err)
	}
	if !info.IsDir() {
		log.Printf("copilot: project path is not a directory, skipping: %s", proj)
		return nil, nil
	}

	instructions := filepath.Join(proj, ".github", "copilot-instructions.md")
	if fileExists(instructions) {
		return []string{instructions}, nil
	}
	return nil, nil
}

// Read returns the current on-disk contents keyed by relative path from the
// Copilot base directory for global files, and by absolute path for per-project
// files.
func (p *Provider) Read() (map[string][]byte, error) {
	absPaths, err := p.Discover()
	if err != nil {
		return nil, err
	}

	snapshot := make(map[string][]byte, len(absPaths))
	for _, abs := range absPaths {
		key := p.relKey(abs)
		data, err := os.ReadFile(abs)
		if err != nil {
			log.Printf("copilot: read: skipping %s: %v", abs, err)
			continue
		}
		snapshot[key] = data
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

// Restore writes snapshot files back to the Copilot base directory for global
// keys, and to the absolute path for per-project keys. Sensitive-named files
// are always skipped.
func (p *Provider) Restore(snapshot map[string][]byte) error {
	for key, data := range snapshot {
		// Skip sensitive-named files regardless of source.
		if isSensitiveFile(filepath.Base(key)) {
			log.Printf("copilot: restore: skipping sensitive file %s", key)
			continue
		}

		dest := p.absPath(key)
		if dest == "" {
			log.Printf("copilot: restore: skipping unrecognised key %q", key)
			continue
		}

		// Guard against path traversal.
		if !isUnder(dest, p.base) && !p.isUnderAProject(dest) {
			log.Printf("copilot: restore: rejecting path traversal attempt: %q", key)
			continue
		}

		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("copilot: restore %s: %w", key, err)
		}
	}
	return nil
}

// relKey converts an absolute path to the snapshot map key.
func (p *Provider) relKey(abs string) string {
	rel, err := filepath.Rel(p.base, abs)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	// Per-project file — keep absolute path as key.
	return abs
}

// absPath resolves a snapshot key to an absolute destination path.
// Returns "" for keys that cannot be resolved safely.
func (p *Provider) absPath(key string) string {
	if filepath.IsAbs(key) {
		// Per-project: must be copilot-instructions.md inside .github/.
		if filepath.Base(key) == "copilot-instructions.md" {
			return key
		}
		return ""
	}
	// Global key — must be a JSON file.
	if !strings.HasSuffix(strings.ToLower(key), ".json") {
		return ""
	}
	return filepath.Join(p.base, key)
}

// isUnderAProject reports whether dest is inside any configured project dir.
func (p *Provider) isUnderAProject(dest string) bool {
	for _, proj := range p.projectPaths {
		proj = expandHome(proj)
		if isUnder(dest, proj) {
			return true
		}
	}
	return false
}

// isUnder reports whether path is inside (or equal to) dir.
func isUnder(path, dir string) bool {
	clean := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	return clean == cleanDir ||
		strings.HasPrefix(clean, cleanDir+string(filepath.Separator))
}

// expandHome replaces a leading "~/" with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// fileExists returns true if the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
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
