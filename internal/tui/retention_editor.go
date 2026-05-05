// Package tui — small two-field numeric editor for retention policy.
//
// Used by the Settings menu: opens, lets the user edit KeepLast and
// MaxAgeDays, returns the new values (or the original ones on cancel).
// Inputs are validated as non-negative integers; non-numeric chars are
// silently dropped during typing so we don't need a separate validation pass.
package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// RetentionEditorResult bundles the editor's exit state.
type RetentionEditorResult struct {
	Saved      bool // true when the user pressed Enter to save
	KeepLast   int
	MaxAgeDays int
}

// RetentionEditorModel is a tiny two-field form. Field 0 = KeepLast,
// field 1 = MaxAgeDays. Tab cycles fields; Enter saves; Esc/q cancels.
type RetentionEditorModel struct {
	fields  [2]string // text buffers for each field
	cursor  int       // 0 or 1
	result  RetentionEditorResult
	errMsg  string
}

// NewRetentionEditorModel seeds the editor with the current policy values.
func NewRetentionEditorModel(keepLast, maxAgeDays int) RetentionEditorModel {
	return RetentionEditorModel{
		fields: [2]string{strconv.Itoa(keepLast), strconv.Itoa(maxAgeDays)},
	}
}

func (m RetentionEditorModel) Init() tea.Cmd { return nil }

func (m RetentionEditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "ctrl+c", "esc":
			m.result.Saved = false
			return m, tea.Quit

		case "tab", "down":
			m.cursor = (m.cursor + 1) % 2
			m.errMsg = ""

		case "shift+tab", "up":
			m.cursor = (m.cursor + 1) % 2 // only 2 fields — same as down
			m.errMsg = ""

		case "enter":
			kl, klErr := strconv.Atoi(strings.TrimSpace(m.fields[0]))
			ma, maErr := strconv.Atoi(strings.TrimSpace(m.fields[1]))
			if klErr != nil || maErr != nil || kl < 0 || ma < 0 {
				m.errMsg = "Both fields must be non-negative integers."
				return m, nil
			}
			m.result.Saved = true
			m.result.KeepLast = kl
			m.result.MaxAgeDays = ma
			return m, tea.Quit

		case "backspace":
			if len(m.fields[m.cursor]) > 0 {
				m.fields[m.cursor] = m.fields[m.cursor][:len(m.fields[m.cursor])-1]
			}

		default:
			// Accept only digits — silently drop everything else so we never
			// need to surface a validation error mid-typing.
			if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
				m.fields[m.cursor] += key
			}
		}
	}
	return m, nil
}

func (m RetentionEditorModel) View() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(AccentStyle.Render("  Retention policy") + "\n\n")
	sb.WriteString("  " + MutedStyle.Render("0 disables that window. Both 0 = retention disabled.") + "\n\n")

	labels := []string{"keep_last     ", "max_age_days  "}
	for i, label := range labels {
		val := m.fields[i]
		if val == "" {
			val = MutedStyle.Render("(empty)")
		}
		if i == m.cursor {
			sb.WriteString("  " + AccentStyle.Render("▸ "+label) + " " + SelectedStyle.Render(val+"_") + "\n")
		} else {
			sb.WriteString("    " + NormalStyle.Render(label+" ") + NormalStyle.Render(val) + "\n")
		}
	}

	if m.errMsg != "" {
		sb.WriteString("\n  " + WarnStyle.Render(m.errMsg) + "\n")
	}
	sb.WriteString("\n  " + MutedStyle.Render("Tab switch · Enter save · Esc cancel") + "\n")
	return sb.String()
}

// Result returns the editor's exit state.
func (m RetentionEditorModel) Result() RetentionEditorResult { return m.result }

// RunRetentionEditor opens the editor and returns the result.
func RunRetentionEditor(keepLast, maxAgeDays int) (RetentionEditorResult, error) {
	model := NewRetentionEditorModel(keepLast, maxAgeDays)
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		return RetentionEditorResult{}, fmt.Errorf("retention editor: %w", err)
	}
	em, ok := final.(RetentionEditorModel)
	if !ok {
		return RetentionEditorResult{}, nil
	}
	return em.Result(), nil
}
