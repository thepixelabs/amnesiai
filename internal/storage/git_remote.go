package storage

import (
	"fmt"
	"os"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/remote"
)

// gitRemoteStorage wraps gitLocalStorage and additionally pushes to a remote
// after each commit (unless noPush is true).
type gitRemoteStorage struct {
	local  *gitLocalStorage
	dir    string
	noPush bool
	// tokenEnv is a slice of extra env vars that scope the push to the right
	// GitHub/GitLab account (e.g. GH_TOKEN=<account-token>).
	tokenEnv []string
}

// newGitRemote returns a gitRemoteStorage.
// noPush disables automatic push (honours --no-push / AutoPush=false).
func newGitRemote(dir string, noPush bool, tokenEnv []string) *gitRemoteStorage {
	return &gitRemoteStorage{
		local:    newGitLocal(dir),
		dir:      dir,
		noPush:   noPush,
		tokenEnv: tokenEnv,
	}
}

func (s *gitRemoteStorage) Save(name string, meta Metadata, payload []byte) (string, error) {
	id, err := s.local.Save(name, meta, payload)
	if err != nil {
		return id, err
	}
	if !s.noPush {
		if err := gitPush(s.dir, s.tokenEnv); err != nil {
			return id, fmt.Errorf("git push: %w", err)
		}
	}
	return id, nil
}

func (s *gitRemoteStorage) Load(id string) (Metadata, []byte, error) {
	return s.local.Load(id)
}

func (s *gitRemoteStorage) List() ([]BackupEntry, error) {
	return s.local.List()
}

func (s *gitRemoteStorage) Latest() (string, error) {
	return s.local.Latest()
}

// ─── InitGitRemote ────────────────────────────────────────────────────────────

// InitGitRemoteOptions holds parameters for setting up a git-remote backup root.
type InitGitRemoteOptions struct {
	// Dir is the backup root directory.
	Dir string
	// RepoURL is the remote URL. If empty and CreateRepo is true, the repo is
	// created via gh/glab and the URL is determined from the response.
	RepoURL string
	// CreateRepo instructs amnesiai to create the remote repository automatically.
	CreateRepo bool
	// RepoName is the name to pass to `gh repo create` / `glab repo create`.
	// Only used when CreateRepo is true.
	RepoName string
}

// InitGitRemote sets up dir as a git-remote backup root:
//  1. InitGitLocal (idempotent).
//  2. Optionally create the remote repo via gh/glab.
//  3. Verify the repo is private (ABORT if public).
//  4. Add `origin` remote (idempotent).
//  5. Bind the account in state.json via config.BindRemote.
func InitGitRemote(opts InitGitRemoteOptions) (repoURL string, err error) {
	if err := InitGitLocal(opts.Dir); err != nil {
		return "", err
	}

	repoURL = opts.RepoURL

	if opts.CreateRepo {
		if opts.RepoName == "" {
			return "", fmt.Errorf("--repo-name is required when --create-repo is set")
		}
		// Detect forge from partial URL hint, or default to GitHub.
		var sel remote.AccountSelection
		if repoURL != "" {
			sel, err = remote.ResolveAccount(repoURL)
		} else {
			// Default to GitHub when no URL is provided.
			accounts, ghErr := remote.GHAuthList()
			if ghErr != nil || len(accounts) == 0 {
				return "", fmt.Errorf("no GitHub accounts found; run `gh auth login` first")
			}
			sel = remote.AccountSelection{Host: remote.HostGitHub, Account: accounts[0]}
			if len(accounts) > 1 {
				sel, err = remote.ResolveAccount("https://github.com/placeholder")
				if err != nil {
					return "", err
				}
			}
		}

		switch sel.Host {
		case remote.HostGitHub:
			repoURL, err = remote.GHCreateRepo(opts.RepoName, sel.Account)
		case remote.HostGitLab:
			repoURL, err = remote.GLabCreateRepo(opts.RepoName, sel.Account)
		default:
			return "", fmt.Errorf("unsupported forge: %s", sel.Host)
		}
		if err != nil {
			return "", err
		}
	}

	if repoURL == "" {
		return "", fmt.Errorf("remote URL is required (pass --remote-url or use --create-repo)")
	}

	// Verify repo is private before wiring anything up.
	sel, err := remote.ResolveAccount(repoURL)
	if err != nil {
		// Non-fatal for verification — we will still warn.
		fmt.Fprintf(os.Stderr, "WARNING: could not resolve account for %s: %v — skipping privacy check\n", repoURL, err)
	} else {
		switch sel.Host {
		case remote.HostGitHub:
			if verr := remote.GHVerifyPrivate(repoURL, sel.Account); verr != nil {
				return "", verr
			}
		case remote.HostGitLab:
			if verr := remote.GLabVerifyPrivate(repoURL, sel.Account); verr != nil {
				return "", verr
			}
		}
	}

	// Add the remote (idempotent — skip if already present).
	if !gitHasRemote(opts.Dir, "origin") {
		if err := gitRemoteAdd(opts.Dir, "origin", repoURL); err != nil {
			return "", err
		}
	}

	// Persist account binding so future backup pushes use the right token.
	if sel.Account != "" {
		if err := config.BindRemote(repoURL, string(sel.Host), sel.Account); err != nil {
			// Non-fatal: log but don't abort.
			fmt.Fprintf(os.Stderr, "WARNING: could not persist remote binding: %v\n", err)
		}
	}

	return repoURL, nil
}

// UpgradeGitLocalToRemote wires an existing git-local repo to a remote.
// Equivalent to InitGitRemote but skips the git init / initial commit steps
// (the repo already has history).
func UpgradeGitLocalToRemote(opts InitGitRemoteOptions) (string, error) {
	// Reuse InitGitRemote; InitGitLocal is idempotent so it will not re-init.
	return InitGitRemote(opts)
}
