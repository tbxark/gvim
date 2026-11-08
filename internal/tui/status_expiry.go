package tui

import "strings"

// statusClearer is the optional part of EditorState that drops the status message of
// the active tab. A state without it keeps its messages until it replaces them.
type statusClearer interface{ ClearStatus() }

// statusFailer is the optional part of EditorState that tells whether the current
// status message reports a failure. The warnings log keeps the failures, and not the
// notes such as "Saved". A state without it logs every message.
type statusFailer interface{ StatusIsError() bool }

// expireStatus ends the messages of the status row when the user acts: the notice
// and the editor's own message. A message belongs to the action that set it, so a
// "Saved" does not stay while the buffer is edited again, and "no word at cursor"
// does not linger through later commands. The action about to run sets its own
// message after this.
func (m *Model) expireStatus() {
	m.notice = ""
	if c, ok := m.ed.(statusClearer); ok {
		c.ClearStatus()
	}
}

// noteStatus keeps the editor's status message in the warnings log when this update
// changed it and it reports a failure. The status row is cleared by the next key, and a long message is cut to
// the row, so the log holds the full text (Show Warnings). A notice is logged by the
// notice log itself, and the progress of a line index scan is not worth keeping.
func (m *Model) noteStatus(before string) {
	s := m.ed.Status()
	if s == "" || s == before || m.notice != "" || strings.HasPrefix(s, "Scanning") {
		return
	}
	if f, ok := m.ed.(statusFailer); ok && !f.StatusIsError() {
		return
	}
	m.notes.keep(s)
}
