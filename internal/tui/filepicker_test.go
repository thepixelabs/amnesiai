package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thepixelabs/amnesiai/internal/core"
)

// sampleEntries returns a small set of ManifestEntry values spanning two
// providers, used by most filepicker tests.
// alice has two files: one inside agents/ folder, one at root level.
// bob has one file at root level.
func sampleEntries() []core.ManifestEntry {
	return []core.ManifestEntry{
		{Provider: "alice", ArchPath: "alice/config.json", OrigPath: "config.json"},
		{Provider: "alice", ArchPath: "alice/agents/foo.md", OrigPath: "agents/foo.md"},
		{Provider: "bob", ArchPath: "bob/settings.toml", OrigPath: "settings.toml"},
	}
}

// send delivers a single key message to a model and returns the next state.
func send(m FilePickerModel, key string) FilePickerModel {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	if fp, ok := next.(FilePickerModel); ok {
		return fp
	}
	return m
}

// sendKey is like send but accepts a tea.KeyType (e.g. tea.KeyEnter).
func sendKey(m FilePickerModel, k tea.KeyType) FilePickerModel {
	next, _ := m.Update(tea.KeyMsg{Type: k})
	if fp, ok := next.(FilePickerModel); ok {
		return fp
	}
	return m
}

// sendWindowSize injects a tea.WindowSizeMsg to simulate a terminal resize.
func sendWindowSize(m FilePickerModel, w, h int) FilePickerModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	if fp, ok := next.(FilePickerModel); ok {
		return fp
	}
	return m
}

// ----------------------------------------------------------------------------
// Construction
// ----------------------------------------------------------------------------

// TestNewFilePickerModel_SelectsAllByDefault verifies that an empty defaults
// slice causes all entries to be pre-selected.
func TestNewFilePickerModel_SelectsAllByDefault(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	got := m.SelectedArchPaths()
	if len(got) != 3 {
		t.Errorf("SelectedArchPaths() = %v (len %d), want all 3 pre-selected", got, len(got))
	}

	// Identity check: every expected arch path must be present in the result.
	expected := map[string]bool{
		"alice/config.json":   true,
		"alice/agents/foo.md": true,
		"bob/settings.toml":   true,
	}
	gotSet := make(map[string]bool, len(got))
	for _, p := range got {
		gotSet[p] = true
	}
	for want := range expected {
		if !gotSet[want] {
			t.Errorf("SelectedArchPaths() missing expected path %q; got %v", want, got)
		}
	}
	for p := range gotSet {
		if !expected[p] {
			t.Errorf("SelectedArchPaths() contains unexpected path %q; got %v", p, got)
		}
	}
}

// TestNewFilePickerModel_HonoursDefaults verifies that a partial defaults list
// pre-selects only those paths.
func TestNewFilePickerModel_HonoursDefaults(t *testing.T) {
	defaults := []string{"alice/config.json"}
	m := NewFilePickerModel(sampleEntries(), defaults)
	got := m.SelectedArchPaths()
	if len(got) != 1 || got[0] != "alice/config.json" {
		t.Errorf("SelectedArchPaths() = %v, want [alice/config.json]", got)
	}
}

// TestNewFilePickerModel_EmptyEntries verifies that an empty entry list
// produces a model with no selection and no panic.
func TestNewFilePickerModel_EmptyEntries(t *testing.T) {
	m := NewFilePickerModel(nil, nil)
	if len(m.SelectedArchPaths()) != 0 {
		t.Errorf("expected empty selection for empty entry list")
	}
}

// ----------------------------------------------------------------------------
// Toggle — file-level
// ----------------------------------------------------------------------------

// TestFilePickerModel_SpaceToggle verifies that pressing space on a file node
// toggles its selection.
// Tree for sampleEntries (sorted):
//
//	row 0: provider alice
//	row 1: folder agents/
//	row 2: file alice/agents/foo.md   (under folder agents/)
//	row 3: file alice/config.json     (directly under alice, sorts after agents/)
//	row 4: provider bob
//	row 5: file bob/settings.toml
//
// We move to row 2 (first file, inside agents/) and toggle it.
func TestFilePickerModel_SpaceToggle(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil) // all selected

	// Move cursor: 0=alice header, 1=agents/ folder, 2=foo.md file
	m = send(m, "j")
	m = send(m, "j")

	// Toggle the file off (alice/agents/foo.md).
	m = send(m, " ")
	got := m.SelectedArchPaths()
	for _, p := range got {
		if p == "alice/agents/foo.md" {
			t.Errorf("alice/agents/foo.md should have been deselected; got %v", got)
		}
	}
	if len(got) != 2 {
		t.Errorf("after deselect expected 2 selected, got %d: %v", len(got), got)
	}

	// Toggle it back on.
	m = send(m, " ")
	got = m.SelectedArchPaths()
	found := false
	for _, p := range got {
		if p == "alice/agents/foo.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("alice/agents/foo.md should be re-selected after second toggle; got %v", got)
	}
}

// ----------------------------------------------------------------------------
// Select-all / deselect-all
// ----------------------------------------------------------------------------

// TestFilePickerModel_SelectAll verifies that pressing "a" selects every file.
func TestFilePickerModel_SelectAll(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), []string{"nonexistent"})
	if len(m.SelectedArchPaths()) != 0 {
		t.Fatalf("pre-condition: expected 0 selected, got %v", m.SelectedArchPaths())
	}

	m = send(m, "a")
	got := m.SelectedArchPaths()
	if len(got) != 3 {
		t.Errorf("after 'a', expected 3 selected, got %v", got)
	}

	// Identity check: every expected arch path must be present.
	expected := map[string]bool{
		"alice/config.json":   true,
		"alice/agents/foo.md": true,
		"bob/settings.toml":   true,
	}
	gotSet := make(map[string]bool, len(got))
	for _, p := range got {
		gotSet[p] = true
	}
	for want := range expected {
		if !gotSet[want] {
			t.Errorf("after 'a', SelectedArchPaths() missing %q; got %v", want, got)
		}
	}
	for p := range gotSet {
		if !expected[p] {
			t.Errorf("after 'a', SelectedArchPaths() contains unexpected path %q; got %v", p, got)
		}
	}
}

