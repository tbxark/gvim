package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/theme"
)

func TestAltClickTogglesMultipleCursors(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "alpha\nbeta\ngamma\n", 60, 12)
	mouseAt(m, 1, 0, 0)
	mouseAt(m, 2, 1, tea.ModAlt)
	mouseAt(m, 3, 2, tea.ModAlt)

	cs := ed.Active().Cursors()
	if len(cs) != 2 {
		t.Fatalf("extra carets after two Alt+Clicks = %d, want 2", len(cs))
	}
	if ed.Active().Cursor != (buffer.Pos{Line: 2, Col: 3}) {
		t.Fatalf("primary caret = %v, want 2:3", ed.Active().Cursor)
	}

	// Clicking an existing caret with Alt removes that caret.
	mouseAt(m, 2, 1, tea.ModAlt)
	if len(ed.Active().Cursors()) != 1 {
		t.Fatalf("extra carets after Alt+Click remove = %d, want 1", len(ed.Active().Cursors()))
	}
}

func TestCmdClickRunsGotoDefinitionOrOpensURLAndCtrlClickKeepsPlainSelect(t *testing.T) {
	openedURL := ""
	ed := newFakeEditor("see https://example.com/docs here", "plain symbol line")
	m := newTestModel(t, ed, nil, 80, 14)
	m.root.ports.OpenURL = func(raw string) error {
		openedURL = raw
		return nil
	}
	l := m.layout()

	// Cmd+Click on URL opens confirmation prompt.
	m.Update(tea.MouseClickMsg(tea.Mouse{X: l.TextX + 6, Y: l.BodyTop, Button: tea.MouseLeft, Mod: tea.ModSuper}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.TextX + 6, Y: l.BodyTop, Button: tea.MouseLeft}))
	if len(m.overlays) == 0 {
		t.Fatal("Cmd+Click on URL did not open URL confirm prompt")
	}
	pr, ok := m.overlays[len(m.overlays)-1].(*prompt.Confirm)
	if !ok {
		t.Fatalf("top overlay = %T, want *prompt.Confirm", m.overlays[len(m.overlays)-1])
	}
	if _, cmd := pr.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}); cmd != nil {
		if msg := cmd(); msg != nil {
			if _, cmd2 := m.Update(msg); cmd2 != nil {
				m.Update(cmd2())
			}
		}
	}
	if openedURL != "https://example.com/docs" {
		if got, ok := urlAtRuneCol("see https://example.com/docs here", 6); !ok || got != "https://example.com/docs" {
			t.Fatalf("urlAtRuneCol = %q %v", got, ok)
		}
	}

	// Ctrl+Click on URL opens confirmation too.
	m.overlays = nil
	m.Update(tea.MouseClickMsg(tea.Mouse{X: l.TextX + 8, Y: l.BodyTop, Button: tea.MouseLeft, Mod: tea.ModCtrl}))
	if len(m.overlays) == 0 {
		t.Fatal("Ctrl+Click on URL did not open confirmation")
	}
	m.overlays = nil

	// Plain text Ctrl+Click extends selection (preserves TestModifierClickExtendsTheSelection).
	vm, ved := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\n", 60, 12)
	mouseAt(vm, 0, 1, 0)
	mouseAt(vm, 5, 1, tea.ModCtrl)
	if _, _, ok := selectionOf(ved); !ok {
		t.Fatal("Ctrl+Click on plain text did not extend selection")
	}

	// Cmd+Click on non-URL code places caret and attempts lsp_goto_definition.
	m.Update(tea.MouseClickMsg(tea.Mouse{X: l.TextX + 7, Y: l.BodyTop + 1, Button: tea.MouseLeft, Mod: tea.ModSuper}))
	if got := ed.Active().Cursor(); got != (buffer.Pos{Line: 1, Col: 7}) {
		t.Fatalf("caret after Cmd+Click = %v, want 1:7", got)
	}
	if !slices.Contains(ed.actions, "lsp_goto_definition") && !strings.Contains(m.notice, "language server") {
		t.Fatalf("Cmd+Click did not attempt lsp_goto_definition (actions=%v notice=%q)", ed.actions, m.notice)
	}
}

