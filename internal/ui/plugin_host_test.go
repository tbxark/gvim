package ui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/palette"
)

// TestMovedIndex checks where a tab index goes when the tab at from moves to to.
func TestMovedIndex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		i, from, to int
		want        int
	}{
		{"the moved tab", 1, 1, 3, 3},
		{"between, moving right", 2, 1, 3, 1},
		{"at the destination, moving right", 3, 1, 3, 2},
		{"before the range, moving right", 0, 1, 3, 0},
		{"after the range, moving right", 4, 1, 3, 4},
		{"between, moving left", 2, 3, 1, 3},
		{"at the destination, moving left", 1, 3, 1, 2},
		{"before the range, moving left", 0, 3, 1, 0},
		{"after the range, moving left", 4, 3, 1, 4},
		{"no tab", -1, 3, 1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := movedIndex(tt.i, tt.from, tt.to); got != tt.want {
				t.Fatalf("movedIndex(%d, %d, %d) = %d, want %d", tt.i, tt.from, tt.to, got, tt.want)
			}
		})
	}
}

// leavesOver sets the split tree to one leaf for each tab index in tabs; the first leaf
// has the focus. It returns the leaves.
func leavesOver(m *Model, tabs ...int) []*splitNode {
	leaves := make([]*splitNode, len(tabs))
	for i, tab := range tabs {
		leaves[i] = &splitNode{tab: tab}
	}
	root := leaves[0]
	if len(leaves) > 1 {
		root = &splitNode{a: leaves[0], b: leaves[1]}
	}
	m.splits.root, m.splits.focus = root, leaves[0]
	m.splits.tabs = len(m.ed.Tabs())
	return leaves
}

// TestHostCloseAndMoveKeepSplitLeaves checks that closing and moving a tab through the
// plugin port updates the split leaves, which name tabs by index, the way the actions do.
func TestHostCloseAndMoveKeepSplitLeaves(t *testing.T) {
	t.Parallel()
	t.Run("close the tab before a pane's tab", func(t *testing.T) {
		t.Parallel()
		m, ed, pe := newPluginEditorModel(t, newFakeRuntime(t))
		leaves := leavesOver(m, 0, 2)
		if err := m.plug.host.Close(2, false); err != nil {
			t.Fatal(err)
		}
		if leaves[1].tab != 1 {
			t.Fatalf("the pane that showed tab 2 shows %d, want 1", leaves[1].tab)
		}
		if len(ed.tabs) != 2 || len(pe.closed) != 1 {
			t.Fatalf("tabs %d, closed %v", len(ed.tabs), pe.closed)
		}
	})
	t.Run("a refused close changes nothing", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newPluginEditorModel(t, newFakeRuntime(t))
		leaves := leavesOver(m, 0, 2)
		if err := m.plug.host.Close(99, false); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("Close(99) = %v, want closed", err)
		}
		if leaves[1].tab != 2 {
			t.Fatalf("leaf moved to %d", leaves[1].tab)
		}
	})
	t.Run("move a tab across a pane's tab", func(t *testing.T) {
		t.Parallel()
		m, ed, _ := newPluginEditorModel(t, newFakeRuntime(t))
		leaves := leavesOver(m, 0, 1)
		m.splits.seenTab, m.splits.prevTab = 1, 2
		// buffer 3 (tab 2) moves two places left, to index 0; b.txt shifts to 2.
		if err := m.plug.host.MoveTab(3, -2); err != nil {
			t.Fatal(err)
		}
		if leaves[1].tab != 2 {
			t.Fatalf("the pane that showed b.txt shows tab %d, want 2", leaves[1].tab)
		}
		if m.splits.seenTab != 2 || m.splits.prevTab != 0 {
			t.Fatalf("history = (%d, %d), want (2, 0)", m.splits.seenTab, m.splits.prevTab)
		}
		if ed.tabs[0].Name != "c.txt" {
			t.Fatalf("tabs = %+v", ed.tabs)
		}
	})
	t.Run("a move that does not move changes nothing", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newPluginEditorModel(t, newFakeRuntime(t))
		leaves := leavesOver(m, 0, 1)
		if err := m.plug.host.MoveTab(1, -1); err != nil {
			t.Fatal(err)
		}
		if leaves[1].tab != 1 {
			t.Fatalf("leaf = %d", leaves[1].tab)
		}
	})
}

