package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/theme"
)

// saveEditor is a fake editor whose explicit saves run on a real editor, so the plan,
// the write and the apply are the real ones. writes counts the calls that write the
// files (WriteSave), so a test can see when the write happens.
type saveEditor struct {
	*fakeEditor
	ed     *editor.Editor
	writes int
}

func (e *saveEditor) PlanSave() (editor.SavePlan, error) { return e.ed.PlanSave() }

func (e *saveEditor) PlanSaveAs(path string, force bool) (editor.SavePlan, error) {
	return e.ed.PlanSaveAs(path, force)
}

func (e *saveEditor) PlanSaveAll() (editor.SavePlan, error) { return e.ed.PlanSaveAll() }

func (e *saveEditor) WriteSave(p editor.SavePlan) []editor.SaveResult {
	e.writes++
	return p.Run()
}

func (e *saveEditor) ApplySavePlan(p editor.SavePlan, results []editor.SaveResult) error {
	return e.ed.ApplySavePlan(p, results)
}

// newSaveModel returns a model over a real editor. The file at path is open, or with
// no path a new unnamed buffer is.
func newSaveModel(t *testing.T, path string, dialogs Dialogs) (*Model, *saveEditor) {
	t.Helper()
	ed := editor.New(editor.Options{})
	if path == "" {
		if res := ed.Do("new", nil); res.Err != nil {
			t.Fatal(res.Err)
		}
	} else {
		must(t, ed.OpenFile(path))
	}
	se := &saveEditor{fakeEditor: newFakeEditor("x"), ed: ed}
	m := New(Deps{
		Editor:   se,
		Explorer: newFakeTree(3),
		Dialogs:  dialogs,
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m, se
}

// saveFileText returns the text of the file at path, or "" for no path or no file.
func saveFileText(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// TestCtrlSSaveStates checks the path from Ctrl+S to the file, by the state of the
// document. The key press writes nothing: the write is the command it returns, and the
// command's result is applied on the update loop. A refused save shows its error in the
// notice row, and an untitled buffer opens the Save As prompt.
func TestCtrlSSaveStates(t *testing.T) {
	t.Parallel()
	const changed = "a.txt changed on disk since it was opened; not saved. Save As keeps your version"
	tests := []struct {
		name       string
		untitled   bool   // the buffer has no file
		edit       bool   // a character is typed before the save
		readOnly   bool   // the document is read-only
		disk       string // the file's text after the edit, when the file changed on disk
		wantWrite  bool   // Ctrl+S returns the write
		wantFile   string // the file's text after the write, or "" when not checked
		wantDirty  bool   // the document is modified after the save
		wantNotice string // the notice row after the result
		wantDialog bool   // the Save As prompt opens
	}{
		{name: "clean", wantWrite: true, wantFile: "abc\n"},
		{name: "modified", edit: true, wantWrite: true, wantFile: "xabc\n"},
		{name: "read-only", edit: true, readOnly: true, wantDirty: true, wantFile: "abc\n", wantNotice: "read-only"},
		{name: "untitled", untitled: true, edit: true, wantDirty: true, wantDialog: true},
		{name: "changed on disk", edit: true, disk: "other\n", wantWrite: true, wantDirty: true, wantFile: "other\n", wantNotice: changed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := ""
			if !tt.untitled {
				path = writeAutoSaveFile(t, "abc\n")
			}
			dialogs := &fakeDialogs{}
			m, se := newSaveModel(t, path, dialogs)
			if tt.edit {
				if res := se.ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
					t.Fatal(res.Err)
				}
			}
			if tt.readOnly {
				if res := se.ed.Do("toggle_read_only", nil); res.Err != nil {
					t.Fatal(res.Err)
				}
			}
			if tt.disk != "" {
				must(t, os.WriteFile(path, []byte(tt.disk), 0o644))
			}
			before := saveFileText(path)

			cmd := press(m, ctrl('s'))
			if se.writes != 0 {
				t.Fatalf("writes during Ctrl+S = %d, want 0 before the command runs", se.writes)
			}
			if got := saveFileText(path); got != before {
				t.Fatalf("file during Ctrl+S = %q, want %q", got, before)
			}
			if tt.wantWrite && cmd == nil {
				t.Fatal("Ctrl+S returned no command for the write")
			}
			for _, msg := range runCmd(cmd) {
				m.Update(msg)
			}
			if tt.wantWrite && se.writes != 1 {
				t.Fatalf("writes after the command = %d, want 1", se.writes)
			}
			if tt.wantFile != "" {
				if got := saveFileText(path); got != tt.wantFile {
					t.Fatalf("file after the save = %q, want %q", got, tt.wantFile)
				}
			}
			if got := se.ed.Active().Modified(); got != tt.wantDirty {
				t.Fatalf("modified after the save = %v, want %v", got, tt.wantDirty)
			}
			if m.notice != tt.wantNotice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.wantNotice)
			}
			if got := len(dialogs.opened) > 0; got != tt.wantDialog {
				t.Fatalf("Save As prompt opened = %v, want %v", got, tt.wantDialog)
			}
		})
	}
}

