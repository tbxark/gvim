package tui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// targetDoc is a fake document that is a plugin buffer, so line targets can find it.
type targetDoc struct {
	*fakeDoc
	id pluginapi.BufferID
}

func (d targetDoc) BufferID() pluginapi.BufferID { return d.id }

// targetEditor is a fake editor that opens files and records the jumps.
type targetEditor struct {
	*fakeEditor
	opened   []string
	gotoLine int
}

func (e *targetEditor) Active() Document { return targetDoc{e.doc, 1} }
func (e *targetEditor) OpenFile(path string) error {
	e.opened = append(e.opened, path)
	return nil
}
func (e *targetEditor) OpenPreview(path string) error { return e.OpenFile(path) }
func (e *targetEditor) GotoLine(line, col int) error {
	e.gotoLine = line
	return nil
}

// targetModel builds a model whose plugin buffer 1 is the active document.
func targetModel(t *testing.T) (*Model, *targetEditor, pluginapi.Runtime) {
	t.Helper()
	rt := newFakeRuntime(t)
	ed := &targetEditor{fakeEditor: newFakeEditor("hello", "world")}
	pe := &fakePluginEditor{ed: ed.fakeEditor, active: 1, bufs: []pluginapi.BufferInfo{
		{ID: 1, Path: "/w/a.go", Name: "a.go", Active: true, LineCount: 2},
	}}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	return m, ed, rt
}

// TestLineTargetsOpenOnEnterAndClick checks the host-side jump: Enter on a target line
// opens its path at the target line, a click does the same, a line without a target keeps
// its normal action, and an unload or buffer close drops the targets.
func TestLineTargetsOpenOnEnterAndClick(t *testing.T) {
	m, ed, rt := targetModel(t)
	h := m.plug.host
	if err := h.SetLineTargets("p", 1, []pluginapi.LineTarget{{Line: 0, Path: "dir/target.go", Target: 5}}); err != nil {
		t.Fatal(err)
	}
	doc := m.ed.Active()
	m.ed.SetCursor(buffer.Pos{Line: 0, Col: 0}, false)
	cmd := m.dispatch(Command{Action: "insert_newline"})
	if cmd != nil {
		cmd()
	}
	want := filepath.Join("", "dir", "target.go")
	if len(ed.opened) != 1 || ed.opened[0] != want {
		t.Fatalf("opened = %v, want %q", ed.opened, want)
	}
	if ed.gotoLine != 5 {
		t.Fatalf("goto line = %d, want 5", ed.gotoLine)
	}
	// A press on the target row opens it too.
	ed.opened = nil
	m.pressEditor(editorpane.Pane{Doc: doc}, editorpane.Hit{Kind: editorpane.HitText, Pos: buffer.Pos{Line: 0, Col: 0}}, 1)
	if len(ed.opened) != 1 {
		t.Fatalf("click opened = %v", ed.opened)
	}
	// A line without a target keeps the editor action.
	ed.opened = nil
	m.ed.SetCursor(buffer.Pos{Line: 1, Col: 0}, false)
	m.dispatch(Command{Action: "insert_newline"})
	if len(ed.opened) != 0 {
		t.Fatalf("a line without a target opened %v", ed.opened)
	}
	// Cleanup: unload, then a direct close drop.
	if err := h.SetLineTargets("p", 1, []pluginapi.LineTarget{{Line: 1, Path: "x.go"}}); err != nil {
		t.Fatal(err)
	}
	h.OwnerGone("p", 1)
	if _, ok := m.lineTargetAt(m.ed.Active(), 1); ok {
		t.Fatal("unload left a line target")
	}
	if err := h.SetLineTargets("p", 1, []pluginapi.LineTarget{{Line: 1, Path: "x.go"}}); err != nil {
		t.Fatal(err)
	}
	m.plugLineTargetsDrop(1)
	if _, ok := m.lineTargetAt(m.ed.Active(), 1); ok {
		t.Fatal("closing the buffer left a line target")
	}
	_ = rt
}
