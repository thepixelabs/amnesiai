// Package claude implements the amnesiai Provider for Claude Code configuration.
//
// Global files backed up from ~/.claude/:
//   - CLAUDE.md
//   - settings.json
//   - keybindings.json  (if present)
//
// Per-project files backed up for each path in ProjectPaths:
//   - <project>/CLAUDE.md
//   - <project>/.claude/settings.json  (NOT settings.local.json — machine-specific)
//
// Everything else under ~/.claude/ is intentionally ignored. This allowlist
// approach is safer than a blocklist: new Claude Code features that add state
// directories (todos/, ide/, statsig/, etc.) are excluded by default rather
// than silently backed up.
package claude

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/thepixelabs/amnesiai/internal/provider"
)

// globalAllowlist is the set of file names directly under ~/.claude/ that
// are in scope for backup. Any other top-level entry is ignored.
var globalAllowlist = map[string]bool{
	"CLAUDE.md":        true,
	"settings.json":    true,
	"keybindings.json": true,
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for Claude Code.
type Provider struct {
	baseDir      string   // absolute path to ~/.claude/
	projectPaths []string // absolute paths to per-project directories to scan
}

func init() {
	provider.RegisterFactory("claude", func(o provider.ProviderOpts) (provider.Provider, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("claude: cannot determine home directory: %w", err)
		}
		baseDir := filepath.Join(home, ".claude")
		if len(o.ProjectPaths) == 0 {
			log.Printf("claude: no project paths configured; skipping per-project backup. " +
				"Add via `amnesiai config set project_paths ~/code/foo,~/code/bar`")
		}
		return &Provider{baseDir: baseDir, projectPaths: o.ProjectPaths}, nil
	})
}

// New returns a new Claude Provider targeting ~/.claude/ with no project paths.
// Call NewWithProjects to include per-project files.
func New() (*Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("claude: cannot determine home directory: %w", err)
	}
	return &Provider{baseDir: filepath.Join(home, ".claude")}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory with
// no project paths. Intended for testing; production code should use New() or
// NewWithProjects().
func NewWithBaseDir(baseDir string) *Provider {
	return &Provider{baseDir: baseDir}
}

// NewWithProjects returns a Provider targeting baseDir for global config and
// the given projectPaths for per-project files. Intended for testing;
// production code should use New() and set ProjectPaths via config.
func NewWithProjects(baseDir string, projectPaths []string) *Provider {
	return &Provider{baseDir: baseDir, projectPaths: projectPaths}
}

// Name returns "claude".
func (p *Provider) Name() string { return "claude" }

// Discover returns the absolute paths of all files managed by this provider.
// If ~/.claude/ does not exist the global section returns nothing — the tool
// is simply not installed on this machine. If ProjectPaths is empty, per-project
// scanning is skipped with a log message.
func (p *Provider) Discover() ([]string, error) {
	var paths []string

	// Global ~/.claude/ files.
	globalPaths, err := p.discoverGlobal()
	if err != nil {
		return nil, err
	}
	paths = append(paths, globalPaths...)

	// Per-project files.
	for _, proj := range p.projectPaths {
		projPaths, err := p.discoverProject(proj)
		if err != nil {
			// Log and continue — one bad project path shouldn't abort the whole backup.
			log.Printf("claude: discover project %s: %v", proj, err)
			continue
		}
		paths = append(paths, projPaths...)
	}

	return paths, nil
}