// TestHostWithoutEditorPortRefuses checks that with no PluginEditor the buffer calls of a
// plugin answer unsupported or nothing, and nothing panics.
func TestHostWithoutEditorPortRefuses(t *testing.T) {
	t.Parallel()
	m, _ := newPluginModel(t, newFakeRuntime(t))
	h := m.plug.host
	if _, err := h.Lines(1, 0, 1); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("Lines = %v, want unsupported", err)
	}
	if err := h.Activate(1); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("Activate = %v, want unsupported", err)
	}
	if err := h.Close(1, true); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("Close = %v, want unsupported", err)
	}
	if h.ActiveBuffer() != 0 || len(h.ListBuffers()) != 0 || h.FindBuffer("x") != 0 {
		t.Fatal("a missing port reported buffers")
	}
	if err := h.Save(1, 1, ""); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Save = %v, want closed (no such buffer)", err)
	}
}

// openEditor is a fake editor that opens files into a fake plugin editor.
type openEditor struct {
	*fakeEditor
	pe     *fakePluginEditor
	fail   map[string]bool
	opened []string
}

func (e *openEditor) OpenFile(path string) error {
	if e.fail[path] {
		return errors.New("cannot read " + path)
	}
	e.opened = append(e.opened, path)
	id := pluginapi.BufferID(len(e.pe.bufs) + 1)
	e.pe.bufs = append(e.pe.bufs, pluginapi.BufferInfo{ID: id, Path: path, Name: filepath.Base(path)})
	e.pe.active = id
	return nil
}
func (e *openEditor) OpenPreview(path string) error { return e.OpenFile(path) }
func (e *openEditor) GotoLine(line, col int) error  { return nil }

// TestHostOpenResolvesWithTheBufferID checks Host.Open: it defers, then opens, places the
// caret, restores the user's tab for a background open, and resolves the wait with the
// BufferID or with why the file did not open.
func TestHostOpenResolvesWithTheBufferID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		req        pluginapi.OpenRequest
		fail       bool
		wantValues []any
		wantErr    string
		wantActive pluginapi.BufferID
		wantCursor []pluginapi.Pos
	}{
		{name: "open", req: pluginapi.OpenRequest{Path: "/w/new.txt"}, wantValues: []any{pluginapi.BufferID(4)}, wantActive: 4},
		{
			name: "with a position", req: pluginapi.OpenRequest{Path: "/w/new.txt", Pos: &pluginapi.Pos{Line: 3, Col: 2}},
			wantValues: []any{pluginapi.BufferID(4)}, wantActive: 4, wantCursor: []pluginapi.Pos{{Line: 3, Col: 2}},
		},
		{
			name: "in the background", req: pluginapi.OpenRequest{Path: "/w/new.txt", Background: true},
			wantValues: []any{pluginapi.BufferID(4)}, wantActive: 1,
		},
		{name: "failure", req: pluginapi.OpenRequest{Path: "/w/bad.txt"}, fail: true, wantErr: "io: ", wantActive: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			rt.strictSlice = true
			base, _, pe := newPluginEditorModel(t, rt)
			oe := &openEditor{fakeEditor: base.ed.(*fakeEditor), pe: pe, fail: map[string]bool{"/w/bad.txt": tt.fail}}
			m := New(Deps{Editor: oe, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			useHost(rt, m)
			rt.onRun = func(string, map[string]any) {
				if err := rt.host.Open(8, tt.req); err != nil {
					t.Error(err)
				}
				if len(oe.opened) != 0 {
					t.Error("the file opened inside the slice")
				}
			}
			must(t, m.Run("x.cmd"))
			if len(rt.resolved) != 1 {
				t.Fatalf("resolved %+v, want one completion", rt.resolved)
			}
			c := rt.resolved[0]
			if c.Token != 8 || len(c.Values) != len(tt.wantValues) || (len(c.Values) == 1 && c.Values[0] != tt.wantValues[0]) {
				t.Fatalf("completion = %+v, want values %v", c, tt.wantValues)
			}
			if tt.wantErr != "" && !strings.HasPrefix(c.Err, tt.wantErr) {
				t.Fatalf("completion error = %q, want prefix %q", c.Err, tt.wantErr)
			}
			if tt.wantErr == "" && c.Err != "" {
				t.Fatalf("completion error = %q", c.Err)
			}
			if pe.active != tt.wantActive {
				t.Fatalf("active buffer = %d, want %d", pe.active, tt.wantActive)
			}
			if len(pe.cursors) != len(tt.wantCursor) || (len(pe.cursors) == 1 && pe.cursors[0] != tt.wantCursor[0]) {
				t.Fatalf("cursor moves = %v, want %v", pe.cursors, tt.wantCursor)
			}
		})
	}
}

