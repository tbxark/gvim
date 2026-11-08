package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/space"
)

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
	// sink, when set, is told of each notice kept (Deps.NoticeSink).
	sink func(text string)
}

// record keeps after when it is a notice that appeared in an update that began with
// before showing. An empty notice is never kept.
func (l *noticeLog) record(before, after string) {
	quiet := l.quiet
	l.quiet = ""
	if after == "" || after == before || after == quiet || after == l.last() {
		return
	}
	l.add(after)
}

// keep keeps text now, for a notice that is set along with others in one update (the
// plugin loader reports each failed plugin in the same drain). record keeps only the
// notice that is showing at the end of an update, so without this the earlier ones are
// lost. Like record, it does not keep a repeat of the last notice kept.
func (l *noticeLog) keep(text string) {
	if text == "" || text == l.last() {
		return
	}
	l.add(text)
}

// add keeps text, dropping the oldest notice beyond warningLimit, and tells the sink.
func (l *noticeLog) add(text string) {
	l.items = append(l.items, text)
	if n := len(l.items) - warningLimit; n > 0 {
		l.items = l.items[n:]
	}
	if l.sink != nil {
		l.sink(text)
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
	v := newListView("Warnings", rows, m.listChrome(), func(_ *listView, r listRow) tea.Cmd {
		text := r.left
		return func() tea.Msg { return warningDetailMsg{text: text} }
	})
	v.maxW, v.cut = warningsMaxWidth, warningText
	m.pushOverlay(listOverlay{v: v})
}

// warningText fits a warning to room cells for its row. A warning reads as a path, then
// clauses, and the reason is the last clause, or the first one after the path ("invalid
// JSON with comments"). The path shrinks first, to a quarter of the row, and keeps its
// base name. The last clause is kept whole when it can be, then the first gets the rest,
// and the clauses between them give way to an ellipsis. The full text is in the detail box.
func warningText(s string, room int) string {
	if ansi.StringWidth(s) <= room {
		return s
	}
	parts := strings.Split(s, ": ")
	if len(parts) < 3 {
		return space.Middle(s, room)
	}
	head := parts[0]
	if strings.Contains(head, "/") {
		head = space.Path(head, room/4)
	} else {
		head = space.End(head, room/4)
	}
	mid := ": "
	if len(parts) > 3 {
		mid = ": …: "
	}
	budget := room - ansi.StringWidth(head) - ansi.StringWidth(": "+mid)
	if budget < 8 {
		return space.Middle(s, room)
	}
	first, last := parts[1], parts[len(parts)-1]
	firstW := min(ansi.StringWidth(first), budget/4)
	lastW := min(ansi.StringWidth(last), budget-firstW)
	firstW = min(ansi.StringWidth(first), budget-lastW)
	return head + ": " + space.Middle(first, firstW) + mid + space.Middle(last, lastW)
}

// warningsMaxWidth is the widest the Warnings list is drawn: a message is often a path
// and a reason, so the list takes the width of the terminal.
const warningsMaxWidth = 240

// warningDetailMsg asks the root to show the full text of a warning in a box. The list
// sends it when a row is chosen. The box opens from Update, not from the list's own
// handler, because the overlay stack replaces the list with what the handler returns.
type warningDetailMsg struct{ text string }

// openWarningDetail shows text, wrapped, in a box over the Warnings list. Enter or Esc
// closes it.
func (m *Model) openWarningDetail(text string) {
	v := &boxView{
		active: true,
		chrome: m.listChrome(),
		content: func() (string, []string, string) {
			return "Warning", space.Wrap(text, boxTextWidth(m.width), maxDetailRows), "Enter or Esc close"
		},
		onKey: func(k tea.Key) bool { return k.Code == tea.KeyEnter },
	}
	m.pushOverlay(boxOverlay{v: v})
}

// maxDetailRows caps the rows of the warning box. The box is centred and never taller
// than the frame, and a longer text ends with "…".
const maxDetailRows = 20

// boxTextWidth is the cells a line of a text box has in a frame w cells wide: the box
// (boxMaxWidth, kept two cells inside each edge) less its border and padding.
func boxTextWidth(w int) int {
	return max(min(boxMaxWidth, w-4)-4, 1)
}

// clearWarnings drops the notices kept.
func (m *Model) clearWarnings() {
	m.notes.items = nil
	m.quietNotice("Warnings cleared")
}
