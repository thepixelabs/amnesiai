// Package tui — enriched findings display with details toggle.
package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/thepixelabs/amnesiai/internal/scan"
)

// FindingEntry pairs a provider name with its secrets findings.
type FindingEntry struct {
	Provider  string
	Findings  []scan.Finding
	Encrypted bool // true when the backup is encrypted
}

// PrintFindings renders the enriched findings summary to stdout.
// When isTTY is true the user is offered a [d] key to expand details.
// When isTTY is false the summary is printed without the interactive toggle
// (mirrors Track B's non-TTY guard).
func PrintFindings(entries []FindingEntry, isTTY bool) {
	if len(entries) == 0 {
		return
	}

	for _, e := range entries {
		if len(e.Findings) == 0 {
			continue
		}
		var msg string
		if e.Encrypted {
			msg = fmt.Sprintf(
				"%d secret(s) found in %s (encrypted in archive)",
				len(e.Findings), e.Provider,
			)
			fmt.Printf("%s %s\n", WarnStyle.Render("Warning:"), msg)
		} else {
			msg = fmt.Sprintf(
				"%d secret(s) found in %s — REDACTED (archive is UNENCRYPTED)",
				len(e.Findings), e.Provider,
			)
			fmt.Printf("%s %s\n", WarnStyle.Render("Warning:"), msg)
		}
	}

	if !isTTY {
		return
	}

	// Offer details toggle only on a real terminal.
	fmt.Println()
	fmt.Printf("%s ", MutedStyle.Render("[press d for details, Enter to continue]"))

	r := bufio.NewReader(os.Stdin)
	ch, _, err := r.ReadRune()
	if err != nil || (ch != 'd' && ch != 'D') {
		fmt.Println()
		return
	}
	fmt.Println()
	printFindingDetails(entries)
}

func printFindingDetails(entries []FindingEntry) {
	fmt.Println()
	fmt.Println(AccentStyle.Render("Finding details"))
	fmt.Println()
	for _, e := range entries {
		if len(e.Findings) == 0 {
			continue
		}
		fmt.Println(IndigoStyle.Render("[" + e.Provider + "]"))
		for _, f := range e.Findings {
			// RuleID and file path — file path is embedded in scan.Finding.Type
			// as "<ruleID>@<file>" when populated; fall back to rule ID only.
			ruleID, filePath := parseFindingType(f.Type)
			if filePath != "" {
				fmt.Printf("  %s  %s\n",
					WarnStyle.Render(ruleID),
					MutedStyle.Render(filePath),
				)
			} else {
				fmt.Printf("  %s\n", WarnStyle.Render(ruleID))
			}
		}
		fmt.Println()
	}
}

// parseFindingType splits a finding type string of the form "ruleID@filePath"
// into its components. If no '@' is present, the whole string is the rule ID.
func parseFindingType(raw string) (ruleID, filePath string) {
	idx := strings.LastIndex(raw, "@")
	if idx < 0 {
		return raw, ""
	}
	return raw[:idx], raw[idx+1:]
}

// BuildFindingEntries converts the per-provider findings map from BackupResult
// into the slice format expected by PrintFindings.
func BuildFindingEntries(findings map[string][]scan.Finding, encrypted bool) []FindingEntry {
	if len(findings) == 0 {
		return nil
	}
	entries := make([]FindingEntry, 0, len(findings))
	for provider, ff := range findings {
		if len(ff) > 0 {
			entries = append(entries, FindingEntry{
				Provider:  provider,
				Findings:  ff,
				Encrypted: encrypted,
			})
		}
	}
	return entries
}
