package terminal

import (
	"net/url"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Line is one row of the scrollback: the cells the row had when it scrolled off
// the top of the main screen, oldest first.
type Line struct {
	Cells []Cell
}

// Text returns the characters of the line with its trailing blanks trimmed.
func (l Line) Text() string {
	var b strings.Builder
	for _, c := range l.Cells {
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return strings.TrimRight(b.String(), " ")
}

// MaxScrollbackSnapshot bounds one Scrollback call: a UI row window is far
// smaller, so a snapshot never copies the whole history.
const MaxScrollbackSnapshot = 512

// maxScrollbackMatches bounds one search: a UI can show a handful of matches, and
// the search runs under the session's lock.
const maxScrollbackMatches = 200

// Scrollback returns up to count rows of the main screen's history, oldest first,
// starting at row from, and the total number of rows the history holds. The rows
// are a copy: they do not change when the child prints more. The alternate screen
// has no history, so its total is zero. The copy is bounded to
// MaxScrollbackSnapshot rows.
func (s *Session) Scrollback(from, count int) (lines []Line, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scrollbackOff {
		return nil, 0
	}
	total = s.emu.ScrollbackLen()
	if total == 0 {
		return nil, 0
	}
	from = min(max(from, 0), total)
	count = min(max(count, 0), MaxScrollbackSnapshot)
	width := s.emu.Width()
	lines = make([]Line, 0, min(count, total-from))
	for y := from; y < total && len(lines) < count; y++ {
		cells := make([]Cell, width)
		for x := range width {
			cells[x] = cellOf(s.emu.ScrollbackCellAt(x, y))
		}
		lines = append(lines, Line{Cells: cells})
	}
	return lines, total
}

// ScrollbackLen is the number of rows in the main screen's history.
func (s *Session) ScrollbackLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scrollbackOff {
		return 0
	}
	return s.emu.ScrollbackLen()
}

// SearchScrollback looks for query in the main screen's history, case-insensitively,
// and returns the matching rows, oldest first, at most limit of them (200 when
// limit is not positive), and the number of rows searched. A row matches when its
// text, trailing blanks trimmed, contains query.
func (s *Session) SearchScrollback(query string, limit int) (matches []int, total int) {
	query = strings.ToLower(strings.TrimSpace(query))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scrollbackOff {
		return nil, 0
	}
	total = s.emu.ScrollbackLen()
	if query == "" || total == 0 {
		return nil, total
	}
	limit = min(max(limit, 1), maxScrollbackMatches)
	width := s.emu.Width()
	for y := 0; y < total && len(matches) < limit; y++ {
		var b strings.Builder
		for x := range width {
			if c := s.emu.ScrollbackCellAt(x, y); c != nil && c.Content != "" {
				b.WriteString(c.Content)
			} else {
				b.WriteByte(' ')
			}
		}
		if strings.Contains(strings.ToLower(strings.TrimRight(b.String(), " ")), query) {
			matches = append(matches, y)
		}
	}
	return matches, total
}

// WorkingDirectory returns the working directory the child reported with OSC 7,
// or "" when it has not reported one. It is the directory relative paths in the
// terminal output resolve against.
func (s *Session) WorkingDirectory() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// AltScreen reports whether the child is on the alternate screen. The alternate
// screen has no scrollback, and it is where full-screen programs live.
func (s *Session) AltScreen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.altScreen
}

// MouseReporting reports the mouse modes the child turned on: reporting when any
// of DECSET 1000/1002/1003 is on, drag when 1002 or 1003 is (motion with a button
// held), and allMotion when 1003 is (buttonless motion too).
func (s *Session) MouseReporting() (reporting, drag, allMotion bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.mouse) == 0 {
		return false, false, false
	}
	allMotion = s.mouse[ansi.ModeMouseAnyEvent]
	drag = allMotion || s.mouse[ansi.ModeMouseButtonEvent]
	return true, drag, allMotion
}

// osc7Path turns an OSC 7 payload into a local path. Children send either a plain
// absolute path or a file URL, "file://host/path", whose host is empty for the
// local machine and is dropped for paths (the terminal has no remote filesystem).
// Percent escapes are decoded. A payload that is not a path yields "".
func osc7Path(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(p, "file://"); ok {
		// "file:///a/b" leaves "/a/b"; "file://host/a/b" leaves "host/a/b".
		if !strings.HasPrefix(rest, "/") {
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				rest = rest[i:]
			} else {
				return ""
			}
		}
		p = rest
	}
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	if unescaped, err := url.PathUnescape(p); err == nil {
		p = unescaped
	}
	return p
}
