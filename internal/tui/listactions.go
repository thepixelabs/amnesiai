// Package tui — interactive backup list with delete action.
//
// Rendered from cmd/tui.go's listFlow. Keeps the same visual format as the
// static tuiPrintBackupTable but adds a cursor and a [d]elete shortcut.
// The model itself does no I/O; the parent passes a delete callback so the
// model can stay testable and decoupled from the storage layer.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thepixelabs/amnesiai/internal/storage"
)

// ListAction is the result code returned when the list view exits.
type ListAction int

const (
	ListActionQuit          ListAction = iota // user pressed q/esc — no further action
	ListActionDeleteRequest                   // user committed a delete in the confirm dialog
)

// ListResult bundles the exit reason and (when relevant) the targeted backup.
type ListResult struct {
	Action     ListAction
	TargetID   string
	TargetMeta storage.BackupEntry
}

// ListModel is a small Bubbletea model: cursor over a sorted list of backups
// with a single action ([d] delete). The actual deletion happens in the
// parent because storage.Storage is not part of the tui package's
// dependency surface.
type ListModel struct {
	entries     []storage.BackupEntry
	cursor      int
	confirming  bool // true while showing delete confirmation
	result      ListResult
	infoMessage string // transient line shown after a non-fatal action
}

// NewListModel builds a list model for the given backup entries (assumed
// sorted newest-first by the caller).
func NewListModel(entries []storage.BackupEntry) ListModel {
	return ListModel{entries: entries}
}

func (m ListModel) Init() tea.Cmd { return nil }

func (m ListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirming {
			switch msg.String() {
			case "y", "Y":
				m.result.Action = ListActionDeleteRequest
				m.result.TargetID = m.entries[m.cursor].ID
				m.result.TargetMeta = m.entries[m.cursor]
				return m, tea.Quit
			case "n", "N", "esc", "q":
				m.confirming = false
				m.infoMessage = "Delete cancelled."
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.result.Action = ListActionQuit
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.infoMessage = ""

		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
			m.infoMessage = ""

		case "d":
			if len(m.entries) == 0 {
				return m, nil
			}
			m.confirming = true
			m.infoMessage = ""
		}
	}
	return m, nil
}

func (m ListModel) View() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(AccentStyle.Render("  Backups") + "\n\n")

	if len(m.entries) == 0 {
		sb.WriteString("  " + MutedStyle.Render("No backups found.") + "\n\n")
		sb.WriteString("  " + MutedStyle.Render("q to quit") + "\n")
		return sb.String()
	}

	for i, e := range m.entries {
		marker := "  "
		if i == m.cursor {
			marker = AccentStyle.Render("▸ ")
		}
		row := fmt.Sprintf("%s  %s  [%s]",
			e.ID,
			e.Timestamp.Format("2006-01-02 15:04:05"),
			strings.Join(e.Providers, ", "),
		)
		if i == m.cursor {
			sb.WriteString("  " + marker + SelectedStyle.Render(row) + "\n")
		} else {
			sb.WriteString("  " + marker + NormalStyle.Render(row) + "\n")
		}
		if meta := formatListMeta(e); meta != "" {
			sb.WriteString("        " + MutedStyle.Render(meta) + "\n")
		}
	}

	if m.confirming {
		target := m.entries[m.cursor]
		sb.WriteString("\n  " + WarnStyle.Render(fmt.Sprintf(
			"Delete backup %s from %s? [y/N]",
			target.ID,
			target.Timestamp.Format("2006-01-02 15:04:05"),
		)) + "\n")
	} else {
		if m.infoMessage != "" {
			sb.WriteString("\n  " + SuccessStyle.Render(m.infoMessage) + "\n")
		}
		sb.WriteString("\n  " + MutedStyle.Render("↑↓ navigate · d delete · q back") + "\n")
	}
	return sb.String()
}

// Result returns the model's exit reason and (when applicable) the
// targeted backup. Callers should switch on Action.
func (m ListModel) Result() ListResult { return m.result }

// formatListMeta is a near-duplicate of cmd/tui.go's formatBackupMeta but kept
// in the tui package so this model is self-contained. The two implementations
// can diverge if the cmd-side table grows columns the interactive list
// doesn't surface.
func formatListMeta(entry storage.BackupEntry) string {
	var parts []string
	if len(entry.Labels) > 0 {
		keys := make([]string, 0, len(entry.Labels))
		for k := range entry.Labels {
			if strings.HasPrefix(k, "_") {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, k+"="+entry.Labels[k])
		}
		if len(pairs) > 0 {
			parts = append(parts, strings.Join(pairs, ", "))
		}
	}
	if entry.Message != "" {
		parts = append(parts, `"`+entry.Message+`"`)
	}
	return strings.Join(parts, " · ")
}

// RunListView opens the interactive list and returns the user's action plus
// the (possibly mutated) entries slice. After a delete is committed, callers
// should re-fetch entries from storage and call RunListView again to keep the
// display fresh.
func RunListView(entries []storage.BackupEntry) (ListResult, error) {
	model := NewListModel(entries)
	p := tea.NewProgram(model, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return ListResult{}, fmt.Errorf("list view: %w", err)
	}
	lm, ok := final.(ListModel)
	if !ok {
		return ListResult{Action: ListActionQuit}, nil
	}
	return lm.Result(), nil
}
