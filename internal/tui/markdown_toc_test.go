package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/theme"
)

// tocDoc is the active document of a tocEditor: the fake document under another
// name, with the file path and text version the Contents section watches.
type tocDoc struct {
	*fakeDoc
	name    string
	path    string
	version uint64
}

func (d tocDoc) Name() string    { return d.name }
func (d tocDoc) Version() uint64 { return d.version }

// FilePath is the document's path, so the section's fold ranges can be published
// for it.
func (d tocDoc) FilePath() string {
	if d.path != "" {
		return d.path
	}
	return d.name
}

// tocEditor is a fake editor whose active buffer is a Markdown (or other) file
// the test names, whose text version the test can bump as an edit would, and
// which records the go-to-line jumps of the Contents section. It also implements
// the optional outline-fold ports: SetOutlineFolds records what the section
// published, and FoldedAt/ToggleFoldAt give the section a fold state to drive.
type tocEditor struct {
	*fakeEditor
	name    string
	version uint64
	gotos   []int

	// outlinePath, outlineRanges, outlineVersion and outlineSet are the last
	// ranges the section published, with the text version they describe.
	outlinePath    string
	outlineRanges  []fold.Range
	outlineVersion uint64
	outlineSet     bool
	// folded is the map of the fold starts the section toggled, keyed by line.
	folded map[int]bool
	// toggles are the fold starts the section toggled, in order.
	toggles []int
}

func newTOCEditor(name string, lines ...string) *tocEditor {
	e := &tocEditor{fakeEditor: newFakeEditor(lines...), name: name, folded: map[int]bool{}}
	e.tabs = []TabInfo{{Name: name, Active: true}}
	return e
}

func (e *tocEditor) Active() Document {
	return tocDoc{fakeDoc: e.doc, name: e.name, path: e.name, version: e.version}
}

// SetOutlineFolds records the heading fold ranges the Contents section published.
func (e *tocEditor) SetOutlineFolds(path string, ranges []fold.Range, textVersion uint64) {
	e.outlinePath, e.outlineRanges, e.outlineVersion, e.outlineSet = path, ranges, textVersion, true
}

// FoldedAt reports whether the fold starting on line is collapsed.
func (e *tocEditor) FoldedAt(line int) bool { return e.folded[line] }

// ToggleFoldAt collapses or expands the fold starting on line and records it.
func (e *tocEditor) ToggleFoldAt(line int) bool {
	e.folded[line] = !e.folded[line]
	e.toggles = append(e.toggles, line)
	return true
}

func (e *tocEditor) GotoLine(line, col int) error {
	e.gotos = append(e.gotos, line)
	e.doc.cursor = buffer.Pos{Line: line - 1}
	return nil
}

// OpenFile and OpenPreview complete the editor's file-opening port; the
// Contents section only needs the go-to-line half of it.
func (e *tocEditor) OpenFile(string) error    { return nil }
func (e *tocEditor) OpenPreview(string) error { return nil }

// newTOCModel builds a model with an open explorer column and one Markdown
// buffer, and gives it a frame. The first Update runs the Contents sync, which
// opens the section for a Markdown buffer.
func newTOCModel(t *testing.T, ed *tocEditor, cfg *MarkdownTOC) *Model {
	t.Helper()
	ed.quitAction = "quit"
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), ShowExplorer: true, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, MarkdownTOC: cfg})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// contentsSection is the Contents section's place in the frame, if it is on
// screen.
func contentsSection(l frame.Layout) (sidebar.Section, bool) {
	for _, sec := range l.Sections {
		if sec.Kind == sidebar.KindContents {
			return sec, true
		}
	}
	return sidebar.Section{}, false
}

// headingTitles is the visible title of every row of the Contents body.
func headingTitles(m *Model) []string {
	var out []string
	for pos := 0; pos < m.contents.Len(); pos++ {
		row, _ := m.contents.RowAt(pos - m.contents.Scroll())
		out = append(out, row.Heading.Title)
	}
	return out
}

const tocText = "# Title\n\nintro\n\n## Setup\n\n```sh\n# not a heading\n```\n\n## Usage\n"

