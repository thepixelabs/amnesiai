// Package codex implements the amnesiai Provider for OpenAI Codex CLI
// configuration.
//
// Backed-up paths under ~/.codex/:
//   - config.toml         (top-level Codex CLI configuration)
//   - AGENTS.md           (custom instructions; if present)
//   - instructions.md     (legacy instructions file; if present)
//   - agents/*.toml       (user-authored agent definitions)
//   - rules/default.rules (global behavioural rules)
//   - memories/**         (durable Codex memory files)
//   - skills/**           (user-authored skills, EXCLUDING any path under
//     skills/.system/ which ships with the binary and is replaced on update)
//
// Excluded:
//   - auth.json, history.jsonl, sessions/, log/, logs_*.sqlite*,
//     state_*.sqlite*, models_cache.json, installation_id, version.json,
//     .personality_migration, .tmp/, tmp/, shell_snapshots/, cache/, .DS_Store
//   - Any file whose base name ends with ".key", or contains "token",
//     "credential", "auth", or "secret" (case-insensitive). The auth/secret
//     additions match the gemini and copilot defenders for consistency.
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

// defaultTopLevelFiles is the set of file basenames directly under ~/.codex/
// that are in scope. User overrides extend or shrink this set.
var defaultTopLevelFiles = map[string]bool{
	"config.toml":      true,
	"AGENTS.md":        true,
	"instructions.md":  true,
}

// allowedTopLevelDirs is the set of directory names directly under ~/.codex/
// that are walked recursively. Each entry pairs the directory name with a
// per-file predicate. Returning false from the predicate skips the file.
var allowedTopLevelDirs = map[string]func(rel string) bool{
	// agents/<name>.toml — only TOML files.
	"agents": func(rel string) bool {
		return strings.HasSuffix(strings.ToLower(filepath.Base(rel)), ".toml")
	},
	// rules/ — only the canonical default.rules file. The previous
	// implementation hard-coded this; preserve the narrow scope.
	"rules": func(rel string) bool {
		return filepath.Base(rel) == "default.rules"
	},
	// memories/ — every regular file under it (Codex itself decides the
	// layout; memory files are core user-state per OpenAI's docs).
	"memories": func(rel string) bool {
		return !strings.HasPrefix(filepath.Base(rel), ".")
	},
	// skills/ — user-authored skills only. Anything inside .system/ ships
	// with the binary (replaced on update) per project decisions and must be
	// excluded.
	"skills": func(rel string) bool {
		// rel is relative to the codex base dir, e.g. "skills/.system/foo".
		// Reject any path component named ".system" (case-sensitive — the
		// upstream convention).
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if part == ".system" {
				return false
			}
		}
		return !strings.HasPrefix(filepath.Base(rel), ".")
	},
}

// isExcludedFile reports whether a file base name matches the codex exclusion
// rules. Case-insensitive substring match on the basename.
func isExcludedFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".key") {
		return true
	}
	for _, term := range []string{"token", "credential", "auth", "secret"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	// Defensive: explicitly reject sqlite databases that ended up in scope
	// (logs_*.sqlite, state_*.sqlite). They aren't user-authored config and
	// they encode hostnames + timing data we shouldn't ship across machines.
	if strings.HasSuffix(lower, ".sqlite") || strings.Contains(lower, ".sqlite-") {
		return true
	}
	return false
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for OpenAI Codex CLI.
type Provider struct {
	baseDir       string          // absolute path to ~/.codex/
	topLevelFiles map[string]bool // effective top-level file allowlist (defaults +/- overrides)
}

func init() {
	provider.RegisterFactory("codex", func(o provider.ProviderOpts) (provider.Provider, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("codex: cannot determine home directory: %w", err)
		}
		return &Provider{
			baseDir:       filepath.Join(home, ".codex"),
			topLevelFiles: applyOverride(defaultTopLevelFiles, o.Overrides["codex"]),
		}, nil
	})
}

// applyOverride returns a copy of base extended by extras and shrunk by
// excludes. Both lists are case-sensitive basenames.
func applyOverride(base map[string]bool, ov provider.ProviderOverride) map[string]bool {
	out := make(map[string]bool, len(base)+len(ov.ExtraFiles))
	for k, v := range base {
		out[k] = v
	}
	for _, f := range ov.ExtraFiles {
		if f != "" {
			out[f] = true
		}
	}
	for _, f := range ov.ExcludeFiles {
		delete(out, f)
	}
	return out
}

// New returns a new Codex Provider targeting ~/.codex/.
func New() (*Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("codex: cannot determine home directory: %w", err)
	}
	return &Provider{
		baseDir:       filepath.Join(home, ".codex"),
		topLevelFiles: applyOverride(defaultTopLevelFiles, provider.ProviderOverride{}),
	}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory.
// Intended for testing; production code should use New().
func NewWithBaseDir(baseDir string) *Provider {
	return &Provider{
		baseDir:       baseDir,
		topLevelFiles: applyOverride(defaultTopLevelFiles, provider.ProviderOverride{}),
	}
}

// NewWithBaseDirOverrides returns a Provider with the given override applied
// to the default top-level allowlist. Intended for testing the override
// mechanism; production code goes through the registered factory.
func NewWithBaseDirOverrides(baseDir string, ov provider.ProviderOverride) *Provider {
	return &Provider{
		baseDir:       baseDir,
		topLevelFiles: applyOverride(defaultTopLevelFiles, ov),
	}
}

