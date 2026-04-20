// Package remote — exec.go
//
// runWithTimeout is a thin wrapper around exec.Command that enforces a deadline
// via context.WithTimeout. All exec calls in this package and in
// internal/storage/git*.go must go through this helper so that slow network
// operations can never hang the process indefinitely.
package remote

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Timeout constants used throughout the remote and storage packages.
const (
	TimeoutAPICall  = 30 * time.Second  // gh api / glab api / gh auth list
	TimeoutGitPush  = 300 * time.Second // git push (large repos on slow links)
	TimeoutGitShort = 60 * time.Second  // git commit / git init / other local ops
)

// runWithTimeout runs name with args, enforcing the given timeout via a
// context deadline.  It returns the combined stdout+stderr output on success.
// On timeout the error is wrapped to include the command name and duration.
func runWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("command %s timed out after %s: %w", name, timeout, ctx.Err())
		}
		return out, err
	}
	return out, nil
}

// runWithTimeoutEnv is like runWithTimeout but also accepts an explicit env
// slice (passed to cmd.Env).  Used by callers that need to inject token vars.
func runWithTimeoutEnv(timeout time.Duration, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("command %s timed out after %s: %w", name, timeout, ctx.Err())
		}
		return out, err
	}
	return out, nil
}
