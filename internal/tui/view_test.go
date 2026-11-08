package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// realEditor is EditorState over a real *editor.Editor, as the app adapter is. The
// view commands run through the editor's own handlers, so their state is the editor's.
type realEditor struct{ ed *editor.Editor }

func (r realEditor) SetViewport(rows, cols int) { r.ed.SetViewport(rows, cols) }

func (r realEditor) Do(a editor.Action, args map[string]any) editor.Result {
	return r.ed.Do(a, args)
}

func (r realEditor) Active() Document {
	d := r.ed.Active()
	if d == nil {
		return nil
	}
	return realDoc{d}
}

func (r realEditor) Tabs() []TabInfo {
	var out []TabInfo
	for _, t := range r.ed.Tabs() {
		out = append(out, TabInfo{Name: t.Name, Modified: t.Modified, Preview: t.Preview, Active: t.Active})
	}
	return out
}

func (r realEditor) Status() string { return r.ed.Status() }

// ClearStatus drops the status message on a key or click, as the app adapter does, so the
// message that opening the file sets does not stay as a notice band over the body.
func (r realEditor) ClearStatus() { r.ed.ClearStatus() }

func (r realEditor) SetCursor(p buffer.Pos, extend bool) { r.ed.SetCursor(p, extend) }
func (r realEditor) ExtendSelection(p buffer.Pos, u editor.Unit) {
	r.ed.ExtendSelection(p, u)
}
func (r realEditor) ScrollBy(rows int) { r.ed.ScrollBy(rows) }
func (r realEditor) ScrollTo(top int)  { r.ed.ScrollTo(top) }

// realDoc is Document over a real *editor.Document, forwarding the optional views the
// way the app adapter does.
type realDoc struct{ d *editor.Document }

func (r realDoc) Modified() bool                    { return r.d.Modified() }
func (r realDoc) LineCount() int                    { return r.d.Buffer.Lines() }
func (r realDoc) Line(i int) string                 { return r.d.Buffer.Line(i) }
func (r realDoc) SavedLines() []string              { return r.d.SavedLines() }
func (r realDoc) Cursor() buffer.Pos                { return r.d.Cursor }
func (r realDoc) LineSpans(int) []syntax.Span       { return nil }
func (r realDoc) Name() string                      { return r.d.Name() }
func (r realDoc) Top() int                          { return r.d.Top }
func (r realDoc) Encoding() string                  { return r.d.Encoding() }
func (r realDoc) LineEnding() string                { return r.d.LineEnding() }
func (r realDoc) ViewSettings() editor.ViewSettings { return r.d.ViewSettings() }
func (r realDoc) Language() string                  { return r.d.Language() }
func (r realDoc) Version() uint64                   { return r.d.Buffer.Version() }
func (r realDoc) Cursors() []editor.Caret           { return r.d.Cursors() }
func (r realDoc) Folds(tab int) []fold.Range        { return r.d.Folds(tab) }
func (r realDoc) Folded(line int) bool              { return r.d.Folded(line) }

func (r realDoc) Selection() (anchor, head buffer.Pos, ok bool) {
	if r.d.Sel == nil {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	return r.d.Sel.Anchor, r.d.Sel.Head, true
}

// viewModel returns a w by h model over a real editor that holds text, opened from a
// file named doc.txt, with the view options o. The theme is the bundled dark theme, so
// the styles have colors to compare.
func viewModel(t *testing.T, o ViewOptions, text string, w, h int) (*Model, *editor.Editor) {
	t.Helper()
	return viewModelWith(t, editor.New(editor.Options{}), o, text, w, h)
}

// viewModelWith is viewModel over ed, so that a test can set the editor's options before
// the file is opened.
func viewModelWith(t *testing.T, ed *editor.Editor, o ViewOptions, text string, w, h int) (*Model, *editor.Editor) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: realEditor{ed}, Keys: testKeys(), Theme: th, Clock: testClock, View: &o})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, ed
}

