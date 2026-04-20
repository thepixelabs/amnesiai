package storage

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/remote"
)

// privacyCacheTTL is how long we trust a previous "repo is private" result
// before re-checking with the API.  This avoids hammering the API on every
// save while still catching a repo that was flipped to public mid-session.
const privacyCacheTTL = 5 * time.Minute

// gitRemoteStorage wraps gitLocalStorage and additionally pushes to a remote
// after each commit (unless noPush is true).
type gitRemoteStorage struct {
	local  *gitLocalStorage
	dir    string
	noPush bool
	// tokenEnv is a slice of extra env vars that scope the push to the right
	// GitHub/GitLab account (e.g. GH_TOKEN=<account-token>).
	tokenEnv []string

	// privacy cache — checked once per privacyCacheTTL to avoid API hammering.
	privacyMu            sync.Mutex
	privacyLastCheckedAt time.Time
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

// verifyPrivateCached checks that the remote repo is still private, but
// skips the API call if we checked within the last privacyCacheTTL.
// Returns an error if the repo is public or the check fails.
func (s *gitRemoteStorage) verifyPrivateCached(repoURL string) error {
	s.privacyMu.Lock()
	last := s.privacyLastCheckedAt
	s.privacyMu.Unlock()

	if time.Since(last) < privacyCacheTTL {
		return nil // still fresh
	}

	// Resolve the account from state bindings (best-effort; fall through on error).
	var account string
	if st, err := config.LoadState(); err == nil {
		if b, ok := st.LookupBinding(repoURL); ok {
			account = b.Account
		}
	}

	host := remote.DetectForge(repoURL)
	var verifyErr error
	switch host {
	case remote.HostGitHub:
		verifyErr = remote.GHVerifyPrivate(repoURL, account)
	case remote.HostGitLab:
		verifyErr = remote.GLabVerifyPrivate(repoURL, account)
	default:
		// Unknown forge: skip check rather than blocking the push.
		return nil
	}

	if verifyErr != nil {
		return verifyErr
	}

	s.privacyMu.Lock()
	s.privacyLastCheckedAt = time.Now()
	s.privacyMu.Unlock()
	return nil
}

func (s *gitRemoteStorage) Save(name string, meta Metadata, payload []byte) (string, error) {
	id, err := s.local.Save(name, meta, payload)
	if err != nil {
		return id, err
	}
	if !s.noPush {
		// Verify the remote repository is still private before every push.
		// On error we skip the push but keep the local commit intact.
		repoURL := gitGetRemoteURL(s.dir)
		if repoURL != "" {
			if verr := s.verifyPrivateCached(repoURL); verr != nil {
				return id, fmt.Errorf("privacy check failed — push aborted (commit saved locally): %w", verr)
			}
		}
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

// gitGetRemoteURL returns the URL of the "origin" remote, or "" if not set.
func gitGetRemoteURL(dir string) string {
	out, err := gitRun(dir, nil, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return trimNL(string(out))
}

// trimNL strips trailing newline characters.
func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
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
		st, stErr := config.LoadState()
		if stErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: could not load state: %v\n", stErr)
		} else if err := st.BindRemote(repoURL, string(sel.Host), sel.Account); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: could not record remote binding: %v\n", err)
		} else if err := st.Save(); err != nil {
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
