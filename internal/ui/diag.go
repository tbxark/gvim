package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// eventRingSize is the number of input events event_debug keeps.
const eventRingSize = 20

// eventRing keeps the last eventRingSize input events, as text, oldest first.
type eventRing struct {
	items []string
}

// add appends one event. The oldest event is dropped when the ring is full.
func (r *eventRing) add(s string) {
	if len(r.items) == eventRingSize {
		copy(r.items, r.items[1:])
		r.items = r.items[:eventRingSize-1]
	}
	r.items = append(r.items, s)
}

// newest returns the events, newest first.
func (r *eventRing) newest() []string {
	out := make([]string, len(r.items))
	for i, s := range r.items {
		out[len(r.items)-1-i] = s
	}
	return out
}

// recordEvent adds a key press, or a mouse press, release or wheel notch, to the
// ring. Mouse motion is left out: the terminal reports every cell the pointer
// crosses, and that would push the useful events out of the ring.
func (u *utilities) recordEvent(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		u.events.add("key " + tea.Key(msg).Keystroke())
	case tea.MouseClickMsg:
		u.events.add("mouse click " + describeMouse(tea.Mouse(msg)))
	case tea.MouseReleaseMsg:
		u.events.add("mouse release " + describeMouse(tea.Mouse(msg)))
	case tea.MouseWheelMsg:
		u.events.add("mouse wheel " + describeMouse(tea.Mouse(msg)))
	}
}

// describeMouse is the button, with its modifiers, and the cell of a mouse event.
func describeMouse(m tea.Mouse) string {
	return fmt.Sprintf("%s %d,%d", m.String(), m.X, m.Y)
}

// calibrateState is the last key press the calibration view received.
type calibrateState struct {
	key  tea.Key
	seen bool
}

// openCalibrate shows the last key press as the terminal reported it: the key name,
// the modifiers and the text. Every key press is shown except Esc, which closes the
// view.
func (m *Model) openCalibrate() {
	st := &calibrateState{}
	v := &boxView{active: true, chrome: m.listChrome()}
	v.content = func() (string, []string, string) {
		if !st.seen {
			return "Keyboard calibration", []string{"Press a key to see how the terminal reports it."}, "Esc closes"
		}
		name, mods, text := keyLabel(st.key)
		return "Keyboard calibration", []string{
			"Key: " + name,
			"Modifiers: " + mods,
			"Text: " + text,
		}, "Esc closes"
	}
	v.onKey = func(k tea.Key) bool {
		st.key, st.seen = k, true
		return false
	}
	m.pushOverlay(boxOverlay{v: v})
}

// openEventDebug shows the last input events, newest first. The view reads the ring
// as it is drawn, so events that arrive while it is open appear too.
func (m *Model) openEventDebug() {
	events := &m.util.events
	v := &boxView{active: true, chrome: m.listChrome()}
	v.content = func() (string, []string, string) {
		lines := events.newest()
		if len(lines) == 0 {
			lines = []string{"No key or mouse events yet."}
		}
		return "Event debug: last 20 events, newest first", lines, "Esc closes"
	}
	m.pushOverlay(boxOverlay{v: v})
}