// viewCell returns the cell at (x, y) of the frame, failing the test when it is outside.
func viewCell(t *testing.T, m *Model, x, y int) grid.Cell {
	t.Helper()
	g := m.paint()
	if x < 0 || y < 0 || x >= g.Width() || y >= g.Height() {
		t.Fatalf("cell (%d, %d) is outside the %dx%d frame", x, y, g.Width(), g.Height())
	}
	return g.At(x, y)
}

// viewRow returns row y of the frame as plain text.
func viewRow(m *Model, y int) string {
	return frameText(m)[y]
}

func TestMenuBarToggleHidesTheMenuRow(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 60, 12)
	if !strings.Contains(viewRow(m, 0), "File") {
		t.Fatalf("setup: row 0 %q has no menu label", viewRow(m, 0))
	}
	runAction(t, m, "toggle_menu_bar")
	if strings.Contains(viewRow(m, 0), "File") {
		t.Fatalf("row 0 still shows the menu after toggle_menu_bar: %q", viewRow(m, 0))
	}
	if l := m.layout(); l.MenuOn || l.BodyTop != 1 {
		t.Fatalf("menuOn %v bodyTop %d, want off and 1", l.MenuOn, l.BodyTop)
	}
	runAction(t, m, "toggle_menu_bar")
	if !strings.Contains(viewRow(m, 0), "File") {
		t.Fatalf("the second toggle did not bring the menu back: %q", viewRow(m, 0))
	}
}

func TestTabBarToggleGivesItsRowToTheBody(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 60, 12)
	row := m.layout().StripY
	if !strings.Contains(viewRow(m, row), "doc.txt") {
		t.Fatalf("setup: the strip row %q has no tab name", viewRow(m, row))
	}
	runAction(t, m, "toggle_tab_bar")
	l := m.layout()
	if l.StripY != -1 || l.BodyTop != row {
		t.Fatalf("stripY %d bodyTop %d, want -1 and %d", l.StripY, l.BodyTop, row)
	}
	if strings.Contains(viewRow(m, row), "doc.txt") {
		t.Fatalf("the tab name is still drawn: %q", viewRow(m, row))
	}
	if got := l.Hit(l.TextX, row); got.Kind != hitEditorText {
		t.Errorf("the freed row hits %v, want the editor text", got.Kind)
	}
	if !strings.Contains(viewRow(m, row), "hello") {
		t.Errorf("the freed row %q does not show the first line", viewRow(m, row))
	}
}

func TestStatusBarToggleGivesItsRowToTheBody(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 60, 12)
	last := m.height - 1
	if !strings.Contains(viewRow(m, last), "Ln 1") {
		t.Fatalf("setup: the last row %q has no position", viewRow(m, last))
	}
	runAction(t, m, "toggle_status_bar")
	if l := m.layout(); l.StatusOn || l.BodyBottom != last {
		t.Fatalf("statusOn %v bodyBottom %d, want off and %d", l.StatusOn, l.BodyBottom, last)
	}
	if strings.Contains(viewRow(m, last), "Ln 1") {
		t.Errorf("the status row is still drawn: %q", viewRow(m, last))
	}
}

func TestPromptLineToggleAddsARowUnderTheStatusRow(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 60, 12)
	if l := m.layout(); l.PromptOn || l.StatusY != 11 {
		t.Fatalf("setup: promptOn %v statusY %d, want off and 11", l.PromptOn, l.StatusY)
	}
	runAction(t, m, "toggle_prompt_line")
	if l := m.layout(); !l.PromptOn || l.StatusY != 10 || l.PromptY != 11 || l.BodyBottom != 9 {
		t.Fatalf("prompt on %v at %d, status %d, body bottom %d; want on at 11, status 10, bottom 9",
			l.PromptOn, l.PromptY, l.StatusY, l.BodyBottom)
	}
	runAction(t, m, "toggle_prompt_line")
	if l := m.layout(); l.PromptOn || l.StatusY != 11 {
		t.Fatalf("second toggle: promptOn %v statusY %d, want off and 11", l.PromptOn, l.StatusY)
	}
}