// discoverGlobal returns absolute paths of allowed files under ~/.claude/.
// Returns nil (not an error) if the directory does not exist.
func (p *Provider) discoverGlobal() ([]string, error) {
	if _, err := os.Lstat(p.baseDir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("claude: stat base dir: %w", err)
	}

	var paths []string
	entries, err := os.ReadDir(p.baseDir)
	if err != nil {
		return nil, fmt.Errorf("claude: read base dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue // all directories under ~/.claude/ are excluded
		}
		// Never follow symlinks.
		info, err := e.Info()
		if err != nil {
			log.Printf("claude: discover global: info %s: %v", e.Name(), err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !globalAllowlist[e.Name()] {
			continue
		}
		paths = append(paths, filepath.Join(p.baseDir, e.Name()))
	}
	return paths, nil
}

// discoverProject returns the per-project files for a single project directory.
// Candidates:
//   - <proj>/CLAUDE.md
//   - <proj>/.claude/settings.json  (NOT settings.local.json)
func (p *Provider) discoverProject(proj string) ([]string, error) {
	// Resolve ~ in the project path.
	proj = expandHome(proj)

	info, err := os.Lstat(proj)
	if os.IsNotExist(err) {
		log.Printf("claude: project path does not exist, skipping: %s", proj)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat project dir %s: %w", proj, err)
	}
	if !info.IsDir() {
		log.Printf("claude: project path is not a directory, skipping: %s", proj)
		return nil, nil
	}

	var paths []string

	// <proj>/CLAUDE.md
	claudeMD := filepath.Join(proj, "CLAUDE.md")
	if fileExists(claudeMD) {
		paths = append(paths, claudeMD)
	}

	// <proj>/.claude/settings.json (NOT settings.local.json)
	settingsJSON := filepath.Join(proj, ".claude", "settings.json")
	if fileExists(settingsJSON) {
		paths = append(paths, settingsJSON)
	}

	return paths, nil
}

// Read returns the current on-disk contents of all discovered files. Global
// files are keyed by path relative to ~/.claude/ (e.g. "CLAUDE.md"). Per-
// project files are keyed by their absolute path to avoid ambiguity across
// multiple projects.
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
			log.Printf("claude: read: skipping %s: %v", abs, err)
			continue
		}
		snapshot[key] = data
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

// Restore writes every file in snapshot back to disk. Global files (keys
// relative to ~/.claude/) are written under baseDir. Per-project files (keys
// that are absolute paths) are written directly to those paths. The allowlist
// is enforced: only known-safe files are ever written.
func (p *Provider) Restore(snapshot map[string][]byte) error {
	for key, data := range snapshot {
		dest := p.absPath(key)
		if dest == "" {
			log.Printf("claude: restore: skipping unrecognised key %q", key)
			continue
		}
		// Guard against path traversal.
		if !isUnder(dest, p.baseDir) && !p.isUnderAProject(dest) {
			log.Printf("claude: restore: rejecting path traversal attempt: %q", key)
			continue
		}
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("claude: restore %s: %w", key, err)
		}
	}
	return nil
}

// relKey converts an absolute discovered path to the snapshot map key.
// Global files get a relative key ("CLAUDE.md"); per-project files keep their
// absolute path as-is to avoid collisions across projects.
func (p *Provider) relKey(abs string) string {
	rel, err := filepath.Rel(p.baseDir, abs)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	// Not under baseDir — must be a per-project file; use absolute path as key.
	return abs
}

// absPath resolves a snapshot key back to an absolute path for writing.
// Returns "" if the key cannot be resolved safely.
func (p *Provider) absPath(key string) string {
	if filepath.IsAbs(key) {
		// Per-project absolute key. Only allowed if it matches a known project file.
		base := filepath.Base(key)
		if base == "CLAUDE.md" || (base == "settings.json" && strings.Contains(key, "/.claude/")) {
			return key
		}
		return ""
	}
	// Global relative key — must be in the allowlist.
	if !globalAllowlist[filepath.Base(key)] {
		return ""
	}
	return filepath.Join(p.baseDir, key)
}

// isUnderAProject reports whether dest is a known per-project file path.
func (p *Provider) isUnderAProject(dest string) bool {
	for _, proj := range p.projectPaths {
		proj = expandHome(proj)
		if isUnder(dest, proj) {
			return true
		}
	}
	return false
}

// isUnder reports whether path is inside (or equal to) the given directory.
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

// fileExists returns true if the path exists and is a regular file (not a dir
// or symlink).
func fileExists(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// atomicWrite creates parent directories then writes data to dest via a
// temporary sibling file, finalising with an atomic rename.
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