// TestFilePickerModel_DeselectAll verifies that pressing "n" clears the selection.
func TestFilePickerModel_DeselectAll(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil) // all selected
	m = send(m, "n")
	if len(m.SelectedArchPaths()) != 0 {
		t.Errorf("after 'n', expected 0 selected, got %v", m.SelectedArchPaths())
	}
}

// ----------------------------------------------------------------------------
// Cancel
// ----------------------------------------------------------------------------

// TestFilePickerModel_QCancels verifies that pressing "q" sets Cancelled=true.
func TestFilePickerModel_QCancels(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	m = send(m, "q")
	if !m.Cancelled() {
		t.Error("expected Cancelled()=true after 'q'")
	}
}

// TestFilePickerModel_EscCancels verifies that the Esc key sets Cancelled=true.
func TestFilePickerModel_EscCancels(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	m = sendKey(m, tea.KeyEsc)
	if !m.Cancelled() {
		t.Error("expected Cancelled()=true after Esc")
	}
}

// TestFilePickerModel_EnterDoesNotCancel verifies Enter confirms without
// setting Cancelled.
func TestFilePickerModel_EnterDoesNotCancel(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	m = sendKey(m, tea.KeyEnter)
	if m.Cancelled() {
		t.Error("expected Cancelled()=false after Enter")
	}
}

// ----------------------------------------------------------------------------
// Collapse / expand
// ----------------------------------------------------------------------------

// TestFilePickerModel_CollapseHidesFileRows verifies that pressing left-arrow
// on a provider header collapses it, hiding all children (folder + files).
func TestFilePickerModel_CollapseHidesFileRows(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	// cursor starts on alice header (row 0)
	beforeVisible := len(m.visibleNodes())

	m = sendKey(m, tea.KeyLeft)
	afterVisible := len(m.visibleNodes())

	// alice has: 1 folder header + 2 files = 3 nodes hidden when collapsed
	wantReduction := 3
	if afterVisible != beforeVisible-wantReduction {
		t.Errorf("visible rows after collapse: got %d, want %d (before=%d)",
			afterVisible, beforeVisible-wantReduction, beforeVisible)
	}
}

// TestFilePickerModel_ExpandRestoresFileRows verifies that pressing right-arrow
// after collapsing restores the file rows.
func TestFilePickerModel_ExpandRestoresFileRows(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	before := len(m.visibleNodes())

	m = sendKey(m, tea.KeyLeft)  // collapse alice provider
	m = sendKey(m, tea.KeyRight) // expand alice provider
	after := len(m.visibleNodes())

	if after != before {
		t.Errorf("visible rows after expand: got %d, want %d", after, before)
	}
}

// ----------------------------------------------------------------------------
// View smoke test
// ----------------------------------------------------------------------------

// TestFilePickerModel_ViewRenders verifies View() does not panic and returns
// non-empty output.
func TestFilePickerModel_ViewRenders(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	v := m.View()
	if v == "" {
		t.Error("View() returned empty string")
	}
}

// TestFilePickerModel_ViewEmptyState verifies that View() returns the
// no-files message rather than panicking when the model has no rows.
func TestFilePickerModel_ViewEmptyState(t *testing.T) {
	m := NewFilePickerModel(nil, nil)
	v := m.View()
	if v == "" {
		t.Fatal("View() returned empty string for empty model")
	}
	if !containsSubstr(v, "No files") {
		t.Errorf("View() for empty model = %q; want it to mention 'No files'", v)
	}
}

// TestFilePickerModel_CollapsedGroupRetainsSelection verifies that collapsing
// a provider group is a display-only operation: SelectedArchPaths still
// returns the paths that were checked inside the now-hidden group.
func TestFilePickerModel_CollapsedGroupRetainsSelection(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil) // all selected
	// Cursor starts at alice header. Collapse alice.
	m = sendKey(m, tea.KeyLeft)

	// Alice's files are hidden from View, but selection must be intact.
	got := m.SelectedArchPaths()
	if len(got) != 3 {
		t.Errorf("SelectedArchPaths() after collapse = %v (len %d), want all 3 still selected", got, len(got))
	}

	aliceCount := 0
	for _, p := range got {
		if p == "alice/config.json" || p == "alice/agents/foo.md" {
			aliceCount++
		}
	}
	if aliceCount != 2 {
		t.Errorf("expected both alice paths in SelectedArchPaths after collapse; got %v", got)
	}
}

// ----------------------------------------------------------------------------
// New tests — folder grouping
// ----------------------------------------------------------------------------