func TestVerticalScrollbarToggleClearsTheColumn(t *testing.T) {
	long := strings.Repeat("line\n", 60)
	m, _ := viewModel(t, DefaultViewOptions(), long, 40, 10)
	x := m.layout().Panes[0].X + m.layout().Panes[0].W - 1
	y := m.layout().BodyTop
	if got := viewCell(t, m, x, y).Text; got != "┃" {
		t.Fatalf("setup: scrollbar cell %q, want the thumb", got)
	}
	runAction(t, m, "toggle_vertical_scrollbar")
	if got := viewCell(t, m, x, y).Text; got != " " {
		t.Errorf("scrollbar cell %q after the toggle, want a blank", got)
	}
	if m.layout().Scrollbar {
		t.Error("the layout still reserves the scrollbar")
	}
}

func TestHorizontalScrollbarToggleGivesItsRowBack(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("x", 200)+"\nb\nc\n", 40, 8)
	runAction(t, m, "toggle_line_wrap")
	// In a 40-column frame the notice band is drawn over the last body row.
	m.notice = ""
	ed.ClearStatus()
	last := m.layout().BodyBottom
	x := m.layout().TextX
	if got := viewCell(t, m, x, last).Text; got != "━" && got != "─" {
		t.Fatalf("setup: the last body row %q is not the horizontal bar", got)
	}
	runAction(t, m, "toggle_horizontal_scrollbar")
	m.notice = ""
	if got := viewCell(t, m, x, last).Text; got == "━" || got == "─" {
		t.Errorf("the bar is still drawn on row %d after the toggle", last)
	}
}

func TestLineWrapToggleChangesTheContinuationRow(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), strings.Repeat("a", 100)+"\nBBB\n", 40, 8)
	x, y := m.layout().TextX, m.layout().BodyTop+1
	if got := viewCell(t, m, x, y).Text; got != "a" {
		t.Fatalf("setup: wrapped row 1 starts with %q, want the continuation a", got)
	}
	runAction(t, m, "toggle_line_wrap")
	if got := viewCell(t, m, x, y).Text; got != "B" {
		t.Errorf("unwrapped row 1 starts with %q, want the second line B", got)
	}
	if h := m.Holds("line_wrap"); h {
		t.Error("the line_wrap menu row is checked with wrap off")
	}
}

func TestLineWrapCurrentBufferIsPerDocument(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), strings.Repeat("a", 100)+"\n", 40, 8)
	runAction(t, m, "toggle_line_wrap_current_buffer")
	if s := docViewOf(m.ed.Active()); s.LineWrap == nil || *s.LineWrap {
		t.Fatalf("line wrap override = %v, want off", s.LineWrap)
	}
	if got := viewCell(t, m, m.layout().TextX, m.layout().BodyTop+1).Text; got == "a" {
		t.Error("the current buffer still wraps after its own toggle")
	}
	runAction(t, m, "new")
	if got := m.ed.Active(); got == nil || docViewOf(got).LineWrap != nil {
		t.Errorf("a new buffer has a line wrap override %v, want none", docViewOf(got).LineWrap)
	}
}

func TestCurrentLineHighlightToggles(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "one\ntwo\n", 40, 8)
	top, next := m.layout().BodyTop, m.layout().BodyTop+1
	x := m.layout().TextX + 2
	if viewCell(t, m, x, top).Style.BG == viewCell(t, m, x, next).Style.BG {
		t.Fatal("setup: the caret line has no tint")
	}
	runAction(t, m, "toggle_current_line_highlight")
	if viewCell(t, m, x, top).Style.BG != viewCell(t, m, x, next).Style.BG {
		t.Error("the caret line is still tinted after toggle_current_line_highlight")
	}
	runAction(t, m, "toggle_current_line_highlight")
	runAction(t, m, "toggle_current_line_highlight_current_buffer")
	if viewCell(t, m, x, top).Style.BG != viewCell(t, m, x, next).Style.BG {
		t.Error("the caret line is still tinted after the current buffer toggle")
	}
}

func TestOccurrenceHighlightToggles(t *testing.T) {
	tests := []struct {
		name   string
		action string
	}{
		{"global", "toggle_occurrence_highlight"},
		{"current buffer", "toggle_occurrence_highlight_current_buffer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := viewModel(t, DefaultViewOptions(), "foo bar foo\n", 40, 8)
			x := m.layout().TextX + 8 // the second foo
			y := m.layout().BodyTop
			before := viewCell(t, m, x, y).Style.BG
			runAction(t, m, tt.action)
			if after := viewCell(t, m, x, y).Style.BG; after == before {
				t.Errorf("the occurrence keeps its background %v after %s", before, tt.action)
			}
		})
	}
}

