package dock

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// The local foreground-process name is the second half of Fresh's terminal tab
// title: Fresh polls tcgetpgrp + /proc once a second, caches the command name per
// buffer and combines it with the OS title through combine_terminal_title. gvim
// polls the dock's local Unix sessions the same way, but only from tea.Cmds (never
// from Update) and only when the terminal transport supports it (a local Unix
// PTY; remote and Windows transports report unsupported and are never polled).

// foregroundPollInterval is Fresh's FG_POLL_INTERVAL.
const foregroundPollInterval = time.Second

// foregroundPollMsg is one foreground-process read. gen ties it to the shell that
// answered, so a late answer after a restart is dropped.
type foregroundPollMsg struct {
	gen  int
	name string
	ok   bool
}

// foregroundPollTickMsg schedules the next poll after one interval.
type foregroundPollTickMsg struct{ gen int }

// foregroundProcessNamer is the optional local-terminal capability. A fake or a
// remote transport that does not implement it is simply never polled.
type foregroundProcessNamer interface {
	ForegroundProcessSupported() bool
	ForegroundProcessName() (string, bool)
}

// foregroundPollSupported reports whether the current shell can be polled. It is a
// cheap capability check (no IO); the actual name read happens in a command.
func (p *Panel) foregroundPollSupported() bool {
	if p.term == nil || p.exited {
		return false
	}
	f, ok := p.term.(foregroundProcessNamer)
	return ok && f.ForegroundProcessSupported()
}

// foregroundPollCmd reads the foreground process name off the update loop.
func (p *Panel) foregroundPollCmd() tea.Cmd {
	t, gen := p.term, p.gen
	if t == nil {
		return nil
	}
	f, ok := t.(foregroundProcessNamer)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		name, read := f.ForegroundProcessName()
		return foregroundPollMsg{gen: gen, name: name, ok: read}
	}
}

// foregroundTickCmd schedules the next poll one interval from now.
func (p *Panel) foregroundTickCmd() tea.Cmd {
	gen := p.gen
	return tea.Tick(foregroundPollInterval, func(time.Time) tea.Msg {
		return foregroundPollTickMsg{gen: gen}
	})
}

// applyForegroundPoll stores one poll answer and schedules the next tick. A late
// answer for another shell generation, or one that arrives after the shell exited,
// is ignored and stops the chain.
func (p *Panel) applyForegroundPoll(msg foregroundPollMsg) tea.Cmd {
	if msg.gen != p.gen || !p.Running() {
		return nil
	}
	// The name is untrusted process data and was sanitized in the terminal
	// package; trim it once more and bound it to the tab-label budget.
	name := cleanTabTitle(msg.name)
	if !msg.ok {
		name = ""
	}
	if name != p.fgName {
		p.fgName = name
	}
	return p.foregroundTickCmd()
}

// applyForegroundTick starts the next poll when the shell still supports it.
func (p *Panel) applyForegroundTick(msg foregroundPollTickMsg) tea.Cmd {
	if msg.gen != p.gen || !p.foregroundPollSupported() {
		return nil
	}
	return p.foregroundPollCmd()
}

// foregroundName is the cached live command name for a tab label. It is empty for
// a restored/exited tab, a remote session or an unsupported platform, so the label
// falls back to the recorded launch program and then the OSC title.
func (p *Panel) foregroundName() string {
	if p.term == nil || p.exited || !p.foregroundPollSupported() {
		return ""
	}
	return cleanTabTitle(p.fgName)
}