// TestFilePickerModel_FoldersGroupFilesByFirstSegment verifies that two files
// sharing the same first path segment are placed under a single folder node.
func TestFilePickerModel_FoldersGroupFilesByFirstSegment(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/agents/a.md", OrigPath: "agents/a.md"},
		{Provider: "claude", ArchPath: "claude/agents/b.md", OrigPath: "agents/b.md"},
	}
	m := NewFilePickerModel(entries, nil)
	visible := m.visibleNodes()

	// Expected layout: provider(claude), folder(agents/), file(a.md), file(b.md)
	if len(visible) != 4 {
		t.Fatalf("expected 4 visible nodes, got %d: %v", len(visible), visible)
	}
	if visible[0].kind != nodeKindProvider {
		t.Errorf("node 0: want provider, got %v", visible[0].kind)
	}
	if visible[1].kind != nodeKindFolder || !segmentsEqual(visible[1].segments, []string{"agents"}) {
		t.Errorf("node 1: want folder with segments [agents], got kind=%v segments=%v", visible[1].kind, visible[1].segments)
	}
	if visible[2].kind != nodeKindFile || visible[2].archPath != "claude/agents/a.md" {
		t.Errorf("node 2: want file claude/agents/a.md, got %v %q", visible[2].kind, visible[2].archPath)
	}
	if visible[3].kind != nodeKindFile || visible[3].archPath != "claude/agents/b.md" {
		t.Errorf("node 3: want file claude/agents/b.md, got %v %q", visible[3].kind, visible[3].archPath)
	}
}

// TestFilePickerModel_FilesWithoutFolderUnderProvider verifies that a file
// with no subdirectory appears directly under the provider header with no
// intervening folder node.
func TestFilePickerModel_FilesWithoutFolderUnderProvider(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/CLAUDE.md", OrigPath: "CLAUDE.md"},
	}
	m := NewFilePickerModel(entries, nil)
	visible := m.visibleNodes()

	// Expected layout: provider(claude), file(CLAUDE.md) — no folder node
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible nodes (provider + file), got %d: %v", len(visible), visible)
	}
	if visible[0].kind != nodeKindProvider {
		t.Errorf("node 0: want provider, got %v", visible[0].kind)
	}
	if visible[1].kind != nodeKindFile {
		t.Errorf("node 1: want file, got %v", visible[1].kind)
	}
	// A root-level file has exactly one segment (the filename); no folder parent.
	if len(visible[1].segments) != 1 {
		t.Errorf("node 1: want 1 segment (root-level file), got %v", visible[1].segments)
	}
}

// ----------------------------------------------------------------------------
// New tests — folder-level toggle (space on folder header)
// ----------------------------------------------------------------------------

// TestFilePickerModel_SpaceOnFolderTogglesAllChildren verifies that pressing
// space on a folder node selects all children when any are unselected, and
// deselects all when all are already selected.
func TestFilePickerModel_SpaceOnFolderTogglesAllChildren(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/agents/a.md", OrigPath: "agents/a.md"},
		{Provider: "claude", ArchPath: "claude/agents/b.md", OrigPath: "agents/b.md"},
	}
	m := NewFilePickerModel(entries, []string{"nonexistent"}) // start with nothing selected

	// Visible: 0=provider, 1=folder agents/, 2=a.md, 3=b.md
	// Move cursor to folder header (row 1).
	m = send(m, "j")
	if m.cursor != 1 {
		t.Fatalf("expected cursor at 1, got %d", m.cursor)
	}

	// Press space — should select all children.
	m = send(m, " ")
	got := m.SelectedArchPaths()
	if len(got) != 2 {
		t.Errorf("after space on folder with none selected: want 2 selected, got %v", got)
	}

	// Press space again — should deselect all children.
	m = send(m, " ")
	got = m.SelectedArchPaths()
	if len(got) != 0 {
		t.Errorf("after second space on folder: want 0 selected, got %v", got)
	}
}

// ----------------------------------------------------------------------------
// New tests — tri-state parent marker
// ----------------------------------------------------------------------------

// TestFilePickerModel_TriStateParentMarker verifies the triState helper returns
// the correct rune for all-selected (·), none-selected ( ), and partial (~).
func TestFilePickerModel_TriStateParentMarker(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/agents/a.md", OrigPath: "agents/a.md"},
		{Provider: "claude", ArchPath: "claude/agents/b.md", OrigPath: "agents/b.md"},
	}

	// all selected
	m := NewFilePickerModel(entries, nil)
	if state := m.triState([]string{"claude/agents/a.md", "claude/agents/b.md"}); state != '·' {
		t.Errorf("all selected: want '·', got %q", state)
	}

	// none selected
	m2 := NewFilePickerModel(entries, []string{"nonexistent"})
	if state := m2.triState([]string{"claude/agents/a.md", "claude/agents/b.md"}); state != ' ' {
		t.Errorf("none selected: want ' ', got %q", state)
	}

	// partial (only a.md selected)
	m3 := NewFilePickerModel(entries, []string{"claude/agents/a.md"})
	if state := m3.triState([]string{"claude/agents/a.md", "claude/agents/b.md"}); state != '~' {
		t.Errorf("partial: want '~', got %q", state)
	}
}

// ----------------------------------------------------------------------------
// New tests — viewport
// ----------------------------------------------------------------------------

// TestFilePickerModel_ViewportScrollFollowsCursor verifies that when the
// terminal is small, scrollOffset advances to keep the cursor visible when the
// user moves past the bottom of the viewport.
func TestFilePickerModel_ViewportScrollFollowsCursor(t *testing.T) {
	// Build 20 file entries (one provider, one folder) — many more than the
	// tiny 10-row terminal we'll simulate.
	entries := make([]core.ManifestEntry, 20)
	for i := range entries {
		ap := fmt.Sprintf("prov/folder/file%02d.txt", i)
		entries[i] = core.ManifestEntry{Provider: "prov", ArchPath: ap, OrigPath: ap}
	}
	m := NewFilePickerModel(entries, nil)

	// Simulate a small terminal: height=10. viewportHeight = 10-6 = 4.
	m = sendWindowSize(m, 80, 10)

	// Total visible nodes: 1 provider + 1 folder + 20 files = 22.
	// Viewport = 4. Move cursor down 10 times — cursor should be at 10,
	// scrollOffset should have advanced so cursor stays on screen.
	for i := 0; i < 10; i++ {
		m = send(m, "j")
	}

	vh := m.viewportHeight()
	if m.cursor < m.scrollOffset || m.cursor >= m.scrollOffset+vh {
		t.Errorf("cursor %d out of viewport [%d, %d)",
			m.cursor, m.scrollOffset, m.scrollOffset+vh)
	}
	if m.scrollOffset == 0 {
		t.Errorf("scrollOffset should have advanced past 0 after moving 10 rows in a 4-row viewport")
	}
}