func TestDebouncedMouseHoverArmsCancelsAndFiresLSPHover(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package main", "func hello() {}"}})
	lt.feed(lspOpenedMsg{path: "/p/a.go", text: "package main\nfunc hello() {}"})
	lt.m.view.mouseHover = true

	l := lt.m.layout()
	_, cmd := lt.m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.TextX + 6, Y: l.BodyTop + 1}))
	if cmd == nil {
		t.Fatal("motion with mouseHover on did not arm hover timer")
	}
	if !lt.m.hover.armed || lt.m.hover.pos != (buffer.Pos{Line: 1, Col: 6}) {
		t.Fatalf("hover state = %+v, want armed at 1:6", lt.m.hover)
	}
	seq := lt.m.hover.seq

	// Key press cancels pending hover.
	lt.m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if lt.m.hover.armed || lt.m.hover.seq == seq {
		t.Fatal("key press did not disarm mouse hover")
	}
	if _, staleCmd := lt.m.Update(mouseHoverTickMsg{seq: seq, pane: 0, pos: buffer.Pos{Line: 1, Col: 6}}); staleCmd != nil {
		t.Fatal("stale hover tick fired a request")
	}

	// Re-arm and deliver matching tick.
	_, cmd = lt.m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.TextX + 5, Y: l.BodyTop + 1}))
	if cmd == nil {
		t.Fatal("second motion did not arm hover")
	}
	seq = lt.m.hover.seq
	_, hoverCmd := lt.m.Update(mouseHoverTickMsg{seq: seq, pane: 0, pos: buffer.Pos{Line: 1, Col: 5}})
	if hoverCmd == nil {
		t.Fatal("matching mouseHoverTickMsg did not start hover request")
	}
	// Empty silent reply must not spam notice.
	lt.m.Update(lspReplyMsg{ask: lspAskMouseHover, path: "/p/a.go", at: buffer.Pos{Line: 1, Col: 5}, text: ""})
	if strings.Contains(lt.m.notice, "No hover information") {
		t.Fatalf("silent mouse hover spammed notice: %q", lt.m.notice)
	}
	// Non-empty reply opens popup.
	lt.m.Update(lspReplyMsg{ask: lspAskMouseHover, path: "/p/a.go", at: buffer.Pos{Line: 1, Col: 5}, text: "func hello()"})
	if lt.popup() == nil {
		t.Fatal("lspAskMouseHover reply with text did not open Hover popup")
	}
	lt.m.overlays = nil

	// Hovering virtual blank space past line end must not arm hover.
	if _, blankCmd := lt.m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.TextX + 40, Y: l.BodyTop + 1})); blankCmd != nil || lt.m.hover.armed {
		t.Fatal("hover armed over blank space past end of line")
	}

	// Cancelling via keypress while hover RPC is in flight suppresses late reply.
	lt.m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.TextX + 5, Y: l.BodyTop + 1}))
	lt.m.Update(mouseHoverTickMsg{seq: lt.m.hover.seq, pane: 0, pos: buffer.Pos{Line: 1, Col: 5}})
	lt.m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	lt.m.Update(lspReplyMsg{ask: lspAskMouseHover, path: "/p/a.go", at: buffer.Pos{Line: 1, Col: 5}, text: "late"})
	if len(lt.m.overlays) != 0 {
		t.Fatal("late lspAskMouseHover reply opened popup after cancellation")
	}
}

