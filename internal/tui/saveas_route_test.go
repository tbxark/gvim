package tui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// untitledDoc is a fake buffer that can be untitled. Its name follows the
// editor's rule: a buffer with no path is named Untitled-N.
type untitledDoc struct {
	fakeDoc
	name string
	path string
}

func (d *untitledDoc) Name() string     { return d.name }
func (d *untitledDoc) FilePath() string { return d.path }

// docsEditor is a fake editor with several buffers. It starts with one untouched
// buffer with no name, as the app does. "new" opens Untitled-N, and replaces the
// untouched startup buffer when it is the only one. insert_char types into the
// active buffer, "save" fails with ErrNoPath for an untitled buffer, and
// "save_as" names the buffer after the path. Other actions are recorded only.
type docsEditor struct {
	docs    []*untitledDoc
	active  int
	seq     int
	actions []editor.Action
}

func newDocsEditor() *docsEditor {
	return &docsEditor{docs: []*untitledDoc{{
		lines: []string{""},
		name:  "[No Name]",
	}}}
}

func (e *docsEditor) cur() *untitledDoc { return e.docs[e.active] }

func (e *docsEditor) SetViewport(int, int) {}

func (e *docsEditor) Do(a editor.Action, args map[string]any) editor.Result {
	e.actions = append(e.actions, a)
	switch a {
	case "new":
		e.seq++
		d := &untitledDoc{
			lines: []string{""},
			name:  fmt.Sprintf("Untitled-%d", e.seq),
		}
		if len(e.docs) == 1 && e.docs[0].name == "[No Name]" {
			e.docs[0] = d
		} else {
			e.docs = append(e.docs, d)
			e.active = len(e.docs) - 1
		}
	case "insert_char":
		e.cur().lines[0] += args["text"].(string)
	case "save":
		if e.cur().path == "" {
			return editor.Result{Err: editor.ErrNoPath}
		}
	case "save_as":
		p := args["path"].(string)
		e.cur().path = p
		e.cur().name = filepath.Base(p)
	}
	return editor.Result{}
}

func (e *docsEditor) Active() Document { return e.cur() }

func (e *docsEditor) Tabs() []TabInfo {
	out := make([]TabInfo, len(e.docs))
	for i, d := range e.docs {
		out[i] = TabInfo{Name: d.name, Active: i == e.active}
	}
	return out
}

func (e *docsEditor) Status() string { return "" }

func (e *docsEditor) SetCursor(p buffer.Pos, _ bool) { e.cur().cursor = p }

func (e *docsEditor) ExtendSelection(p buffer.Pos, _ editor.Unit) { e.cur().cursor = p }

func (e *docsEditor) ScrollBy(int) {}

func (e *docsEditor) ScrollTo(int) {}

// realDialogs builds the real Save As and unsaved dialogs with no file on disk,
// so the root flow runs as it does in the app. The clock is the test's own, so
// the test decides when the grace period is over. paths records the prefill of
// each Save As prompt.
type realDialogs struct {
	clock func() time.Time
	paths []string
}

func (d *realDialogs) Unsaved(name string, tag any, ov style.Overlay) Overlay {
	return dialog.NewUnsavedChanges(d.clock, name, tag, ov)
}

func (d *realDialogs) SaveAs(path string, tag any, ov style.Overlay) Overlay {
	d.paths = append(d.paths, path)
	return dialog.NewSaveAs(d.clock, path, tag, dialog.PathChecks{}, ov)
}

// TestSaveAsReplacesPrefillAfterTwoUntitledBuffers types a name into the Save
// As prompt of the second untitled buffer. The untouched prefill must be
// replaced, not appended to.
func TestSaveAsReplacesPrefillAfterTwoUntitledBuffers(t *testing.T) {
	ed := newDocsEditor()
	now := testClock()
	clock := func() time.Time { return now }
	dialogs := &realDialogs{clock: clock}
	keys := testKeys()
	keys["normal|n|ctrl"] = Command{Action: "new"}
	m := New(Deps{
		Editor: ed, Explorer: newFakeTree(3), Dialogs: dialogs, Keys: keys,
		Theme: theme.Theme{}, Clock: clock,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	press(m, ctrl('n'))
	press(m, ctrl('n'))
	typeText(m, "hello")
	press(m, ctrl('s'))

	if len(dialogs.paths) != 1 || dialogs.paths[0] != "/ws/Untitled-2.txt" {
		t.Fatalf("Save As prefill = %q, want [/ws/Untitled-2.txt]", dialogs.paths)
	}

	// The grace period of the dialog is over by the time a user types a name.
	now = now.Add(time.Second)
	typeText(m, "new.txt")
	drain(t, m, press(m, special(tea.KeyEnter)))

	if got := ed.docs[1].path; got != "new.txt" {
		t.Fatalf("saved path = %q, want new.txt", got)
	}
	if got := ed.docs[1].name; got != "new.txt" {
		t.Fatalf("tab name = %q, want new.txt", got)
	}
	if got := ed.docs[1].lines[0]; got != "hello" {
		t.Fatalf("buffer text = %q, want hello", got)
	}
}
