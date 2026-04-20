package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GLabRepo holds the fields we care about from `glab api projects/:id`.
type GLabRepo struct {
	Visibility string `json:"visibility"` // "private" | "internal" | "public"
}

// GLabCreateRepo calls `glab repo create` to create a private GitLab project.
// Returns the HTTP clone URL of the new project.
func GLabCreateRepo(name, account string) (string, error) {
	args := []string{"repo", "create", name, "--private"}
	cmd := exec.Command("glab", args...)
	cmd.Env = scoped(os.Environ(), "glab", account)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("glab repo create: %w — %s", err, stderrOf(err))
	}
	// glab prints a URL among the output lines; find the first https:// line.
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "git@") {
			return line, nil
		}
	}
	return "", fmt.Errorf("glab repo create: could not find repo URL in output: %q", string(out))
}

// GLabVerifyPrivate asserts that the repository at repoURL is private.
// Calls `glab api projects/:encoded-path` and checks the visibility field.
func GLabVerifyPrivate(repoURL, account string) error {
	projectPath, err := glabProjectPath(repoURL)
	if err != nil {
		return err
	}
	encoded := strings.ReplaceAll(projectPath, "/", "%2F")
	args := []string{"api", fmt.Sprintf("projects/%s", encoded)}
	cmd := exec.Command("glab", args...)
	cmd.Env = scoped(os.Environ(), "glab", account)

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("glab api projects/%s: %w — %s", encoded, err, stderrOf(err))
	}

	var repo GLabRepo
	if err := json.Unmarshal(out, &repo); err != nil {
		return fmt.Errorf("parse glab api response: %w", err)
	}
	if repo.Visibility != "private" {
		return fmt.Errorf("repository %s visibility is %q — amnesiai requires a private repository; aborting", repoURL, repo.Visibility)
	}
	return nil
}

// GLabAuthList returns the list of authenticated GitLab accounts.
func GLabAuthList() ([]string, error) {
	out, err := exec.Command("glab", "auth", "status").Output()
	if err != nil {
		// glab auth status exits non-zero when no accounts are logged in.
		return nil, fmt.Errorf("glab auth status: %w", err)
	}
	return parseAuthList(string(out)), nil
}

// glabProjectPath extracts "namespace/project" from a GitLab URL.
func glabProjectPath(url string) (string, error) {
	url = strings.TrimSuffix(url, ".git")
	for _, host := range []string{"gitlab.com/", "gitlab."} {
		if idx := strings.Index(url, host); idx >= 0 {
			after := url[idx+len(host):]
			// Skip the host portion for self-hosted instances.
			if host == "gitlab." {
				// url after "gitlab." looks like "example.com/namespace/project"
				// Advance past the hostname.
				slashIdx := strings.Index(after, "/")
				if slashIdx < 0 {
					continue
				}
				after = after[slashIdx+1:]
			}
			return after, nil
		}
	}
	// Fallback: strip scheme and domain.
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@")
	if idx := strings.Index(url, "/"); idx >= 0 {
		return url[idx+1:], nil
	}
	return "", fmt.Errorf("cannot parse GitLab project path from URL: %s", url)
}