func TestContentsAutoOpensForMarkdown(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", strings.Split(tocText, "\n")...)
	m := newTOCModel(t, ed, nil)
	if !m.contents.Open() {
		t.Fatal("the Contents section did not open for a Markdown buffer")
	}
	if got := strings.Join(headingTitles(m), ","); got != "Title,Setup,Usage" {
		t.Fatalf("rows = %s, want Title,Setup,Usage", got)
	}
	sec, ok := contentsSection(m.layout())
	if !ok {
		t.Fatal("the section is not in the layout")
	}
	if sec.Rows() != sidebar.DefaultRows {
		t.Fatalf("section rows = %d, want the default %d", sec.Rows(), sidebar.DefaultRows)
	}
	if m.contentsFocus {
		t.Fatal("the section took the keyboard on its own")
	}
}

func TestContentsStaysClosedForText(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("a.txt", "# not markdown")
	m := newTOCModel(t, ed, nil)
	if m.contents.Open() {
		t.Fatal("the Contents section opened for a text buffer")
	}
	if got := len(m.layout().Sections); got != 1 {
		t.Fatalf("sections = %d, want the explorer alone", got)
	}
}

func TestContentsAutoOpenDisabled(t *testing.T) {
	t.Parallel()
	cfg := DefaultMarkdownTOC()
	cfg.Settings.AutoOpen = false
	ed := newTOCEditor("notes.md", "# A")
	m := newTOCModel(t, ed, &cfg)
	if m.contents.Open() {
		t.Fatal("the section opened with auto_open off")
	}
	// The command opens it regardless.
	if err := m.Run("markdown_toc_show"); err != nil {
		t.Fatal(err)
	}
	if !m.contents.Open() || !m.contentsFocus {
		t.Fatalf("after Show Outline: open %v focus %v, want both true", m.contents.Open(), m.contentsFocus)
	}
}