// TestHostOpenValidation checks what Host.Open refuses at once without taking the token.
func TestHostOpenValidation(t *testing.T) {
	t.Parallel()
	m, _ := newPluginModel(t, newFakeRuntime(t))
	if err := m.plug.host.Open(1, pluginapi.OpenRequest{}); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("Open with no path = %v, want invalid", err)
	}
	if err := m.plug.host.Open(1, pluginapi.OpenRequest{Path: "/x"}); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("Open on an editor that cannot open files = %v, want unsupported", err)
	}
	if len(m.plug.waits) != 0 || len(m.plug.later) != 0 {
		t.Fatal("a refused Open took the token or queued work")
	}
}

// tabsPluginEditor is a plugin port over the tabs of an editor: the id of a tab is its
// index plus one.
type tabsPluginEditor struct {
	pluginapi.Unsupported
	ed EditorState
}

func (p *tabsPluginEditor) ListBuffers() []pluginapi.BufferInfo {
	var out []pluginapi.BufferInfo
	for i, t := range p.ed.Tabs() {
		out = append(out, pluginapi.BufferInfo{ID: pluginapi.BufferID(i + 1), Path: t.Path, Name: t.Name, Active: t.Active})
	}
	return out
}

func (p *tabsPluginEditor) ActiveBuffer() pluginapi.BufferID {
	for _, b := range p.ListBuffers() {
		if b.Active {
			return b.ID
		}
	}
	return 0
}

func (p *tabsPluginEditor) Buffer(id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	for _, b := range p.ListBuffers() {
		if b.ID == id {
			return b, true
		}
	}
	return pluginapi.BufferInfo{}, false
}

func (p *tabsPluginEditor) FindBuffer(path string) pluginapi.BufferID {
	for _, b := range p.ListBuffers() {
		if b.Path == path {
			return b.ID
		}
	}
	return 0
}

// TestHostOpenReadsALargeFileInACommand checks that Host.Open of a large file goes through
// the command that reads it, and resolves the wait only once the file is open.
func TestHostOpenReadsALargeFileInACommand(t *testing.T) {
	t.Parallel()
	big := filepath.Join(t.TempDir(), "big.txt")
	must(t, os.WriteFile(big, []byte(strings.Repeat("some line\n", 400)), 0o644))
	rt := newFakeRuntime(t, "x.cmd")
	ae := &asyncOpenEditor{largeEditor: newLargeEditor(t)}
	m := New(Deps{Editor: ae, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: &tabsPluginEditor{ed: ae}}})
	start(m)
	useHost(rt, m)
	rt.onRun = func(string, map[string]any) {
		if err := rt.host.Open(6, pluginapi.OpenRequest{Path: big}); err != nil {
			t.Error(err)
		}
	}
	_, cmd := m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	if len(rt.resolved) != 0 {
		t.Fatalf("the wait resolved before the file was read: %+v", rt.resolved)
	}
	feedAll(t, m, cmd, 10)
	if len(rt.resolved) != 1 || rt.resolved[0].Token != 6 || rt.resolved[0].Err != "" || rt.resolved[0].Values[0] != pluginapi.BufferID(1) {
		t.Fatalf("resolved %+v, want token 6 with buffer 1", rt.resolved)
	}
	if tabs := ae.Tabs(); len(tabs) != 1 || tabs[0].Path != big || ae.loads != 1 {
		t.Fatalf("tabs %+v, loads %d", tabs, ae.loads)
	}
}

