package tui

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	xterm "github.com/charmbracelet/x/term"

	"github.com/thepixelabs/amnesiai/internal/core"
)

// nodeKind distinguishes the three kinds of rows in the file-picker tree.
type nodeKind int

const (
	nodeKindProvider nodeKind = iota // top-level provider header
	nodeKindFolder                   // any subdirectory within a provider (any depth)
	nodeKindFile                     // individual file
)

// treeNode is one rendered row in the file-picker tree.
//
// segments holds the path components BELOW the provider:
//   - nodeKindProvider: empty
//   - nodeKindFolder:   e.g. ["skills"] or ["skills","foo"] — the folder's full
//     path beneath the provider, not including the provider itself
//   - nodeKindFile:     e.g. ["skills","foo","SKILL.md"] — last element is the
//     filename; for files directly under the provider it is just ["CLAUDE.md"]
//
// archPath and origPath are set for nodeKindFile only.
type treeNode struct {
	kind     nodeKind
	provider string
	segments []string // path segments below the provider (see above)
	archPath string   // file only
	origPath string   // file only
}

// FilePickerModel is a Bubbletea model for cherry-pick file selection within a
// single backup. Entries are grouped by provider, then recursively by
// subdirectory to arbitrary depth. Each provider and folder is collapsible.
// Viewport scrolling keeps the cursor visible at all times.
type FilePickerModel struct {
	// all nodes (the full tree, always stable)
	nodes []treeNode

	// selection state: keyed by archPath
	chosen map[string]bool

	// collapse state: provider -> bool, folder -> bool
	// Keys for providers use the provider name; keys for folders use
	// "<provider>/<seg0>/<seg1>/..." to avoid collision.
	collapsed map[string]bool

	// viewport
	cursor       int
	scrollOffset int
	winHeight    int // last known terminal height (0 = unknown)
	winWidth     int // last known terminal width (0 = unknown)

	cancelled bool
}

// collapseKey returns a stable map key for collapsed state.
//   - provider node: just the provider name
//   - folder node: "<provider>/<seg0>/<seg1>/..."
func collapseKey(n treeNode) string {
	switch n.kind {
	case nodeKindProvider:
		return n.provider
	case nodeKindFolder:
		return n.provider + "/" + strings.Join(n.segments, "/")
	default:
		return ""
	}
}

// folderCollapseKey returns the collapse-map key for a folder identified by
// provider and segments. Mirrors the nodeKindFolder branch of collapseKey.
func folderCollapseKey(provider string, segments []string) string {
	return provider + "/" + strings.Join(segments, "/")
}

// NewFilePickerModel constructs a FilePickerModel from manifest entries.
// defaults is the set of archive paths that start pre-selected; pass nil or
// an empty slice to pre-select all entries.
func NewFilePickerModel(entries []core.ManifestEntry, defaults []string) FilePickerModel {
	// ── seed terminal height up front ──────────────────────────────────────
	// alt-screen is not used at the picker level; seed initial size so the
	// viewport engages on first render even before a WindowSizeMsg arrives.
	var initialHeight int
	if w, h, err := xterm.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		initialHeight = h
	}

	// ── group by provider ──────────────────────────────────────────────────
	byProvider := make(map[string][]core.ManifestEntry)
	var provOrder []string
	seenProv := make(map[string]bool)
	for _, e := range entries {
		// Defensive: skip entries whose ArchPath doesn't start with
		// "<provider>/" — they would be mis-grouped under the wrong folder.
		if !strings.HasPrefix(e.ArchPath, e.Provider+"/") {
			continue
		}
		if !seenProv[e.Provider] {
			provOrder = append(provOrder, e.Provider)
			seenProv[e.Provider] = true
		}
		byProvider[e.Provider] = append(byProvider[e.Provider], e)
	}
	sort.Strings(provOrder)

	// ── defaults set ───────────────────────────────────────────────────────
	defaultSet := make(map[string]bool, len(defaults))
	selectAll := len(defaults) == 0
	for _, d := range defaults {
		defaultSet[d] = true
	}

	chosen := make(map[string]bool)
	var nodes []treeNode

	for _, prov := range provOrder {
		pEntries := byProvider[prov]
		sort.Slice(pEntries, func(i, j int) bool {
			return pEntries[i].ArchPath < pEntries[j].ArchPath
		})

		// ── emit provider header ───────────────────────────────────────────
		nodes = append(nodes, treeNode{
			kind:     nodeKindProvider,
			provider: prov,
		})

		// ── emit folder headers and file nodes ────────────────────────────
		// Walk entries in sorted order; for each unique folder-path prefix
		// (any depth) emit a folder node the first time we see it, then emit
		// the file node.  emittedFolders tracks which prefix keys we've
		// already written so we never duplicate a folder header.
		emittedFolders := make(map[string]bool)

		for _, e := range pEntries {
			// Strip the "<provider>/" prefix and split into segments.
			rel := strings.TrimPrefix(e.ArchPath, prov+"/")
			if rel == "" {
				// Defensive: archPath equal to provider name with no path.
				continue
			}
			parts := strings.Split(rel, "/")
			// parts = ["skills","foo","SKILL.md"] for claude/skills/foo/SKILL.md

			// Emit a folder node for every ancestor prefix (all but the last
			// segment which is the filename).
			for depth := 1; depth < len(parts); depth++ {
				folderSegs := parts[:depth]
				key := strings.Join(folderSegs, "/")
				if !emittedFolders[key] {
					emittedFolders[key] = true
					segCopy := make([]string, len(folderSegs))
					copy(segCopy, folderSegs)
					nodes = append(nodes, treeNode{
						kind:     nodeKindFolder,
						provider: prov,
						segments: segCopy,
					})
				}
			}

			// Emit the file node.
			segCopy := make([]string, len(parts))
			copy(segCopy, parts)
			nodes = append(nodes, treeNode{
				kind:     nodeKindFile,
				provider: prov,
				segments: segCopy,
				archPath: e.ArchPath,
				origPath: e.OrigPath,
			})
			if selectAll || defaultSet[e.ArchPath] {
				chosen[e.ArchPath] = true
			}
		}
	}

	return FilePickerModel{
		nodes:     nodes,
		chosen:    chosen,
		collapsed: make(map[string]bool),
		winHeight: initialHeight,
	}
}

