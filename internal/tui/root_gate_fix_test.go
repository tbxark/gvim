package tui

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/editor"
)

// TestExtractTabToNewWorkspaceSaysItCannotMove checks that the palette command answers
// with a notice: a workspace is a project root, so the tab cannot move to a new window.
func TestExtractTabToNewWorkspaceSaysItCannotMove(t *testing.T) {
	t.Parallel()
	m := newRevertModel(t, newDocEditor("/ws/a.txt", false))
	cmd := rootPaletteRun(m, "extract_tab_to_new_workspace")
	if cmd != nil {
		t.Fatal("the extract command started a command, want none")
	}
	const want = "Extract Tab to New Workspace is not available: a workspace is a project root, " +
		"so a tab cannot move to a new window"
	if m.notice != want {
		t.Fatalf("notice = %q, want %q", m.notice, want)
	}
}

// TestToggleAutoRevertFlagsTheCurrentBuffer checks the flag on the current buffer: auto-revert
// is on until a toggle turns it off, the next toggle turns it on again, and a buffer with no
// file has none.
func TestToggleAutoRevertFlagsTheCurrentBuffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ed      EditorState
		toggles int
		notice  string
		on      bool
	}{
		{name: "no tab", ed: docEditor{fakeEditor: newFakeEditor("x")}, toggles: 1, notice: noDocumentNotice, on: true},
		{name: "untitled buffer", ed: newDocEditor("", false), toggles: 1, notice: editor.ErrNoPath.Error(), on: true},
		{name: "on before any toggle", ed: newDocEditor("/ws/a.txt", false), on: true},
		{name: "first toggle turns it off", ed: newDocEditor("/ws/a.txt", false), toggles: 1, notice: "Auto-revert off for a.txt"},
		{name: "second toggle turns it on", ed: newDocEditor("/ws/a.txt", false), toggles: 2, notice: "Auto-revert on for a.txt", on: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newRevertModel(t, tt.ed)
			for range tt.toggles {
				rootPaletteRun(m, "toggle_auto_revert")
			}
			if m.notice != tt.notice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.notice)
			}
			if on := m.autoRevertOn("/ws/a.txt"); on != tt.on {
				t.Fatalf("auto-revert flag = %v, want %v", on, tt.on)
			}
		})
	}
}

// TestAutoRevertReloadsABufferWhenItsFileChanges checks that the watcher's report of a
// changed file reloads the clean buffer through the revert split, as every buffer has
// auto-revert on until it is switched off. A modified buffer, one switched off, a sibling file
// in the same folder and a folder that changed without the file are left alone.
func TestAutoRevertReloadsABufferWhenItsFileChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		off       bool // auto-revert is switched off for the buffer
		dirty     bool
		dirs      []string
		files     []string
		wantPlans int // the PlanRevert calls
		notice    string
	}{
		{name: "clean, file changed", dirs: []string{"/ws"}, files: []string{"/ws/a.txt"}, wantPlans: 1},
		{name: "clean, a sibling file changed", dirs: []string{"/ws"}, files: []string{"/ws/b.txt"}},
		{name: "clean, only the folder changed", dirs: []string{"/ws"}},
		{name: "modified, file changed", dirty: true, dirs: []string{"/ws"}, files: []string{"/ws/a.txt"}, notice: "a.txt changed on disk; unsaved changes kept"},
		{name: "switched off, file changed", off: true, dirs: []string{"/ws"}, files: []string{"/ws/a.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := &revertEditor{docEditor: newDocEditor("/ws/a.txt", tt.dirty)}
			m := newRevertModel(t, ed)
			if tt.off {
				rootPaletteRun(m, "toggle_auto_revert")
			}
			m.Update(watchMsg{dirs: tt.dirs, files: tt.files})
			if ed.planned != tt.wantPlans {
				t.Fatalf("PlanRevert calls = %d, want %d", ed.planned, tt.wantPlans)
			}
			if tt.notice != "" && m.notice != tt.notice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.notice)
			}
		})
	}
}

// autoTabsEditor is a revert editor with several open documents. Like the editor, its PlanRevert
// plans the active document only, and RunRevert and ApplyRevert work on the path of the plan.
type autoTabsEditor struct {
	*fakeEditor
	tabs    []revertDoc
	active  int
	reads   []string // the paths that RunRevert read, in order
	applied []string // the paths that ApplyRevert applied, in order
}

func newAutoTabsEditor(active int, tabs ...revertDoc) *autoTabsEditor {
	return &autoTabsEditor{fakeEditor: newFakeEditor("x"), tabs: tabs, active: active}
}

// openDoc returns an open document at path. dirty is true when it has unsaved edits.
func openDoc(path string, dirty bool) revertDoc {
	return revertDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: path, dirty: dirty}
}

func (e *autoTabsEditor) Active() Document { return &e.tabs[e.active] }

func (e *autoTabsEditor) Tabs() []TabInfo {
	out := make([]TabInfo, len(e.tabs))
	for i, d := range e.tabs {
		out[i] = TabInfo{Name: filepath.Base(d.path), Path: d.path, Modified: d.dirty, Active: i == e.active}
	}
	return out
}

func (e *autoTabsEditor) PlanRevert(force bool) (editor.RevertPlan, error) {
	d := e.tabs[e.active]
	return editor.RevertPlan{Path: d.path, Force: force, Modified: d.dirty}, nil
}

func (e *autoTabsEditor) RunRevert(plan editor.RevertPlan) editor.RevertResult {
	e.reads = append(e.reads, plan.Path)
	return editor.RevertResult{}
}

