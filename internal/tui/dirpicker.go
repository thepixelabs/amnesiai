package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thepixelabs/amnesiai/internal/config"
)

// DirPickerModel is a Bubbletea model for interactive directory selection with
// filesystem-backed autocomplete.  The user types a path; matching child
// directories are listed beneath the input and can be navigated with arrow keys
// or j/k.  Tab completes the highlighted (or first) suggestion.
type DirPickerModel struct {
	input       string   // what the user has typed so far
	defaultDir  string   // ctrl+r resets to this
	suggestions []string // full absolute paths of matching dirs
	cursor      int      // which suggestion is highlighted (-1 = none)
	confirmed   bool
	cancelled   bool
}

func (m DirPickerModel) Init() tea.Cmd { return nil }

func (m DirPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.suggestions) {
				m.input = m.suggestions[m.cursor]
			}
			m.confirmed = true
			return m, tea.Quit

		case "esc", "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit

		case "ctrl+r":
			m.input = m.defaultDir
			m.cursor = -1
			m.refreshSuggestions()

		case "tab":
			if m.cursor >= 0 && m.cursor < len(m.suggestions) {
				m.input = m.suggestions[m.cursor] + "/"
			} else if len(m.suggestions) > 0 {
				m.input = m.suggestions[0] + "/"
			}
			m.cursor = -1
			m.refreshSuggestions()

		case "down":
			if m.cursor < len(m.suggestions)-1 {
				m.cursor++
			}

		case "up":
			if m.cursor > -1 {
				m.cursor--
			}

		case "backspace":
			runes := []rune(m.input)
			if len(runes) > 0 {
				m.input = string(runes[:len(runes)-1])
			}
			m.cursor = -1
			m.refreshSuggestions()

		default:
			// Append printable single-rune input only.
			if len(msg.Runes) > 0 {
				m.input += string(msg.Runes)
				m.cursor = -1
				m.refreshSuggestions()
			}
		}
	}
	return m, nil
}

func (m DirPickerModel) View() string {
	var sb strings.Builder

	// Input line with block cursor.
	sb.WriteString("  ")
	sb.WriteString(AccentStyle.Render("Path:"))
	sb.WriteString(" ")
	sb.WriteString(AccentStyle.Render(m.input + "█"))
	sb.WriteString("\n\n")

	if len(m.suggestions) == 0 && m.input != "" {
		sb.WriteString(MutedStyle.Render("  (no matching directories)"))
		sb.WriteString("\n")
	} else {
		for i, s := range m.suggestions {
			if i == m.cursor {
				sb.WriteString(SelectedStyle.Render("  ▸ " + s))
			} else {
				sb.WriteString(MutedStyle.Render("    " + s))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(MutedStyle.Render("  Tab=complete · ↑↓=navigate · Ctrl+R=reset default · Enter=confirm · Esc=cancel"))
	sb.WriteString("\n")

	return sb.String()
}

// refreshSuggestions repopulates m.suggestions based on the current m.input.
func (m *DirPickerModel) refreshSuggestions() {
	m.suggestions = nil

	dir, prefix := splitDirPrefix(m.input)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if prefix != "" && !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		m.suggestions = append(m.suggestions, filepath.Join(dir, entry.Name()))
		if len(m.suggestions) >= 10 {
			break
		}
	}
}

// splitDirPrefix splits a typed path into the parent directory to read and the
// partial basename to filter on.  A leading "~/" is expanded to the home dir.
func splitDirPrefix(input string) (dir, prefix string) {
	// Expand leading ~/ before anything else.
	if strings.HasPrefix(input, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			input = filepath.Join(home, input[2:])
			// filepath.Join strips the trailing slash, so preserve it if the
			// original had one after the second character (e.g. "~/").
		}
	} else if input == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home, ""
		}
	}

	if input == "" {
		return "/", ""
	}
	if strings.HasSuffix(input, "/") {
		return input, ""
	}
	return filepath.Dir(input), filepath.Base(input)
}

// RunDirPicker launches the interactive directory picker pre-filled with
// initial and returns the confirmed path.  It returns ErrCancelled if the user
// pressed Esc / ctrl+c / q.
func RunDirPicker(initial string) (string, error) {
	m := DirPickerModel{input: initial, defaultDir: config.DefaultBackupDir(), cursor: -1}
	m.refreshSuggestions()
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	dm, ok := final.(DirPickerModel)
	if !ok {
		return "", nil
	}
	if dm.cancelled {
		return "", ErrCancelled
	}
	return dm.input, nil
}
