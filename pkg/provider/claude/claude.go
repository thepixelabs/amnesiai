// Package claude implements the amnesiai Provider for Claude Code configuration.
//
// Global files backed up from ~/.claude/:
//   - CLAUDE.md
//   - settings.json
//   - keybindings.json  (if present)
//
// Global directories backed up from ~/.claude/ (recursively, *.md files only):
//   - agents/      user-authored subagents
//   - commands/    user-authored slash commands
//   - skills/      user-authored agent skills (every regular file under each skill dir)
//
// Per-project files backed up for each path in ProjectPaths:
//   - <project>/CLAUDE.md
//   - <project>/.claude/settings.json  (NOT settings.local.json — machine-specific)
//
// Everything else under ~/.claude/ is intentionally ignored. Notably skipped:
// projects/ (conversation history), todos/, ide/, statsig/, plugins/ (re-
// installable from marketplace), sessions/, paste-cache/, file-history/,
// shell-snapshots/, telemetry/, history.jsonl, .credentials.json.
//
// Symlinks-to-files are followed so dotfile-management setups (where the user
// symlinks CLAUDE.md/settings.json into ~/.claude/) are correctly captured.
// Symlinks-to-directories are skipped to avoid traversal loops.
package claude

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

// defaultGlobalAllowlist is the set of file basenames directly under ~/.claude/
// that are in scope for backup. User-supplied ExtraFiles add to this set,
// ExcludeFiles remove from it.
var defaultGlobalAllowlist = map[string]bool{
	"CLAUDE.md":        true,
	"settings.json":    true,
	"keybindings.json": true,
}

// allowedSubdirs is the set of subdirectories beneath ~/.claude/ that are
// walked recursively. Each entry pairs the directory name with a per-file
// predicate; files that fail the predicate are skipped.
var allowedSubdirs = map[string]func(name string) bool{
	"agents":   isMarkdown,
	"commands": isMarkdown,
	// skills/<name>/ may contain SKILL.md plus assets (scripts, README.md,
	// resource files). Take everything that isn't an obvious cache or hidden
	// file — Anthropic doesn't currently document hidden cache files under
	// skills/, but we exclude dot-prefixed entries defensively.
	"skills": func(name string) bool {
		return !strings.HasPrefix(name, ".")
	},
}

func isMarkdown(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".md")
}

// Compile-time assertion: *Provider must satisfy the provider.Provider interface.
var _ provider.Provider = (*Provider)(nil)

// Provider implements provider.Provider for Claude Code.
type Provider struct {
	baseDir         string          // absolute path to ~/.claude/
	projectPaths    []string        // absolute paths to per-project directories to scan
	globalAllowlist map[string]bool // effective top-level allowlist (defaults +/- overrides)
}

func init() {
	provider.RegisterFactory("claude", func(o provider.ProviderOpts) (provider.Provider, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("claude: cannot determine home directory: %w", err)
		}
		baseDir := filepath.Join(home, ".claude")
		return &Provider{
			baseDir:         baseDir,
			projectPaths:    o.ProjectPaths,
			globalAllowlist: applyOverride(defaultGlobalAllowlist, o.Overrides["claude"]),
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

// New returns a new Claude Provider targeting ~/.claude/ with no project paths.
// Call NewWithProjects to include per-project files.
func New() (*Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("claude: cannot determine home directory: %w", err)
	}
	return &Provider{
		baseDir:         filepath.Join(home, ".claude"),
		globalAllowlist: applyOverride(defaultGlobalAllowlist, provider.ProviderOverride{}),
	}, nil
}

// NewWithBaseDir returns a Provider targeting an explicit base directory with
// no project paths. Intended for testing; production code should use New() or
// NewWithProjects().
func NewWithBaseDir(baseDir string) *Provider {
	return &Provider{
		baseDir:         baseDir,
		globalAllowlist: applyOverride(defaultGlobalAllowlist, provider.ProviderOverride{}),
	}
}

// NewWithProjects returns a Provider targeting baseDir for global config and
// the given projectPaths for per-project files. Intended for testing;
// production code should use New() and set ProjectPaths via config.
func NewWithProjects(baseDir string, projectPaths []string) *Provider {
	return &Provider{
		baseDir:         baseDir,
		projectPaths:    projectPaths,
		globalAllowlist: applyOverride(defaultGlobalAllowlist, provider.ProviderOverride{}),
	}
}

// Name returns "claude".
func (p *Provider) Name() string { return "claude" }

// BaseDir returns the absolute path to ~/.claude/. Used by the restore
// orchestrator to refuse --out-dir values that clash with the real destination.
func (p *Provider) BaseDir() string { return p.baseDir }

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
//
// Top-level files are matched against globalAllowlist. Top-level directories
// in allowedSubdirs are walked recursively; their files are matched against
// the subdir's per-file predicate. Symlinks-to-files are followed so the
// linked content is captured. Symlinks-to-dirs are skipped to avoid loops.
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
		name := e.Name()
		full := filepath.Join(p.baseDir, name)

		// os.Stat follows symlinks — we want to backup the *target's* contents
		// when a config file is symlinked (common dotfile-management pattern).
		info, err := os.Stat(full)
		if err != nil {
			log.Printf("claude: discover global: stat %s: %v", name, err)
			continue
		}

		if info.IsDir() {
			pred, ok := allowedSubdirs[name]
			if !ok {
				continue
			}
			subPaths, err := walkAllowedSubdir(full, pred)
			if err != nil {
				log.Printf("claude: discover %s/: %v", name, err)
				continue
			}
			paths = append(paths, subPaths...)
			continue
		}

		if !p.globalAllowlist[name] {
			continue
		}
		paths = append(paths, full)
	}
	return paths, nil
}