// TestFilePickerModel_PageUpDownJumps verifies that pgdn moves the cursor by
// approximately one viewport height, and pgup moves it back.
func TestFilePickerModel_PageUpDownJumps(t *testing.T) {
	entries := make([]core.ManifestEntry, 30)
	for i := range entries {
		ap := fmt.Sprintf("prov/folder/file%02d.txt", i)
		entries[i] = core.ManifestEntry{Provider: "prov", ArchPath: ap, OrigPath: ap}
	}
	m := NewFilePickerModel(entries, nil)
	m = sendWindowSize(m, 80, 12) // viewportHeight = 12-6 = 6

	startCursor := m.cursor // 0
	m = sendKey(m, tea.KeyPgDown)
	afterDown := m.cursor

	vh := m.viewportHeight()
	if afterDown < startCursor+vh-1 || afterDown > startCursor+vh+1 {
		t.Errorf("pgdn: cursor moved from %d to %d, want ~%d", startCursor, afterDown, startCursor+vh)
	}

	m = sendKey(m, tea.KeyPgUp)
	afterUp := m.cursor
	if afterUp > afterDown-vh+2 {
		t.Errorf("pgup: cursor moved from %d back to %d, want ~%d", afterDown, afterUp, afterDown-vh)
	}
}

// ----------------------------------------------------------------------------
// New tests — collapse hides visually but not from selection
// ----------------------------------------------------------------------------

// TestFilePickerModel_CollapseFolderHidesFilesNotSelection verifies that
// collapsing a folder removes children from visibleNodes but keeps them in
// SelectedArchPaths.
func TestFilePickerModel_CollapseFolderHidesFilesNotSelection(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/agents/a.md", OrigPath: "agents/a.md"},
		{Provider: "claude", ArchPath: "claude/agents/b.md", OrigPath: "agents/b.md"},
	}
	m := NewFilePickerModel(entries, nil) // both selected

	beforeVisible := len(m.visibleNodes()) // 4: provider + folder + 2 files

	// Move cursor to folder header (row 1) and press left to collapse it.
	m = send(m, "j") // cursor -> folder header
	m = sendKey(m, tea.KeyLeft)

	afterVisible := len(m.visibleNodes())
	if afterVisible != beforeVisible-2 {
		t.Errorf("expected %d visible nodes after folder collapse, got %d", beforeVisible-2, afterVisible)
	}

	// Both files must still be selected.
	got := m.SelectedArchPaths()
	if len(got) != 2 {
		t.Errorf("after folder collapse, SelectedArchPaths should return 2; got %v", got)
	}
}

// ----------------------------------------------------------------------------
// New tests — WindowSizeMsg edge cases
// ----------------------------------------------------------------------------

// TestFilePickerModel_WindowSizeZeroNoPanel verifies that View() does not panic
// and viewportHeight() floors to 4 when Height=0.
func TestFilePickerModel_WindowSizeZeroNoPanic(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	m = sendWindowSize(m, 80, 0)
	if m.viewportHeight() < 4 {
		t.Errorf("viewportHeight() = %d for Height=0, want >= 4", m.viewportHeight())
	}
	v := m.View()
	if v == "" {
		t.Error("View() returned empty string for Height=0")
	}
}

// TestFilePickerModel_WindowSizeOneNoPanic verifies no panic for Height=1.
func TestFilePickerModel_WindowSizeOneNoPanic(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	m = sendWindowSize(m, 80, 1)
	if m.viewportHeight() < 4 {
		t.Errorf("viewportHeight() = %d for Height=1, want >= 4", m.viewportHeight())
	}
	_ = m.View()
}

// TestFilePickerModel_WindowSizeSixNoPanic verifies no panic for Height=6 and
// that viewportHeight floors at 4 (6-6=0, floor to 4).
func TestFilePickerModel_WindowSizeSixNoPanic(t *testing.T) {
	m := NewFilePickerModel(sampleEntries(), nil)
	m = sendWindowSize(m, 80, 6)
	if m.viewportHeight() != 4 {
		t.Errorf("viewportHeight() = %d for Height=6, want 4 (floor)", m.viewportHeight())
	}
	_ = m.View()
}

// TestFilePickerModel_ResizeClampsScrollAndCursor verifies that a resize that
// shrinks the terminal keeps the cursor on a visible row and adjusts scrollOffset.
func TestFilePickerModel_ResizeClampsScrollAndCursor(t *testing.T) {
	entries := make([]core.ManifestEntry, 30)
	for i := range entries {
		ap := fmt.Sprintf("prov/folder/file%02d.txt", i)
		entries[i] = core.ManifestEntry{Provider: "prov", ArchPath: ap, OrigPath: ap}
	}
	m := NewFilePickerModel(entries, nil)
	m = sendWindowSize(m, 80, 30) // large terminal first

	// Move cursor to row 15.
	for i := 0; i < 15; i++ {
		m = send(m, "j")
	}
	if m.cursor != 15 {
		t.Fatalf("pre-condition: cursor = %d, want 15", m.cursor)
	}

	// Shrink terminal to 8 rows (viewportHeight = 8-6 = 2, floored to 4).
	m = sendWindowSize(m, 80, 8)

	vh := m.viewportHeight()
	if m.cursor < m.scrollOffset || m.cursor >= m.scrollOffset+vh {
		t.Errorf("after resize: cursor %d outside viewport [%d, %d)",
			m.cursor, m.scrollOffset, m.scrollOffset+vh)
	}
	// Ensure View() doesn't panic after the resize.
	_ = m.View()
}

