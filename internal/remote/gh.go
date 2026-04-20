// Package remote provides thin wrappers over the gh and glab CLIs for
// repository creation and verification. No Go git library is used — all git
// and forge operations shell out to the respective CLI tools.
package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GHRepo holds the fields we care about from `gh api repos/:owner/:repo`.
type GHRepo struct {
	Private bool `json:"private"`
}

// GHCreateRepo calls `gh repo create` to create a private GitHub repository.
// If account is non-empty, the GH_TOKEN for that account is scoped via
// `gh auth token --user <account>`.
//
// Returns the clone URL of the new repository.
func GHCreateRepo(name, account string) (string, error) {
	args := []string{"repo", "create", name, "--private", "--clone=false"}
	cmd := exec.Command("gh", args...)
	cmd.Env = scoped(os.Environ(), "gh", account)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh repo create: %w — %s", err, stderrOf(err))
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", fmt.Errorf("gh repo create: no URL returned")
	}
	return url, nil
}

// GHVerifyPrivate asserts that the repository at repoURL is private.
// It calls `gh api repos/:owner/:repo` and reads the "private" field.
// Returns an error (causing abort) if the repo is public.
func GHVerifyPrivate(repoURL, account string) error {
	ownerRepo, err := ghOwnerRepo(repoURL)
	if err != nil {
		return err
	}
	args := []string{"api", fmt.Sprintf("repos/%s", ownerRepo)}
	cmd := exec.Command("gh", args...)
	cmd.Env = scoped(os.Environ(), "gh", account)

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh api repos/%s: %w — %s", ownerRepo, err, stderrOf(err))
	}

	var repo GHRepo
	if err := json.Unmarshal(out, &repo); err != nil {
		return fmt.Errorf("parse gh api response: %w", err)
	}
	if !repo.Private {
		return fmt.Errorf("repository %s is public — amnesiai requires a private repository; aborting", repoURL)
	}
	return nil
}

// GHAuthList returns the list of authenticated GitHub accounts from `gh auth list`.
func GHAuthList() ([]string, error) {
	out, err := exec.Command("gh", "auth", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("gh auth list: %w", err)
	}
	return parseAuthList(string(out)), nil
}

// GHToken returns the token for a given account via `gh auth token --user <account>`.
// This is used to scope individual gh commands to a specific account.
func GHToken(account string) (string, error) {
	out, err := exec.Command("gh", "auth", "token", "--user", account).Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token --user %s: %w", account, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ghOwnerRepo extracts "owner/repo" from a GitHub URL such as
// "https://github.com/owner/repo" or "git@github.com:owner/repo.git".
func ghOwnerRepo(url string) (string, error) {
	url = strings.TrimSuffix(url, ".git")
	// HTTPS style
	if idx := strings.Index(url, "github.com/"); idx >= 0 {
		return url[idx+len("github.com/"):], nil
	}
	// SSH style  git@github.com:owner/repo
	if idx := strings.Index(url, "github.com:"); idx >= 0 {
		return url[idx+len("github.com:"):], nil
	}
	return "", fmt.Errorf("cannot parse GitHub owner/repo from URL: %s", url)
}
