package remote

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Host identifies which forge is in use.
type Host string

const (
	HostGitHub Host = "github"
	HostGitLab Host = "gitlab"
)

// AccountSelection is the result of resolving a forge account.
type AccountSelection struct {
	Host    Host
	Account string
}

// ResolveAccount detects the forge from repoURL and, if multiple accounts are
// authenticated, prompts the user interactively to pick one.
//
// If exactly one account is found it is returned silently.
// If no accounts are found an error is returned telling the user to `gh auth login`.
func ResolveAccount(repoURL string) (AccountSelection, error) {
	host := hostFromURL(repoURL)
	switch host {
	case HostGitHub:
		return resolveGHAccount()
	case HostGitLab:
		return resolveGLabAccount()
	default:
		return AccountSelection{}, fmt.Errorf("unrecognised forge URL: %s", repoURL)
	}
}

// DetectForge returns which forge a URL belongs to.
func DetectForge(repoURL string) Host {
	return hostFromURL(repoURL)
}

func hostFromURL(url string) Host {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "github.com") {
		return HostGitHub
	}
	if strings.Contains(lower, "gitlab") {
		return HostGitLab
	}
	return ""
}

func resolveGHAccount() (AccountSelection, error) {
	accounts, err := GHAuthList()
	if err != nil || len(accounts) == 0 {
		return AccountSelection{}, fmt.Errorf("no GitHub accounts found; run `gh auth login` first")
	}
	if len(accounts) == 1 {
		return AccountSelection{Host: HostGitHub, Account: accounts[0]}, nil
	}
	account, err := promptAccount(accounts, "GitHub")
	if err != nil {
		return AccountSelection{}, err
	}
	return AccountSelection{Host: HostGitHub, Account: account}, nil
}

func resolveGLabAccount() (AccountSelection, error) {
	accounts, err := GLabAuthList()
	if err != nil || len(accounts) == 0 {
		return AccountSelection{}, fmt.Errorf("no GitLab accounts found; run `glab auth login` first")
	}
	if len(accounts) == 1 {
		return AccountSelection{Host: HostGitLab, Account: accounts[0]}, nil
	}
	account, err := promptAccount(accounts, "GitLab")
	if err != nil {
		return AccountSelection{}, err
	}
	return AccountSelection{Host: HostGitLab, Account: account}, nil
}

// promptAccount writes a numbered list to stdout and reads a selection from stdin.
func promptAccount(accounts []string, forgeLabel string) (string, error) {
	fmt.Printf("Multiple %s accounts found. Choose one:\n", forgeLabel)
	for i, a := range accounts {
		fmt.Printf("  %d. %s\n", i+1, a)
	}
	fmt.Print("Enter number: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "", fmt.Errorf("no input received")
	}
	input := strings.TrimSpace(scanner.Text())
	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(accounts) {
		return "", fmt.Errorf("invalid selection %q", input)
	}
	return accounts[idx-1], nil
}

// parseAuthList extracts usernames from the output of `gh auth list` or
// `glab auth status`. Both tools print lines like "username  ..." or
// "Token: ...". We grab lines that look like account entries (not headers).
func parseAuthList(output string) []string {
	var accounts []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// gh auth list: first field is the account name
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		// Skip obvious non-account lines.
		if strings.Contains(name, ":") || strings.Contains(name, "=") {
			continue
		}
		accounts = append(accounts, name)
	}
	return accounts
}

// scoped returns an env slice with GH_TOKEN/GITLAB_TOKEN set to the token for
// the given account (when account is non-empty). For gh we call
// `gh auth token --user <account>` to retrieve the token.
func scoped(env []string, tool, account string) []string {
	if account == "" {
		return env
	}
	var token string
	switch tool {
	case "gh":
		t, err := GHToken(account)
		if err == nil {
			token = t
		}
	case "glab":
		// glab uses GITLAB_TOKEN.
		t, err := GLabToken(account)
		if err == nil {
			token = t
		}
	}
	if token == "" {
		return env
	}
	// Remove any existing token vars and prepend the scoped one.
	filtered := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "GH_TOKEN=") || strings.HasPrefix(e, "GITHUB_TOKEN=") || strings.HasPrefix(e, "GITLAB_TOKEN=") {
			continue
		}
		filtered = append(filtered, e)
	}
	switch tool {
	case "gh":
		filtered = append(filtered, "GH_TOKEN="+token)
	case "glab":
		filtered = append(filtered, "GITLAB_TOKEN="+token)
	}
	return filtered
}