// ----------------------------------------------------------------------------
// New tests — tri-state across multiple folders
// ----------------------------------------------------------------------------

// TestFilePickerModel_TriStateMultiFolderPartial verifies that a provider with
// folder A all-selected and folder B partial reports triState as '~'.
func TestFilePickerModel_TriStateMultiFolderPartial(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "prov", ArchPath: "prov/a/x.md", OrigPath: "a/x.md"},
		{Provider: "prov", ArchPath: "prov/a/y.md", OrigPath: "a/y.md"},
		{Provider: "prov", ArchPath: "prov/b/z.md", OrigPath: "b/z.md"},
	}
	// Select folder A (both files) and leave folder B unselected.
	m := NewFilePickerModel(entries, []string{"prov/a/x.md", "prov/a/y.md"})

	all := m.filesForProvider("prov")
	state := m.triState(all)
	if state != '~' {
		t.Errorf("tri-state for 2-of-3 selected: want '~', got %q", state)
	}

	// Select everything — state should flip to '·'.
	m = send(m, "a")
	state = m.triState(m.filesForProvider("prov"))
	if state != '·' {
		t.Errorf("tri-state for all 3 selected: want '·', got %q", state)
	}
}

// ----------------------------------------------------------------------------
// New tests — deep paths (N-level nesting)
// ----------------------------------------------------------------------------

// TestFilePickerModel_DeepPathGrouping verifies that claude/agents/sub/foo.md
// produces folder nodes for both "agents" and "agents/sub", plus the file node.
func TestFilePickerModel_DeepPathGrouping(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/agents/sub/foo.md", OrigPath: "agents/sub/foo.md"},
	}
	m := NewFilePickerModel(entries, nil)
	visible := m.visibleNodes()

	// Expected: provider(claude), folder(agents), folder(agents/sub), file
	if len(visible) != 4 {
		t.Fatalf("expected 4 visible nodes (provider + 2 folders + file), got %d: %v", len(visible), visible)
	}
	if visible[1].kind != nodeKindFolder || !segmentsEqual(visible[1].segments, []string{"agents"}) {
		t.Errorf("node 1: want folder segments [agents], got kind=%v segments=%v", visible[1].kind, visible[1].segments)
	}
	if visible[2].kind != nodeKindFolder || !segmentsEqual(visible[2].segments, []string{"agents", "sub"}) {
		t.Errorf("node 2: want folder segments [agents sub], got kind=%v segments=%v", visible[2].kind, visible[2].segments)
	}
	if visible[3].kind != nodeKindFile || visible[3].archPath != "claude/agents/sub/foo.md" {
		t.Errorf("node 3: want file claude/agents/sub/foo.md, got %v %q", visible[3].kind, visible[3].archPath)
	}
	// View must not panic.
	_ = m.View()
}

// TestPicker_NestsTwoLevels verifies that a 3-segment path (provider + 2
// folder components + file) produces exactly the right node layout.
func TestPicker_NestsTwoLevels(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
	}
	m := NewFilePickerModel(entries, nil)
	visible := m.visibleNodes()

	// Expected: provider, folder[skills], folder[skills/foo], file
	if len(visible) != 4 {
		t.Fatalf("NestsTwoLevels: want 4 nodes, got %d: %v", len(visible), visible)
	}
	if visible[1].kind != nodeKindFolder || !segmentsEqual(visible[1].segments, []string{"skills"}) {
		t.Errorf("node 1: want folder [skills], got %v %v", visible[1].kind, visible[1].segments)
	}
	if visible[2].kind != nodeKindFolder || !segmentsEqual(visible[2].segments, []string{"skills", "foo"}) {
		t.Errorf("node 2: want folder [skills foo], got %v %v", visible[2].kind, visible[2].segments)
	}
	if visible[3].kind != nodeKindFile || visible[3].archPath != "claude/skills/foo/SKILL.md" {
		t.Errorf("node 3: want file claude/skills/foo/SKILL.md, got %v %q", visible[3].kind, visible[3].archPath)
	}
}

// TestPicker_NestsThreeLevels verifies a 4-component path produces 3 intermediate
// folder nodes plus the file.
func TestPicker_NestsThreeLevels(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/a/b/c/deep.md", OrigPath: "a/b/c/deep.md"},
	}
	m := NewFilePickerModel(entries, nil)
	visible := m.visibleNodes()

	// Expected: provider, folder[a], folder[a/b], folder[a/b/c], file
	if len(visible) != 5 {
		t.Fatalf("NestsThreeLevels: want 5 nodes, got %d: %v", len(visible), visible)
	}
	if !segmentsEqual(visible[1].segments, []string{"a"}) {
		t.Errorf("node 1: want [a], got %v", visible[1].segments)
	}
	if !segmentsEqual(visible[2].segments, []string{"a", "b"}) {
		t.Errorf("node 2: want [a b], got %v", visible[2].segments)
	}
	if !segmentsEqual(visible[3].segments, []string{"a", "b", "c"}) {
		t.Errorf("node 3: want [a b c], got %v", visible[3].segments)
	}
	if visible[4].archPath != "claude/a/b/c/deep.md" {
		t.Errorf("node 4: want file claude/a/b/c/deep.md, got %q", visible[4].archPath)
	}
}