func (m FilePickerModel) Init() tea.Cmd { return nil }

// viewportHeight returns how many list rows are visible given the window height.
// We reserve 3 lines at the top (title + blank + blank) and 3 at the bottom
// (blank + help line + indicator line).
func (m FilePickerModel) viewportHeight() int {
	if m.winHeight == 0 {
		return 20 // sensible default before first WindowSizeMsg
	}
	h := m.winHeight - 6
	if h < 4 {
		h = 4
	}
	return h
}

// clampScroll adjusts scrollOffset so that cursor stays within the viewport.
// It also clamps the cursor itself so it never points past the last visible
// node — important after a collapse that hides the node the cursor was on.
func (m *FilePickerModel) clampScroll() {
	visible := m.visibleNodes()
	n := len(visible)
	vh := m.viewportHeight()

	// Clamp cursor to the valid visible range first.
	if n == 0 {
		m.cursor = 0
		m.scrollOffset = 0
		return
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+vh {
		m.scrollOffset = m.cursor - vh + 1
	}
	// Clamp offset to valid range.
	maxOff := n - vh
	if maxOff < 0 {
		maxOff = 0
	}
	if m.scrollOffset > maxOff {
		m.scrollOffset = maxOff
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m FilePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.winHeight = msg.Height
		m.winWidth = msg.Width
		m.clampScroll()
		return m, nil

	case tea.KeyMsg:
		// Compute visible nodes once for this Update call; pass to helpers
		// that need them so we don't recompute per-handler on large manifests.
		visible := m.visibleNodes()

		switch msg.String() {
		case "up", "k":
			m.moveCursorV(-1, visible)
		case "down", "j":
			m.moveCursorV(1, visible)

		case "pgup":
			m.moveCursorV(-(m.viewportHeight() - 1), visible)
		case "pgdown":
			m.moveCursorV(m.viewportHeight()-1, visible)

		case "home", "g":
			m.cursor = 0
			m.scrollOffset = 0
		case "end", "G":
			m.cursor = len(visible) - 1
			m.clampScroll()

		case "left":
			if m.cursor < 0 || m.cursor >= len(visible) {
				break
			}
			cur := visible[m.cursor]
			switch cur.kind {
			case nodeKindProvider:
				if !m.collapsed[collapseKey(cur)] {
					m.collapsed[collapseKey(cur)] = true
					// Cursor may now point into a hidden region; snap it back
					// to the (still-visible) provider header.
					m.jumpToProviderHeader(cur.provider)
				}
				// When already collapsed: no-op.
			case nodeKindFolder:
				if !m.collapsed[collapseKey(cur)] {
					// Collapse this folder; keep cursor here (still visible).
					m.collapsed[collapseKey(cur)] = true
					m.clampScroll()
				} else {
					// Already collapsed: jump to parent.
					// Parent is either the folder one level up (segments[:n-1])
					// or the provider header if this is a top-level folder.
					if len(cur.segments) > 1 {
						parentSegs := cur.segments[:len(cur.segments)-1]
						m.jumpToFolderHeaderBySegments(cur.provider, parentSegs)
					} else {
						m.jumpToProviderHeader(cur.provider)
					}
				}
			case nodeKindFile:
				// Jump to the immediate parent: the folder whose segments match
				// all but the last segment of the file's segments.
				// If the file is directly under the provider (len==1), jump to
				// the provider header.
				if len(cur.segments) > 1 {
					parentSegs := cur.segments[:len(cur.segments)-1]
					m.jumpToFolderHeaderBySegments(cur.provider, parentSegs)
				} else {
					m.jumpToProviderHeader(cur.provider)
				}
			}

		case "right":
			if m.cursor < 0 || m.cursor >= len(visible) {
				break
			}
			cur := visible[m.cursor]
			key := collapseKey(cur)
			if key != "" && m.collapsed[key] {
				m.collapsed[key] = false
				m.clampScroll()
			}

		case " ":
			if m.cursor < 0 || m.cursor >= len(visible) {
				break
			}
			cur := visible[m.cursor]
			switch cur.kind {
			case nodeKindFile:
				m.chosen[cur.archPath] = !m.chosen[cur.archPath]
			case nodeKindFolder:
				m.toggleFolder(cur.provider, cur.segments)
			case nodeKindProvider:
				m.toggleProvider(cur.provider)
			}

		case "a":
			for _, n := range m.nodes {
				if n.kind == nodeKindFile {
					m.chosen[n.archPath] = true
				}
			}
		case "n":
			m.chosen = make(map[string]bool)

		case "enter":
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// jumpToProviderHeader moves the cursor to the provider header for the given
// provider name among the currently visible nodes.
func (m *FilePickerModel) jumpToProviderHeader(provider string) {
	visible := m.visibleNodes()
	for i, n := range visible {
		if n.kind == nodeKindProvider && n.provider == provider {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
}

// jumpToFolderHeaderBySegments moves the cursor to the folder header whose
// provider and segments match exactly.
func (m *FilePickerModel) jumpToFolderHeaderBySegments(provider string, segments []string) {
	visible := m.visibleNodes()
	for i, n := range visible {
		if n.kind == nodeKindFolder && n.provider == provider && segmentsEqual(n.segments, segments) {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
	// Fallback: if the exact folder is not visible (e.g. it was also collapsed),
	// walk up to provider.
	m.jumpToProviderHeader(provider)
}

// segmentsEqual returns true when a and b have identical length and content.
func segmentsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// segmentsHasPrefix returns true when candidate starts with prefix.
func segmentsHasPrefix(candidate, prefix []string) bool {
	if len(candidate) < len(prefix) {
		return false
	}
	for i, s := range prefix {
		if candidate[i] != s {
			return false
		}
	}
	return true
}

// toggleFolder selects all descendants if any are unselected, else deselects all.
// folderSegs are the segments identifying this folder.
func (m *FilePickerModel) toggleFolder(provider string, folderSegs []string) {
	children := m.filesForFolderBySegments(provider, folderSegs)
	allSelected := true
	for _, ap := range children {
		if !m.chosen[ap] {
			allSelected = false
			break
		}
	}
	for _, ap := range children {
		m.chosen[ap] = !allSelected
	}
}

// toggleProvider selects all children of the provider if any are unselected,
// else deselects all.
func (m *FilePickerModel) toggleProvider(provider string) {
	var children []string
	for _, n := range m.nodes {
		if n.kind == nodeKindFile && n.provider == provider {
			children = append(children, n.archPath)
		}
	}
	allSelected := true
	for _, ap := range children {
		if !m.chosen[ap] {
			allSelected = false
			break
		}
	}
	for _, ap := range children {
		m.chosen[ap] = !allSelected
	}
}

// moveCursorV moves the cursor by delta positions among the pre-computed
// visible slice, clamped to valid bounds, then scrolls to keep it visible.
// Callers should pass the result of visibleNodes() so it is not recomputed.
func (m *FilePickerModel) moveCursorV(delta int, visible []treeNode) {
	n := len(visible)
	if n == 0 {
		return
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= n {
		next = n - 1
	}
	m.cursor = next
	m.clampScroll()
}

// visibleNodes returns the subset of nodes that should be rendered, respecting
// collapse state.
//
// Visibility rules (N-level generalisation):
//   - Provider headers are always shown.
//   - A folder node at depth D is visible iff its provider is not collapsed AND
//     none of its ancestor folder prefixes are collapsed.
//   - A file node is visible iff its provider is not collapsed AND none of the
//     folder ancestors implied by its segments are collapsed.
//
// Ancestor check for a node with k segments: check collapse keys for every
// prefix of length 1..k-1 (for folders) or 1..k-1 (for files, where the last
// segment is the filename).
func (m *FilePickerModel) visibleNodes() []treeNode {
	out := make([]treeNode, 0, len(m.nodes))
	for _, n := range m.nodes {
		switch n.kind {
		case nodeKindProvider:
			out = append(out, n)

		case nodeKindFolder:
			if m.collapsed[n.provider] {
				continue
			}
			// A folder is visible unless one of its *strict ancestors* is
			// collapsed.  Its own collapsed state only hides its children, not
			// itself.  Pass the parent path (all segments except the last) so
			// we check depths 1 … len-1 only.
			if len(n.segments) > 1 && m.anyAncestorFolderCollapsed(n.provider, n.segments[:len(n.segments)-1]) {
				continue
			}
			out = append(out, n)

		case nodeKindFile:
			if m.collapsed[n.provider] {
				continue
			}
			// A file is hidden when any of its ancestor folders (segments[:1]
			// through segments[:len-1]) is collapsed.
			if len(n.segments) > 1 && m.anyAncestorFolderCollapsed(n.provider, n.segments[:len(n.segments)-1]) {
				continue
			}
			out = append(out, n)
		}
	}
	return out
}

// anyAncestorFolderCollapsed returns true when any prefix of segs (from
// length 1 up to len(segs)) maps to a collapsed folder key.  Callers are
// responsible for passing only the *ancestor* portion of a node's segments —
// i.e. they must strip the node's own segment before calling this so that the
// node's own collapse key is never tested here.
func (m *FilePickerModel) anyAncestorFolderCollapsed(provider string, segs []string) bool {
	for depth := 1; depth <= len(segs); depth++ {
		key := folderCollapseKey(provider, segs[:depth])
		if m.collapsed[key] {
			return true
		}
	}
	return false
}

// triState returns ' ', '~', or '·' based on how many of the given archPaths
// are selected.
func (m FilePickerModel) triState(archPaths []string) rune {
	if len(archPaths) == 0 {
		return ' '
	}
	selCount := 0
	for _, ap := range archPaths {
		if m.chosen[ap] {
			selCount++
		}
	}
	switch selCount {
	case 0:
		return ' '
	case len(archPaths):
		return '·'
	default:
		return '~'
	}
}

// filesForProvider returns all archPaths directly under the given provider.
func (m FilePickerModel) filesForProvider(provider string) []string {
	var out []string
	for _, n := range m.nodes {
		if n.kind == nodeKindFile && n.provider == provider {
			out = append(out, n.archPath)
		}
	}
	return out
}

// filesForFolderBySegments returns all archPaths for file nodes whose provider
// matches and whose segments have folderSegs as a prefix.  This includes files
// at any depth inside the folder.
func (m FilePickerModel) filesForFolderBySegments(provider string, folderSegs []string) []string {
	var out []string
	for _, n := range m.nodes {
		if n.kind != nodeKindFile || n.provider != provider {
			continue
		}
		// A file belongs to this folder iff the file's segments (excluding the
		// filename, i.e. all but the last) start with folderSegs, OR the file is
		// directly inside (its full segments start with folderSegs and has at
		// least one more component).
		if len(n.segments) > len(folderSegs) && segmentsHasPrefix(n.segments, folderSegs) {
			out = append(out, n.archPath)
		}
	}
	return out
}

func (m FilePickerModel) View() string {
	if len(m.nodes) == 0 {
		return MutedStyle.Render("No files found in this backup.") + "\n"
	}

	var sb strings.Builder
	sb.WriteString(AccentStyle.Render("Select files to restore") + "\n\n")

	visible := m.visibleNodes()
	total := len(visible)
	vh := m.viewportHeight()

	// Clamp offset defensively (in case winHeight arrived between cursor moves).
	scrollOff := m.scrollOffset
	maxOff := total - vh
	if maxOff < 0 {
		maxOff = 0
	}
	if scrollOff > maxOff {
		scrollOff = maxOff
	}
	if scrollOff < 0 {
		scrollOff = 0
	}

	end := scrollOff + vh
	if end > total {
		end = total
	}
	window := visible[scrollOff:end]

	for i, node := range window {
		absIdx := scrollOff + i
		isCursor := absIdx == m.cursor

		cursorStr := "  "
		if isCursor {
			cursorStr = "▸ "
		}

		var line string
		switch node.kind {
		case nodeKindProvider:
			collapseIcon := "▾"
			if m.collapsed[node.provider] {
				collapseIcon = "▸"
			}
			files := m.filesForProvider(node.provider)
			state := m.triState(files)
			selCount := 0
			for _, ap := range files {
				if m.chosen[ap] {
					selCount++
				}
			}
			total := len(files)
			label := fmt.Sprintf("%s [%s]  [%c] %d/%d files",
				collapseIcon, node.provider, state, selCount, total)
			if isCursor {
				line = SelectedStyle.Render(cursorStr + label)
			} else {
				line = AccentStyle.Render("  " + label)
			}

		case nodeKindFolder:
			key := collapseKey(node)
			collapseIcon := "▾"
			if m.collapsed[key] {
				collapseIcon = "▸"
			}
			files := m.filesForFolderBySegments(node.provider, node.segments)
			state := m.triState(files)
			selCount := 0
			for _, ap := range files {
				if m.chosen[ap] {
					selCount++
				}
			}
			total := len(files)
			// Display only the last segment as the folder name (e.g. "foo" not
			// "skills/foo"), since the tree indentation supplies the context.
			displayName := node.segments[len(node.segments)-1]
			label := fmt.Sprintf("%s %s/  [%c] %d/%d",
				collapseIcon, displayName, state, selCount, total)
			// depth = len(segments)-1 extra indent levels (top-level folders
			// have depth 0 extra beyond the base 2-space provider-children indent).
			extraDepth := len(node.segments) - 1
			baseIndent := strings.Repeat("  ", extraDepth)
			if isCursor {
				line = SelectedStyle.Render("▸ " + "  " + baseIndent + label)
			} else {
				line = "  " + IndigoStyle.Render(baseIndent+"  "+label)
			}

		case nodeKindFile:
			state := ' '
			if m.chosen[node.archPath] {
				state = '·'
			}
			// Display name: always the basename for files under any folder;
			// relative-to-provider path for files directly under the provider.
			var displayName string
			if len(node.segments) > 1 {
				displayName = path.Base(node.archPath)
			} else {
				// File is directly under the provider root.
				displayName = strings.TrimPrefix(node.archPath, node.provider+"/")
			}
			label := fmt.Sprintf("[%c] %s", state, displayName)
			// Indent: files under a folder at depth D get 2*(D+1) spaces of
			// extra indent (D = len(segments)-1 for the parent folder).
			// Files directly under the provider (len==1) get 0 extra.
			if isCursor {
				extraDepth := len(node.segments) - 1
				indentStr := "  " + strings.Repeat("  ", extraDepth)
				line = SelectedStyle.Render("▸ " + indentStr + label)
			} else {
				// Non-cursor files: base 4 spaces (2 for provider children, 2
				// for the standard file indent), plus 2 per additional depth level.
				extraDepth := len(node.segments) - 1
				indentStr := "  " + strings.Repeat("  ", extraDepth)
				line = indentStr + NormalStyle.Render("  "+label)
			}
		}

		sb.WriteString(line)
		sb.WriteRune('\n')
	}

	// ── footer ─────────────────────────────────────────────────────────────
	sb.WriteRune('\n')

	// scroll indicator
	indicator := ""
	if total > vh {
		above := scrollOff > 0
		below := end < total
		switch {
		case above && below:
			indicator = fmt.Sprintf(" ↑↓ row %d/%d", m.cursor+1, total)
		case above:
			indicator = fmt.Sprintf(" ↑ row %d/%d", m.cursor+1, total)
		case below:
			indicator = fmt.Sprintf(" ↓ row %d/%d", m.cursor+1, total)
		}
	}

	help := "Space=toggle  a=all  n=none  ←=collapse  →=expand  PgUp/PgDn=scroll  Enter=confirm  q=quit"
	if indicator != "" {
		sb.WriteString(MutedStyle.Render(help) + MutedStyle.Render(indicator))
	} else {
		sb.WriteString(MutedStyle.Render(help))
	}
	sb.WriteRune('\n')
	return sb.String()
}

// Cancelled reports whether the user backed out.
func (m FilePickerModel) Cancelled() bool { return m.cancelled }

// SelectedArchPaths returns the archive paths of all currently-selected files
// in deterministic order.
func (m FilePickerModel) SelectedArchPaths() []string {
	out := make([]string, 0, len(m.chosen))
	for _, n := range m.nodes {
		if n.kind == nodeKindFile && m.chosen[n.archPath] {
			out = append(out, n.archPath)
		}
	}
	return out
}