func TestWhitespaceIndicatorsToggleOnTheCurrentBuffer(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "a b\n", 40, 8)
	x, y := m.layout().TextX+1, m.layout().BodyTop
	if got := viewCell(t, m, x, y).Text; got == "·" {
		t.Fatal("setup: a space shows as a dot with whitespace indicators off")
	}
	runAction(t, m, "toggle_whitespace_indicators")
	if got := viewCell(t, m, x, y).Text; got != "·" {
		t.Errorf("space cell %q with indicators on, want the middle dot", got)
	}
	runAction(t, m, "toggle_whitespace_indicators")
	if got := viewCell(t, m, x, y).Text; got == "·" {
		t.Error("the space still shows a dot after the second toggle")
	}
}

func TestTabIndicatorsHideTheArrowOfATab(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "\tx\n", 40, 8)
	runAction(t, m, "toggle_whitespace_indicators")
	x, y := m.layout().TextX, m.layout().BodyTop
	if got := viewCell(t, m, x, y).Text; got != "→" {
		t.Fatalf("setup: the tab shows %q, want the arrow", got)
	}
	runAction(t, m, "toggle_tab_indicators")
	if got := viewCell(t, m, x, y).Text; got == "→" {
		t.Error("the tab arrow is still drawn after toggle_tab_indicators")
	}
}

func TestIndentGuidesAndFoldIndicatorsAreStoredPerBuffer(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "func a() {\n\tx\n}\n", 40, 8)
	runAction(t, m, "toggle_indentation_guide_current_buffer")
	if p := m.viewPane(editorpane.Pane{}, m.ed.Active()); !p.IndentGuides {
		t.Error("indentation guides are not on in the pane after the toggle")
	}
	runAction(t, m, "toggle_fold_indicators_current_buffer")
	if p := m.viewPane(editorpane.Pane{}, m.ed.Active()); !p.HideFoldMarkers {
		t.Error("fold indicators are still drawn after the toggle")
	}
}

func TestLineNumbersCurrentBufferHidesOnlyTheCurrentBuffer(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 40, 8)
	y := m.layout().BodyTop
	if got := viewCell(t, m, 2, y).Text; got != "1" {
		t.Fatalf("setup: gutter cell %q, want the line number 1", got)
	}
	runAction(t, m, "toggle_line_numbers_current_buffer")
	if got := viewCell(t, m, 0, y).Text; got != " " {
		t.Errorf("after the toggle the first cell is %q, want the padding", got)
	}
	if got := viewCell(t, m, 1, y).Text; got != "a" {
		t.Errorf("after the toggle the text starts at %q, want the text a after the padding", got)
	}
	if m.hideLineNumbers {
		t.Error("the global line numbers setting changed with the current buffer toggle")
	}
	runAction(t, m, "new")
	if s := docViewOf(m.ed.Active()); s.LineNumbers != nil {
		t.Errorf("a new buffer inherited the override %v", *s.LineNumbers)
	}
}

func TestRulerAddAndRemoveThroughThePrompt(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\nxyz\n", 40, 8)
	runAction(t, m, "add_ruler")
	if len(m.overlays) != 1 {
		t.Fatalf("add_ruler opened %d overlays, want the column prompt", len(m.overlays))
	}
	press(m, typed("5"))
	settle(t, m, press(m, special(tea.KeyEnter)))
	if got := docViewOf(m.ed.Active()).Rulers; !reflect.DeepEqual(got, []int{5}) {
		t.Fatalf("rulers = %v after the prompt, want [5]", got)
	}
	// The ruler shows on a plain row, so the test reads line 2, not the caret line.
	x := m.layout().TextX + 4
	y := m.layout().BodyTop + 1
	if a, b := viewCell(t, m, x, y).Style.BG, viewCell(t, m, x-1, y).Style.BG; a == b {
		t.Error("the ruler column has the same background as the column before it")
	}
	runAction(t, m, "remove_ruler")
	press(m, typed("5"))
	settle(t, m, press(m, special(tea.KeyEnter)))
	if got := docViewOf(m.ed.Active()).Rulers; len(got) != 0 {
		t.Errorf("rulers = %v after remove_ruler, want none", got)
	}
}