// Name returns "codex".
func (p *Provider) Name() string { return "codex" }

// BaseDir returns the absolute path to ~/.codex/. Used by the restore
// orchestrator to refuse --out-dir values that clash with the real destination.
func (p *Provider) BaseDir() string { return p.baseDir }

// Discover returns absolute paths of all files managed by this provider.
// Returns (nil, nil) if ~/.codex/ does not exist.
//
// Symlink handling: symlinks-to-files are followed (so dotfile-management
// setups capture the target's bytes). Symlinks-to-directories are skipped
// to avoid traversal loops.
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

		// Symlink resolution: follow file targets, skip directory targets.
		linfo, statErr := os.Lstat(path)
		if statErr != nil {
			log.Printf("codex: discover: lstat %s: %v", path, statErr)
			return nil
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			tinfo, terr := os.Stat(path)
			if terr != nil {
				log.Printf("codex: discover: skip broken symlink %s: %v", path, terr)
				return nil
			}
			if tinfo.IsDir() {
				return filepath.SkipDir
			}
			// Symlink-to-file: fall through to per-entry filters below.
		}

		if path == p.baseDir {
			return nil // descend into root
		}

		rel, relErr := filepath.Rel(p.baseDir, path)
		if relErr != nil {
			return nil
		}
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]

		// At the top level there are two valid kinds of entries:
		// (a) files in topLevelFiles, (b) directories in allowedTopLevelDirs.
		if rel == topLevel {
			// We're at depth 1. Decide based on what kind of entry this is.
			if d.IsDir() || (linfo.Mode()&os.ModeSymlink != 0 && isSymlinkToDir(path)) {
				if _, ok := allowedTopLevelDirs[topLevel]; !ok {
					return filepath.SkipDir
				}
				return nil // descend into allowed top-level dir
			}
			// Regular file (or symlink-to-file) at the top level.
			if isExcludedFile(d.Name()) {
				return nil
			}
			if !p.topLevelFiles[d.Name()] {
				return nil
			}
			paths = append(paths, path)
			return nil
		}

		// Below depth 1: enforce the subdir's per-file predicate.
		pred, ok := allowedTopLevelDirs[topLevel]
		if !ok {
			// Defensive — shouldn't reach here because we SkipDir at depth 1.
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// For skills/, reject .system/ subtree without descending.
			if topLevel == "skills" && filepath.Base(path) == ".system" {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcludedFile(d.Name()) {
			return nil
		}
		if !pred(rel) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("codex: walk: %w", err)
	}
	return paths, nil
}

// isSymlinkToDir returns true when path is a symlink whose target is a directory.
func isSymlinkToDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
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

// Restore writes snapshot files back under ~/.codex/. Thin wrapper around
// RestoreTo("", snapshot).
func (p *Provider) Restore(snapshot map[string][]byte) error {
	return p.RestoreTo("", snapshot)
}

// RestoreTo writes snapshot files, enforcing the same allowlist Discover uses.
// When root is empty, files land at their real destinations under p.baseDir;
// otherwise destinations are re-rooted under <root>/<provider-base>/...
func (p *Provider) RestoreTo(root string, snapshot map[string][]byte) error {
	for rel, data := range snapshot {
		name := filepath.Base(rel)

		if isExcludedFile(name) {
			log.Printf("codex: restore: skipping excluded file %s", rel)
			continue
		}

		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if rel == topLevel {
			// Top-level file path.
			if !p.topLevelFiles[name] {
				log.Printf("codex: restore: skipping non-allowlisted top-level file %s", rel)
				continue
			}
		} else {
			// Path inside a subdirectory.
			pred, ok := allowedTopLevelDirs[topLevel]
			if !ok {
				log.Printf("codex: restore: skipping non-allowlisted subdir %s", rel)
				continue
			}
			if !pred(rel) {
				log.Printf("codex: restore: skipping non-matching file under %s/: %s",
					topLevel, rel)
				continue
			}
		}

		dest := filepath.Join(p.baseDir, rel)
		// Path-traversal check runs against the real baseDir, before rerooting.
		if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator),
			filepath.Clean(p.baseDir)+string(filepath.Separator)) {
			log.Printf("codex: restore: rejecting path traversal attempt: %s", rel)
			continue
		}
		dest = rerootPath(root, dest)
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("codex: restore %s: %w", rel, err)
		}
	}
	return nil
}

// rerootPath re-roots an absolute destination under root. Returns dest
// unchanged when root is empty.
func rerootPath(root, dest string) string {
	if root == "" {
		return dest
	}
	clean := filepath.Clean(dest)
	if filepath.IsAbs(clean) {
		return filepath.Join(root, clean[1:])
	}
	return filepath.Join(root, clean)
}

// atomicWrite creates parent directories then writes data to dest atomically.
//
// Symlink preservation: if dest itself is a symlink (the user dotfile-manages
// their codex config), we resolve the link and write to the underlying target
// rather than replacing the link with a regular file. This matches our
// Discover behaviour, which follows file-target symlinks.
func atomicWrite(dest string, data []byte) error {
	if info, err := os.Lstat(dest); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if resolved, evErr := filepath.EvalSymlinks(dest); evErr == nil {
			dest = resolved
		}
	}

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