// walkAllowedSubdir walks root recursively and returns absolute paths of
// regular files (or symlinks-to-files) whose basename satisfies pred.
// Symlinks-to-directories are skipped to avoid traversal loops.
func walkAllowedSubdir(root string, pred func(name string) bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("claude: walk %s: %v", path, walkErr)
			return nil
		}

		// Resolve symlinks: follow file targets, skip directory targets.
		linfo, lstatErr := os.Lstat(path)
		if lstatErr != nil {
			return nil
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			tinfo, terr := os.Stat(path)
			if terr != nil {
				log.Printf("claude: skip broken symlink %s: %v", path, terr)
				return nil
			}
			if tinfo.IsDir() {
				return filepath.SkipDir
			}
			// Symlink-to-file: fall through to predicate check below.
			if pred(d.Name()) {
				out = append(out, path)
			}
			return nil
		}

		if d.IsDir() {
			return nil // descend
		}
		if pred(d.Name()) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
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
	if fileOrSymlinkToFileExists(claudeMD) {
		paths = append(paths, claudeMD)
	}

	// <proj>/.claude/settings.json (NOT settings.local.json)
	settingsJSON := filepath.Join(proj, ".claude", "settings.json")
	if fileOrSymlinkToFileExists(settingsJSON) {
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

// Restore writes every file in snapshot back to disk at its real destination.
// Thin wrapper around RestoreTo("", snapshot).
func (p *Provider) Restore(snapshot map[string][]byte) error {
	return p.RestoreTo("", snapshot)
}

// RestoreTo writes snapshot files. When root is empty, files land at their real
// destinations. When root is non-empty, every destination is re-rooted under
// <root> (see rerootPath). The allowlist is enforced; only known-safe files
// are ever written.
func (p *Provider) RestoreTo(root string, snapshot map[string][]byte) error {
	for key, data := range snapshot {
		dest := p.absPath(key)
		if dest == "" {
			log.Printf("claude: restore: skipping unrecognised key %q", key)
			continue
		}
		// Path-traversal check is against the real layout (pre-reroot); only
		// known good destinations get re-rooted into out-dir.
		if !isUnder(dest, p.baseDir) && !p.isUnderAProject(dest) {
			log.Printf("claude: restore: rejecting path traversal attempt: %q", key)
			continue
		}
		dest = p.rerootPath(root, dest)
		if err := atomicWrite(dest, data); err != nil {
			return fmt.Errorf("claude: restore %s: %w", key, err)
		}
	}
	return nil
}

// rerootPath returns dest unchanged when root is empty. Otherwise it joins
// dest under root, preserving the absolute layout so the user can see exactly
// which real path each file would overwrite (e.g. /Users/x/code/foo/CLAUDE.md
// → <root>/Users/x/code/foo/CLAUDE.md).
func (p *Provider) rerootPath(root, dest string) string {
	if root == "" {
		return dest
	}
	clean := filepath.Clean(dest)
	if filepath.IsAbs(clean) {
		// Strip the leading separator so filepath.Join doesn't drop the prefix.
		return filepath.Join(root, clean[1:])
	}
	return filepath.Join(root, clean)
}

// relKey converts an absolute discovered path to the snapshot map key.
// Global files get a relative key ("CLAUDE.md", "agents/foo.md"); per-project
// files keep their absolute path as-is to avoid collisions across projects.
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
	// Global relative key. Two shapes are valid:
	//   1. Top-level allowlisted file (e.g. "CLAUDE.md").
	//   2. File inside an allowed subdirectory whose basename satisfies the
	//      subdir's predicate (e.g. "agents/foo.md", "skills/x/SKILL.md").
	parts := strings.SplitN(key, string(filepath.Separator), 2)
	if len(parts) == 1 {
		if !p.globalAllowlist[key] {
			return ""
		}
		return filepath.Join(p.baseDir, key)
	}
	pred, ok := allowedSubdirs[parts[0]]
	if !ok || !pred(filepath.Base(key)) {
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

// fileOrSymlinkToFileExists returns true if the path exists AND, after
// following symlinks, refers to a regular file (not a directory).
func fileOrSymlinkToFileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// atomicWrite creates parent directories then writes data to dest via a
// temporary sibling file, finalising with an atomic rename.
//
// Symlink preservation: if dest itself is a symlink, the rename in the
// straightforward implementation would replace the symlink with a regular
// file — silently breaking the user's dotfile-management setup. Instead we
// resolve dest through EvalSymlinks and write to the underlying target.
// This is consistent with our discovery behaviour, which already follows
// symlinks to capture the target's contents.
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
