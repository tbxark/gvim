package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/session"
)

// dualModel returns a model over a Markdown document, with the caret on a line
// after the content so the compose tests can see concealed text.
func dualModel(t *testing.T) (*Model, *editor.Editor) {
	t.Helper()
	m, ed := viewModelWith(t, editor.New(editor.Options{}), DefaultViewOptions(),
		"Some **x** text\nline two\nline three\nline four\n", 80, 16)
	ed.Active().SetLanguage("markdown")
	return m, ed
}

// pathRealEditor is realEditor with the path-bearing TabInfo the app adapter returns,
// so split persistence tests can resolve tab identities.
type pathRealEditor struct{ realEditor }

func (p pathRealEditor) Tabs() []TabInfo {
	var out []TabInfo
	for _, tab := range p.ed.Tabs() {
		out = append(out, TabInfo{Name: tab.Name, Path: tab.Path, Modified: tab.Modified, Preview: tab.Preview, Active: tab.Active})
	}
	return out
}

// paneRows renders the pane at index i of the layout and returns its plain rows.
func paneRows(m *Model, i int) []string {
	l := m.layout()
	if i < 0 || i >= len(l.Panes) {
		return nil
	}
	sp := l.Panes[i]
	doc := m.paneDoc(sp)
	if doc == nil {
		return nil
	}
	return m.paneFor(sp, doc).Render(sp.W, sp.H).Plain()
}

func paneHas(m *Model, i int, want string) bool {
	for _, row := range paneRows(m, i) {
		if strings.Contains(row, want) {
			return true
		}
	}
	return false
}

func TestMarkdownDualOpensSideBySide(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	d := m.validDual()
	if d == nil {
		t.Fatalf("dual view did not open: notice %q", m.notice)
	}
	if got := m.markdownMode(); got != markdownModeDual {
		t.Fatalf("mode = %q, want dual", got)
	}
	if !strings.Contains(m.notice, "dual view") {
		t.Fatalf("notice = %q, want the dual mode text", m.notice)
	}
	l := m.layout()
	if len(l.Panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(l.Panes))
	}
	// 50/50 side by side: the widths differ by at most the odd column.
	if d := l.Panes[0].W - l.Panes[1].W; d < -1 || d > 1 {
		t.Fatalf("pane widths = %d and %d, want an even split", l.Panes[0].W, l.Panes[1].W)
	}
	if l.Panes[0].Y != l.Panes[1].Y {
		t.Fatalf("panes are not side by side: %+v", l.Panes)
	}
	// The source pane has the focus, like Fresh.
	if m.splits.Focus != d.source {
		t.Fatal("the focus is not on the source pane")
	}
}

func TestDualPerPanePageView(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	doc := m.ed.Active()
	l := m.layout()
	if p := m.panePageView(l.Panes[0], doc); p.On {
		t.Fatalf("single pane page view on: %+v", p)
	}
	runAction(t, m, "markdown_mode_dual")
	l = m.layout()
	src := m.panePageView(l.Panes[0], doc)
	prev := m.panePageView(l.Panes[1], doc)
	if src.On {
		t.Fatalf("source pane page view = %+v, want source", src)
	}
	if !prev.On || !prev.Markdown {
		t.Fatalf("preview pane page view = %+v, want the composed page", prev)
	}
	// The document's own view settings are untouched.
	if v, _ := docViewOf(doc).PageViewOverride(); v {
		t.Fatal("the dual view wrote the page view into the document")
	}
}

func TestDualEditInSourceUpdatesTheCompose(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	if !paneHas(m, 0, "**x**") {
		t.Fatal("the source pane does not show the raw markers")
	}
	if paneHas(m, 1, "**") {
		t.Fatal("the compose pane shows the concealed markers")
	}
	// Type at the start of the source: the same document feeds both panes.
	m.ed.SetCursor(buffer.Pos{Line: 0, Col: 0}, false)
	m.editorDo(editor.Action("insert_char"), map[string]any{"text": "y"})
	if !paneHas(m, 0, "ySome") || !paneHas(m, 1, "ySome") {
		t.Fatalf("the edit is not in both panes: source %q preview %q", paneRows(m, 0), paneRows(m, 1))
	}
}