// TestHostSaveWritesTheFile runs Host.Save over a real editor: the write is a command, the
// result is applied on the update loop, and the wait resolves with the outcome. A refused
// save resolves with its reason.
func TestHostSaveWritesTheFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		readOnly bool
		path     string // the path argument
		wantFile string
		wantErr  string
	}{
		{name: "save", wantFile: "xabc\n"},
		{name: "read-only", readOnly: true, wantFile: "abc\n", wantErr: "read_only: read-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := writeAutoSaveFile(t, "abc\n")
			m, se := newSaveModel(t, file, nil)
			must(t, func() error { return se.ed.Do("insert_char", map[string]any{"text": "x"}).Err }())
			if tt.readOnly {
				must(t, se.ed.Do("toggle_read_only", nil).Err)
			}
			rt := newFakeRuntime(t, "x.cmd")
			pe := &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1, Path: file, Name: "a.txt"}}}
			m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: pe})
			useHost(rt, m)
			rt.onRun = func(string, map[string]any) {
				if err := rt.host.Save(3, 1, tt.path); err != nil {
					t.Error(err)
				}
			}
			_, cmd := m.Update(palette.RunCommandMsg{Action: "x.cmd"})
			if se.writes != 0 {
				t.Fatal("the file was written in Update")
			}
			feedAll(t, m, cmd, 10)
			if got := saveFileText(file); got != tt.wantFile {
				t.Fatalf("file = %q, want %q", got, tt.wantFile)
			}
			if len(rt.resolved) != 1 || rt.resolved[0].Token != 3 || rt.resolved[0].Err != tt.wantErr {
				t.Fatalf("resolved %+v, want token 3 with error %q", rt.resolved, tt.wantErr)
			}
			if tt.wantErr == "" && rt.resolved[0].Values[0] != true {
				t.Fatalf("resolved %+v, want true", rt.resolved[0])
			}
		})
	}
}

// TestHostSaveActivatesTheBufferOnlyWhileItPlansTheSave checks that saving a buffer that
// is not the user's tab leaves the user's tab active afterwards.
func TestHostSaveActivatesTheBufferOnlyWhileItPlansTheSave(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	m, ed, pe := newPluginEditorModel(t, rt)
	rt.onRun = func(string, map[string]any) {
		if err := rt.host.Save(3, 2, ""); err != nil {
			t.Error(err)
		}
	}
	must(t, m.Run("x.cmd"))
	if want := []pluginapi.BufferID{2, 1}; len(pe.activations) != 2 || pe.activations[0] != want[0] || pe.activations[1] != want[1] {
		t.Fatalf("activations = %v, want %v", pe.activations, want)
	}
	if pe.active != 1 {
		t.Fatalf("active = %d, want the user's buffer back", pe.active)
	}
	if got := actionsOf(ed); len(got) != 1 || got[0] != "save" {
		t.Fatalf("editor actions = %v, want the save", got)
	}
	if len(rt.resolved) != 1 || rt.resolved[0].Values[0] != true {
		t.Fatalf("resolved %+v", rt.resolved)
	}
}

// TestHostRevert checks Host.Revert: it validates at once, defers, and resolves with the
// outcome; a read that failed comes back as the error.
func TestHostRevert(t *testing.T) {
	t.Parallel()
	t.Run("validation", func(t *testing.T) {
		t.Parallel()
		m, _, pe := newPluginEditorModel(t, newFakeRuntime(t))
		pe.bufs = append(pe.bufs, pluginapi.BufferInfo{ID: 4, Name: "[No Name]"})
		if err := m.plug.host.Revert(1, 99); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("Revert(99) = %v, want closed", err)
		}
		if err := m.plug.host.Revert(1, 4); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Fatalf("Revert(untitled) = %v, want invalid", err)
		}
		if len(m.plug.waits) != 0 {
			t.Fatal("a refused Revert took the token")
		}
	})
	tests := []struct {
		name    string
		err     error
		wantErr string
	}{
		{name: "reverted"},
		{name: "refused by the editor", err: editor.ErrReadOnly, wantErr: "read_only: read-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			base, _, pe := newPluginEditorModel(t, rt)
			ee := &errEditor{fakeEditor: base.ed.(*fakeEditor), err: tt.err, only: []string{"revert"}}
			m := New(Deps{Editor: ee, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			useHost(rt, m)
			rt.onRun = func(string, map[string]any) {
				if err := rt.host.Revert(5, 2); err != nil {
					t.Error(err)
				}
			}
			must(t, m.Run("x.cmd"))
			if len(rt.resolved) != 1 || rt.resolved[0].Err != tt.wantErr {
				t.Fatalf("resolved %+v, want error %q", rt.resolved, tt.wantErr)
			}
			if pe.active != 1 {
				t.Fatalf("active = %d, want the user's buffer back", pe.active)
			}
		})
	}
	t.Run("a failed read", func(t *testing.T) {
		t.Parallel()
		rt := newFakeRuntime(t)
		m, _, _ := newPluginEditorModel(t, rt)
		must(t, m.plugWait(5, "revert"))
		m.Update(pluginRevertedMsg{t: 5, inner: revertDoneMsg{result: editor.RevertResult{Err: errors.New("file is gone")}}})
		if len(rt.resolved) != 1 || rt.resolved[0].Err != "invalid: file is gone" {
			t.Fatalf("resolved %+v", rt.resolved)
		}
	})
}

