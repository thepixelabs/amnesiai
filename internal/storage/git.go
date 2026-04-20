// Package storage — git.go
//
// Shared git helpers used by both gitLocalStorage and gitRemoteStorage.
// All git operations shell out to the system `git` binary; go-git is not used.
package storage

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitRun executes a git command inside dir and returns combined output.
// Env additions are appended to the process environment.
func gitRun(dir string, extraEnv []string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// gitInit runs `git init` inside dir, creating the repo if it does not exist.
func gitInit(dir string) error {
	_, err := gitRun(dir, nil, "init", "-q")
	return err
}

// gitConfigAuthor writes local user.name and user.email to the repo config
// only if the global config does not already have values for them.
// Falls back to "amnesiai <amnesiai@localhost>".
func gitConfigAuthor(dir string) error {
	name := gitGlobalConfig("user.name")
	email := gitGlobalConfig("user.email")
	if name == "" {
		name = "amnesiai"
	}
	if email == "" {
		email = "amnesiai@localhost"
	}
	if _, err := gitRun(dir, nil, "config", "user.name", name); err != nil {
		return err
	}
	if _, err := gitRun(dir, nil, "config", "user.email", email); err != nil {
		return err
	}
	return nil
}

// gitGlobalConfig reads a single git config value from global scope.
// Returns empty string if the key is unset.
func gitGlobalConfig(key string) string {
	out, err := exec.Command("git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitAddAll stages all files in the working tree.
func gitAddAll(dir string) error {
	_, err := gitRun(dir, nil, "add", "-A")
	return err
}

// gitCommit creates a commit with the given message.
// If nothing is staged it does nothing (treats clean index as success).
func gitCommit(dir, message string) error {
	// Check if there is anything to commit.
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err == nil && len(strings.TrimSpace(string(out))) == 0 {
		return nil // nothing to commit
	}
	_, err = gitRun(dir, nil, "commit", "-m", message, "--allow-empty-message=false", "--no-gpg-sign")
	return err
}

// gitWriteGitignore writes a minimal .gitignore into the repo root that
// excludes the .tmp/ directory used by amnesiai during backup assembly.
func gitWriteGitignore(dir string) error {
	content := "# amnesiai — temporary files\n.tmp/\n"
	path := dir + "/.gitignore"
	// Only write if it doesn't already exist; don't overwrite user additions.
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// gitEmptyCommit creates a commit with no files (used as the initial commit
// when setting up the backup repo so there is always a parent to diff against).
func gitEmptyCommit(dir, message string) error {
	_, err := gitRun(dir, nil, "commit", "--allow-empty", "-m", message, "--no-gpg-sign")
	return err
}

// gitRemoteAdd adds a named remote to the repo.
func gitRemoteAdd(dir, name, url string) error {
	_, err := gitRun(dir, nil, "remote", "add", name, url)
	return err
}

// gitPush pushes the current branch to origin.
func gitPush(dir string, extraEnv []string) error {
	// --set-upstream covers both first push and subsequent ones.
	_, err := gitRun(dir, extraEnv, "push", "--set-upstream", "origin", "HEAD")
	return err
}

// gitIsRepo returns true when dir is already a git repository root.
func gitIsRepo(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// gitHasRemote returns true if the named remote is configured.
func gitHasRemote(dir, name string) bool {
	out, err := exec.Command("git", "-C", dir, "remote").Output()
	if err != nil {
		return false
	}
	for _, r := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(r) == name {
			return true
		}
	}
	return false
}
