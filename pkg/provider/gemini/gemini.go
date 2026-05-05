// Package gemini implements the amnesiai Provider for Gemini CLI configuration.
//
// Backed-up paths under ~/.gemini/:
//   - settings.json        (model prefs, MCP config, custom instructions)
//   - GEMINI.md            (global custom instructions)
//   - projects.json        (which project paths the user has trusted)
//   - trustedFolders.json  (per-folder trust)
//   - themes/              (user-authored UI themes; recursive)
//   - agents/              (user-authored subagent definitions; recursive)
//   - commands/            (user-authored slash commands; recursive)
//   - extensions/          (user-installed CLI extensions; recursive)
//
// Excluded:
//   - oauth_creds.json, google_accounts.json, installation_id, state.json,
//     antigravity*/, tmp/, history/, projects.json.lock/, acknowledgments/
//   - Any file whose base name ends with ".key", or starts with "auth"
//     (case-insensitive), or contains "oauth", "creds", "credential",
//     "token", or "secret" (case-insensitive). This matches the credential
//     filters the codex / copilot providers apply, for defense-in-depth.
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

// defaultTopLevelFiles is the set of file basenames directly under ~/.gemini/
// that are in scope. Per-user overrides extend or shrink this set.
var defaultTopLevelFiles = map[string]bool{
	"settings.json":       true,
	"GEMINI.md":           true,
	"projects.json":       true,
	"trustedFolders.json": true,
}

// allowedTopLevelDirs is the set of directories beneath ~/.gemini/ that are
// walked recursively. Each entry pairs the directory name with a per-file
// predicate (returning false skips the file).
var allowedTopLevelDirs = map[string]func(rel string) bool{
	"themes":     notHidden,
	"agents":     notHidden,
	"commands":   notHidden,
	"extensions": notHidden,
}

// notHidden returns true for any basename that doesn't begin with ".".
func notHidden(rel string) bool {
	return !strings.HasPrefix(filepath.Base(rel), ".")
}

// isCredentialFile returns true for any file basename that looks like a
// credential file. Compared case-insensitively against the basename.
func isCredentialFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".key") {
		return true
	}
	if strings.HasPrefix(lower, "auth") {
		return true
	}
	for _, term := range []string{"oauth", "creds", "credential", "token", "secret"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for Gemini CLI.
type Provider struct {
	baseDir       string          // absolute path to ~/.gemini/
	topLevelFiles map[string]bool // effective top-level file allowlist (defaults +/- overrides)
}

func init() {
	provider.RegisterFactory("gemini", func(o provider.ProviderOpts) (provider.Provider, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("gemini: cannot determine home directory: %w", err)
		}
		return &Provider{
			baseDir:       filepath.Join(home, ".gemini"),
			topLevelFiles: applyOverride(defaultTopLevelFiles, o.Overrides["gemini"]),
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

// New returns a new Gemini Provider targeting ~/.gemini/.
func New() (*Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("gemini: cannot determine home directory: %w", err)
	}
	return &Provider{
		baseDir:       filepath.Join(home, ".gemini"),
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

// Name returns "gemini".
func (p *Provider) Name() string { return "gemini" }

// BaseDir returns the absolute path to ~/.gemini/. Used by the restore
// orchestrator to refuse --out-dir values that clash with the real destination.
func (p *Provider) BaseDir() string { return p.baseDir }

// Discover returns the absolute paths of all files managed by this provider.
// Returns (nil, nil) if ~/.gemini/ does not exist.
//
// Symlink handling: symlinks-to-files are followed; symlinks-to-directories
// are skipped to avoid traversal loops.
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

		linfo, statErr := os.Lstat(path)
		if statErr != nil {
			log.Printf("gemini: discover: lstat %s: %v", path, statErr)
			return nil
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			tinfo, terr := os.Stat(path)
			if terr != nil {
				log.Printf("gemini: discover: skip broken symlink %s: %v", path, terr)
				return nil
			}
			if tinfo.IsDir() {
				return filepath.SkipDir
			}
			// Symlink-to-file: fall through to per-entry filters.
		}

		if path == p.baseDir {
			return nil // descend into root
		}

		rel, relErr := filepath.Rel(p.baseDir, path)
		if relErr != nil {
			return nil
		}
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]

		// Depth-1 entries: file-or-directory dispatch.
		if rel == topLevel {
			if d.IsDir() || (linfo.Mode()&os.ModeSymlink != 0 && isSymlinkToDir(path)) {
				if _, ok := allowedTopLevelDirs[topLevel]; !ok {
					return filepath.SkipDir
				}
				return nil // descend
			}
			if isCredentialFile(d.Name()) {
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
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if isCredentialFile(d.Name()) {
			return nil
		}
		if !pred(rel) {
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

// isSymlinkToDir returns true when path is a symlink whose target is a directory.
func isSymlinkToDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
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
// Thin wrapper around RestoreTo("", snapshot).
func (p *Provider) Restore(snapshot map[string][]byte) error {
	return p.RestoreTo("", snapshot)
}

// RestoreTo writes snapshot files. When root is empty, files land at their
// real destinations under p.baseDir. When root is non-empty, the destination
// is re-rooted under <root>/<provider-base>/... so users can inspect what
// would be overwritten.
func (p *Provider) RestoreTo(root string, snapshot map[string][]byte) error {
	for rel, data := range snapshot {
		base := filepath.Base(rel)
		if isCredentialFile(base) {
			log.Printf("gemini: restore: skipping credential file %s", rel)
			continue
		}

		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if rel == topLevel {
			if !p.topLevelFiles[base] {
				log.Printf("gemini: restore: skipping non-allowlisted top-level file %s", rel)
				continue
			}
		} else {
			pred, ok := allowedTopLevelDirs[topLevel]
			if !ok {
				log.Printf("gemini: restore: skipping non-allowlisted subdir %s", rel)
				continue
			}
			if !pred(rel) {
				log.Printf("gemini: restore: skipping non-matching file under %s/: %s",
					topLevel, rel)
				continue
			}
		}

		dest := filepath.Join(p.baseDir, rel)
		// Path-traversal check is against the real baseDir, before any rerooting.
		if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator),
			filepath.Clean(p.baseDir)+string(filepath.Separator)) {
			log.Printf("gemini: restore: rejecting path traversal attempt: %s", rel)
			continue
		}
		dest = rerootPath(root, dest)
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("gemini: restore %s: %w", rel, err)
		}
	}
	return nil
}

// rerootPath re-roots an absolute destination under root. When root is empty
// dest is returned unchanged.
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
// Symlink preservation: if dest is a symlink, we resolve it and write to the
// underlying target instead of replacing the link with a regular file. This
// matches the discovery behaviour, which follows file-target symlinks.
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