func TestDualComposePaneShowsTheCaretAndMapsHits(t *testing.T) {
	t.Parallel()
	m, ed := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	d := m.validDual()
	m.focusPane(1)
	if m.splits.Focus != d.preview {
		t.Fatal("the compose pane did not take the focus")
	}
	l := m.layout()
	if !l.Panes[1].Focused {
		t.Fatal("the layout does not mark the compose pane focused")
	}
	doc := m.ed.Active()
	ed.SetCursor(buffer.Pos{Line: 0, Col: 1}, false)
	if _, _, ok := m.paneFor(l.Panes[1], doc).Caret(l.Panes[1].W, l.Panes[1].H); !ok {
		t.Fatal("the compose pane shows no caret")
	}
	// The preview is composed: find the "Some" text in its rendered rows, then
	// hit that cell.
	rows := paneRows(m, 1)
	row, col := -1, -1
	for y, r := range rows {
		if i := strings.Index(r, "Some"); i >= 0 {
			row, col = y, i
			break
		}
	}
	if row < 0 {
		t.Fatalf("no composed row: %q", rows)
	}
	p := m.paneGeometry(l.Panes[1], doc)
	hit := p.Hit(l.Panes[1].W, l.Panes[1].H, col, row)
	if hit.Kind != editorpane.HitText || hit.Pos.Line != 0 || hit.Pos.Col != 0 {
		t.Fatalf("hit = %+v, want source line 0 column 0", hit)
	}
}

func TestDualSelectionInTheComposePane(t *testing.T) {
	t.Parallel()
	m, ed := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	m.focusPane(1)
	ed.SetCursor(buffer.Pos{Line: 0, Col: 0}, false)
	ed.ExtendSelection(buffer.Pos{Line: 0, Col: 4}, editor.UnitChar)
	rows := paneRows(m, 1)
	if len(rows) == 0 {
		t.Fatal("no compose rows")
	}
	// The composed source line starts with "Some"; the selection over its source
	// columns paints those visible cells with the selection style.
	g := m.paneFor(m.layout().Panes[1], m.ed.Active()).Render(m.layout().Panes[1].W, m.layout().Panes[1].H)
	selected := 0
	for y, row := range g.Plain() {
		if !strings.Contains(row, "Some") {
			continue
		}
		for x := range len(row) {
			if c := g.At(x, y); c.Style.Reverse || c.Style.BG != nil {
				selected++
			}
		}
	}
	if selected == 0 {
		t.Fatalf("no selected cell in the composed pane: %q", rows)
	}
}

func TestDualClosesThroughTheSplitCommand(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	m.focusPane(1)
	runAction(t, m, "close_split")
	// The per-update sync clears the record on the next frame; the derived mode
	// never sees the stale state.
	m.syncDual()
	if m.validDual() != nil || m.splits.dual != nil {
		t.Fatal("the dual state survived close_split")
	}
	if got := m.markdownMode(); got != markdownModeSource {
		t.Fatalf("mode after close_split = %q, want source", got)
	}
}

func TestDualScrollSync(t *testing.T) {
	t.Parallel()
	m, ed := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	d := m.validDual()
	ed.ScrollTo(2)
	m.syncDual()
	if d.preview.Top != 2 {
		t.Fatalf("preview top = %d after the source scrolled, want 2", d.preview.Top)
	}
	m.focusPane(1)
	ed.ScrollTo(3)
	m.syncDual()
	if d.source.Top != 3 {
		t.Fatalf("source top = %d after the preview scrolled, want 3", d.source.Top)
	}
}

