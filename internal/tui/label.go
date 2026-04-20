// Package tui — label entry step with inline help.
package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// labelHelp is the inline help text shown before the label prompt.
const labelHelp = `Labels are arbitrary key=value pairs stored in metadata.json alongside the backup.
They appear in the output of 'amnesiai list' and can be used to annotate backups
with context such as environment (env=prod), purpose (reason=pre-upgrade), etc.

Labels are optional — press Enter to skip.`

// PromptLabels prints the label help text then reads a comma-separated
// key=value string from the terminal.  Returns a parsed map (may be empty).
// An empty/blank input is valid and returns an empty map.
func PromptLabels() (map[string]string, error) {
	fmt.Println()
	fmt.Println(AccentStyle.Render("Labels"))
	fmt.Println(MutedStyle.Render(labelHelp))
	fmt.Println()

	r := bufio.NewReader(os.Stdin)
	fmt.Printf("%s ", AccentStyle.Render("Labels (key=value,... or Enter to skip):"))
	line, err := r.ReadString('\n')
	if err != nil {
		// EOF with partial input is fine.
		if len(strings.TrimSpace(line)) == 0 {
			return map[string]string{}, nil
		}
	}

	return ParseLabels(strings.TrimSpace(line))
}

// ParseLabels converts a comma-separated "key=value" string into a map.
// Keys must be non-empty.  Values may contain additional '=' characters.
// Empty or whitespace-only input returns an empty map without error.
func ParseLabels(input string) (map[string]string, error) {
	labels := make(map[string]string)
	if strings.TrimSpace(input) == "" {
		return labels, nil
	}
	for _, part := range splitCSV(input) {
		pieces := strings.SplitN(part, "=", 2)
		if len(pieces) != 2 || pieces[0] == "" {
			return nil, fmt.Errorf("invalid label %q: expected key=value", part)
		}
		labels[pieces[0]] = pieces[1]
	}
	return labels, nil
}

func splitCSV(input string) []string {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
