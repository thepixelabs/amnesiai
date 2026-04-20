// Package tui provides the Bubbletea models and helpers for the amnesiai
// interactive terminal UI.
//
// Design principles:
//   - No runtime figlet dependency — banner is pre-rendered and embedded.
//   - Arrow-key navigation + single-letter hotkeys throughout.
//   - Middle-dot (·, U+00B7) as the selection marker and passphrase mask rune.
//   - Passphrase entry uses charmbracelet/x/term.ReadPassword for TTY masking.
package tui

import (
	_ "embed"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

//go:embed assets/banner.txt
var bannerRaw string

// ─── Ocean palette — matches altergo's "ocean" theme ─────────────────────────

const (
	ColorCyan   = "#00d7ff" // altergo grad stop 0 — electric cyan
	ColorBlue   = "#005fd7" // altergo grad stop 1 — slate blue
	ColorIndigo = "#8787ff" // altergo brand/accent — indigo
	ColorGreen  = "#5faf5f" // success
	ColorAmber  = "#ffaf00" // warning
	ColorRed    = "#ff5f5f" // error
	ColorDim    = "#585858" // muted
	ColorWhite  = "#d0d0d0" // normal text
)

// Exported styles so cmd/tui.go can reuse them without duplicating constants.
var (
	AccentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorCyan))
	IndigoStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorIndigo))
	SuccessStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorGreen))
	WarnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorAmber))
	ErrorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorRed))
	MutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDim))
	NormalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWhite))

	SelectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(ColorCyan))

	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.Border{
			Top:         "─",
			Bottom:      "─",
			Left:        "│",
			Right:       "│",
			TopLeft:     "╭",
			TopRight:    "╮",
			BottomLeft:  "╰",
			BottomRight: "╯",
		}).
		BorderForeground(lipgloss.Color(ColorBlue)).
		Padding(0, 1)
)

// ─── Banner ───────────────────────────────────────────────────────────────────

// Banner returns the pre-rendered "amnesiai" ASCII art from the embedded file,
// with the ocean gradient applied character-by-character across each line.
// The gradient runs left-to-right: electric cyan → slate blue.
func Banner() string {
	raw := strings.TrimRight(bannerRaw, "\n")
	lines := strings.Split(raw, "\n")

	var sb strings.Builder
	for _, line := range lines {
		runes := []rune(line)
		n := len(runes)
		for i, ch := range runes {
			t := float64(i) / maxF(float64(n-1), 1)
			col := lerpColor(ColorCyan, ColorBlue, t)
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(col)).Render(string(ch)))
		}
		sb.WriteRune('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ─── Gradient helpers ─────────────────────────────────────────────────────────

func lerpColor(from, to string, t float64) string {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	r1, g1, b1 := hexToRGB(from)
	r2, g2, b2 := hexToRGB(to)
	r := int(float64(r1) + (float64(r2)-float64(r1))*t)
	g := int(float64(g1) + (float64(g2)-float64(g1))*t)
	b := int(float64(b1) + (float64(b2)-float64(b1))*t)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func hexToRGB(hex string) (r, g, b int) {
	hex = strings.TrimPrefix(hex, "#")
	var v int64
	for _, ch := range hex {
		v <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			v |= int64(ch - '0')
		case ch >= 'a' && ch <= 'f':
			v |= int64(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			v |= int64(ch-'A') + 10
		}
	}
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)
}

// GradientText renders each rune in text with a left-to-right ocean gradient.
func GradientText(text string) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}
	var sb strings.Builder
	for i, ch := range runes {
		t := float64(i) / maxF(float64(n-1), 1)
		col := lerpColor(ColorCyan, ColorBlue, t)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(col)).Render(string(ch)))
	}
	return sb.String()
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ─── Provider picker model ────────────────────────────────────────────────────

// ProviderPickerDoneMsg is sent by the picker when the user presses Enter.
type ProviderPickerDoneMsg struct {
	Selected []string
}

// ProviderPickerModel is a Bubbletea model for arrow-key + space multi-select
// of providers.  Selection marker is · (U+00B7 middle-dot).
type ProviderPickerModel struct {
	Available []string
	chosen    map[int]bool
	cursor    int
}

// NewProviderPickerModel creates a picker with the given available providers and
// pre-selects the ones listed in defaults.
func NewProviderPickerModel(available, defaults []string) ProviderPickerModel {
	chosen := make(map[int]bool, len(defaults))
	for i, name := range available {
		for _, d := range defaults {
			if name == d {
				chosen[i] = true
				break
			}
		}
	}
	return ProviderPickerModel{
		Available: available,
		chosen:    chosen,
		cursor:    0,
	}
}

func (m ProviderPickerModel) Init() tea.Cmd { return nil }

