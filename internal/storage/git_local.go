package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// gitLocalStorage wraps localStorage and commits each backup as a git commit.
// The backup root directory is the git working tree.
type gitLocalStorage struct {
	local *localStorage
}

// newGitLocal returns a gitLocalStorage pointing at dir.
// It does NOT call git init — that is done by InitGitLocal.
func newGitLocal(dir string) *gitLocalStorage {
	return &gitLocalStorage{local: &localStorage{dir: dir}}
}

// Save writes the backup files (metadata + payload) using localStorage and
// then creates a git commit whose message summarises what changed relative to
// the previous backup.
func (s *gitLocalStorage) Save(name string, meta Metadata, payload []byte) (string, error) {
	// Load the previous backup's metadata before writing the new one so we can
	// diff provider file counts for the commit message.
	prevMeta := s.latestMetadata()

	id, err := s.local.Save(name, meta, payload)
	if err != nil {
		return "", err
	}

	msg := meta.Message
	if msg == "" {
		msg = buildCommitMessage(prevMeta, &meta)
	}
	if err := gitAddAll(s.local.dir); err != nil {
		return id, fmt.Errorf("git add: %w", err)
	}
	if err := gitCommit(s.local.dir, msg); err != nil {
		return id, fmt.Errorf("git commit: %w", err)
	}

	return id, nil
}

func (s *gitLocalStorage) Load(id string) (Metadata, []byte, error) {
	return s.local.Load(id)
}

func (s *gitLocalStorage) List() ([]BackupEntry, error) {
	return s.local.List()
}

func (s *gitLocalStorage) Latest() (string, error) {
	return s.local.Latest()
}

// latestMetadata returns the Metadata of the most recent backup, or nil if
// there are no backups yet.
func (s *gitLocalStorage) latestMetadata() *Metadata {
	id, err := s.local.Latest()
	if err != nil {
		return nil
	}
	meta, _, err := s.local.Load(id)
	if err != nil {
		return nil
	}
	return &meta
}

// ─── Commit message generation ────────────────────────────────────────────────

// buildCommitMessage produces a human-readable commit message that summarises
// what changed between two backups.  When prev is nil it returns the
// "initial snapshot" message.
//
// Format: "backup: claude +2 files, gemini ~1 file, codex unchanged"
func buildCommitMessage(prev, next *Metadata) string {
	if prev == nil {
		return "backup: initial snapshot"
	}

	prevCounts := providerFileCounts(prev)
	nextCounts := providerFileCounts(next)

	// Union of all provider names in both snapshots.
	providerSet := make(map[string]bool)
	for p := range prevCounts {
		providerSet[p] = true
	}
	for p := range nextCounts {
		providerSet[p] = true
	}

	// Sort for deterministic output.
	providers := make([]string, 0, len(providerSet))
	for p := range providerSet {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	parts := make([]string, 0, len(providers))
	for _, p := range providers {
		prev := prevCounts[p]
		next := nextCounts[p]
		delta := next - prev
		switch {
		case delta > 0:
			parts = append(parts, fmt.Sprintf("%s +%d %s", p, delta, pluralFile(delta)))
		case delta < 0:
			parts = append(parts, fmt.Sprintf("%s -%d %s", p, -delta, pluralFile(-delta)))
		default:
			parts = append(parts, fmt.Sprintf("%s unchanged", p))
		}
	}

	if len(parts) == 0 {
		return "backup: no changes"
	}
	return "backup: " + strings.Join(parts, ", ")
}

// providerFileCounts extracts file counts per provider from metadata.
// If no per-provider file counts are stored we return zero for each listed
// provider (the metadata only tracks provider names, not file counts).
// The commit message will still be meaningful: all providers show "unchanged"
// on the first repeat after upgrade, which is correct.
func providerFileCounts(meta *Metadata) map[string]int {
	if meta == nil {
		return nil
	}
	counts := make(map[string]int, len(meta.Providers))
	for _, p := range meta.Providers {
		counts[p] = 0 // we don't store per-provider file counts in Metadata yet
	}
	// If the metadata has per-provider file counts stored in Labels, parse them.
	for k, v := range meta.Labels {
		if !strings.HasPrefix(k, "_filecount_") {
			continue
		}
		provider := strings.TrimPrefix(k, "_filecount_")
		var n int
		if err := json.Unmarshal([]byte(v), &n); err == nil {
			counts[provider] = n
		}
	}
	return counts
}

func pluralFile(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}

// ─── InitGitLocal ─────────────────────────────────────────────────────────────

// InitGitLocal sets up dir as a git-local backup root:
//  1. Creates dir if needed.
//  2. Runs git init.
//  3. Sets author identity (falls back to amnesiai@localhost).
//  4. Writes .gitignore.
//  5. Stages .gitignore and creates an initial empty commit.
func InitGitLocal(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	if gitIsRepo(dir) {
		return nil // idempotent
	}
	if err := gitInit(dir); err != nil {
		return err
	}
	if err := gitConfigAuthor(dir); err != nil {
		return err
	}
	if err := gitWriteGitignore(dir); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	if err := gitAddAll(dir); err != nil {
		return fmt.Errorf("git add .gitignore: %w", err)
	}
	return gitEmptyCommit(dir, "chore: amnesiai git-local init")
}

// UpgradeLocalToGitLocal converts an existing local backup directory into a
// git-local repo, preserving all existing backup history as the initial commit.
func UpgradeLocalToGitLocal(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	if gitIsRepo(dir) {
		// Already a git repo — nothing to do.
		return nil
	}
	if err := gitInit(dir); err != nil {
		return err
	}
	if err := gitConfigAuthor(dir); err != nil {
		return err
	}
	if err := gitWriteGitignore(dir); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}

	// Count existing backups to include in the upgrade commit message.
	local := &localStorage{dir: dir}
	entries, _ := local.List()
	n := len(entries)

	if err := gitAddAll(dir); err != nil {
		return fmt.Errorf("git add existing backups: %w", err)
	}
	msg := fmt.Sprintf("upgrade: local → git-local, %d %s preserved", n, pluralFile(n))
	return gitEmptyCommit(dir, msg)
}