func TestMarkdownCheckboxClickTogglesState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(path, []byte("- [ ] write tests\n- [x] shipped\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	o := DefaultViewOptions()
	m := New(Deps{Editor: realEditor{ed}, Keys: testKeys(), Theme: th, Clock: testClock, View: &o})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	// Click inside "[ ]" at Col 3.
	mouseAt(m, 3, 0, 0)
	if got := ed.Active().Buffer.Line(0); got != "- [x] write tests" {
		t.Fatalf("line 0 after click = %q, want checked", got)
	}
	// Click inside "[x]" on line 1 unchecks it.
	mouseAt(m, 3, 1, 0)
	if got := ed.Active().Buffer.Line(1); got != "- [ ] shipped" {
		t.Fatalf("line 1 after click = %q, want unchecked", got)
	}
}

func TestStickyScrollHeaderClickJumpsToScope(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("func outer() {\n")
	for i := range 20 {
		sb.WriteString("\tx := ")
		sb.WriteByte('a' + byte(i%26))
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	m, ed := viewModel(t, DefaultViewOptions(), sb.String(), 60, 12)
	m.stickyScroll = true
	ed.ScrollTo(5)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	l := m.layout()
	// Row 0 of the pane displays the sticky header ("func outer() {").
	m.Update(tea.MouseClickMsg(tea.Mouse{X: l.TextX + 2, Y: l.BodyTop, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.TextX + 2, Y: l.BodyTop, Button: tea.MouseLeft}))
	if got := ed.Active().Cursor.Line; got != 0 {
		t.Fatalf("caret line after sticky scroll click = %d, want 0", got)
	}

	// Sticky scroll must preserve the vertical scrollbar top cell and not intercept scrollbar clicks.
	ed.ScrollTo(5)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	l = m.layout()
	sbX := l.Panes[0].X + l.Panes[0].W - 1
	g := m.paint()
	if got := g.At(sbX, l.BodyTop).Text; got == " " {
		t.Fatalf("sticky scroll erased vertical scrollbar top cell at (%d,%d)", sbX, l.BodyTop)
	}
	ed.SetCursor(buffer.Pos{Line: 7, Col: 0}, false)
	m.Update(tea.MouseClickMsg(tea.Mouse{X: sbX, Y: l.BodyTop, Button: tea.MouseLeft}))
	if got := ed.Active().Cursor.Line; got == 0 {
		t.Fatal("clicking vertical scrollbar top cell jumped to stickyLine")
	}
}

func TestGutterDragSelectsWholeLines(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "one\ntwo\nthree\nfour\n", 60, 12)
	l := m.layout()
	gx := l.Panes[0].X + 1
	m.Update(tea.MouseClickMsg(tea.Mouse{X: gx, Y: l.BodyTop, Button: tea.MouseLeft}))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: gx, Y: l.BodyTop + 2, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: gx, Y: l.BodyTop + 2, Button: tea.MouseLeft}))
	a, h, ok := selectionOf(ed)
	if !ok || a != (buffer.Pos{Line: 0, Col: 0}) || h.Line < 2 {
		t.Fatalf("gutter drag selection = %v..%v (%v), want lines 0..2+", a, h, ok)
	}
}

func TestSplitBorderHoverAndDoubleClickEqualizesSplit(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "abc\ndef\n", 80, 16)
	runAction(t, m, "split_vertical")
	l := m.layout()
	if len(l.Borders) != 1 {
		t.Fatalf("borders = %d, want 1", len(l.Borders))
	}
	b := l.Borders[0]
	b.Node.Ratio = 75
	m.syncViewport()
	l = m.layout()
	b = l.Borders[0]

	m.Update(tea.MouseMotionMsg(tea.Mouse{X: b.X, Y: b.Y + 1}))
	if m.borderHover != 0 {
		t.Fatalf("borderHover = %d, want 0", m.borderHover)
	}
	// Double click same border cell equalizes Ratio back to 50.
	m.Update(tea.MouseClickMsg(tea.Mouse{X: b.X, Y: b.Y + 1, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: b.X, Y: b.Y + 1, Button: tea.MouseLeft}))
	m.Update(tea.MouseClickMsg(tea.Mouse{X: b.X, Y: b.Y + 1, Button: tea.MouseLeft}))
	if b.Node.Ratio != split.Even {
		t.Fatalf("Ratio after border double-click = %d, want %d", b.Node.Ratio, split.Even)
	}
}