// TestHostUIMethods checks the small ui calls: notices, focus, the clipboard, the screen
// size and the workspace reads.
func TestHostUIMethods(t *testing.T) {
	t.Parallel()
	t.Run("Notify", func(t *testing.T) {
		t.Parallel()
		m, _ := newPluginModel(t, newFakeRuntime(t))
		h := m.plug.host
		h.Notify("p", pluginapi.LevelInfo, "hello")
		if m.notice != "hello" || m.notes.quiet != "hello" {
			t.Fatalf("info notice = %q (quiet %q), want hello kept out of the log", m.notice, m.notes.quiet)
		}
		h.Notify("p", pluginapi.LevelWarn, "careful")
		if m.notice != "p: careful" {
			t.Fatalf("warn notice = %q", m.notice)
		}
		// the runtime's messages name the plugin themselves: the name is not put in front twice
		h.Notify("rterr", pluginapi.LevelError, "rterr: rterr/rterr.lua:4: boom")
		if m.notice != "rterr: rterr/rterr.lua:4: boom" {
			t.Fatalf("doubled prefix: %q", m.notice)
		}
		h.Notify("wc", pluginapi.LevelError, "wc suspended after 3 strikes (Plugin: Manager)")
		if m.notice != "wc suspended after 3 strikes (Plugin: Manager)" {
			t.Fatalf("doubled name: %q", m.notice)
		}
		h.Notify("", pluginapi.LevelError, "bad")
		if m.notice != "bad" {
			t.Fatalf("error notice = %q", m.notice)
		}
		h.Notify("p", pluginapi.LevelError, "")
		if m.notice != "bad" {
			t.Fatalf("an empty message changed the notice to %q", m.notice)
		}
	})
	t.Run("Notify keeps every warning of one update", func(t *testing.T) {
		t.Parallel()
		m, _ := newPluginModel(t, newFakeRuntime(t))
		h := m.plug.host
		h.Notify("", pluginapi.LevelWarn, "plugin 'a' failed to load")
		h.Notify("", pluginapi.LevelError, "plugin 'b' failed to load")
		h.Notify("", pluginapi.LevelError, "plugin 'b' failed to load")
		h.Notify("", pluginapi.LevelInfo, "loaded")
		m.notes.record("", m.notice)
		want := []string{"plugin 'a' failed to load", "plugin 'b' failed to load"}
		if !slices.Equal(m.notes.items, want) {
			t.Fatalf("warnings kept = %q, want %q", m.notes.items, want)
		}
	})
	t.Run("Focus", func(t *testing.T) {
		t.Parallel()
		m, _ := newPluginModel(t, newFakeRuntime(t))
		if err := m.plug.host.Focus("explorer"); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
			t.Fatalf("Focus(explorer) with no explorer = %v, want unsupported", err)
		}
		if err := m.plug.host.Focus("sidebar"); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Fatalf("Focus(sidebar) = %v, want invalid", err)
		}
		if err := m.plug.host.Focus("editor"); err != nil {
			t.Fatal(err)
		}
		withTree := New(Deps{Editor: newFakeEditor("x"), Explorer: newFakeTree(2), Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: newFakeRuntime(t)}})
		withTree.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		if err := withTree.plug.host.Focus("explorer"); err != nil || !withTree.explorerFocus {
			t.Fatalf("Focus(explorer) = %v, focus %v", err, withTree.explorerFocus)
		}
	})
	t.Run("clipboard and screen", func(t *testing.T) {
		t.Parallel()
		m, _ := newPluginModel(t, newFakeRuntime(t))
		m.plug.host.SetClipboard("copied")
		// The write is coalesced and emitted by the pump (S13-3), not collected per call.
		if m.pending != nil || len(m.plug.later) != 1 {
			t.Fatalf("SetClipboard collected %v with %d deferred closures", m.pending != nil, len(m.plug.later))
		}
		if m.pluginPump() == nil {
			t.Fatal("the pump produced no clipboard command")
		}
		if w, h := m.plug.host.ScreenSize(); w != 80 || h != 24 {
			t.Fatalf("ScreenSize = %d x %d", w, h)
		}
	})
	t.Run("workspace", func(t *testing.T) {
		t.Parallel()
		ed := newFakeEditor("x")
		m := New(Deps{
			Editor: ed, Explorer: newFakeTree(1), Keys: testKeys(), Clock: testClock, HideLineNumbers: true,
			Files: fakeFiles{"a", "b", "c"}, Settings: Settings{Theme: "dark", TabSize: 3, InsertSpaces: true},
			Root: RootPorts{Plugins: newFakeRuntime(t)},
		})
		h := m.plug.host
		if h.Root() != "/ws" {
			t.Fatalf("Root = %q", h.Root())
		}
		if files, loading := h.Files(2); len(files) != 2 || !loading {
			t.Fatalf("Files(2) = %v, %v", files, loading)
		}
		if files, _ := h.Files(0); len(files) != 3 {
			t.Fatalf("Files(0) = %v, want all", files)
		}
		cfg := map[string]any{"theme": "dark", "tab_size": 3, "insert_spaces": true, "line_numbers": false}
		for k, want := range cfg {
			if got, ok := h.Config(k); !ok || got != want {
				t.Errorf("Config(%q) = %v, %v, want %v", k, got, ok, want)
			}
		}
		if _, ok := h.Config("shell_command"); ok {
			t.Error("Config answered a key outside the allowlist")
		}
	})
	t.Run("config keys", func(t *testing.T) {
		t.Parallel()
		m := New(Deps{
			Editor: newFakeEditor("x"), Keys: testKeys(), Clock: testClock,
			Settings: Settings{LineEnding: "crlf", TrimTrailingWhitespace: true, EnsureFinalNewline: true},
			View:     &ViewOptions{LineWrap: true, HighlightCurrentLine: false, ShowWhitespace: true, CursorStyle: "bar"},
			Root:     RootPorts{Plugins: newFakeRuntime(t), AutoSave: 45 * time.Second},
		})
		want := map[string]any{
			"line_ending": "crlf", "trim_trailing_whitespace": true, "ensure_final_newline": true,
			"line_wrap": true, "highlight_current_line": false, "show_whitespace": true,
			"cursor_style": "bar", "auto_save_enabled": true, "auto_save_interval_secs": 45,
		}
		for k, v := range want {
			if got, ok := m.plug.host.Config(k); !ok || got != v {
				t.Errorf("Config(%q) = %v, %v, want %v", k, got, ok, v)
			}
		}
		// The keys of plugin.ConfigKeys, which ui cannot import: keep the two lists equal.
		for _, k := range []string{
			"theme", "tab_size", "insert_spaces", "line_ending", "trim_trailing_whitespace",
			"ensure_final_newline", "auto_indent", "line_numbers", "line_wrap", "highlight_current_line",
			"show_whitespace", "cursor_style", "auto_save_enabled", "auto_save_interval_secs",
		} {
			if _, ok := m.plug.host.Config(k); !ok {
				t.Errorf("Config(%q) is not available, but gvim.config.get lists it", k)
			}
		}
		off := New(Deps{Editor: newFakeEditor("x"), Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: newFakeRuntime(t)}})
		if got, ok := off.plug.host.Config("auto_save_enabled"); !ok || got != false {
			t.Errorf("auto_save_enabled without auto-save = %v, %v", got, ok)
		}
	})
	t.Run("theme", func(t *testing.T) {
		t.Parallel()
		for _, tt := range []struct {
			name     string
			wantDark bool
		}{{"dark", true}, {"light", false}} {
			th, err := theme.Load(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			m := New(Deps{Editor: newFakeEditor("x"), Keys: testKeys(), Clock: testClock, Theme: th, Root: RootPorts{Plugins: newFakeRuntime(t)}})
			info := m.plug.host.Theme()
			if info.Name != tt.name || info.Dark != tt.wantDark {
				t.Errorf("Theme() for %s = %+v", tt.name, info)
			}
			r, g, b, ok := m.plug.host.ThemeColor("editor.bg")
			want := th.RGBOr("editor", "bg", theme.RGB{})
			if !ok || r != want.R || g != want.G || b != want.B {
				t.Errorf("ThemeColor(editor.bg) = %d %d %d %v, want %v", r, g, b, ok, want)
			}
			if _, _, _, ok := m.plug.host.ThemeColor("nonsense"); ok {
				t.Error("ThemeColor answered a key with no section")
			}
		}
	})
}