func TestConfigRulersReachThePane(t *testing.T) {
	tests := []struct {
		name   string
		config []int
		add    string
		remove string
		want   []int
	}{
		{"config only", []int{20, 40}, "", "", []int{20, 40}},
		{"buffer ruler joins config", []int{20}, "5", "", []int{20, 5}},
		{"duplicate stays once", []int{5}, "5", "", []int{5}},
		{"none", nil, "", "", nil},
		{"a configured ruler can be removed from the buffer", []int{20, 40}, "", "20", []int{40}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ed := editor.New(editor.Options{})
			ed.SetRulers(tc.config)
			m, _ := viewModelWith(t, ed, DefaultViewOptions(), "abc\nxyz\n", 40, 8)
			if tc.add != "" {
				runAction(t, m, "add_ruler")
				press(m, typed(tc.add))
				settle(t, m, press(m, special(tea.KeyEnter)))
			}
			if tc.remove != "" {
				runAction(t, m, "remove_ruler")
				press(m, typed(tc.remove))
				settle(t, m, press(m, special(tea.KeyEnter)))
			}
			p := m.viewPane(editorpane.Pane{}, m.ed.Active())
			if !slices.Equal(p.Rulers, tc.want) {
				t.Errorf("pane rulers = %v, want %v", p.Rulers, tc.want)
			}
		})
	}
}

func TestTabSizeFromThePromptSetsTheTabStop(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "\tx\n", 40, 8)
	runAction(t, m, "set_tab_size")
	press(m, typed("2"))
	settle(t, m, press(m, special(tea.KeyEnter)))
	x, y := m.layout().TextX, m.layout().BodyTop
	if got := viewCell(t, m, x+2, y).Text; got != "x" {
		t.Errorf("cell at +2 is %q, want x for a tab size of 2", got)
	}
}

func TestSetPageWidthStoresTheWidthAndSaysItIsNotDrawn(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 120, 8)
	runAction(t, m, "set_page_width")
	press(m, typed("120"))
	settle(t, m, press(m, special(tea.KeyEnter)))
	if got := docViewOf(m.ed.Active()).PageWidth; got != 120 {
		t.Fatalf("page width = %d, want 120", got)
	}
	if !frameHas(m, "not drawn in this port") {
		t.Error("the status row does not say the page width is not drawn")
	}
}

func TestPageViewIsStoredAndNotDrawn(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 120, 8)
	runAction(t, m, "toggle_page_view")
	if !docViewOf(m.ed.Active()).PageView {
		t.Fatal("page view was not stored")
	}
	if !frameHas(m, "not drawn in this port") {
		t.Error("the status row does not say the page view is not drawn")
	}
}

func TestSetLanguageNamesTheDocumentInTheStatusRow(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "abc\n", 60, 8)
	runAction(t, m, "set_language")
	press(m, typed("Go"))
	settle(t, m, press(m, special(tea.KeyEnter)))
	if got := ed.Tabs()[0].Language; got != "Go" {
		t.Fatalf("tab language = %q, want Go", got)
	}
	if got := m.statusView().Language; got != "Go" {
		t.Fatalf("status language = %q, want Go", got)
	}
	if row := viewRow(m, m.height-1); !strings.Contains(row, " Go ") || strings.Contains(row, " Text ") {
		t.Errorf("status row %q does not show the language Go in place of Text", row)
	}
}

func TestSelectCursorStyleChangesTheCaretAndTheTerminalCursor(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 40, 8)
	settle(t, m, m.dispatch(Command{Action: "select_cursor_style", Args: map[string]any{"style": "bar"}}))
	x, y := m.layout().TextX, m.layout().BodyTop
	if got := viewCell(t, m, x, y).Text; got != "▏" {
		t.Errorf("caret cell %q with the bar style, want ▏", got)
	}
	if v := m.View(); v.Cursor == nil || v.Cursor.Shape != tea.CursorBar {
		t.Errorf("terminal cursor shape is not the bar")
	}
}

