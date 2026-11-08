package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/theme"
)

// autoSaveEditor is a fake editor with the auto-save surface. due is what
// AutoSaveDue answers.
type autoSaveEditor struct {
	*fakeEditor
	due bool
}

func (e *autoSaveEditor) AutoSaveDue(time.Time) bool { return e.due }

// asyncEditor is a fake editor whose auto-save is backed by a real editor, so the
// plan, the write and the apply run as they do in the app. plans and applies count
// the calls that reach the editor.
type asyncEditor struct {
	*fakeEditor
	ed      *editor.Editor
	due     bool
	plans   int
	applies int
}

func (e *asyncEditor) AutoSaveDue(time.Time) bool { return e.due }

func (e *asyncEditor) PlanAutoSave(now time.Time) []editor.SaveJob {
	e.plans++
	return e.ed.PlanAutoSave(now)
}

func (e *asyncEditor) ApplyAutoSave(results []editor.SaveResult) error {
	e.applies++
	return e.ed.ApplySaves(results)
}

// newAutoSaveModel returns a model over ed, with the auto-save interval set to
// interval. Zero means auto-save is off.
func newAutoSaveModel(t *testing.T, interval time.Duration, ed EditorState) *Model {
	t.Helper()
	m := New(Deps{
		Editor: ed,
		Keys:   testKeys(),
		Theme:  theme.Theme{},
		Clock:  testClock,
		Root:   RootPorts{AutoSave: interval},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

// newAsyncAutoSaveModel returns a model over a real editor that has the file at path
// open with one typed character, and whose auto-save is due when due is set.
func newAsyncAutoSaveModel(t *testing.T, due bool, path string) (*Model, *asyncEditor) {
	t.Helper()
	ed := editor.New(editor.Options{AutoSaveInterval: time.Second})
	must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	ae := &asyncEditor{fakeEditor: newFakeEditor("x"), ed: ed, due: due}
	return newAutoSaveModel(t, time.Second, ae), ae
}

// writeAutoSaveFile writes content to a new file and returns its path.
func writeAutoSaveFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.txt")
	must(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// TestAutoSaveTickStartsOnlyWhenTheIntervalIsSet checks that the tick is armed at
// start when auto-save is on, and that Init sends nothing when it is off.
func TestAutoSaveTickStartsOnlyWhenTheIntervalIsSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		interval time.Duration
		armed    bool
	}{
		{"off", 0, false},
		{"on", 30 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := &autoSaveEditor{fakeEditor: newFakeEditor("x")}
			m := newAutoSaveModel(t, tt.interval, ed)
			if got := m.Init() != nil; got != tt.armed {
				t.Fatalf("Init armed the tick = %v, want %v", got, tt.armed)
			}
		})
	}
}

// TestAutoSaveTickPlansButWritesNothing checks that a due tick plans the save on the
// update loop and returns the write as a command. The file is not touched until
// that command runs.
func TestAutoSaveTickPlansButWritesNothing(t *testing.T) {
	t.Parallel()
	path := writeAutoSaveFile(t, "abc\n")
	m, ae := newAsyncAutoSaveModel(t, true, path)
	_, cmd := m.Update(autoSaveTickMsg{})
	if ae.plans != 1 {
		t.Fatalf("plans = %d, want 1", ae.plans)
	}
	if cmd == nil {
		t.Fatal("the tick returned no command for the write")
	}
	if got, _ := os.ReadFile(path); string(got) != "abc\n" {
		t.Fatalf("file after the tick = %q, want the old text before the command runs", got)
	}
	if ae.applies != 0 {
		t.Fatalf("applies = %d before the write, want 0", ae.applies)
	}
}

// TestAutoSaveWriteResultAppliesAndArmsTheNextTick checks that running the write
// command writes the file, and that its result is applied on the update loop, which
// marks the document saved and arms the next tick.
func TestAutoSaveWriteResultAppliesAndArmsTheNextTick(t *testing.T) {
	t.Parallel()
	path := writeAutoSaveFile(t, "abc\n")
	m, ae := newAsyncAutoSaveModel(t, true, path)
	_, cmd := m.Update(autoSaveTickMsg{})
	msg := cmd()
	done, ok := msg.(autoSaveDoneMsg)
	if !ok {
		t.Fatalf("write command returned %T, want autoSaveDoneMsg", msg)
	}
	if got, _ := os.ReadFile(path); string(got) != "xabc\n" {
		t.Fatalf("file after the write = %q, want the typed text", got)
	}
	_, next := m.Update(done)
	if ae.applies != 1 {
		t.Fatalf("applies = %d, want 1", ae.applies)
	}
	if ae.ed.Active().Modified() {
		t.Fatal("the document is still modified after its save was applied")
	}
	if next == nil {
		t.Fatal("the next tick was not armed after the write")
	}
}

// TestAutoSaveTickNotDueOnlyArmsTheNextTick checks that a tick before the editor says
// a save is due plans nothing and arms the next tick.
func TestAutoSaveTickNotDueOnlyArmsTheNextTick(t *testing.T) {
	t.Parallel()
	path := writeAutoSaveFile(t, "abc\n")
	m, ae := newAsyncAutoSaveModel(t, false, path)
	_, cmd := m.Update(autoSaveTickMsg{})
	if ae.plans != 0 {
		t.Fatalf("plans = %d while not due, want 0", ae.plans)
	}
	if cmd == nil {
		t.Fatal("the tick was not armed again")
	}
	if got, _ := os.ReadFile(path); string(got) != "abc\n" {
		t.Fatalf("file = %q, want it untouched", got)
	}
}

// TestAutoSaveFailureShowsInTheNoticeRow checks that a write that the editor refuses
// shows its error in the notice row when its result is applied, and that the next
// tick is still armed.
func TestAutoSaveFailureShowsInTheNoticeRow(t *testing.T) {
	t.Parallel()
	path := writeAutoSaveFile(t, "abc\n")
	m, _ := newAsyncAutoSaveModel(t, true, path)
	_, cmd := m.Update(autoSaveTickMsg{})
	must(t, os.WriteFile(path, []byte("other\n"), 0o644))
	done, ok := cmd().(autoSaveDoneMsg)
	if !ok {
		t.Fatal("write command did not return autoSaveDoneMsg")
	}
	_, next := m.Update(done)
	if got := m.statusView().message; got == "" || !strings.Contains(got, "changed on disk") {
		t.Fatalf("status = %q, want the auto-save error about the changed file", got)
	}
	if next == nil {
		t.Fatal("a failed save stopped the tick")
	}
}

// refusingSaveEditor is a tabFake whose save is refused with err, as the app's editor
// adapter refuses it while an auto-save writes. Its active document has a path, so the
// unsaved-changes flow goes on to save it.
type refusingSaveEditor struct {
	*tabFake
	err error
}

func (e refusingSaveEditor) Do(a editor.Action, args map[string]any) editor.Result {
	if a == "save" {
		return editor.Result{Err: e.err}
	}
	return e.tabFake.Do(a, args)
}

// TestSaveRefusedWhileAutoSaveWritesShowsInTheStatusRow checks that a save the editor
// refuses while an auto-save writes says so in the status row, from Ctrl+S and from the
// unsaved-changes dialog. Without it the user sees nothing and cannot tell why the file
// did not save.
func TestSaveRefusedWhileAutoSaveWritesShowsInTheStatusRow(t *testing.T) {
	t.Parallel()
	const refusal = "saving in progress, try again"
	tests := []struct {
		name string
		act  func(m *Model)
	}{
		{name: "Ctrl+S", act: func(m *Model) { press(m, ctrl('s')) }},
		{name: "unsaved changes dialog answered with Save", act: func(m *Model) {
			m.unsavedAnswer(flow{}, dialog.ChoiceSave)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := New(Deps{
				Editor:   refusingSaveEditor{tabFake: newTabFake("a.txt"), err: errors.New(refusal)},
				Explorer: newFakeTree(3),
				Keys:     testKeys(),
				Theme:    theme.Theme{},
				Clock:    testClock,
			})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			tt.act(m)
			if got := m.statusView().message; got != refusal {
				t.Fatalf("status = %q, want %q", got, refusal)
			}
		})
	}
}

// TestAutoSaveTickWithoutAnEditorPortIsOnlyArmed checks that a tick with no
// auto-save editor does nothing but arm the next tick.
func TestAutoSaveTickWithoutAnEditorPortIsOnlyArmed(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: newFakeEditor("x"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{AutoSave: time.Second}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	_, cmd := m.Update(autoSaveTickMsg{})
	if cmd == nil {
		t.Fatal("the tick was not armed again")
	}
	if got := m.statusView().message; got != "" {
		t.Fatalf("status = %q, want none", got)
	}
}
