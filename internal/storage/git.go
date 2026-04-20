// Package storage — git.go
//
// Shared git helpers used by both gitLocalStorage and gitRemoteStorage.
// All git operations shell out to the system `git` binary; go-git is not used.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/thepixelabs/amnesiai/internal/remote"
)

// gitRun executes a git command inside dir and returns combined output.
// Env additions are appended to the process environment.
// tokenEnv entries (matching *_TOKEN=<value>) are redacted from any error
// output before the error is returned, so tokens never leak into logs.
func gitRun(dir string, extraEnv []string, args ...string) ([]byte, error) {
	return gitRunTimeout(dir, extraEnv, remote.TimeoutGitShort, args...)
}

// gitRunTimeout is like gitRun but honours an explicit timeout.
func gitRunTimeout(dir string, extraEnv []string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Redact token values from output before embedding in the error message.
		sanitised := redactTokens(out, extraEnv)
		if ctx.Err() != nil {
			return sanitised, fmt.Errorf("git %s timed out after %s: %w", strings.Join(args, " "), timeout, ctx.Err())
		}
		return sanitised, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(sanitised)))
	}
	return out, nil
}

// redactTokens replaces the value portion of any "*_TOKEN=<value>" entries in
// envVars with "<REDACTED:TOKEN>" inside data.  This prevents tokens from
// appearing in error messages when git echoes its environment (e.g. GIT_TRACE).
func redactTokens(data []byte, envVars []string) []byte {
	for _, e := range envVars {
		if !strings.Contains(e, "_TOKEN=") {
			continue
		}
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		data = bytes.ReplaceAll(data, []byte(parts[1]), []byte("<REDACTED:TOKEN>"))
	}
	return data
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
	ctx, cancel := context.WithTimeout(context.Background(), remote.TimeoutGitShort)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "config", "--global", key).Output()
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
	ctx, cancel := context.WithTimeout(context.Background(), remote.TimeoutGitShort)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain").Output()
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
	// Use the longer push timeout since this is a network operation.
	_, err := gitRunTimeout(dir, extraEnv, remote.TimeoutGitPush, "push", "--set-upstream", "origin", "HEAD")
	return err
}

// gitIsRepo returns true when dir is already a git repository root.
func gitIsRepo(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), remote.TimeoutGitShort)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// gitHasRemote returns true if the named remote is configured.
func gitHasRemote(dir, name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), remote.TimeoutGitShort)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "remote").Output()
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
