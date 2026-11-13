package uiadapter

import (
	"errors"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/highlight"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// TestAutoSaveAdapterWritesTheModifiedFileOnSchedule checks the editor adapter's
// auto-save methods: a save is due at once, a planned save writes the modified file, and
// the next one is due only after the interval.
func TestAutoSaveAdapterWritesTheModifiedFileOnSchedule(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{AutoSaveInterval: time.Second})
	apptest.Must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	a := &Editor{ed: ed, spans: highlight.New()}

	now := time.Now()
	if !a.AutoSaveDue(now) {
		t.Fatal("auto-save is not due before its first save")
	}
	jobs := a.PlanAutoSave(now)
	results := make([]editor.SaveResult, len(jobs))
	for i, j := range jobs {
		results[i] = j.Run()
	}
	apptest.Must(t, a.ApplyAutoSave(results))
	data, err := os.ReadFile(path)
	apptest.Must(t, err)
	if !strings.HasPrefix(string(data), "xabc") {
		t.Fatalf("file after auto-save = %q, want the typed x", data)
	}
	if a.AutoSaveDue(now) {
		t.Fatal("auto-save is due again before the interval")
	}
	if !a.AutoSaveDue(now.Add(time.Second)) {
		t.Fatal("auto-save is not due after the interval")
	}
}

// TestAutoSaveAdapterPlanAndApplyRoundTrip checks the split auto-save through the
// adapter: a plan writes nothing, the planned write is applied, and the document is
// saved afterwards.
func TestAutoSaveAdapterPlanAndApplyRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{AutoSaveInterval: time.Second})
	apptest.Must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	a := &Editor{ed: ed, spans: highlight.New(), saves: &autoSaveGate{}}

	jobs := a.PlanAutoSave(time.Now())
	if len(jobs) != 1 {
		t.Fatalf("PlanAutoSave returned %d jobs, want 1", len(jobs))
	}
	if data, _ := os.ReadFile(path); string(data) != "abc\n" {
		t.Fatalf("file after the plan = %q, want the old text", data)
	}
	results := make([]editor.SaveResult, len(jobs))
	for i, j := range jobs {
		results[i] = j.Run()
	}
	apptest.Must(t, a.ApplyAutoSave(results))
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "xabc") {
		t.Fatalf("file after the write = %q, want the typed x", data)
	}
	if ed.Active().Modified() {
		t.Fatal("the document is still modified after its save was applied")
	}
}

// TestAutoSaveGateRefusesExplicitSaveWhileWriting checks that an explicit save of a
// document is refused while a planned auto-save is writing, because the disk state
// is not yet recorded and the save would wrongly see a changed file. Once the
// result is applied the save goes through, and a second plan waits for the first.
func TestAutoSaveGateRefusesExplicitSaveWhileWriting(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{AutoSaveInterval: time.Second})
	apptest.Must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	a := &Editor{ed: ed, spans: highlight.New(), saves: &autoSaveGate{}}

	jobs := a.PlanAutoSave(time.Now())
	for _, act := range []editor.Action{"save", "save_as", "save_all"} {
		if res := a.Do(act, map[string]any{"path": path}); !errors.Is(res.Err, errAutoSaveWriting) {
			t.Fatalf("%s while writing error = %v, want errAutoSaveWriting", act, res.Err)
		}
	}
	if again := a.PlanAutoSave(time.Now()); len(again) != 0 {
		t.Fatalf("a second plan returned %d jobs while writing, want none", len(again))
	}
	results := []editor.SaveResult{jobs[0].Run()}
	apptest.Must(t, a.ApplyAutoSave(results))
	if res := a.Do("save", nil); res.Err != nil {
		t.Fatalf("save after the apply error = %v, want nil", res.Err)
	}
}

// TestSaveRefusedWhileAutoSaveWritesShowsInTheStatusRow checks the path from Ctrl+S to
// the status row of the built UI. A save refused while an auto-save writes says to try
// again, so the user knows the file is not saved yet.
func TestSaveRefusedWhileAutoSaveWritesShowsInTheStatusRow(t *testing.T) {
	apptest.IsolateConfig(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{AutoSaveInterval: time.Second})
	apptest.Must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	deps := NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil)
	if jobs := deps.Editor.(*Editor).PlanAutoSave(time.Now()); len(jobs) != 1 {
		t.Fatalf("PlanAutoSave returned %d jobs, want 1", len(jobs))
	}
	m := tui.New(deps)
	// The status row cuts a long message at 80 columns, so the frame is wider here.
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "saving in progress, try again") {
		t.Fatal("the status row does not say that the save was refused")
	}
}

// TestEmacsCtrlXCtrlSSavesWithoutCuttingTheLine checks that the first key of an Emacs
// sequence only starts it: C-x does not cut the line, and C-x C-s saves the file.
func TestEmacsCtrlXCtrlSSavesWithoutCuttingTheLine(t *testing.T) {
	apptest.IsolateConfig(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\nxyz\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "X"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	em, err := keymap.ByName("emacs")
	apptest.Must(t, err)
	m := tui.New(NewDeps(ed, em, theme.Theme{}, nil, nil))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if got := ed.Active().Buffer.Text(); got != "Xabc\nxyz\n" {
		t.Fatalf("buffer after C-x = %q, want it unchanged", got)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	drainCmd(cmd)
	data, err := os.ReadFile(path)
	apptest.Must(t, err)
	if string(data) != "Xabc\nxyz\n" {
		t.Errorf("file after C-x C-s = %q, want the edit saved", data)
	}
}
