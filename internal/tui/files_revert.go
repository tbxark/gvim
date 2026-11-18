package tui

import (
	"errors"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// diskPoller is the optional disk check of the editor. The poll asks it for the check
// on the update loop and runs the check in a command. An editor without it is not polled.
type diskPoller interface {
	PlanDiskCheck() func() []editor.DiskChange
}

// revertPollMsg is the tick of the auto-revert poll.
type revertPollMsg struct{}

// revertStatMsg is the result of a disk check that a poll tick started.
type revertStatMsg struct {
	changes []editor.DiskChange
}

// revertPollCmd arms the next tick of the auto-revert poll. It returns nil when the poll
// is off (RootPorts.AutoRevertPoll is zero) or the editor cannot check the disk.
func (m *Model) revertPollCmd() tea.Cmd {
	d := m.root.ports.AutoRevertPoll
	if _, ok := m.ed.(diskPoller); !ok || d <= 0 {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return revertPollMsg{} })
}

// revertPollTick starts the disk check of the open files. The editor records what it
// expects on the update loop, and the command compares it with the disk, so Update reads
// no file. The tick is armed again when the result is applied.
func (m *Model) revertPollTick() tea.Cmd {
	p, ok := m.ed.(diskPoller)
	if !ok {
		return nil
	}
	check := p.PlanDiskCheck()
	return func() tea.Msg { return revertStatMsg{changes: check()} }
}

// revertPolled handles the result of a disk check. A file that changed since the last
// check reloads like a file the watcher reported, which covers the files whose folder no
// watcher sees. A change is acted on once: its stamp is remembered, so a buffer with
// unsaved edits is told once and not on every tick.
func (m *Model) revertPolled(msg revertStatMsg) tea.Cmd {
	var changed []string
	for _, c := range msg.changes {
		if !c.Changed {
			delete(m.fix.polled, c.Path)
			continue
		}
		if m.fix.polled[c.Path] == c.Stamp {
			continue
		}
		if m.fix.polled == nil {
			m.fix.polled = map[string]string{}
		}
		m.fix.polled[c.Path] = c.Stamp
		changed = append(changed, c.Path)
	}
	return tea.Batch(m.autoRevertChanged(changed), m.revertPollCmd())
}

// asyncReverter is the editor's revert surface, as the app adapter provides it. A revert
// is planned on the update loop, read in a command and applied in the message, as a save
// is. RunRevert reads only the plan, and ApplyRevert finds the document by the plan's path.
// An editor without the surface reverts through Do, which reads the file on the update loop.
type asyncReverter interface {
	PlanRevert(force bool) (editor.RevertPlan, error)
	RunRevert(plan editor.RevertPlan) editor.RevertResult
	ApplyRevert(plan editor.RevertPlan, res editor.RevertResult) error
}

// pathReverter is the optional part of the revert surface that plans the reload of a tab
// that is not the active one. Its plan carries the editor's settings, so a large file is
// reopened as a large file. Without it the plan holds the path alone, and the file is read
// whole.
type pathReverter interface {
	PlanRevertPath(path string) editor.RevertPlan
}

// revertTag is the tag of the Discard changes? confirm that Revert File opens.
type revertTag struct{}

// revertRetryTag is the tag of the confirm that asks again when the text changed while the
// file was read.
type revertRetryTag struct{}

// revertDoneMsg is the file that a revert read, with the plan it was read for. It is applied
// on the update loop. auto is set when auto-revert started the reload, which asks nothing.
type revertDoneMsg struct {
	plan   editor.RevertPlan
	result editor.RevertResult
	auto   bool
}

// revertFile runs Revert File. A buffer with no file has nothing to revert to, and a
// modified one asks before its changes are discarded, unless force is set.
func (m *Model) revertFile(force bool) tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	if docPath(doc) == "" {
		m.notice = editor.ErrNoPath.Error()
		return nil
	}
	if doc.Modified() && !force {
		m.pushOverlay(prompt.NewConfirm(m.styles, "Discard changes?", revertTag{}))
		return nil
	}
	return m.startRevert(force)
}

// revertAnswer runs the revert that the Discard changes? confirm answered Yes to.
func (m *Model) revertAnswer(d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Yes {
		return nil
	}
	return m.startRevert(true)
}

// startRevert plans the revert of the active buffer and returns the command that reads
// the file. The plan's refusal is the notice. An editor without the revert surface runs
// the revert action, and the editor's refusal is the notice.
func (m *Model) startRevert(force bool) tea.Cmd {
	cmd, _ := m.startRevertErr(force)
	return cmd
}

// startRevertErr is startRevert that also returns the refusal, which the plugin host
// reports instead of guessing it from the notice row.
func (m *Model) startRevertErr(force bool) (tea.Cmd, error) {
	as, ok := m.ed.(asyncReverter)
	if !ok {
		res := m.editorDo("revert", map[string]any{"force": force})
		m.syncViewport()
		if res.Err != nil {
			m.notice = res.Err.Error()
		}
		return nil, res.Err
	}
	plan, err := as.PlanRevert(force)
	if err != nil {
		m.notice = err.Error()
		return nil, err
	}
	return readRevert(as, plan, false), nil
}

// readRevert returns the command that reads the file of plan. The read runs in the command,
// and the result is applied when its message reaches Update.
func readRevert(as asyncReverter, plan editor.RevertPlan, auto bool) tea.Cmd {
	plan.Auto = auto
	return func() tea.Msg {
		return revertDoneMsg{plan: plan, result: as.RunRevert(plan), auto: auto}
	}
}