// TestSaveAllWritesOffTheUpdateLoop checks that Save All plans its writes on the update
// loop and writes them in its command. The summary is the status of the applied result.
func TestSaveAllWritesOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	path := writeAutoSaveFile(t, "abc\n")
	m, se := newSaveModel(t, path, &fakeDialogs{})
	if res := se.ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}

	cmd := m.saveAll()
	if se.writes != 0 || saveFileText(path) != "abc\n" {
		t.Fatalf("Save All wrote during Update: writes %d, file %q", se.writes, saveFileText(path))
	}
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
	if se.writes != 1 {
		t.Fatalf("writes after the command = %d, want 1", se.writes)
	}
	if got := saveFileText(path); got != "xabc\n" {
		t.Fatalf("file after Save All = %q, want the typed text", got)
	}
	if got := se.ed.Status(); got != "Saved 1 file" {
		t.Fatalf("status = %q, want %q", got, "Saved 1 file")
	}
}

// TestSaveAsWritesOffTheUpdateLoop checks that Save As writes the new file in its
// command, and that the document takes the new path once the result is applied.
func TestSaveAsWritesOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	path := writeAutoSaveFile(t, "abc\n")
	target := filepath.Join(filepath.Dir(path), "new.txt")
	m, se := newSaveModel(t, path, &fakeDialogs{})
	if res := se.ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}

	cmd := m.saveAs(target, false, flow{})
	if se.writes != 0 || saveFileText(target) != "" {
		t.Fatalf("Save As wrote during Update: writes %d, target %q", se.writes, saveFileText(target))
	}
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
	if se.writes != 1 {
		t.Fatalf("writes after the command = %d, want 1", se.writes)
	}
	if got := saveFileText(target); got != "xabc\n" {
		t.Fatalf("target after Save As = %q, want the typed text", got)
	}
	if got := se.ed.Active().Path; got != target {
		t.Fatalf("document path = %q, want %q", got, target)
	}
}

// saveTabEditor is a tab fake whose saves run on a real editor, as the app's do. The
// write records how many tabs were closed when it ran, so a test can see that a tab
// closes only after its save.
type saveTabEditor struct {
	*tabFake
	ed            *editor.Editor
	writes        int
	closedAtWrite int
}

func (e *saveTabEditor) PlanSave() (editor.SavePlan, error) { return e.ed.PlanSave() }

func (e *saveTabEditor) PlanSaveAs(path string, force bool) (editor.SavePlan, error) {
	return e.ed.PlanSaveAs(path, force)
}

func (e *saveTabEditor) PlanSaveAll() (editor.SavePlan, error) { return e.ed.PlanSaveAll() }

func (e *saveTabEditor) WriteSave(p editor.SavePlan) []editor.SaveResult {
	e.writes++
	e.closedAtWrite = len(e.closed)
	return p.Run()
}

// ApplySavePlan records the save, and the fake tab is clean once it worked.
func (e *saveTabEditor) ApplySavePlan(p editor.SavePlan, results []editor.SaveResult) error {
	err := e.ed.ApplySavePlan(p, results)
	if err == nil {
		e.dirty[e.active] = false
		e.sync()
	}
	return err
}

// TestUnsavedChangesSaveClosesTheTabAfterItsWrite checks the unsaved-changes flow of a
// close: the Save answer writes the tab, and only then does the tab close.
func TestUnsavedChangesSaveClosesTheTabAfterItsWrite(t *testing.T) {
	t.Parallel()
	path := writeAutoSaveFile(t, "abc\n")
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	tf := newTabFake("doc.txt")
	tf.dirty[0] = true
	tf.sync()
	se := &saveTabEditor{tabFake: tf, ed: ed, closedAtWrite: -1}
	m := New(Deps{
		Editor:   se,
		Explorer: newFakeTree(3),
		Dialogs:  &fakeDialogs{},
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	cmd := m.unsavedAnswer(flow{then: nextClose, index: 0}, dialog.ChoiceSave)
	if len(tf.closed) != 0 {
		t.Fatalf("tabs closed before the save = %v, want none", tf.closed)
	}
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
	if se.writes != 1 || se.closedAtWrite != 0 {
		t.Fatalf("writes = %d with %d tabs closed at the write, want 1 write before any close", se.writes, se.closedAtWrite)
	}
	if !slices.Equal(tf.closed, []int{0}) {
		t.Fatalf("closed tabs = %v, want [0] after the save", tf.closed)
	}
	if got := saveFileText(path); got != "xabc\n" {
		t.Fatalf("file after the save = %q, want the typed text", got)
	}
}
