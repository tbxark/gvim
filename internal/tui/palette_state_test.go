package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// dirtyEd is an lspEd whose tabs report the modified flag and a selection.
type dirtyEd struct {
	*lspEd
	modified  bool
	selection bool
}

func (e *dirtyEd) Tabs() []TabInfo {
	tabs := e.lspEd.Tabs()
	for i := range tabs {
		tabs[i].Modified = e.modified
	}
	return tabs
}

func (e *dirtyEd) Active() Document {
	d := e.lspEd.Active()
	if d == nil {
		return nil
	}
	return dirtyDoc{Document: d, modified: e.modified, selection: e.selection}
}

type dirtyDoc struct {
	Document
	modified, selection bool
}

func (d dirtyDoc) Modified() bool { return d.modified }
func (d dirtyDoc) HasPath() bool  { return true }
func (d dirtyDoc) Selection() (a, h buffer.Pos, ok bool) {
	return buffer.Pos{}, buffer.Pos{Col: 1}, d.selection
}

func TestPaletteStateFollowsTheEditor(t *testing.T) {
	tests := []struct {
		name      string
		modified  bool
		selection bool
		lsp       bool
		want      commands.State
	}{
		{"clean", false, false, false, commands.State{TextBuffer: true, Editable: true, HasPath: true}},
		{"modified", true, false, false, commands.State{TextBuffer: true, Editable: true, HasPath: true, Modified: true, AnyModified: true}},
		{"selection", false, true, false, commands.State{TextBuffer: true, Editable: true, HasPath: true, SelectionActive: true}},
		{"lsp", false, false, true, commands.State{TextBuffer: true, Editable: true, HasPath: true, HasLSPConfig: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var port *fakeLSP
			if tc.lsp {
				port = newFakeLSP("go")
			}
			lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
			lt.m.ed = &dirtyEd{lspEd: lt.ed, modified: tc.modified, selection: tc.selection}
			if got := lt.m.paletteState(); got != tc.want {
				t.Errorf("paletteState() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPaletteSaveFileRunsOnADirtyBuffer(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.m.ed = &dirtyEd{lspEd: lt.ed, modified: true}
	lt.m.openPalette(">save file")

	cmd := press(lt.m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on Save File of a dirty buffer started no command")
	}
	run, ok := cmd().(palette.RunCommandMsg)
	if !ok || run.Action != "save" {
		t.Fatalf("Enter ran %#v, want RunCommandMsg{Action: save}", run)
	}
}

// TestPaletteHistoryRecordsACommandRun: a real palette Enter → RunCommandMsg run records
// the action at the front of the palette history, which the next open passes to ranking.
func TestPaletteHistoryRecordsACommandRun(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.m.ed = &dirtyEd{lspEd: lt.ed, modified: true}
	lt.m.openPalette(">save file")
	cmd := press(lt.m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on Save File started no command")
	}
	run, ok := cmd().(palette.RunCommandMsg)
	if !ok || run.Action != "save" {
		t.Fatalf("Enter ran %#v, want RunCommandMsg{save}", cmd())
	}
	lt.m.Update(run)
	if len(lt.m.commandHistory) != 1 || lt.m.commandHistory[0] != "save" {
		t.Fatalf("command history = %v, want [save]", lt.m.commandHistory)
	}
}

// TestMenuCommandEntersPaletteHistory: a command run from the real menubar port is
// recorded in palette usage history and appears before the alphabetical first row when
// the command palette opens with an empty ">" query. Macro replay and the palette's own
// run path must not double-record; they are covered by the same history reset.
func TestMenuCommandEntersPaletteHistory(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.m.openMenu("View")
	idx := -1
	rows := lt.m.menu.Rows()
	for i, row := range rows {
		if row.Action == "toggle_line_numbers" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("the View menu has no Line Numbers row")
	}
	for i := 0; i < len(rows) && lt.m.menu.Selected() != idx; i++ {
		lt.m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if lt.m.menu.Selected() != idx {
		t.Fatalf("menu selected %d, want %d", lt.m.menu.Selected(), idx)
	}
	lt.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(lt.m.commandHistory) != 1 || lt.m.commandHistory[0] != "toggle_line_numbers" {
		t.Fatalf("menu command history = %v, want [toggle_line_numbers]", lt.m.commandHistory)
	}

	lt.m.openPalette(">")
	text := ansi.Strip(lt.m.View().Content)
	recent := strings.Index(text, "Toggle Line Numbers")
	alpha := strings.Index(text, "Add Cursor Above")
	if recent < 0 || alpha < 0 || recent > alpha {
		t.Fatalf("palette ordering after menu run: recent at %d, alphabetical at %d", recent, alpha)
	}
}

// realFileEditor is realEditor plus the file-opening surface, so root goto paths
// can move a real *editor.Editor while the view model still paints it.
type realFileEditor struct{ realEditor }

func (e realFileEditor) OpenFile(path string) error    { return e.ed.OpenFile(path) }
func (e realFileEditor) OpenPreview(path string) error { return e.ed.OpenPreview(path) }
func (e realFileEditor) GotoLine(line, col int) error  { return e.ed.GotoLine(line, col) }

// TestRelativeQuickOpenJumpsFromTheCurrentLine drives the real ':' palette path:
// +2 moves down two lines and -99 clamps at line 1, as Fresh's relative goto does.
func TestRelativeQuickOpenJumpsFromTheCurrentLine(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "a\nb\nc\nd\ne\nf\n", 80, 24)
	m.ed = realFileEditor{realEditor{ed}}
	if err := ed.GotoLine(5, 1); err != nil {
		t.Fatal(err)
	}
	m.openPalette(":+2")
	for _, msg := range deliver(press(m, special(tea.KeyEnter))) {
		m.Update(msg)
	}
	if got := ed.Active().Cursor.Line; got != 6 {
		t.Fatalf("+2 from 1-based line 5 moved to 0-based line %d, want 6", got)
	}

	if err := ed.GotoLine(2, 1); err != nil {
		t.Fatal(err)
	}
	m.openPalette(":-99")
	for _, msg := range deliver(press(m, special(tea.KeyEnter))) {
		m.Update(msg)
	}
	if got := ed.Active().Cursor.Line; got != 0 {
		t.Fatalf("-99 from line 2 clamped to 0-based line %d, want 0", got)
	}
}

// TestPaletteTabAcceptsSuggestionWithoutRunningTheCommand drives Tab through the root:
// it must reach the palette overlay (not the editor), leave the palette open and run no
// command; Enter still runs the selected row.
func TestPaletteTabAcceptsSuggestionWithoutRunningTheCommand(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.m.ed = &dirtyEd{lspEd: lt.ed, modified: true}
	lt.m.openPalette(">save file")
	before := len(lt.m.overlays)
	if _, ok := lt.m.overlays[before-1].(*palette.Palette); !ok {
		t.Fatalf("the top overlay is %T, not the palette", lt.m.overlays[before-1])
	}
	cmd := press(lt.m, special(tea.KeyTab))
	for _, msg := range deliver(cmd) {
		if run, ok := msg.(palette.RunCommandMsg); ok {
			t.Fatalf("Tab ran %q; only Enter should run the selected command", run.Action)
		}
	}
	if len(lt.m.overlays) != before {
		t.Fatalf("Tab closed/changed the palette overlay: %d -> %d", before, len(lt.m.overlays))
	}
	if _, ok := lt.m.overlays[len(lt.m.overlays)-1].(*palette.Palette); !ok {
		t.Fatal("the palette is no longer the top overlay after Tab")
	}

	cmd = press(lt.m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on the accepted Save File suggestion started no command")
	}
	run, ok := cmd().(palette.RunCommandMsg)
	if !ok || run.Action != "save" {
		t.Fatalf("Enter ran %#v, want RunCommandMsg{Action: save}", cmd())
	}
}
