// Package copilot implements the amnesiai Provider for the GitHub Copilot CLI.
//
// As of the 2026 Copilot CLI release the configuration directory is
// `~/.copilot/` on every platform; the location can be overridden via the
// COPILOT_HOME environment variable. Earlier VS Code-bundled Copilot kept
// state under `~/Library/Application Support/GitHub Copilot/` (macOS) and
// equivalent locations elsewhere; that path is no longer in scope. Auth
// tokens are kept in the OS keychain, never on disk.
//
// Backed-up files (top-level under the config dir):
//   - settings.json     user-editable settings (themes, defaults, etc.)
//   - config.json       app state including trustedFolders + allowed_urls
//   - mcp-config.json   MCP server configuration (Bearer headers handled by
//     the secret scanner; not excluded by name)
//   - lsp-config.json   Language Server Protocol configuration
//
// Backed-up subdirectories (recursive):
//   - agents/   user-authored custom agents (`*.agent.md`)
//
// Per-project files (one path in ProjectPaths per project):
//   - <project>/.github/copilot-instructions.md
//
// Excluded:
//   - command-history-state.json (chat / command history)
//   - logs/, session-state/, ide/ (machine-local runtime state)
//   - skills/, hooks/ (executable code; out of scope for v1)
//   - Any file whose base name contains "token", "secret", "key",
//     "credential", or "auth" (case-insensitive).
package copilot

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

// defaultTopLevelFiles is the set of file basenames directly under the
// Copilot config dir that are in scope. User overrides extend or shrink it.
var defaultTopLevelFiles = map[string]bool{
	"settings.json":   true,
	"config.json":     true,
	"mcp-config.json": true,
	"lsp-config.json": true,
}