// TestPicker_SiblingDeepFolders verifies that two sibling sub-folders under a
// common parent each appear as distinct folder nodes.
func TestPicker_SiblingDeepFolders(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
		{Provider: "claude", ArchPath: "claude/skills/bar/REFERENCE.md", OrigPath: "skills/bar/REFERENCE.md"},
	}
	m := NewFilePickerModel(entries, nil)
	visible := m.visibleNodes()

	// Expected: provider, skills/, skills/bar/, REFERENCE.md, skills/foo/, SKILL.md
	// (sorted by archPath: bar < foo)
	if len(visible) != 6 {
		t.Fatalf("SiblingDeepFolders: want 6 nodes, got %d: %v", len(visible), visible)
	}
	// Node 1 must be skills/ (top-level folder)
	if visible[1].kind != nodeKindFolder || !segmentsEqual(visible[1].segments, []string{"skills"}) {
		t.Errorf("node 1: want folder [skills], got %v %v", visible[1].kind, visible[1].segments)
	}
	// Nodes 2 and 4 must be the two sibling folders (bar before foo alphabetically)
	if !segmentsEqual(visible[2].segments, []string{"skills", "bar"}) {
		t.Errorf("node 2: want [skills bar], got %v", visible[2].segments)
	}
	if !segmentsEqual(visible[4].segments, []string{"skills", "foo"}) {
		t.Errorf("node 4: want [skills foo], got %v", visible[4].segments)
	}
	// Files must follow their respective parent folders
	if visible[3].archPath != "claude/skills/bar/REFERENCE.md" {
		t.Errorf("node 3: want REFERENCE.md, got %q", visible[3].archPath)
	}
	if visible[5].archPath != "claude/skills/foo/SKILL.md" {
		t.Errorf("node 5: want SKILL.md, got %q", visible[5].archPath)
	}
}

// TestPicker_ToggleParentFolderCascades verifies that toggling a parent folder
// selects all descendants including those in nested sub-folders.
func TestPicker_ToggleParentFolderCascades(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
		{Provider: "claude", ArchPath: "claude/skills/foo/notes.md", OrigPath: "skills/foo/notes.md"},
		{Provider: "claude", ArchPath: "claude/skills/bar/REFERENCE.md", OrigPath: "skills/bar/REFERENCE.md"},
	}
	m := NewFilePickerModel(entries, []string{"nonexistent"}) // nothing selected

	// Find the skills/ folder node index in visibleNodes.
	visible := m.visibleNodes()
	skillsIdx := -1
	for i, n := range visible {
		if n.kind == nodeKindFolder && segmentsEqual(n.segments, []string{"skills"}) {
			skillsIdx = i
			break
		}
	}
	if skillsIdx < 0 {
		t.Fatal("skills/ folder not found in visible nodes")
	}

	// Move cursor to skills/ and press space to toggle all descendants.
	m.cursor = skillsIdx
	m = send(m, " ")

	got := m.SelectedArchPaths()
	expected := map[string]bool{
		"claude/skills/foo/SKILL.md":     true,
		"claude/skills/foo/notes.md":     true,
		"claude/skills/bar/REFERENCE.md": true,
	}
	if len(got) != len(expected) {
		t.Errorf("ToggleParentFolderCascades: want %d selected after space on skills/, got %d: %v", len(expected), len(got), got)
	}
	for _, p := range got {
		if !expected[p] {
			t.Errorf("ToggleParentFolderCascades: unexpected path in selection: %q", p)
		}
	}
	for want := range expected {
		found := false
		for _, p := range got {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ToggleParentFolderCascades: expected path missing from selection: %q", want)
		}
	}

	// Toggle off again — all should deselect.
	m = send(m, " ")
	got = m.SelectedArchPaths()
	if len(got) != 0 {
		t.Errorf("ToggleParentFolderCascades: want 0 after second toggle, got %d: %v", len(got), got)
	}
}

// TestPicker_TriStatePartialAcrossDepths verifies that a parent folder shows
// '~' when one descendant at a deeper level is unselected.
func TestPicker_TriStatePartialAcrossDepths(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
		{Provider: "claude", ArchPath: "claude/skills/foo/notes.md", OrigPath: "skills/foo/notes.md"},
	}
	// Select only the first file.
	m := NewFilePickerModel(entries, []string{"claude/skills/foo/SKILL.md"})

	// filesForFolderBySegments for ["skills"] should return both files.
	skillsFiles := m.filesForFolderBySegments("claude", []string{"skills"})
	if len(skillsFiles) != 2 {
		t.Fatalf("want 2 files under skills/, got %d: %v", len(skillsFiles), skillsFiles)
	}

	state := m.triState(skillsFiles)
	if state != '~' {
		t.Errorf("TriStatePartialAcrossDepths: want '~' for 1-of-2 selected, got %q", state)
	}
}