func TestSelectCursorStyleListPicksAShape(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 40, 8)
	runAction(t, m, "select_cursor_style")
	press(m, special(tea.KeyDown))
	settle(t, m, press(m, special(tea.KeyEnter)))
	if m.view.cursor != "bar" {
		t.Fatalf("cursor style = %q after choosing the second row, want bar", m.view.cursor)
	}
}

func TestSelectThemeAppliesTheChosenThemeLive(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 40, 8)
	runAction(t, m, "select_theme")
	if !frameHas(m, "dracula") {
		t.Fatal("the theme list does not show the bundled themes")
	}
	for _, r := range "light" {
		press(m, typed(string(r)))
	}
	settle(t, m, press(m, special(tea.KeyEnter)))
	want, err := theme.Load("light")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.theme, want) {
		t.Error("the frame does not carry the light theme after the choice")
	}
	if m.util.settings.Theme != "light" {
		t.Errorf("settings theme = %q, want light", m.util.settings.Theme)
	}
}

func TestSelectLocaleSaysOnlyEnglishIsBuiltIn(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 60, 12)
	runAction(t, m, "select_locale")
	if !frameHas(m, "English") {
		t.Fatal("the locale list does not show English")
	}
	settle(t, m, press(m, special(tea.KeyEnter)))
	if !strings.Contains(m.notice, "not built in") {
		t.Errorf("notice %q does not say the other locales are not built in", m.notice)
	}
}

func TestBackgroundCommandsAreNotApplicable(t *testing.T) {
	for _, action := range []string{"set_background", "set_background_blend"} {
		m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 60, 8)
		if err := m.Run(action); err != nil {
			t.Fatalf("Run(%s) = %v, want it handled", action, err)
		}
		if !strings.Contains(m.notice, "Not applicable in a terminal") {
			t.Errorf("%s notice %q does not say why", action, m.notice)
		}
	}
}

func TestMouseCaptureOffHandsTheMouseToTheTerminal(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 40, 8)
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Fatalf("setup: mouse mode %v, want all motion", got)
	}
	runAction(t, m, "toggle_mouse_capture")
	if got := m.View().MouseMode; got != tea.MouseModeNone {
		t.Errorf("mouse mode %v with capture off, want none", got)
	}
	if h := m.Holds("mouse_capture"); h {
		t.Error("the mouse_capture menu row is checked with capture off")
	}
	runAction(t, m, "toggle_mouse_capture")
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("mouse mode %v after the second toggle, want all motion", got)
	}
}

func TestMouseHoverAndScrollSyncAreStored(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 40, 8)
	runAction(t, m, "toggle_mouse_hover")
	runAction(t, m, "toggle_scroll_sync")
	if !m.view.mouseHover || !m.view.scrollSync {
		t.Fatalf("mouse hover %v scroll sync %v, want both stored on", m.view.mouseHover, m.view.scrollSync)
	}
	if !m.Holds("mouse_hover") || !m.Holds("scroll_sync") {
		t.Error("the menu rows for hover and scroll sync are not checked")
	}
}

func TestMaximizeSplitShowsOnlyTheFocusedPane(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 60, 10)
	runAction(t, m, "split_vertical")
	if n := len(m.layout().Panes); n != 2 {
		t.Fatalf("setup: %d panes after a split, want 2", n)
	}
	runAction(t, m, "toggle_maximize_split")
	shown := 0
	for _, p := range m.layout().Panes {
		if p.Rect != (rect{}) {
			shown++
		}
	}
	if shown != 1 {
		t.Errorf("%d panes with area with the split maximized, want 1", shown)
	}
	runAction(t, m, "toggle_maximize_split")
	if n := len(m.layout().Panes); n != 2 {
		t.Errorf("%d panes after the second toggle, want 2", n)
	}
}

func TestDebugHighlightsShowsRangesInTheStatusRow(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 120, 8)
	runAction(t, m, "toggle_debug_highlights")
	if !frameHas(m, "hl Ln 1") {
		t.Errorf("the status row does not list the highlight ranges of line 1")
	}
}