func (m ProviderPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.Available)-1 {
				m.cursor++
			}
		case " ": // toggle selection
			m.chosen[m.cursor] = !m.chosen[m.cursor]
		case "a": // select all
			for i := range m.Available {
				m.chosen[i] = true
			}
		case "n": // deselect all
			m.chosen = make(map[int]bool, len(m.Available))
		case "enter":
			sel := m.SelectedProviders()
			return m, func() tea.Msg {
				return ProviderPickerDoneMsg{Selected: sel}
			}
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m ProviderPickerModel) View() string {
	var sb strings.Builder
	sb.WriteString(AccentStyle.Render("Select providers") + "\n\n")

	for i, name := range m.Available {
		marker := " "
		if m.chosen[i] {
			marker = "·" // U+00B7 middle-dot
		}
		label := fmt.Sprintf("[%s] %s", marker, name)
		if i == m.cursor {
			sb.WriteString("  " + SelectedStyle.Render("▸ "+label))
		} else {
			sb.WriteString("  " + NormalStyle.Render("  "+label))
		}
		sb.WriteRune('\n')
	}

	sb.WriteString("\n")
	sb.WriteString(MutedStyle.Render("Space=toggle · a=all · n=none · Enter=confirm · q=quit"))
	sb.WriteRune('\n')
	return sb.String()
}

// SelectedProviders returns the currently-selected provider names in order.
func (m ProviderPickerModel) SelectedProviders() []string {
	sel := make([]string, 0, len(m.chosen))
	for i, name := range m.Available {
		if m.chosen[i] {
			sel = append(sel, name)
		}
	}
	return sel
}

// ─── Main menu model ──────────────────────────────────────────────────────────

// MenuAction is the result returned when the user selects a menu item.
type MenuAction int

const (
	ActionNone       MenuAction = iota
	ActionBackup                // b — run backup flow
	ActionRestore               // r — run restore flow
	ActionDiff                  // d — run diff flow
	ActionList                  // l — run list flow
	ActionCompletion            // c — show completion help
	ActionSettings              // s — open settings menu
	ActionQuit                  // q — exit
)

// menuEntry pairs a display label, hotkey, and action.
type menuEntry struct {
	hotkey string
	label  string
	action MenuAction
}

var menuEntries = []menuEntry{
	{"b", "Backup providers", ActionBackup},
	{"r", "Restore a backup", ActionRestore},
	{"d", "Diff against a backup", ActionDiff},
	{"l", "List backups", ActionList},
	{"c", "Completion help", ActionCompletion},
	{"s", "Settings", ActionSettings},
	{"q", "Quit", ActionQuit},
}

// SelectedMsg carries the chosen action when the user presses Enter or a hotkey.
type SelectedMsg struct{ Action MenuAction }

// MenuModel is the main Bubbletea menu model.
type MenuModel struct {
	cursor   int
	Selected MenuAction
	Greeting Greeting
	Width    int
	Height   int
	version  string
}

// NewMenuModel creates a menu model with a fresh time-of-day greeting.
func NewMenuModel(version string) MenuModel {
	return MenuModel{
		cursor:   0,
		Greeting: PickGreeting(),
		version:  version,
	}
}

func (m MenuModel) Init() tea.Cmd { return nil }

func (m MenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case SelectedMsg:
		m.Selected = msg.Action
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(menuEntries)-1 {
				m.cursor++
			}
		case "enter", " ":
			action := menuEntries[m.cursor].action
			return m, func() tea.Msg { return SelectedMsg{Action: action} }

		// Single-letter hotkeys — jump directly to action.
		case "b":
			return m, func() tea.Msg { return SelectedMsg{Action: ActionBackup} }
		case "r":
			return m, func() tea.Msg { return SelectedMsg{Action: ActionRestore} }
		case "d":
			return m, func() tea.Msg { return SelectedMsg{Action: ActionDiff} }
		case "l":
			return m, func() tea.Msg { return SelectedMsg{Action: ActionList} }
		case "c":
			return m, func() tea.Msg { return SelectedMsg{Action: ActionCompletion} }
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m MenuModel) View() string {
	var sb strings.Builder

	// Banner + version + greeting inside a Unicode box.
	banner := Banner()
	verSuffix := ""
	if m.version != "" && m.version != "dev" {
		verSuffix = " " + m.version
	}
	verLine := MutedStyle.Render("  amnesiai" + verSuffix + " — back up AI assistant configs")
	greetLine := IndigoStyle.Render("  "+m.Greeting.Icon+"  ") + GradientText(m.Greeting.Text)

	bannerBox := BorderStyle.Render(banner + "\n" + verLine + "\n" + greetLine)
	sb.WriteString(bannerBox)
	sb.WriteString("\n\n")

	// Menu items — selected row gets a ▸ cursor in accent color.
	for i, entry := range menuEntries {
		hotkey := MutedStyle.Render("[" + entry.hotkey + "]")
		if i == m.cursor {
			label := SelectedStyle.Render("▸ " + entry.label)
			sb.WriteString("  " + hotkey + " " + label)
		} else {
			label := NormalStyle.Render("  " + entry.label)
			sb.WriteString("  " + hotkey + " " + label)
		}
		sb.WriteRune('\n')
	}
	sb.WriteRune('\n')

	// Static footer — no animation.
	sb.WriteString(MutedStyle.Render(" ↑↓ navigate · Enter select · hotkey direct · q quit"))
	sb.WriteRune('\n')

	return sb.String()
}