func TestContentsCommands(t *testing.T) {
	t.Parallel()
	t.Run("close hides and gives the keyboard back", func(t *testing.T) {
		t.Parallel()
		ed := newTOCEditor("notes.md", "# A")
		m := newTOCModel(t, ed, nil)
		if err := m.Run("markdown_toc_show"); err != nil {
			t.Fatal(err)
		}
		m.Update(special(tea.KeyEscape))
		_ = m.Run("markdown_toc_show")
		if err := m.Run("markdown_toc_close"); err != nil {
			t.Fatal(err)
		}
		if m.contents.Open() || m.contentsFocus {
			t.Fatalf("after Close Outline: open %v focus %v, want both false", m.contents.Open(), m.contentsFocus)
		}
		if m.stage() != stageEditor {
			t.Fatalf("stage %v after closing, want the editor", m.stage())
		}
	})
	t.Run("show refuses a text buffer", func(t *testing.T) {
		t.Parallel()
		ed := newTOCEditor("a.txt", "text")
		m := newTOCModel(t, ed, nil)
		if err := m.Run("markdown_toc_show"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(m.notice, "Not a Markdown buffer") {
			t.Fatalf("notice = %q, want the Markdown refusal", m.notice)
		}
	})
	t.Run("a disabled section says so", func(t *testing.T) {
		t.Parallel()
		cfg := DefaultMarkdownTOC()
		cfg.Enabled = false
		ed := newTOCEditor("notes.md", "# A")
		m := newTOCModel(t, ed, &cfg)
		if err := m.Run("markdown_toc_show"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(m.notice, "disabled") {
			t.Fatalf("notice = %q, want the disabled refusal", m.notice)
		}
	})
}

func TestContentsReopensOnABufferSwitchAfterClose(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("one.md", "# One")
	m := newTOCModel(t, ed, nil)
	if err := m.Run("markdown_toc_close"); err != nil {
		t.Fatal(err)
	}
	if m.contents.Open() {
		t.Fatal("the section stayed open after Close Outline")
	}
	// A buffer activation opens it again, as Fresh remounts its plugin section.
	ed.name = "two.md"
	ed.doc.lines = []string{"# Two"}
	ed.tabs = []TabInfo{{Name: "two.md", Active: true}}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !m.contents.Open() {
		t.Fatal("the section did not come back on the buffer switch")
	}
	if got := strings.Join(headingTitles(m), ","); got != "Two" {
		t.Fatalf("rows = %s, want Two", got)
	}
}

func TestContentsFollowsTheCursor(t *testing.T) {
	t.Parallel()
	lines := []string{"# One", "text", "# Two", "more", "# Three"}
	ed := newTOCEditor("notes.md", lines...)
	m := newTOCModel(t, ed, nil)
	ed.doc.cursor = buffer.Pos{Line: 3}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if got, _ := m.contents.Selected(); got.Title != "Two" {
		t.Fatalf("selected = %q, want Two", got.Title)
	}
	// A caret move takes the selection with it.
	ed.doc.cursor = buffer.Pos{Line: 4}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if got, _ := m.contents.Selected(); got.Title != "Three" {
		t.Fatalf("selected = %q after the move, want Three", got.Title)
	}
}

func TestContentsFollowsTheViewportInScrollMode(t *testing.T) {
	t.Parallel()
	lines := []string{"# One", "text", "# Two", "more", "# Three"}
	ed := newTOCEditor("notes.md", lines...)
	cfg := DefaultMarkdownTOC()
	cfg.Settings.Follow = sidebar.FollowScroll
	m := newTOCModel(t, ed, &cfg)
	ed.doc.top = 2
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if got, _ := m.contents.Selected(); got.Title != "Two" {
		t.Fatalf("selected = %q, want the heading at the pane top", got.Title)
	}
	// The follow command flips the mode for the session.
	if err := m.Run("markdown_toc_toggle_follow"); err != nil {
		t.Fatal(err)
	}
	if got := m.contents.FollowMode(); got != sidebar.FollowCursor {
		t.Fatalf("follow = %v after the toggle, want cursor", got)
	}
}

func TestContentsEnterJumps(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two", "## Three")
	m := newTOCModel(t, ed, nil)
	if err := m.Run("markdown_toc_show"); err != nil {
		t.Fatal(err)
	}
	m.Update(special(tea.KeyDown)) // Two
	m.Update(special(tea.KeyEnter))
	if len(ed.gotos) != 1 || ed.gotos[0] != 2 {
		t.Fatalf("jumps = %v, want one to line 2", ed.gotos)
	}
	if m.contentsFocus {
		t.Fatal("the keyboard stayed in the sidebar after Enter")
	}
}

func TestContentsKeysBrowseAndFold(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two", "## Three")
	m := newTOCModel(t, ed, nil)
	if err := m.Run("markdown_toc_show"); err != nil {
		t.Fatal(err)
	}
	// Down moves the selection without moving the buffer caret.
	m.Update(special(tea.KeyDown))
	if got, _ := m.contents.Selected(); got.Title != "Two" {
		t.Fatalf("selected = %q, want Two", got.Title)
	}
	if ed.doc.cursor.Line != 0 {
		t.Fatalf("the caret moved to line %d, want it left on line 0", ed.doc.cursor.Line)
	}
	// Left on a leaf goes to its parent, then Left folds it.
	m.Update(special(tea.KeyLeft)) // parent One
	if got, _ := m.contents.Selected(); got.Title != "One" {
		t.Fatalf("selected = %q, want the parent One", got.Title)
	}
	m.Update(special(tea.KeyLeft)) // fold One
	if m.contents.Len() != 1 {
		t.Fatalf("rows = %d after folding, want 1", m.contents.Len())
	}
	// Escape leaves the section.
	m.Update(special(tea.KeyEscape))
	if m.contentsFocus || m.stage() != stageEditor {
		t.Fatalf("Escape left focus %v stage %v, want the editor", m.contentsFocus, m.stage())
	}
}

func TestContentsClickJumpsAndKeepsTheSidebar(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two", "## Three")
	m := newTOCModel(t, ed, nil)
	l := m.layout()
	sec, ok := contentsSection(l)
	if !ok {
		t.Fatal("no Contents section")
	}
	// The second body row is the "## Two" heading: row 1 of the section.
	m.Update(click(4, sec.BodyTop+1, tea.MouseLeft))
	if len(ed.gotos) != 1 || ed.gotos[0] != 2 {
		t.Fatalf("jumps = %v, want one to line 2", ed.gotos)
	}
	if !m.contentsFocus {
		t.Fatal("a click took the keyboard out of the sidebar")
	}
}

func TestContentsHeaderFoldsAndCloses(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two")
	m := newTOCModel(t, ed, nil)
	l := m.layout()
	sec, _ := contentsSection(l)
	// A press and release on the header folds the section.
	m.Update(click(5, sec.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 5, Y: sec.HeaderY, Button: tea.MouseLeft}))
	if !m.contents.Collapsed() {
		t.Fatal("a click on the header did not fold the section")
	}
	l = m.layout()
	sec, _ = contentsSection(l)
	if sec.Rows() != 0 {
		t.Fatalf("folded section has %d rows, want none", sec.Rows())
	}
	// The explorer takes the rows the folded section gave up.
	if first := l.Sections[0]; first.BodyBottom != sec.HeaderY-1 {
		t.Fatalf("the explorer ends at %d, the header is at %d", first.BodyBottom, sec.HeaderY)
	}
	// A second click opens it again.
	m.Update(click(5, sec.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 5, Y: sec.HeaderY, Button: tea.MouseLeft}))
	if m.contents.Collapsed() {
		t.Fatal("the second click did not unfold the section")
	}
	// The × closes the section.
	l = m.layout()
	sec, _ = contentsSection(l)
	x := l.SectionClose[sec.Index]
	if x < 0 {
		t.Fatal("the header has no close button")
	}
	m.Update(click(x, sec.HeaderY, tea.MouseLeft))
	if m.contents.Open() {
		t.Fatal("the × did not close the section")
	}
}

