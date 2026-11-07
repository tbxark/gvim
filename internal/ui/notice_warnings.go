package ui

import "slices"

// warningLimit caps the notices that Show Warnings keeps. The oldest go first.
const warningLimit = 500

// noticeLog keeps the notices the root showed this session, oldest first. The status
// row is the root's only report to the user, so each notice that shows is a warning
// here. A notice that is still showing is kept once, and a notice that repeats the
// last one kept is not kept again, so a key that fails again and again does not fill
// the list.
type noticeLog struct {
	items []string
	// quiet is the notice a warnings command set. It is not kept, so Show Warnings
	// does not list its own answer. record clears it.
	quiet string
}

// record keeps after when it is a notice that appeared in an update that began with
// before showing. An empty notice is never kept.
func (l *noticeLog) record(before, after string) {
	quiet := l.quiet
	l.quiet = ""
	if after == "" || after == before || after == quiet || after == l.last() {
		return
	}
	l.items = append(l.items, after)
	if n := len(l.items) - warningLimit; n > 0 {
		l.items = l.items[n:]
	}
}

// keep keeps text now, for a notice that is set along with others in one update (the
// plugin loader reports each failed plugin in the same drain). record keeps only the
// notice that is showing at the end of an update, so without this the earlier ones are
// lost. Like record, it does not keep a repeat of the last notice kept.
func (l *noticeLog) keep(text string) {
	if text == "" || text == l.last() {
		return
	}
	l.items = append(l.items, text)
	if n := len(l.items) - warningLimit; n > 0 {
		l.items = l.items[n:]
	}
}

// last returns the newest notice kept, or "" when none is.
func (l *noticeLog) last() string {
	if len(l.items) == 0 {
		return ""
	}
	return l.items[len(l.items)-1]
}

// quietNotice shows text on the status row and keeps it out of the log.
func (m *Model) quietNotice(text string) {
	m.notice = text
	m.notes.quiet = text
}

// showWarnings opens the list of the notices kept, newest first. With none it says so
// on the status row and opens no list.
func (m *Model) showWarnings() {
	if len(m.notes.items) == 0 {
		m.quietNotice("No warnings this session")
		return
	}
	rows := make([]listRow, 0, len(m.notes.items))
	for _, v := range slices.Backward(m.notes.items) {
		rows = append(rows, listRow{left: v})
	}
	m.pushOverlay(listOverlay{v: newListView("Warnings", rows, m.listChrome(), nil)})
}

// clearWarnings drops the notices kept.
func (m *Model) clearWarnings() {
	m.notes.items = nil
	m.quietNotice("Warnings cleared")
}