// TestPicker_CollapseIntermediateFolderHidesDescendants verifies that
// collapsing skills/ hides both skills/foo/ and the files under it.
func TestPicker_CollapseIntermediateFolderHidesDescendants(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
		{Provider: "claude", ArchPath: "claude/skills/foo/notes.md", OrigPath: "skills/foo/notes.md"},
	}
	m := NewFilePickerModel(entries, nil)
	beforeVisible := len(m.visibleNodes())
	// Expected before: provider, skills/, skills/foo/, SKILL.md, notes.md = 5 nodes

	// Find skills/ folder and collapse it.
	visible := m.visibleNodes()
	for i, n := range visible {
		if n.kind == nodeKindFolder && segmentsEqual(n.segments, []string{"skills"}) {
			m.cursor = i
			break
		}
	}
	m = sendKey(m, tea.KeyLeft) // collapse skills/

	afterVisible := len(m.visibleNodes())
	// skills/foo/ and both files are now hidden; only provider + skills/ remain.
	wantVisible := beforeVisible - 3
	if afterVisible != wantVisible {
		t.Errorf("CollapseIntermediateFolderHidesDescendants: before=%d, after=%d, want=%d",
			beforeVisible, afterVisible, wantVisible)
	}

	// The two surviving nodes must be specifically: provider(claude) and folder[skills].
	remaining := m.visibleNodes()
	if len(remaining) >= 1 && remaining[0].kind != nodeKindProvider {
		t.Errorf("CollapseIntermediateFolderHidesDescendants: remaining[0]: want nodeKindProvider, got %v", remaining[0].kind)
	}
	if len(remaining) >= 2 {
		if remaining[1].kind != nodeKindFolder {
			t.Errorf("CollapseIntermediateFolderHidesDescendants: remaining[1]: want nodeKindFolder, got %v", remaining[1].kind)
		}
		if !segmentsEqual(remaining[1].segments, []string{"skills"}) {
			t.Errorf("CollapseIntermediateFolderHidesDescendants: remaining[1]: want segments [skills], got %v", remaining[1].segments)
		}
	}

	_ = m.View()
}

// TestPicker_LeftArrowFromDeepFileJumpsToImmediateParentFolder verifies that
// pressing left on a file deep in the tree jumps to its immediate parent folder,
// not to the provider header.
func TestPicker_LeftArrowFromDeepFileJumpsToImmediateParentFolder(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
	}
	m := NewFilePickerModel(entries, nil)
	m = sendWindowSize(m, 80, 20)

	// Visible: 0=provider, 1=skills/, 2=skills/foo/, 3=SKILL.md
	// Move cursor to SKILL.md (row 3).
	visible := m.visibleNodes()
	fileIdx := -1
	for i, n := range visible {
		if n.kind == nodeKindFile {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 {
		t.Fatal("file node not found")
	}
	m.cursor = fileIdx
	m = sendKey(m, tea.KeyLeft)

	// Cursor must now be on skills/foo/ (segments [skills, foo]).
	visible = m.visibleNodes()
	if m.cursor < 0 || m.cursor >= len(visible) {
		t.Fatalf("cursor %d out of range after left arrow", m.cursor)
	}
	cur := visible[m.cursor]
	if cur.kind != nodeKindFolder || !segmentsEqual(cur.segments, []string{"skills", "foo"}) {
		t.Errorf("after left on SKILL.md: want folder [skills foo], got kind=%v segments=%v", cur.kind, cur.segments)
	}
}

// TestPicker_LeftArrowFromCollapsedDeepFolderJumpsToParentFolder verifies that
// pressing left on an already-collapsed sub-folder jumps to its parent folder,
// not to the provider header.
func TestPicker_LeftArrowFromCollapsedDeepFolderJumpsToParentFolder(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
	}
	m := NewFilePickerModel(entries, nil)
	m = sendWindowSize(m, 80, 20)

	// First collapse skills/foo/ by navigating to it and pressing left.
	visible := m.visibleNodes()
	fooIdx := -1
	for i, n := range visible {
		if n.kind == nodeKindFolder && segmentsEqual(n.segments, []string{"skills", "foo"}) {
			fooIdx = i
			break
		}
	}
	if fooIdx < 0 {
		t.Fatal("skills/foo/ not found")
	}
	m.cursor = fooIdx
	m = sendKey(m, tea.KeyLeft) // collapse skills/foo/

	// Now press left again on the (still-visible but now collapsed) skills/foo/.
	// This should jump to skills/ (the parent folder).
	m = sendKey(m, tea.KeyLeft)

	visible = m.visibleNodes()
	if m.cursor < 0 || m.cursor >= len(visible) {
		t.Fatalf("cursor %d out of range", m.cursor)
	}
	cur := visible[m.cursor]
	if cur.kind != nodeKindFolder || !segmentsEqual(cur.segments, []string{"skills"}) {
		t.Errorf("after left on collapsed skills/foo/: want folder [skills], got kind=%v segments=%v", cur.kind, cur.segments)
	}
}

// TestPicker_FilesAtProviderRootStillWorkAlongsideDeepFolders verifies that a
// manifest mixing a root-level file and a deep-nested file produces a correct
// layout with both accessible.
func TestPicker_FilesAtProviderRootStillWorkAlongsideDeepFolders(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/CLAUDE.md", OrigPath: "CLAUDE.md"},
		{Provider: "claude", ArchPath: "claude/skills/foo/SKILL.md", OrigPath: "skills/foo/SKILL.md"},
	}
	m := NewFilePickerModel(entries, nil)
	visible := m.visibleNodes()

	// Expected: provider, CLAUDE.md (root), skills/, skills/foo/, SKILL.md = 5 nodes
	// Root-level files sort before "skills/" because "" < "skills" lexicographically
	// (archPath "claude/CLAUDE.md" < "claude/skills/foo/SKILL.md").
	if len(visible) != 5 {
		t.Fatalf("FilesAtProviderRootAlongsideDeepFolders: want 5 nodes, got %d: %v", len(visible), visible)
	}
	// Root file comes first (sorts before skills/).
	if visible[1].kind != nodeKindFile || visible[1].archPath != "claude/CLAUDE.md" {
		t.Errorf("node 1: want root file CLAUDE.md, got kind=%v archPath=%q", visible[1].kind, visible[1].archPath)
	}
	if visible[2].kind != nodeKindFolder || !segmentsEqual(visible[2].segments, []string{"skills"}) {
		t.Errorf("node 2: want folder [skills], got %v %v", visible[2].kind, visible[2].segments)
	}
	// Both files must be selected (selectAll=true).
	got := m.SelectedArchPaths()
	if len(got) != 2 {
		t.Errorf("want 2 selected, got %d: %v", len(got), got)
	}
	// View must not panic.
	_ = m.View()
}