func TestContentsDividerDragResizes(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two")
	m := newTOCModel(t, ed, nil)
	l := m.layout()
	sec, _ := contentsSection(l)
	if sec.Rows() != sidebar.DefaultRows {
		t.Fatalf("rows = %d, want %d", sec.Rows(), sidebar.DefaultRows)
	}
	startRows := m.contents.Rows()
	m.Update(click(5, sec.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: 5, Y: sec.HeaderY - 3, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 5, Y: sec.HeaderY - 3, Button: tea.MouseLeft}))
	if got := m.contents.Rows(); got != startRows+3 {
		t.Fatalf("rows after the drag = %d, want %d", got, startRows+3)
	}
	if m.contents.Collapsed() {
		t.Fatal("a drag folded the section")
	}
}

func TestContentsRescansAfterTheDebounce(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two")
	m := newTOCModel(t, ed, nil)
	before := m.toc.seq
	// An edit: the text version moved and the rows are stale until the pause.
	ed.version++
	ed.doc.lines = []string{"# One", "# Renamed"}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.toc.seq == before {
		t.Fatal("the edit did not arm the debounce")
	}
	if got := strings.Join(headingTitles(m), ","); got != "One,Two" {
		t.Fatalf("rows before the scan = %s, want the old One,Two", got)
	}
	// An older tick is dropped; the newest scans.
	m.Update(tocRescanMsg{seq: m.toc.seq - 1})
	if got := strings.Join(headingTitles(m), ","); got != "One,Two" {
		t.Fatalf("a stale tick rescanned: %s", got)
	}
	m.Update(tocRescanMsg{seq: m.toc.seq})
	if got := strings.Join(headingTitles(m), ","); got != "One,Renamed" {
		t.Fatalf("rows after the rescan = %s, want One,Renamed", got)
	}
}

func TestContentsFocusCycle(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One")
	m := newTOCModel(t, ed, nil)
	// Next from the editor: explorer, then the Contents section, then the editor.
	for i, want := range []string{"explorer", "contents", "editor"} {
		if err := m.Run("focus_next_sidebar_section"); err != nil {
			t.Fatal(err)
		}
		if got := tocFocusName(m); got != want {
			t.Fatalf("step %d: focus is %s, want %s", i, got, want)
		}
	}
	// And backwards.
	if err := m.Run("focus_prev_sidebar_section"); err != nil {
		t.Fatal(err)
	}
	if got := tocFocusName(m); got != "contents" {
		t.Fatalf("previous from the editor is %s, want contents", got)
	}
}

