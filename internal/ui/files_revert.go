package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
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
