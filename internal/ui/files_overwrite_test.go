package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
)

// forceSaveEditor adds the forced save to saveEditor, as the app adapter has it.
type forceSaveEditor struct{ *saveEditor }

func (e forceSaveEditor) PlanSaveForce() (editor.SavePlan, error) { return e.ed.PlanSaveForce() }

// settleAll runs cmd and feeds every message back to m, and the commands those start,
// as the program does.
func settleAll(m *Model, cmd tea.Cmd) {
	for _, msg := range runCmd(cmd) {
		_, next := m.Update(msg)
		settleAll(m, next)
	}
}

// changedOnDiskModel returns a model whose open file was edited in the buffer and then
// changed on disk, and the path of the file.
func changedOnDiskModel(t *testing.T) (*Model, *saveEditor, string) {
	t.Helper()
	path := writeAutoSaveFile(t, "abc\n")
	m, se := newSaveModel(t, path, &fakeDialogs{})
	m.ed = forceSaveEditor{se}
	if res := se.ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	must(t, os.WriteFile(path, []byte("other\n"), 0o644))
	return m, se, path
}

func TestSaveOverExternalChangeAsksToOverwrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		key        tea.KeyPressMsg
		wantFile   string
		wantDirty  bool
		wantNotice string
	}{
		{"yes overwrites", typed("y"), "xabc\n", false, ""},
		{"no keeps the disk", typed("n"), "other\n", true, "changed on disk"},
		{"esc keeps the disk", special(tea.KeyEscape), "other\n", true, "changed on disk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, se, path := changedOnDiskModel(t)
			settleAll(m, press(m, ctrl('s')))
			if len(m.overlays) != 1 {
				t.Fatalf("overlays after the refused save = %d, want the overwrite prompt", len(m.overlays))
			}
			if _, ok := m.overlays[0].(confirmOverlay); !ok {
				t.Fatalf("overlay = %T, want the confirm prompt", m.overlays[0])
			}
			if got := saveFileText(path); got != "other\n" {
				t.Fatalf("file while the prompt is open = %q, want it untouched", got)
			}
			settleAll(m, press(m, tt.key))
			if got := saveFileText(path); got != tt.wantFile {
				t.Errorf("file = %q, want %q", got, tt.wantFile)
			}
			if got := se.ed.Active().Modified(); got != tt.wantDirty {
				t.Errorf("modified = %v, want %v", got, tt.wantDirty)
			}
			if tt.wantNotice == "" && m.notice != "" || !strings.Contains(m.notice, tt.wantNotice) {
				t.Errorf("notice = %q, want it to contain %q", m.notice, tt.wantNotice)
			}
			if len(m.overlays) != 0 {
				t.Errorf("overlays left after the answer = %d", len(m.overlays))
			}
		})
	}
}

func TestSaveAllOverExternalChangeOnlyShowsTheError(t *testing.T) {
	t.Parallel()
	m, _, path := changedOnDiskModel(t)
	settleAll(m, m.saveAll())
	if len(m.overlays) != 0 {
		t.Fatalf("Save All opened %d overlays, want none", len(m.overlays))
	}
	if got := saveFileText(path); got != "other\n" {
		t.Fatalf("file = %q, want it untouched", got)
	}
}
