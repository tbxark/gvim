package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// RecoveryPort writes the crash-recovery copy of the unsaved text. Plan is called on
// the update loop and reads what it needs from the editor; the function it returns does
// the disk work and runs in a command.
type RecoveryPort interface {
	Plan() func() error
}

// recoveryTickMsg is the tick of the crash-recovery copy.
type recoveryTickMsg struct{}

// recoveryDoneMsg is the end of one write of the copy. err is the failure, if any.
type recoveryDoneMsg struct {
	err error
}

// recoveryCmd arms the next tick of the crash-recovery copy. It returns nil when there
// is no port or no interval (RootPorts.RecoveryEvery).
func (m *Model) recoveryCmd() tea.Cmd {
	d := m.root.ports.RecoveryEvery
	if m.root.ports.Recovery == nil || d <= 0 {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return recoveryTickMsg{} })
}

// recoveryTick plans one write on the update loop and returns the command that does
// it. The tick is armed again when the write is done, so writes never overlap.
func (m *Model) recoveryTick() tea.Cmd {
	p := m.root.ports.Recovery
	if p == nil {
		return nil
	}
	write := p.Plan()
	return func() tea.Msg { return recoveryDoneMsg{err: write()} }
}

// recoveryDone arms the next tick. A failure is shown once, and again only after a
// write worked in between, so a full disk does not keep the notice row busy.
func (m *Model) recoveryDone(msg recoveryDoneMsg) tea.Cmd {
	switch {
	case msg.err == nil:
		m.root.recoveryFailed = false
	case !m.root.recoveryFailed:
		m.root.recoveryFailed = true
		m.notice = "crash recovery copy failed: " + msg.err.Error()
	}
	return m.recoveryCmd()
}
