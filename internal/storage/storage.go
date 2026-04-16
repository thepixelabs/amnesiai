// Package storage defines the Storage interface and provides a factory function
// for creating storage backends (local, git-local, git-remote).
package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ErrNoBackups is returned when no backups exist in the storage.
var ErrNoBackups = errors.New("no backups found")

// Metadata holds information about a single backup.
type Metadata struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Providers []string          `json:"providers"`
	GitCommit string            `json:"git_commit,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// BackupEntry is a summary of a backup for listing purposes.
type BackupEntry struct {
	ID        string
	Timestamp time.Time
	Providers []string
}

// Storage is the interface for backup storage backends.
type Storage interface {
	// Save writes payload bytes for a named backup; returns the backup ID.
	Save(name string, metadata Metadata, payload []byte) (id string, err error)

	// Load retrieves a backup by ID.
	Load(id string) (metadata Metadata, payload []byte, err error)

	// List returns all known backup IDs sorted newest-first.
	List() ([]BackupEntry, error)

	// Latest returns the most recent backup ID, or ErrNoBackups.
	Latest() (string, error)
}

// New creates a Storage implementation for the given mode and backup directory.
// "git-local" and "git-remote" are planned for a future release; passing them
// returns an explicit error rather than silently falling back to local-only
// storage (which would give users a false sense that backups are being committed
// or pushed when they are not).
func New(mode string, backupDir string) (Storage, error) {
	switch mode {
	case "local":
		return &localStorage{dir: backupDir}, nil
	case "git-local", "git-remote":
		return nil, fmt.Errorf("storage mode %q is not yet implemented; use \"local\" for now", mode)
	default:
		return nil, fmt.Errorf("unsupported storage mode: %q", mode)
	}
}

// localStorage stores backups as files under a directory tree:
//
//	<backupDir>/<id>/metadata.json
//	<backupDir>/<id>/payload.bin
type localStorage struct {
	dir string
}

func (s *localStorage) Save(name string, meta Metadata, payload []byte) (string, error) {
	if meta.ID == "" {
		meta.ID = time.Now().UTC().Format("20060102T150405Z")
	}
	if meta.Timestamp.IsZero() {
		meta.Timestamp = time.Now().UTC()
	}

	backupPath := filepath.Join(s.dir, meta.ID)
	if err := os.MkdirAll(backupPath, 0700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}

	// Write metadata.
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "metadata.json"), metaBytes, 0600); err != nil {
		return "", fmt.Errorf("write metadata: %w", err)
	}

	// Write payload.
	if err := os.WriteFile(filepath.Join(backupPath, "payload.bin"), payload, 0600); err != nil {
		return "", fmt.Errorf("write payload: %w", err)
	}

	return meta.ID, nil
}

func (s *localStorage) Load(id string) (Metadata, []byte, error) {
	backupPath := filepath.Join(s.dir, id)

	metaBytes, err := os.ReadFile(filepath.Join(backupPath, "metadata.json"))
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("read metadata: %w", err)
	}

	var meta Metadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return Metadata{}, nil, fmt.Errorf("parse metadata: %w", err)
	}

	payload, err := os.ReadFile(filepath.Join(backupPath, "payload.bin"))
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("read payload: %w", err)
	}

	return meta, payload, nil
}

func (s *localStorage) List() ([]BackupEntry, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}

	var backups []BackupEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.dir, entry.Name(), "metadata.json")
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			continue // skip dirs without metadata
		}
		var meta Metadata
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			continue
		}
		backups = append(backups, BackupEntry{
			ID:        meta.ID,
			Timestamp: meta.Timestamp,
			Providers: meta.Providers,
		})
	}

	// Sort newest-first.
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

func (s *localStorage) Latest() (string, error) {
	entries, err := s.List()
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", ErrNoBackups
	}
	return entries[0].ID, nil
}