func TestContentsFoldsIntoTheLayout(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two")
	m := newTOCModel(t, ed, nil)
	l := m.layout()
	if len(l.Sections) != 2 {
		t.Fatalf("sections = %d, want explorer and contents", len(l.Sections))
	}
	// The column is the explorer header, its body, the shared header row and the
	// contents body, over the bottom border.
	ex, cs := l.Sections[0], l.Sections[1]
	if ex.HeaderY != l.ExTitleY || cs.HeaderY != ex.BodyBottom+1 {
		t.Fatalf("headers at %d and %d, explorer body ends at %d", ex.HeaderY, cs.HeaderY, ex.BodyBottom)
	}
	if cs.BodyBottom+1 != l.ExBottom {
		t.Fatalf("contents body ends at %d, bottom border at %d", cs.BodyBottom, l.ExBottom)
	}
	// A press on the contents header row is a sidebar header target, not the
	// explorer's body any more.
	if got := l.Hit(4, cs.HeaderY); got.Kind != frame.HitSidebarHeader || got.Section != 1 {
		t.Fatalf("hit on the contents header = %+v, want the sidebar header of section 1", got)
	}
	// A press on a contents row is a sidebar row target with the row offset.
	if got := l.Hit(4, cs.BodyTop+1); got.Kind != frame.HitSidebarRow || got.Section != 1 || got.Index != 1 {
		t.Fatalf("hit on contents row 1 = %+v, want a sidebar row", got)
	}
}

func TestContentsPaint(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two", "## Three")
	m := newTOCModel(t, ed, nil)
	if err := m.Run("markdown_toc_show"); err != nil {
		t.Fatal(err)
	}
	l := m.layout()
	g := m.paint()
	sec, _ := contentsSection(l)
	header := g.Plain()[sec.HeaderY]
	if !strings.Contains(header, "▼ Contents") || !strings.Contains(header, "×") {
		t.Fatalf("contents header = %q, want the title, chevron and close", header)
	}
	if !strings.HasPrefix(header, "├") || !strings.Contains(ansi.Cut(header, 0, l.Ex), "┤") {
		t.Fatalf("contents header = %q, want the shared border row", header)
	}
	row := g.Plain()[sec.BodyTop]
	if !strings.Contains(row, "One") || !strings.Contains(row, "▌") {
		t.Fatalf("selected row = %q, want the heading and the focus marker", row)
	}
	// The explorer header grew its chevron in the stacked column.
	if got := g.Plain()[l.Sections[0].HeaderY]; !strings.Contains(got, "▼ File Explorer") {
		t.Fatalf("explorer header = %q, want the chevron", got)
	}
}

// tocFocusName names where keyboard focus is, the Contents section included.
func tocFocusName(m *Model) string {
	switch m.stage() {
	case stageExplorer:
		return "explorer"
	case stageContents:
		return "contents"
	}
	return "editor"
}

// TestContentsDebounceArmsOnce checks that a stream of messages about the same
// edit does not keep pushing the rescan out: one tick is in flight at a time.
func TestContentsDebounceArmsOnce(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two")
	m := newTOCModel(t, ed, nil)
	before := m.toc.seq
	ed.version++
	ed.doc.lines = []string{"# One", "# Renamed"}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	armed := m.toc.seq
	if armed == before {
		t.Fatal("the edit did not arm the debounce")
	}
	// Another message while the scan is pending does not arm a second tick.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.toc.seq != armed {
		t.Fatalf("debounce sequence advanced from %d to %d on an unrelated message", armed, m.toc.seq)
	}
	m.Update(tocRescanMsg{seq: armed})
	if got := strings.Join(headingTitles(m), ","); got != "One,Renamed" {
		t.Fatalf("rows after the rescan = %s, want One,Renamed", got)
	}
	// A later edit arms a new tick.
	ed.version++
	ed.doc.lines = []string{"# Last"}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.toc.seq != armed+1 {
		t.Fatalf("the second edit armed sequence %d, want %d", m.toc.seq, armed+1)
	}
}