func TestFileExplorerSideSaysItIsNotBuiltYet(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 60, 8)
	runAction(t, m, "toggle_file_explorer_side")
	if !strings.Contains(m.notice, "not built in this port") {
		t.Errorf("notice %q does not say the explorer side is not built", m.notice)
	}
}

func TestViewOptionsSetTheStartingState(t *testing.T) {
	o := DefaultViewOptions()
	o.ShowTabBar = false
	o.LineWrap = false
	o.CursorStyle = "underline"
	m, _ := viewModel(t, o, "abc\n", 60, 8)
	if m.layout().StripY != -1 {
		t.Error("the tab bar is shown with ShowTabBar false")
	}
	if m.Holds("line_wrap") {
		t.Error("line wrap is on with LineWrap false")
	}
	if m.caretShape() != editorpane.CaretUnderline {
		t.Error("the caret shape is not underline")
	}
}

func TestViewDefaultsKeepTheStartingLayout(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 60, 8)
	l := m.layout()
	if l.StripY != 1 || l.StatusY != 7 || l.PromptOn || !l.MenuOn {
		t.Errorf("default layout: strip %d status %d prompt %v menu %v, want 1, 7, off, on",
			l.StripY, l.StatusY, l.PromptOn, l.MenuOn)
	}
}

func TestHidingTheTabBarReleasesItsKeyboardFocus(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 60, 8)
	m.tabFocus = true
	runAction(t, m, "toggle_tab_bar")
	if m.tabFocus {
		t.Error("the tab strip keeps the keyboard after it is hidden")
	}
}

func TestIndentGuidesDrawOnTheFrameRow(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "func a() {\n\tx\n}\n", 40, 8)
	y := m.layout().BodyTop + 1 // the indented line
	before := viewRow(m, y)
	runAction(t, m, "toggle_indentation_guide_current_buffer")
	if after := viewRow(m, y); after == before {
		t.Errorf("row %d is the same with the indentation guides on: %q", y, after)
	}
	runAction(t, m, "toggle_indentation_guide_current_buffer")
	if again := viewRow(m, y); again != before {
		t.Errorf("row %d after the second toggle %q, want %q back", y, again, before)
	}
}

func TestFoldIndicatorsCurrentBufferHidesTheArrow(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "func a() {\n\tx\n}\n", 40, 8)
	y := m.layout().BodyTop
	x := m.layout().Gutter - 1
	if got := viewCell(t, m, x, y).Text; got != "▾" {
		t.Fatalf("setup: the fold arrow cell %q, want ▾", got)
	}
	runAction(t, m, "toggle_fold_indicators_current_buffer")
	if got := viewCell(t, m, x, y).Text; got == "▾" || got == "▸" {
		t.Errorf("the fold arrow is still drawn: %q", got)
	}
}

// TestOccurrenceHighlightGoesOffWithAnEditUntilTheCaretMoves is Fresh's rule: typing
// drops the highlight, and it stays off until the caret moves for another reason.
func TestOccurrenceHighlightGoesOffWithAnEditUntilTheCaretMoves(t *testing.T) {
	o := DefaultViewOptions()
	o.HighlightOccurrences = true
	m, _ := viewModel(t, o, "fo foo foo\n", 40, 8)
	l := m.layout()
	marked := func(col int) bool {
		return viewCell(t, m, l.TextX+col, l.BodyTop).Style.BG != viewCell(t, m, l.TextX+3, l.BodyTop).Style.BG
	}
	frame := func() { m.Update(tea.WindowSizeMsg{Width: 40, Height: 8}) }
	move := func() {
		runAction(t, m, "move_right")
		frame()
	}
	move()
	move()
	m.Update(typed("o")) // "foo foo foo", the caret at the end of the first foo
	if marked(4) || marked(8) {
		t.Error("the other foos are still marked right after the edit")
	}
	frame() // a frame that changes nothing
	if marked(4) {
		t.Error("the highlight came back without the caret moving")
	}
	move() // onto the space after the first foo
	move() // the start of the second foo
	if !marked(8) {
		t.Error("the highlight did not come back after the caret moved")
	}
}
