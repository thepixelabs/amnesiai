// Package config — app-owned state file at ~/.amnesiai/state.json.
//
// state.json is distinct from config.toml:
//   - config.toml is user-owned (settings the user deliberately configures).
//   - state.json is app-owned (runtime bookkeeping amnesiai writes on its own).
//
// Atomic write protocol: write to ~/.amnesiai/.tmp/state.json.tmp then
// os.Rename into place.  On POSIX the rename is atomic within the same
// filesystem, so readers never see a partial file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateSchemaVersion = 1

// RemoteBinding records which host account is bound to a remote repo URL.
// Track F writes to this via BindRemote; the wizard reads it to show
// existing bindings.
type RemoteBinding struct {
	Host        string    `json:"host"`          // "github" | "gitlab"
	Account     string    `json:"account"`       // username
	LastBoundAt time.Time `json:"last_bound_at"` // RFC3339
}

// State is the app-owned runtime state persisted to ~/.amnesiai/state.json.
type State struct {
	SchemaVersion            int                      `json:"schema_version"`
	RemoteBindings           map[string]RemoteBinding `json:"remote_bindings"`
	OnboardingLastSeenVersion string                  `json:"onboarding_last_seen_version"`
}

// defaultState returns a zero-value State with the current schema version.
func defaultState() *State {
	return &State{
		SchemaVersion:  stateSchemaVersion,
		RemoteBindings: make(map[string]RemoteBinding),
	}
}

// stateFilePath returns the path to the state file.
func stateFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

// LoadState reads state.json from disk.  If the file does not exist a
// default State is returned — callers should treat this the same as an
// existing file with empty values.  Parse errors are propagated.
func LoadState() (*State, error) {
	path, err := stateFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultState(), nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}

	// Schema version guard: refuse to read state written by a newer client so
	// we don't silently truncate fields we don't understand on the next Save().
	if s.SchemaVersion > stateSchemaVersion {
		return nil, fmt.Errorf("state.json schema v%d is newer than supported v%d; upgrade amnesiai", s.SchemaVersion, stateSchemaVersion)
	}
	// Treat schema_version < 1 (missing or zeroed) as a fresh default.
	if s.SchemaVersion < 1 {
		return defaultState(), nil
	}

	// Guarantee the map is non-nil even for old state files that omitted the field.
	if s.RemoteBindings == nil {
		s.RemoteBindings = make(map[string]RemoteBinding)
	}

	return &s, nil
}

// Save atomically writes the state to ~/.amnesiai/state.json.
// The write goes to ~/.amnesiai/.tmp/state.json.tmp first, then
// os.Rename moves it into place so readers never see a partial file.
func (s *State) Save() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}

	// Ensure both the main dir and the .tmp subdir exist.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}
	tmpDir := filepath.Join(dir, ".tmp")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return fmt.Errorf("cannot create temp directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmpPath := filepath.Join(tmpDir, "state.json.tmp")
	finalPath := filepath.Join(dir, "state.json")

	// Write to temp file with restrictive permissions.
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}

	// Atomic rename into final position.
	if err := os.Rename(tmpPath, finalPath); err != nil {
		// Clean up the temp file on rename failure.
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename state file: %w", err)
	}

	return nil
}

// LookupBinding returns the RemoteBinding for repoURL, if one exists.
// Callers should use this rather than reading RemoteBindings directly.
func (s *State) LookupBinding(repoURL string) (RemoteBinding, bool) {
	b, ok := s.RemoteBindings[repoURL]
	return b, ok
}

// BindRemote records (or updates) the host+account binding for repoURL.
// Exported so Track F (storage/git*.go, internal/remote/*) can call it
// without importing an internal/state package.
func (s *State) BindRemote(repoURL, host, account string) error {
	if repoURL == "" {
		return fmt.Errorf("repoURL must not be empty")
	}
	switch host {
	case "github", "gitlab":
		// ok
	default:
		return fmt.Errorf("host must be \"github\" or \"gitlab\", got %q", host)
	}
	if account == "" {
		return fmt.Errorf("account must not be empty")
	}

	s.RemoteBindings[repoURL] = RemoteBinding{
		Host:        host,
		Account:     account,
		LastBoundAt: time.Now().UTC(),
	}
	return nil
}
