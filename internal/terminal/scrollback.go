package terminal

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Line is one row of the scrollback: the cells the row had when it scrolled off
// the top of the main screen, oldest first.
type Line struct {
	Cells []Cell
}

// Text returns the characters of the line with its trailing blanks trimmed. The
// right half of a wide character (a cell with no rune) adds nothing, so the text is
// what was printed.
func (l Line) Text() string {
	var b strings.Builder
	for _, c := range l.Cells {
		if c.Rune != 0 {
			b.WriteRune(c.Rune)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// ColumnText returns one rune per cell, a cell with no rune as a space, with the
// trailing blanks trimmed: rune i is the character in column i, for hit tests.
func (l Line) ColumnText() string {
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
	matches, total, _ = s.SearchScrollbackMode(query, false, false, limit)
	return matches, total
}

// SearchScrollbackMode is SearchScrollback with the scrollback search options: a
// regular expression query instead of a literal one, and a match that respects
// case. A literal query is matched as plain text; a regex query is compiled and
// matched against each row's text (trailing blanks trimmed). An invalid regular
// expression returns an error and no matches.
func (s *Session) SearchScrollbackMode(query string, regex, matchCase bool, limit int) (matches []int, total int, err error) {
	query = strings.TrimSpace(query)
	var re *regexp.Regexp
	if regex && query != "" {
		if !matchCase {
			query = "(?i)" + query
		}
		re, err = regexp.Compile(query)
		if err != nil {
			return nil, 0, err
		}
	} else if !matchCase {
		query = strings.ToLower(query)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scrollbackOff {
		return nil, 0, nil
	}
	total = s.emu.ScrollbackLen()
	if query == "" || total == 0 {
		return nil, total, nil
	}
	limit = min(max(limit, 1), maxScrollbackMatches)
	width := s.emu.Width()
	for y := 0; y < total && len(matches) < limit; y++ {
		var b strings.Builder
		for x := range width {
			c := s.emu.ScrollbackCellAt(x, y)
			switch {
			case c != nil && c.Width == 0:
				// The right half of a wide character: the character is already written.
			case c != nil && c.Content != "":
				b.WriteString(c.Content)
			default:
				b.WriteByte(' ')
			}
		}
		text := strings.TrimRight(b.String(), " ")
		hit := false
		if re != nil {
			hit = re.MatchString(text)
		} else {
			hit = strings.Contains(text, query)
		}
		if hit {
			matches = append(matches, y)
		}
	}
	return matches, total, nil
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

// maxWorkingDirectoryBytes bounds an OSC 7 working-directory payload. Fresh's
// Osc7Scanner abandons a payload longer than 4096 bytes; the emulator has already
// parsed ours, so an over-long or tempered path is refused here instead.
const maxWorkingDirectoryBytes = 4096

// osc7Path turns an OSC 7 payload into a working-directory path, mirroring Fresh's
// parse_osc7_path: a "file://host/path" URL drops its authority (everything up to
// the first '/'), the URI path is percent-decoded (invalid escapes stay verbatim),
// and a "/C:/..." path from a Windows file URL loses its leading slash so it reads
// as a drive-absolute Windows path. A path is accepted when it is absolute in
// POSIX OR Windows terms — leading "/", leading "\", or an ASCII letter followed
// by ':' — because the payload's flavour follows the shell's OS, not gvim's. A
// bare payload (no file://) is not percent-decoded, as in Fresh. gvim additionally
// refuses invalid UTF-8, control characters and over-long payloads
// (cleanWorkingDirectory); an empty or non-absolute payload yields "".
func osc7Path(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(p, "file://"); ok {
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			return ""
		}
		p = decodeOSC7Percent(rest[i:])
	}
	// "/C:/dir" from a file URL reads as the Windows drive path "C:/dir".
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return cleanWorkingDirectory(p)
}

// decodeOSC7Percent decodes %XX escapes in an OSC 7 URI path and leaves an invalid
// escape verbatim (Fresh's percent_decode), so a broken escape cannot lose the
// rest of the path. Bytes are decoded rather than runes, so a non-ASCII UTF-8 path
// survives; the caller later rejects invalid UTF-8 as untrusted.
func decodeOSC7Percent(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi, ok := osc7Hex(s[i+1]); ok {
				if lo, ok := osc7Hex(s[i+2]); ok {
					b.WriteByte(hi<<4 | lo)
					i += 2
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func osc7Hex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// cleanWorkingDirectory validates an OSC 7 path before it is kept or used: the
// child's output is untrusted input. It must be a non-empty absolute path (POSIX
// "/...", Windows UNC "\\..." or a drive "X:...") of at most
// maxWorkingDirectoryBytes, valid UTF-8 and free of control characters; a path
// that fails any check is ignored (no cwd) rather than repaired, so a reported
// directory can never smuggle escapes into the record or a start.
func cleanWorkingDirectory(p string) string {
	if p == "" || len(p) > maxWorkingDirectoryBytes || !utf8.ValidString(p) || !osc7Absolute(p) {
		return ""
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return p
}

// osc7Absolute reports whether p is absolute in POSIX or Windows convention:
// a leading '/' (POSIX), a UNC "\\server\share" path, or a drive followed by a
// separator ("X:/..." or "X:\..."). A drive-relative "C:dir" (or a bare "C:")
// is NOT accepted: unlike Fresh's letter-colon check it is not safe to use as a
// terminal start cwd. This deliberately diverges from Fresh, which accepts the
// drive-relative form.
func osc7Absolute(p string) bool {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\\\") {
		return true
	}
	return len(p) >= 3 && isASCIIAlpha(p[0]) && p[1] == ':' && (p[2] == '/' || p[2] == '\\')
}

// isASCIIAlpha reports whether b is an ASCII letter.
func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