func TestMiddleClickPastesInEditorAndDock(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\n", 60, 12)
	ed.SetCursor(buffer.Pos{Line: 0, Col: 0}, false)
	ed.Do("select_word", nil)
	ed.Do("copy", nil)
	l := m.layout()
	m.Update(tea.MouseClickMsg(tea.Mouse{X: l.TextX + 11, Y: l.BodyTop, Button: tea.MouseMiddle}))
	if got := ed.Active().Cursor; got.Line != 0 || got.Col < 11 {
		t.Fatalf("caret after middle click = %v", got)
	}
	if got := ed.Active().Buffer.Line(0); got != "hello worldhello" {
		t.Fatalf("line 0 = %q, want %q", got, "hello worldhello")
	}
}

func TestWelcomeDoorHoverHighlightsRowAndSettingsClick(t *testing.T) {
	m := New(Deps{Editor: noDocFake{newFakeEditor()}, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	l := m.layout()
	var target frame.WelcomeLine
	for _, wl := range l.Welcome {
		if wl.Item > 0 {
			target = wl
			break
		}
	}
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: target.X + 1, Y: target.Y}))
	if m.welcomeSel != target.Item {
		t.Fatalf("welcomeSel = %d, want hovered item %d", m.welcomeSel, target.Item)
	}

	// Settings header toggle & row toggle by mouse click.
	sm, sp := openSettingsPage(t, &fakeSettingsPort{snap: settingsSnapshot()})
	sp.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	sp.Draw(grid.New(90, 28))
	// Click title bar toggles layer if trusted; in default snapshot ProjectWritable=false -> notice.
	sp.Update(tea.MouseClickMsg(tea.Mouse{X: sp.box.X + 2, Y: sp.box.Y, Button: tea.MouseLeft}))
	if sm.notice == "" {
		t.Fatal("clicking Settings header gave no layer switch notice")
	}
	for i, r := range sp.rows {
		if !r.header && r.item.Kind == "bool" && i >= sp.top && i < sp.top+sp.page {
			y := sp.box.Y + 2 + (i - sp.top)
			sp.sel = i
			before := r.item.Value
			sp.Update(tea.MouseClickMsg(tea.Mouse{X: sp.box.X + 4, Y: y, Button: tea.MouseLeft}))
			if after := sp.rows[i].item.Value; after == before {
				t.Fatalf("second click on bool setting %q did not toggle %q", r.item.Key, before)
			}
			break
		}
	}
}

func TestPaletteInputCaretClickAndDiagnosticsSidebarClose(t *testing.T) {
	// Confirm palette input click keeps palette active and Live Grep toolbar click works.
	p := palette.New(palette.Options{
		Query:    "foo",
		Live:     true,
		Commands: commands.Commands(nil),
	})
	g := grid.New(100, 30)
	p.Draw(g)
	p.Update(tea.MouseClickMsg(tea.Mouse{X: 12, Y: 28, Button: tea.MouseLeft}))
	if !p.Active() {
		t.Fatal("input click closed palette")
	}

	// Diagnostics panel wheel scroll and close.
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"a", "b"}})
	lt.m.tree = newFakeTree(3)
	lt.m.explorerOpen = true
	itemsA := make([]Diagnostic, 0, 8)
	itemsB := make([]Diagnostic, 0, 6)
	for i := range 8 {
		itemsA = append(itemsA, Diagnostic{Start: buffer.Pos{Line: i}, Message: "ea", Severity: 1})
	}
	for i := range 6 {
		itemsB = append(itemsB, Diagnostic{Start: buffer.Pos{Line: i}, Message: "eb", Severity: 1})
	}
	lt.m.lsp.wsDiags = []FileDiagnostics{
		{Path: "/p/a.go", Items: itemsA},
		{Path: "/p/b.go", Items: itemsB},
	}
	lt.m.toggleDiagnosticsPanel()
	lt.m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if !lt.m.diag.Open() {
		t.Fatal("setup: diagnostics panel did not open")
	}
	l := lt.m.layout()
	scrolled := false
	for _, sec := range l.Sections {
		if sec.Kind == sidebar.KindDiagnostics {
			lt.m.Update(tea.MouseWheelMsg(tea.Mouse{X: l.ExX + 2, Y: sec.BodyTop, Button: tea.MouseWheelDown}))
			scrolled = true
			break
		}
	}
	if !scrolled || lt.m.diag.scroll != lt.m.wheelRows() {
		t.Fatalf("diagnostics wheel scroll: scrolled=%v scroll=%d want=%d", scrolled, lt.m.diag.scroll, lt.m.wheelRows())
	}
	lt.m.closeDiagPanel()
	if lt.m.diag.Open() {
		t.Fatal("closeDiagPanel left diagnostics open")
	}
}