// ----------------------------------------------------------------------------
// New tests — defensive ArchPath mismatch (finding #6)
// ----------------------------------------------------------------------------

// TestFilePickerModel_MismatchedArchPathSkipped verifies that an entry whose
// ArchPath does not start with "<Provider>/" is silently skipped and never
// appears in SelectedArchPaths.
func TestFilePickerModel_MismatchedArchPathSkipped(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "alice", ArchPath: "alice/config.json", OrigPath: "config.json"},
		// Mismatched: ArchPath says "bob/" but Provider is "alice".
		{Provider: "alice", ArchPath: "bob/malicious.md", OrigPath: "malicious.md"},
	}
	m := NewFilePickerModel(entries, nil)

	paths := m.SelectedArchPaths()
	for _, p := range paths {
		if p == "bob/malicious.md" {
			t.Errorf("mismatched ArchPath should be skipped; got %v", paths)
		}
	}
	if len(paths) != 1 || paths[0] != "alice/config.json" {
		t.Errorf("SelectedArchPaths() = %v, want [alice/config.json]", paths)
	}
}

// ----------------------------------------------------------------------------
// New tests — cursor clamp after collapse (hidden region)
// ----------------------------------------------------------------------------

// TestFilePickerModel_CollapseProviderClampscursor verifies that collapsing a
// provider when the cursor is on one of its children moves the cursor to the
// (still-visible) provider header, not into a hidden region.
func TestFilePickerModel_CollapseProviderClampseCursor(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "alice", ArchPath: "alice/agents/foo.md", OrigPath: "agents/foo.md"},
		{Provider: "alice", ArchPath: "alice/agents/bar.md", OrigPath: "agents/bar.md"},
		{Provider: "bob", ArchPath: "bob/settings.toml", OrigPath: "settings.toml"},
	}
	m := NewFilePickerModel(entries, nil)
	m = sendWindowSize(m, 80, 20)

	// Tree: 0=alice, 1=agents/, 2=foo.md, 3=bar.md, 4=bob, 5=settings.toml
	// Move cursor to row 3 (bar.md under alice/agents/).
	for i := 0; i < 3; i++ {
		m = send(m, "j")
	}
	if m.cursor != 3 {
		t.Fatalf("pre-condition: cursor = %d, want 3 (bar.md)", m.cursor)
	}

	// Collapse the alice provider header: move cursor back to row 0, then left.
	m.cursor = 0
	m = sendKey(m, tea.KeyLeft) // collapse alice

	// cursor must be on a visible row; alice provider header (row 0) is always visible.
	visible := m.visibleNodes()
	if m.cursor < 0 || m.cursor >= len(visible) {
		t.Errorf("after provider collapse: cursor=%d out of visible range [0,%d)", m.cursor, len(visible))
	}
	// Verify View() doesn't panic or index-out-of-bounds.
	_ = m.View()
}

// TestFilePickerModel_CollapseFolderClampseCursor verifies that collapsing a
// folder when the cursor is on a file inside that folder keeps the cursor on
// the folder header (still visible).
func TestFilePickerModel_CollapseFolderClampseCursor(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/agents/a.md", OrigPath: "agents/a.md"},
		{Provider: "claude", ArchPath: "claude/agents/b.md", OrigPath: "agents/b.md"},
		{Provider: "claude", ArchPath: "claude/agents/c.md", OrigPath: "agents/c.md"},
	}
	m := NewFilePickerModel(entries, nil)
	m = sendWindowSize(m, 80, 20)

	// Tree: 0=claude, 1=agents/, 2=a.md, 3=b.md, 4=c.md
	// Put cursor on c.md (row 4), then collapse agents/ via its header.
	for i := 0; i < 4; i++ {
		m = send(m, "j")
	}
	if m.cursor != 4 {
		t.Fatalf("pre-condition: cursor = %d, want 4 (c.md)", m.cursor)
	}

	// Move cursor to agents/ header (row 1) and collapse it.
	m.cursor = 1
	m = sendKey(m, tea.KeyLeft) // collapse agents/

	visible := m.visibleNodes()
	if m.cursor < 0 || m.cursor >= len(visible) {
		t.Errorf("after folder collapse: cursor=%d out of visible range [0,%d)", m.cursor, len(visible))
	}
	_ = m.View()
}

// ----------------------------------------------------------------------------
// New tests — enter confirms selection
// ----------------------------------------------------------------------------

// TestFilePickerModel_EnterConfirmsSelection verifies that pressing enter returns
// Cancelled()==false and SelectedArchPaths() reflects the toggled state.
func TestFilePickerModel_EnterConfirmsSelection(t *testing.T) {
	entries := []core.ManifestEntry{
		{Provider: "claude", ArchPath: "claude/agents/a.md", OrigPath: "agents/a.md"},
		{Provider: "claude", ArchPath: "claude/agents/b.md", OrigPath: "agents/b.md"},
	}
	m := NewFilePickerModel(entries, nil) // both selected

	// Deselect b.md: cursor to row 3 then space.
	m = sendWindowSize(m, 80, 20)
	for i := 0; i < 3; i++ {
		m = send(m, "j")
	}
	m = send(m, " ") // deselect b.md

	// Confirm with enter.
	m = sendKey(m, tea.KeyEnter)

	if m.Cancelled() {
		t.Error("Cancelled() = true after enter, want false")
	}
	got := m.SelectedArchPaths()
	if len(got) != 1 || got[0] != "claude/agents/a.md" {
		t.Errorf("SelectedArchPaths() = %v, want [claude/agents/a.md]", got)
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// containsSubstr is a small helper so the test file has no import on strings.
func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