// revertDone applies the file that a revert read. A read that failed is the notice, and
// the buffer keeps its text. A text that changed after the plan asks again, since the file
// read may no longer match the buffer. An auto-revert does not ask: its notice says why.
func (m *Model) revertDone(msg revertDoneMsg) tea.Cmd {
	_ = m.revertDoneErr(msg)
	return nil
}

// revertDoneErr is revertDone that also returns why the buffer was not reverted.
func (m *Model) revertDoneErr(msg revertDoneMsg) error {
	as, ok := m.ed.(asyncReverter)
	if !ok {
		return msg.result.Err
	}
	if msg.result.Err != nil {
		m.notice = msg.result.Err.Error()
		return msg.result.Err
	}
	err := as.ApplyRevert(msg.plan, msg.result)
	m.syncViewport()
	if err == nil {
		m.plugReverted(msg.plan.Path)
	}
	switch {
	case errors.Is(err, editor.ErrStaleRevert) && !msg.auto:
		m.pushOverlay(prompt.NewConfirm(m.styles, "Text changed during the revert. Try again?", revertRetryTag{}))
	case err != nil:
		m.notice = err.Error()
	}
	return err
}

// revertRetryAnswer runs the revert again when the retry confirm answered Yes. It goes
// through revertFile, so a buffer the user has changed since asks before it discards.
func (m *Model) revertRetryAnswer(d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Yes {
		return nil
	}
	return m.revertFile(false)
}

// autoRevertOn reports whether auto-revert follows the file at path. Every file has it
// on, as in Fresh, until Toggle Auto-Revert switches it off.
func (m *Model) autoRevertOn(path string) bool {
	on, set := m.fix.autoRevert[path]
	return !set || on
}

// toggleAutoRevert turns auto-revert on or off for the active buffer's file. The choice
// follows the path, so it stays with the file while the file is open.
func (m *Model) toggleAutoRevert() tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	p := docPath(doc)
	if p == "" {
		m.notice = editor.ErrNoPath.Error()
		return nil
	}
	name := filepath.Base(p)
	if m.fix.autoRevert == nil {
		m.fix.autoRevert = map[string]bool{}
	}
	on := !m.autoRevertOn(p)
	m.fix.autoRevert[p] = on
	m.notice = "Auto-revert off for " + name
	if on {
		m.notice = "Auto-revert on for " + name
	}
	return nil
}

// tabFile is an open document that has a file, as auto-revert sees it.
type tabFile struct {
	path     string
	modified bool
	active   bool
	// image marks a tab that shows an image. Images take no part in automatic
	// reloads, watcher registration or the disk poll: closing and reopening the tab
	// is what reads the file again.
	image bool
}

// openTabFiles returns the open documents that have a file: the active one first, then the
// background tabs in tab order. An untitled tab has no file and is left out.
func (m *Model) openTabFiles() []tabFile {
	var out []tabFile
	if doc := m.ed.Active(); doc != nil {
		if p := docPath(doc); p != "" {
			out = append(out, tabFile{path: p, modified: doc.Modified(), active: true, image: isImageDoc(doc)})
		}
	}
	for i, t := range m.ed.Tabs() {
		if !t.Active && t.Path != "" {
			image := false
			if doc := m.docAt(i); doc != nil {
				image = isImageDoc(doc)
			}
			out = append(out, tabFile{path: t.Path, modified: t.Modified, image: image})
		}
	}
	return out
}

// autoRevertChanged reloads each document whose own file the watcher reports as
// changed, whether it is the active tab or a background tab, unless auto-revert is off for it. A folder that changed without
// its file reloads nothing. A document with unsaved edits keeps its text, and the notice says
// so. It returns the commands that read the files.
func (m *Model) autoRevertChanged(files []string) tea.Cmd {
	if len(files) == 0 {
		return nil
	}
	changed := make(map[string]bool, len(files))
	for _, f := range files {
		changed[filepath.Clean(f)] = true
	}
	var cmds []tea.Cmd
	for _, f := range m.openTabFiles() {
		if !m.autoRevertOn(f.path) || !changed[filepath.Clean(f.path)] {
			continue
		}
		if f.image {
			// An open image is not reloaded under the viewer: closing and reopening is
			// what reads the file again.
			continue
		}
		if f.modified {
			m.notice = filepath.Base(f.path) + " changed on disk; unsaved changes kept"
			continue
		}
		cmds = append(cmds, m.autoReload(f))
	}
	return tea.Batch(cmds...)
}

// autoReload returns the command that reads the file of a clean document. The
// active tab is planned by the editor's PlanRevert, as Revert File plans it. PlanRevert plans
// only the active tab, so a background tab's plan is built from its path, and its apply finds
// the tab by that path. An editor without the revert split reverts its active tab through the
// revert action.
func (m *Model) autoReload(f tabFile) tea.Cmd {
	as, ok := m.ed.(asyncReverter)
	switch {
	case f.active && !ok:
		return m.startRevert(false)
	case !ok:
		return nil
	case f.active:
		plan, err := as.PlanRevert(false)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		return readRevert(as, plan, true)
	}
	plan := editor.RevertPlan{Path: f.path}
	if pp, ok := m.ed.(pathReverter); ok {
		plan = pp.PlanRevertPath(f.path)
	}
	return readRevert(as, plan, true)
}
