package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// forceSaver is the optional forced save of the editor. A save that finds the file
// changed on disk asks whether to overwrite it, and Yes plans the save with
// PlanSaveForce. An editor without it only shows the refusal.
type forceSaver interface {
	PlanSaveForce() (editor.SavePlan, error)
}

// overwriteSaveTag is the tag of the Overwrite question. f is the flow the save
// belongs to, which goes on once the file is written.
type overwriteSaveTag struct {
	f flow
}

// askOverwriteSave asks whether the active document may be saved over the file that
// changed on disk. It reports false, and asks nothing, when the editor cannot force a
// save. The refusal stays in the notice row, so No or Esc leave it showing.
func (m *Model) askOverwriteSave(err error, f flow) bool {
	if _, ok := m.ed.(forceSaver); !ok {
		return false
	}
	m.notice = err.Error()
	q := "File changed on disk. Overwrite it?"
	m.pushOverlay(prompt.NewConfirm(m.styles, q, overwriteSaveTag{f: f}))
	return true
}

// overwriteAnswer applies the answer to the Overwrite question: Yes writes the buffer
// over the file and goes on with the flow.
func (m *Model) overwriteAnswer(tag overwriteSaveTag, d prompt.Done) tea.Cmd {
	fs, ok := m.ed.(forceSaver)
	as, ok2 := m.ed.(asyncSaver)
	if d.Outcome != prompt.Yes || !ok || !ok2 {
		return nil
	}
	m.notice = ""
	p, err := fs.PlanSaveForce()
	return m.planned(as, p, err, tag.f, false, false)
}
