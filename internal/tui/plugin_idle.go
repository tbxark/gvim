package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// The idle event. While some plugin subscribes to `idle`, the root keeps one
// timer going and raises idle{ms} as the user stays away from the keyboard and mouse.
//
// The contract with the runtime (api_events.go, idlePasses): every idle event carries
// "period", a number that changes with each input, and within one period the ms values
// grow. The runtime runs a handler once per period, the first time ms reaches its
// after_ms. ms alone cannot tell a new period from the old one: it is the real elapsed
// time, so a late first tick of a short pause can report more than the last ms of the
// period before. The root raises idle on a ladder, not at one fixed ms: every
// 100 ms up to 1 s, every 250 ms up to 10 s, every second up to a minute and every ten
// seconds up to idleMax, an hour (the largest after_ms). ms is the time that has really
// passed since the last input, so a late timer reports a larger value, never a smaller.
// After idleMax the root raises nothing more until the next input.

const idleMax = 3_600_000 // ms; the runtime accepts after_ms up to this

// idleNext is the ms of the idle event that follows one at ms.
func idleNext(ms int) int {
	switch {
	case ms < 1_000:
		return ms + 100
	case ms < 10_000:
		return ms + 250
	case ms < 60_000:
		return ms + 1_000
	}
	return ms + 10_000
}

// pluginIdle is the idle timer state.
type pluginIdle struct {
	last  time.Time // the latest input
	per   int       // the idle period: counts the inputs
	sent  int       // ms of the latest idle event of this period; 0 when none was raised
	gen   int       // the generation of the timer in flight
	armed bool      // a timer is in flight
	due   time.Time // when it fires
}

// pluginIdleMsg is the idle timer.
type pluginIdleMsg struct{ gen int }

// isInput reports whether msg is the user at the keyboard or the mouse.
func isInput(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseWheelMsg, tea.PasteMsg:
		return true
	}
	return false
}

// pluginIdle runs at the end of Update. An input ends the idle period and moves the timer
// up, so the first idle event comes 100 ms after the input and not when an old timer
// fires. Without a subscriber nothing is armed.
func (m *Model) pluginIdle(msg tea.Msg) tea.Cmd {
	if m.plug.rt == nil || !m.plug.started {
		return nil
	}
	id := &m.plug.keys.idle
	now := m.now()
	input := isInput(msg)
	if input || id.last.IsZero() {
		id.last, id.sent = now, 0
		id.per++
	}
	if !m.plug.rt.Wants("idle") || id.sent >= idleMax {
		return nil
	}
	first := time.Duration(idleNext(0)) * time.Millisecond
	switch {
	case !id.armed:
	case input && id.due.After(now.Add(first)):
		// The timer in flight is far off; a stale one is ignored by its generation.
	default:
		return nil
	}
	return m.idleArm(first, true)
}

// idleArm starts the timer for d. A new generation supersedes the timer in flight.
func (m *Model) idleArm(d time.Duration, newGen bool) tea.Cmd {
	id := &m.plug.keys.idle
	if newGen {
		id.gen++
	}
	id.armed = true
	id.due = m.now().Add(d)
	gen := id.gen
	return tea.Tick(d, func(time.Time) tea.Msg { return pluginIdleMsg{gen} })
}

// pluginIdleTick handles the timer: it raises the idle event that is due and arms the
// timer for the next one.
func (m *Model) pluginIdleTick(msg pluginIdleMsg) tea.Cmd {
	id := &m.plug.keys.idle
	if msg.gen != id.gen {
		return nil
	}
	id.armed = false
	if m.plug.rt == nil || !m.plug.rt.Wants("idle") || id.sent >= idleMax {
		return nil
	}
	elapsed := int(m.now().Sub(id.last) / time.Millisecond)
	next := idleNext(id.sent)
	if elapsed < next {
		return m.idleArm(max(time.Duration(next-elapsed)*time.Millisecond, 10*time.Millisecond), false)
	}
	id.sent = elapsed
	m.plugEmit("idle", map[string]any{"ms": elapsed, "period": id.per})
	if elapsed >= idleMax {
		return nil
	}
	return m.idleArm(max(time.Duration(idleNext(elapsed)-elapsed)*time.Millisecond, 10*time.Millisecond), false)
}
