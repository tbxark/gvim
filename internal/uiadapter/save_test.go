package uiadapter

import (
	"errors"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/highlight"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// drainCmd runs cmd and returns the messages it produces, looking into batches.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestCtrlSWritesTheFileOnlyWhenItsCommandRuns checks the explicit save through the real
// adapter and the root. The key press leaves the file as it was. Running the command it
// returns writes the file, and the result is applied to the document.
func TestCtrlSWritesTheFileOnlyWhenItsCommandRuns(t *testing.T) {
	apptest.IsolateConfig(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	m := tui.New(NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if got, _ := os.ReadFile(path); string(got) != "abc\n" {
		t.Fatalf("file after Ctrl+S = %q, want the old text until the command runs", got)
	}
	for _, msg := range drainCmd(cmd) {
		m.Update(msg)
	}
	if got, _ := os.ReadFile(path); string(got) != "xabc\n" {
		t.Fatalf("file after the command = %q, want the typed text", got)
	}
	if ed.Active().Modified() {
		t.Fatal("the document is still modified after its save was applied")
	}
	if got := ed.Status(); got != "Saved" {
		t.Fatalf("status = %q, want %q", got, "Saved")
	}
}

// TestExplicitSaveGateRefusesAWriteThatIsPending checks the gate of the adapter for the
// explicit saves. While one planned save writes, every other plan is refused, as Do
// refuses a save. When the write's result is applied, the next plan goes through.
func TestExplicitSaveGateRefusesAWriteThatIsPending(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan func(a *Editor, dir string) (editor.SavePlan, error)
	}{
		{name: "save", plan: func(a *Editor, _ string) (editor.SavePlan, error) { return a.PlanSave() }},
		{name: "save as", plan: func(a *Editor, dir string) (editor.SavePlan, error) {
			return a.PlanSaveAs(filepath.Join(dir, "b.txt"), false)
		}},
		{name: "save all", plan: func(a *Editor, _ string) (editor.SavePlan, error) { return a.PlanSaveAll() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "a.txt")
			apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
			ed := editor.New(editor.Options{})
			apptest.Must(t, ed.OpenFile(path))
			if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
				t.Fatal(res.Err)
			}
			a := &Editor{ed: ed, spans: highlight.New(), saves: &autoSaveGate{}}

			p, err := tt.plan(a, dir)
			if err != nil {
				t.Fatalf("first plan err = %v, want nil", err)
			}
			if _, err := tt.plan(a, dir); !errors.Is(err, errAutoSaveWriting) {
				t.Fatalf("second plan err = %v, want errAutoSaveWriting", err)
			}
			if res := a.Do("save", nil); res.Err == nil {
				t.Fatal("Do save while a plan writes went through, want it refused")
			}
			if err := a.ApplySavePlan(p, a.WriteSave(p)); err != nil {
				t.Fatalf("ApplySavePlan err = %v", err)
			}
			if _, err := a.PlanSave(); err != nil {
				t.Fatalf("plan after the apply err = %v, want nil", err)
			}
		})
	}
}