// allowedTopLevelDirs is the set of subdirectories that are walked
// recursively. Each entry pairs the directory name with a per-file predicate.
var allowedTopLevelDirs = map[string]func(rel string) bool{
	// agents/ — user-authored custom agents end in `.agent.md` per the
	// upstream convention; accept any markdown file defensively.
	"agents": func(rel string) bool {
		base := filepath.Base(rel)
		return strings.HasSuffix(strings.ToLower(base), ".md")
	},
}

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
// term. mcp-config.json does NOT contain any of these terms; its Bearer
// headers are caught by the secret scanner instead.
func isSensitiveFile(name string) bool {
	lower := strings.ToLower(name)
	for _, term := range sensitiveTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// baseDir returns the Copilot config directory. COPILOT_HOME wins when set;
// otherwise we use ~/.copilot/ regardless of OS, matching the upstream CLI.
func baseDir() (string, error) {
	if v := os.Getenv("COPILOT_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("copilot: home dir: %w", err)
	}
	return filepath.Join(home, ".copilot"), nil
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for GitHub Copilot CLI.
type Provider struct {
	base          string          // absolute config directory
	projectPaths  []string        // per-project directories to scan for copilot-instructions.md
	topLevelFiles map[string]bool // effective top-level file allowlist (defaults +/- overrides)
}

func init() {
	provider.RegisterFactory("copilot", func(o provider.ProviderOpts) (provider.Provider, error) {
		dir, err := baseDir()
		if err != nil {
			return nil, err
		}
		return &Provider{
			base:          dir,
			projectPaths:  o.ProjectPaths,
			topLevelFiles: applyOverride(defaultTopLevelFiles, o.Overrides["copilot"]),
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

// New returns a new Copilot Provider with no project paths.
func New() (*Provider, error) {
	dir, err := baseDir()
	if err != nil {
		return nil, err
	}
	return &Provider{
		base:          dir,
		topLevelFiles: applyOverride(defaultTopLevelFiles, provider.ProviderOverride{}),
	}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory with
// no project paths. Intended for testing; production code should use New().
func NewWithBaseDir(base string) *Provider {
	return &Provider{
		base:          base,
		topLevelFiles: applyOverride(defaultTopLevelFiles, provider.ProviderOverride{}),
	}
}

// NewWithProjects returns a Provider targeting an explicit base directory and
// the given project paths. Intended for testing; production code should use
// New() and set ProjectPaths via config.
func NewWithProjects(base string, projectPaths []string) *Provider {
	return &Provider{
		base:          base,
		projectPaths:  projectPaths,
		topLevelFiles: applyOverride(defaultTopLevelFiles, provider.ProviderOverride{}),
	}
}

// Name returns "copilot".
func (p *Provider) Name() string { return "copilot" }

// BaseDir returns the absolute config dir. Used by the restore orchestrator
// to refuse --out-dir values that clash with it.
func (p *Provider) BaseDir() string { return p.base }

// Discover returns absolute paths of all in-scope files. Returns (nil, nil)
// for the global section if the directory does not exist.
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

// discoverGlobal returns in-scope files from the Copilot config dir.
//
// Symlink handling: file-target symlinks are followed; directory-target
// symlinks are skipped to avoid traversal loops.
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

		linfo, statErr := os.Lstat(path)
		if statErr != nil {
			log.Printf("copilot: discover: lstat %s: %v", path, statErr)
			return nil
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			tinfo, terr := os.Stat(path)
			if terr != nil {
				log.Printf("copilot: discover: skip broken symlink %s: %v", path, terr)
				return nil
			}
			if tinfo.IsDir() {
				return filepath.SkipDir
			}
			// Symlink-to-file: fall through to per-entry filters.
		}

		if path == p.base {
			return nil // descend into root
		}

		rel, relErr := filepath.Rel(p.base, path)
		if relErr != nil {
			return nil
		}
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]

		// Depth-1 dispatch.
		if rel == topLevel {
			if d.IsDir() || (linfo.Mode()&os.ModeSymlink != 0 && isSymlinkToDir(path)) {
				if _, ok := allowedTopLevelDirs[topLevel]; !ok {
					return filepath.SkipDir
				}
				return nil
			}
			if isSensitiveFile(d.Name()) {
				return nil
			}
			if !p.topLevelFiles[d.Name()] {
				return nil
			}
			paths = append(paths, path)
			return nil
		}

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
		if isSensitiveFile(d.Name()) {
			return nil
		}
		if !pred(rel) {
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

func isSymlinkToDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
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
	if fileOrSymlinkToFileExists(instructions) {
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
// keys, and to the absolute path for per-project keys. Thin wrapper around
// RestoreTo("", snapshot).
func (p *Provider) Restore(snapshot map[string][]byte) error {
	return p.RestoreTo("", snapshot)
}

// RestoreTo writes snapshot files. When root is empty files land at their real
// destinations; otherwise destinations are re-rooted under <root>. Sensitive-
// named files are always skipped.
func (p *Provider) RestoreTo(root string, snapshot map[string][]byte) error {
	for key, data := range snapshot {
		if isSensitiveFile(filepath.Base(key)) {
			log.Printf("copilot: restore: skipping sensitive file %s", key)
			continue
		}

		dest := p.absPath(key)
		if dest == "" {
			log.Printf("copilot: restore: skipping unrecognised key %q", key)
			continue
		}

		if !isUnder(dest, p.base) && !p.isUnderAProject(dest) {
			log.Printf("copilot: restore: rejecting path traversal attempt: %q", key)
			continue
		}

		dest = p.rerootPath(root, dest)
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("copilot: restore %s: %w", key, err)
		}
	}
	return nil
}

// rerootPath re-roots an absolute destination under root. Returns dest
// unchanged when root is empty.
func (p *Provider) rerootPath(root, dest string) string {
	if root == "" {
		return dest
	}
	clean := filepath.Clean(dest)
	if filepath.IsAbs(clean) {
		return filepath.Join(root, clean[1:])
	}
	return filepath.Join(root, clean)
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
	// Global key. Two valid shapes: top-level allowlisted file, or a path
	// inside an allowed subdirectory whose basename satisfies its predicate.
	parts := strings.SplitN(key, string(filepath.Separator), 2)
	if len(parts) == 1 {
		if !p.topLevelFiles[key] {
			return ""
		}
		return filepath.Join(p.base, key)
	}
	pred, ok := allowedTopLevelDirs[parts[0]]
	if !ok || !pred(key) {
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

// fileOrSymlinkToFileExists returns true if the path refers to a regular file
// after symlink resolution.
func fileOrSymlinkToFileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
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