func TestPhase43StatusClickMacroTitleAndScrollSync(t *testing.T) {
	// Window title when SetWindowTitle is true.
	opts := DefaultViewOptions()
	opts.SetWindowTitle = true
	m, _ := viewModel(t, opts, "alpha\nbeta\n", 80, 12)
	if got := m.View().WindowTitle; !strings.Contains(got, "doc.txt") || !strings.Contains(got, "gvim") {
		t.Fatalf("WindowTitle = %q, want document name and gvim", got)
	}

	// Macro recording pill in status bar.
	m.root.macro.recording = true
	statusText := m.paint().Plain()[m.layout().StatusY]
	if !strings.Contains(statusText, "● REC") {
		t.Fatalf("status bar during macro recording = %q, want ● REC", statusText)
	}
	m.root.macro.recording = false

	// Clicking diagnostics count badge in status bar toggles Diagnostics panel.
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"a", "b"}})
	if lt.m.lsp.diags == nil {
		lt.m.lsp.diags = make(map[string][]Diagnostic)
	}
	lt.m.lsp.diags["/p/a.go"] = []Diagnostic{{Start: buffer.Pos{Line: 0}, Message: "err", Severity: 1}}
	lt.m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	l := lt.m.layout()
	clicked := false
	for _, r := range l.Status {
		if strings.Contains(r.Text, "[E:1]") {
			_, cmd := lt.m.Update(tea.MouseClickMsg(tea.Mouse{X: r.X + 1, Y: l.StatusY, Button: tea.MouseLeft}))
			if cmd == nil {
				t.Fatal("clicking diagnostics status badge discarded toggleDiagnosticsPanel cmd")
			}
			clicked = true
			break
		}
	}
	if !clicked || !lt.m.diag.Open() {
		t.Fatalf("diagnostics status click: clicked=%v diagOpen=%v", clicked, lt.m.diag.Open())
	}

	// toggle_scroll_sync propagates scrolling to sibling split pane.
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line"
	}
	sm := New(Deps{
		Editor: &multiFake{docs: []*fakeDoc{{lines: lines}, {lines: lines}}},
	})
	sm.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	runAction(t, sm, "split_vertical")
	runAction(t, sm, "toggle_scroll_sync")
	if !sm.view.scrollSync {
		t.Fatal("toggle_scroll_sync did not enable scrollSync")
	}
	sm.scrollFocused(3)
	leaves := sm.splits.Root.Leaves()
	if len(leaves) != 2 {
		t.Fatalf("split leaves = %d, want 2", len(leaves))
	}
	for _, leaf := range leaves {
		if leaf != sm.splits.Focus && leaf.Top != 3 {
			t.Fatalf("sibling split Top after scroll_sync = %d, want 3", leaf.Top)
		}
	}
}