// fakeFiles is a file index snapshot that is still loading.
type fakeFiles []string

func (f fakeFiles) Snapshot() ([]string, bool) { return f, true }
func (fakeFiles) Done() <-chan struct{}        { return nil }
func (fakeFiles) Close()                       {}

// TestHostRefusalsDoNotDependOnTheNotice checks that a refused Save, Revert or Open is
// reported from the error of the plan, not from whether the status row changed: a second
// refusal with the same text as the notice already on the row is still a refusal, and a
// stale notice is not the reason a file did not open.
func TestHostRefusalsDoNotDependOnTheNotice(t *testing.T) {
	t.Parallel()
	t.Run("save", func(t *testing.T) {
		t.Parallel()
		file := writeAutoSaveFile(t, "abc\n")
		m, se := newSaveModel(t, file, nil)
		must(t, se.ed.Do("insert_char", map[string]any{"text": "x"}).Err)
		must(t, se.ed.Do("toggle_read_only", nil).Err)
		rt := newFakeRuntime(t, "x.cmd")
		pe := &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1, Path: file, Name: "a.txt"}}}
		m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: pe})
		useHost(rt, m)
		rt.onRun = func(string, map[string]any) {
			for _, tok := range []pluginapi.Token{3, 4} {
				if err := rt.host.Save(tok, 1, ""); err != nil {
					t.Error(err)
				}
			}
		}
		_, cmd := m.Update(palette.RunCommandMsg{Action: "x.cmd"})
		feedAll(t, m, cmd, 10)
		if len(rt.resolved) != 2 {
			t.Fatalf("resolved %+v, want two completions", rt.resolved)
		}
		for _, c := range rt.resolved {
			if c.Err == "" || len(c.Values) != 0 || !strings.HasPrefix(c.Err, string(pluginapi.CodeReadOnly)+": ") {
				t.Fatalf("completion %+v, want a read_only refusal for each save", c)
			}
		}
	})
	t.Run("revert", func(t *testing.T) {
		t.Parallel()
		rt := newFakeRuntime(t, "x.cmd")
		base, _, pe := newPluginEditorModel(t, rt)
		ee := &errEditor{fakeEditor: base.ed.(*fakeEditor), err: editor.ErrReadOnly, only: []string{"revert"}}
		m := New(Deps{Editor: ee, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		useHost(rt, m)
		rt.onRun = func(string, map[string]any) {
			for _, tok := range []pluginapi.Token{5, 6} {
				if err := rt.host.Revert(tok, 2); err != nil {
					t.Error(err)
				}
			}
		}
		must(t, m.Run("x.cmd"))
		if len(rt.resolved) != 2 {
			t.Fatalf("resolved %+v, want two completions", rt.resolved)
		}
		for _, c := range rt.resolved {
			if c.Err == "" || len(c.Values) != 0 || !strings.HasPrefix(c.Err, string(pluginapi.CodeReadOnly)+": ") {
				t.Fatalf("completion %+v, want a read_only refusal for each revert", c)
			}
		}
	})
	t.Run("open", func(t *testing.T) {
		t.Parallel()
		rt := newFakeRuntime(t, "x.cmd")
		base, _, pe := newPluginEditorModel(t, rt)
		oe := &openEditor{fakeEditor: base.ed.(*fakeEditor), pe: pe, fail: map[string]bool{"/w/bad.txt": true}}
		m := New(Deps{Editor: oe, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		useHost(rt, m)
		rt.onRun = func(string, map[string]any) {
			m.notice = "an unrelated notice"
			if err := rt.host.Open(8, pluginapi.OpenRequest{Path: "/w/bad.txt"}); err != nil {
				t.Error(err)
			}
		}
		must(t, m.Run("x.cmd"))
		if len(rt.resolved) != 1 || !strings.Contains(rt.resolved[0].Err, "cannot read /w/bad.txt") || strings.Contains(rt.resolved[0].Err, "unrelated") {
			t.Fatalf("resolved %+v, want the open error and not the stale notice", rt.resolved)
		}
	})
}
