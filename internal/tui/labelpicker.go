// Package tui — multi-select picker for previously-used backup labels.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// LabelPickerModel is a Bubbletea multi-select for "key=value" label strings
// gathered from prior backups.  Mirrors ProviderPickerModel ergonomics: arrow
// keys + space to toggle, Enter confirms, q/esc cancels.
type LabelPickerModel struct {
	labels    []string
	chosen    map[int]bool
	cursor    int
	cancelled bool
}

// NewLabelPickerModel builds a picker for the supplied "key=value" strings.
// Order is preserved (caller decides recency or alphabetical sort).
func NewLabelPickerModel(labels []string) LabelPickerModel {
	return LabelPickerModel{
		labels: labels,
		chosen: make(map[int]bool, len(labels)),
	}
}

func (m LabelPickerModel) Init() tea.Cmd { return nil }

func (m LabelPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.labels)-1 {
				m.cursor++
			}
		case " ":
			m.chosen[m.cursor] = !m.chosen[m.cursor]
		case "a":
			for i := range m.labels {
				m.chosen[i] = true
			}
		case "n":
			m.chosen = make(map[int]bool, len(m.labels))
		case "enter":
			return m, tea.Quit
		case "q", "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m LabelPickerModel) View() string {
	var sb strings.Builder
	sb.WriteString(AccentStyle.Render("Reuse a label from a previous backup?") + "\n\n")

	for i, label := range m.labels {
		marker := " "
		if m.chosen[i] {
			marker = "·"
		}
		line := fmt.Sprintf("[%s] %s", marker, label)
		if i == m.cursor {
			sb.WriteString("  " + SelectedStyle.Render("▸ "+line))
		} else {
			sb.WriteString("  " + NormalStyle.Render("  "+line))
		}
		sb.WriteRune('\n')
	}

	sb.WriteString("\n")
	sb.WriteString(MutedStyle.Render("Space=toggle · a=all · n=none · Enter=continue · q=skip"))
	sb.WriteRune('\n')
	return sb.String()
}

// SelectedLabels returns the toggled "key=value" strings in display order.
func (m LabelPickerModel) SelectedLabels() []string {
	sel := make([]string, 0, len(m.chosen))
	for i, label := range m.labels {
		if m.chosen[i] {
			sel = append(sel, label)
		}
	}
	return sel
}

// Cancelled reports whether the user backed out (q / ctrl+c / esc).
func (m LabelPickerModel) Cancelled() bool { return m.cancelled }