func (e *autoTabsEditor) ApplyRevert(plan editor.RevertPlan, _ editor.RevertResult) error {
	e.applied = append(e.applied, plan.Path)
	return nil
}

// TestAutoRevertFollowsTheChangedFile checks which open documents reload when the watcher
// reports a changed file. A clean document reloads when its own file changed, whether it is
// the active tab, a background tab, or a file in a folder that is not the root. A document with
// unsaved edits keeps its text, and one switched off, a sibling file and a folder that changed
// without the file are left alone.
func TestAutoRevertFollowsTheChangedFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		active    int
		tabs      []revertDoc
		off       []string // the paths auto-revert is switched off for
		files     []string // the changed files the watcher reports
		dirs      []string // the changed folders the watcher reports
		wantReads []string // the paths read, which are also the paths applied
		notice    string
	}{
		{
			name:  "a background tab reloads when its file changes",
			tabs:  []revertDoc{openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", false)},
			files: []string{"/ws/b.txt"}, wantReads: []string{"/ws/b.txt"},
		},
		{
			name:  "the active tab reloads when its file changes",
			tabs:  []revertDoc{openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", false)},
			files: []string{"/ws/a.txt"}, wantReads: []string{"/ws/a.txt"},
		},
		{
			name:  "a file in a folder that is not the root reloads",
			tabs:  []revertDoc{openDoc("/ws/a.txt", false), openDoc("/other/b.txt", false)},
			files: []string{"/other/b.txt"}, wantReads: []string{"/other/b.txt"},
		},
		{
			name:      "two files that changed both reload, active first",
			tabs:      []revertDoc{openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", false)},
			files:     []string{"/ws/b.txt", "/ws/a.txt"},
			wantReads: []string{"/ws/a.txt", "/ws/b.txt"},
		},
		{
			name:  "a background tab switched off does not reload",
			tabs:  []revertDoc{openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", false)},
			off:   []string{"/ws/b.txt"},
			files: []string{"/ws/b.txt"},
		},
		{
			name:  "a file does not reload for a sibling that changed",
			tabs:  []revertDoc{openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", false)},
			files: []string{"/ws/c.txt"},
		},
		{
			name: "a file does not reload when only its folder changed",
			tabs: []revertDoc{openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", false)},
			dirs: []string{"/ws"},
		},
		{
			name:   "a background tab with unsaved edits is kept",
			tabs:   []revertDoc{openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", true)},
			files:  []string{"/ws/b.txt"},
			notice: "b.txt changed on disk; unsaved changes kept",
		},
		{
			name:   "the active tab with unsaved edits is kept",
			tabs:   []revertDoc{openDoc("/ws/a.txt", true), openDoc("/ws/b.txt", false)},
			files:  []string{"/ws/a.txt"},
			notice: "a.txt changed on disk; unsaved changes kept",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newAutoTabsEditor(tt.active, tt.tabs...)
			m := newRevertModel(t, ed)
			m.fix.autoRevert = make(map[string]bool)
			for _, p := range tt.off {
				m.fix.autoRevert[p] = false
			}
			_, cmd := m.Update(watchMsg{dirs: tt.dirs, files: tt.files})
			feedMessages(m, cmd)
			if !slices.Equal(ed.reads, tt.wantReads) {
				t.Fatalf("reads = %q, want %q", ed.reads, tt.wantReads)
			}
			if !slices.Equal(ed.applied, tt.wantReads) {
				t.Fatalf("applied = %q, want %q", ed.applied, tt.wantReads)
			}
			if tt.notice != "" && m.notice != tt.notice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.notice)
			}
		})
	}
}

// TestAutoRevertFallsBackToTheRevertAction checks that an editor without the revert split
// is reloaded through the revert action when its buffer's file changes.
func TestAutoRevertFallsBackToTheRevertAction(t *testing.T) {
	t.Parallel()
	ed := newDocEditor("/ws/a.txt", false)
	m := newRevertModel(t, ed)
	m.Update(watchMsg{dirs: []string{"/ws"}, files: []string{"/ws/a.txt"}})
	var reverts int
	for _, a := range ed.actions {
		if a == "revert" {
			reverts++
		}
	}
	if reverts != 1 {
		t.Fatalf("revert actions = %d, want 1 after the folder changed", reverts)
	}
}

// formatterDocEditor is a document editor that says whether its active buffer has a
// formatter, as the app adapter does.
type formatterDocEditor struct {
	docEditor
	formatter bool
}

func (f formatterDocEditor) HasFormatter() bool { return f.formatter }

// TestFormatBufferRowFollowsHasFormatter checks that the Format Buffer row of the Edit menu
// is enabled when the editor has a formatter for the active buffer, and disabled when it has
// none.
func TestFormatBufferRowFollowsHasFormatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		formatter bool
		disabled  bool
	}{
		{name: "a buffer with a formatter enables the row", formatter: true},
		{name: "a buffer with no formatter disables the row", disabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := formatterDocEditor{docEditor: newDocEditor("/ws/a.go", false), formatter: tt.formatter}
			m := newRevertModel(t, ed)
			m.openMenu("Edit")
			for _, r := range m.menu.Rows() {
				if r.Action != "format_buffer" {
					continue
				}
				if r.Disabled != tt.disabled {
					t.Fatalf("Format Buffer disabled = %v, want %v", r.Disabled, tt.disabled)
				}
				return
			}
			t.Fatal("the Edit menu has no Format Buffer row")
		})
	}
}