func TestDualClearsOnClosedPane(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	m.focusPane(1)
	m.closeFocused()
	// The closed preview is gone; the state clears on the next sync.
	m.syncDual()
	if m.splits.dual != nil || m.validDual() != nil {
		t.Fatalf("the dual state survived closing the preview pane (focus %+v)", m.splits.Focus)
	}
	if !m.splits.Root.Leaf() {
		t.Fatal("the split tree did not collapse to one pane")
	}
	if got := m.markdownMode(); got != markdownModeSource {
		t.Fatalf("mode after closing = %q, want source", got)
	}
	if m.paneFor(m.layout().FocusedPane(), m.ed.Active()).PageView.On {
		t.Fatal("the remaining pane kept the composed page view")
	}
}

func TestOnlyOneDualViewAtATime(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	if len(m.layout().Panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(m.layout().Panes))
	}
	// Asking again is a no-op: it does not nest another split.
	runAction(t, m, "markdown_mode_dual")
	if len(m.layout().Panes) != 2 || m.validDual() == nil {
		t.Fatalf("second dual command changed the view: %d panes", len(m.layout().Panes))
	}
}

func TestDualEndsWhenTheTabChanges(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	m.editorDo(editor.Action("new"), nil)
	m.syncDual()
	if m.validDual() != nil || m.splits.dual != nil {
		t.Fatal("the dual view survived a tab change")
	}
}

func TestDualClearsWhenThePanesStopMatching(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	runAction(t, m, "markdown_mode_dual")
	d := m.validDual()
	d.preview.Tab = 7
	m.syncDual()
	if m.splits.dual != nil {
		t.Fatal("a stale dual view was kept")
	}
}

func TestDualModeCommands(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	doc := m.ed.Active()
	runAction(t, m, "markdown_mode_rendered")
	if got := m.markdownMode(); got != markdownModeRendered {
		t.Fatalf("mode = %q, want rendered", got)
	}
	if v, ok := docViewOf(doc).PageViewOverride(); !ok || !v {
		t.Fatalf("rendered did not set the document page view: %v %v", v, ok)
	}
	if !strings.Contains(m.notice, "rendered view") {
		t.Fatalf("notice = %q", m.notice)
	}
	runAction(t, m, "markdown_mode_source")
	if got := m.markdownMode(); got != markdownModeSource {
		t.Fatalf("mode = %q, want source", got)
	}
	// Cycle source -> rendered -> dual -> source.
	runAction(t, m, "markdown_cycle_mode")
	if got := m.markdownMode(); got != markdownModeRendered {
		t.Fatalf("cycle from source = %q, want rendered", got)
	}
	runAction(t, m, "markdown_cycle_mode")
	if got := m.markdownMode(); got != markdownModeDual {
		t.Fatalf("cycle from rendered = %q, want dual", got)
	}
	runAction(t, m, "markdown_cycle_mode")
	if got := m.markdownMode(); got != markdownModeSource {
		t.Fatalf("cycle from dual = %q, want source", got)
	}
	// Toggle opens and closes the dual view.
	runAction(t, m, "markdown_toggle_dual")
	if got := m.markdownMode(); got != markdownModeDual {
		t.Fatalf("toggle on = %q, want dual", got)
	}
	runAction(t, m, "markdown_toggle_dual")
	if got := m.markdownMode(); got != markdownModeSource {
		t.Fatalf("toggle off = %q, want source", got)
	}
}

func TestDualRefusals(t *testing.T) {
	t.Parallel()
	// A non-Markdown buffer.
	m, _ := viewModel(t, DefaultViewOptions(), "plain text\n", 80, 16)
	runAction(t, m, "markdown_mode_dual")
	if !strings.Contains(m.notice, "not Markdown") {
		t.Fatalf("notice = %q, want the not-Markdown text", m.notice)
	}
	if m.validDual() != nil {
		t.Fatal("a dual view opened on a non-Markdown buffer")
	}
	// A compare view is open.
	m2, _ := dualModel(t)
	m2.splits.comparing = true
	runAction(t, m2, "markdown_mode_dual")
	if !strings.Contains(m2.notice, "compare view") {
		t.Fatalf("notice = %q, want the compare conflict", m2.notice)
	}
	m2.splits.comparing = false
	// More than one pane already.
	m3, _ := dualModel(t)
	m3.splitFocused(sideBySide)
	runAction(t, m3, "markdown_mode_dual")
	if !strings.Contains(m3.notice, "single pane") {
		t.Fatalf("notice = %q, want the single-pane refusal", m3.notice)
	}
}

func TestDualRefusesLargeDocuments(t *testing.T) {
	t.Parallel()
	m, _ := newLargeModel(t, writeLines(t, t.TempDir(), "a.md", bigFile, 40, 0))
	start(m)
	if !isLargeDoc(m.ed.Active()) {
		t.Skip("the test document did not open in large-file mode")
	}
	runAction(t, m, "markdown_mode_dual")
	if !strings.Contains(m.notice, "too large") {
		t.Fatalf("notice = %q, want the large-file refusal", m.notice)
	}
}

// TestDualStateCaptureAndRestore: a saved Markdown dual split round-trips through the
// session split format: marker, per-leaf source/rendered modes, focus on the source, and
// restored panes derive the right page-view override; closing it returns the document's
// own view and clears the persisted leaf modes.
func TestDualStateCaptureAndRestore(t *testing.T) {
	t.Parallel()
	m, _ := dualModel(t)
	m.ed = pathRealEditor{m.ed.(realEditor)}
	runAction(t, m, "markdown_mode_dual")
	if d := m.validDual(); d == nil {
		t.Fatalf("dual view did not open: %q", m.notice)
	}
	// Focus the preview before saving: capture must still write the source as the
	// one-dual policy focus.
	m.focusPane(1)
	layout := m.SplitState()
	if layout == nil || !layout.Dual || layout.Focus != 0 {
		t.Fatalf("captured dual layout = %+v", layout)
	}
	if layout.Root.Dir != session.SplitSideBySide || len(layout.Root.Leaves()) != 2 {
		t.Fatalf("captured dual root = %+v", layout.Root)
	}
	if layout.Root.A == nil || layout.Root.B == nil ||
		layout.Root.A.ViewMode != "source" || layout.Root.B.ViewMode != "rendered" {
		t.Fatalf("captured dual modes = %+v", layout.Root)
	}

	// Restore over a fresh model/tab on the same path.
	m2, _ := dualModel(t)
	m2.ed = pathRealEditor{m2.ed.(realEditor)}
	path := m2.tabPath(0)
	layout.Root.A.Path = path
	layout.Root.B.Path = path
	if !m2.RestoreSplits(layout, []session.FileState{{Path: path}}) {
		t.Fatal("the saved dual layout was rejected")
	}
	if got := m2.validDual(); got == nil {
		t.Fatal("the restored model has no valid dual view")
	}
	panes := m2.layout().Panes
	if len(panes) != 2 {
		t.Fatalf("restored panes = %d", len(panes))
	}
	doc := m2.ed.Active()
	if pv := m2.panePageView(panes[0], doc); pv.On {
		t.Fatalf("restored source pane page view = %+v, want off", pv)
	}
	if pv := m2.panePageView(panes[1], doc); !pv.On || !pv.Markdown {
		t.Fatalf("restored preview pane page view = %+v, want on markdown", pv)
	}
	// Ending the dual view clears the saved leaf modes and leaves the document mode.
	m2.closeMarkdownDual()
	if m2.validDual() != nil || !m2.splits.Root.Leaf() {
		t.Fatalf("dual close left %d panes dual=%v", len(m2.splits.Root.Leaves()), m2.validDual())
	}
	for _, leaf := range m2.splits.Root.Leaves() {
		if leaf.ViewMode != "" {
			t.Fatalf("closed dual left a leaf mode %+v", leaf)
		}
	}
}
