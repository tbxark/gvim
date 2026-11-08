package space

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const ellipsis = "…"

// End shortens s to at most w cells, ending with "…" when it is cut.
func End(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w-1, "") + ellipsis
}

// Middle shortens s to at most w cells by replacing the middle with "…", so the
// start and the end both stay: the end of a message is usually the reason.
func Middle(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w == 1 {
		return ellipsis
	}
	keep := w - 1
	tail := Tail(s, keep/2)
	return ansi.Truncate(s, keep-ansi.StringWidth(tail), "") + ellipsis + tail
}

// Tail returns the last at most w cells of s. A wide character that the cut would
// split is dropped whole, so the result is never wider than w.
func Tail(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := ansi.StringWidth(s)
	if n <= w {
		return s
	}
	for cut := n - w; ; cut++ {
		if t := ansi.TruncateLeft(s, cut, ""); ansi.StringWidth(t) <= w {
			return t
		}
	}
}

// Path shortens a slash-separated path to at most w cells. The base name is kept
// whole when it fits, and the front of the path keeps as many leading cells as the
// room allows: "internal/…/search.go". A base name longer than the room is cut at
// its end.
func Path(p string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(p) <= w {
		return p
	}
	cut := strings.LastIndexByte(p, '/')
	if cut < 0 {
		return End(p, w)
	}
	dir, base := p[:cut+1], p[cut+1:]
	bw := ansi.StringWidth(base)
	// "…/" and the base name need bw+2 cells; the rest goes to the front.
	if bw+2 > w {
		return End(base, w)
	}
	room := w - bw - 2
	prefix := ansi.Truncate(dir, room, "")
	if i := strings.LastIndexByte(prefix, '/'); i >= 0 && len(prefix) < len(dir) {
		prefix = prefix[:i+1]
	} else if len(prefix) < len(dir) {
		prefix = ""
	}
	return prefix + ellipsis + "/" + base
}

// Name shortens a file name to at most w cells. The extension stays when it is
// short enough, so "file_open_orchestrator.rs" becomes "file_open_orc….rs" and the
// kind of file is still visible. A name with no usable extension is cut at its end.
func Name(name string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(name) <= w {
		return name
	}
	dot := strings.LastIndexByte(name, '.')
	if dot > 0 {
		ext := name[dot:]
		if ew := ansi.StringWidth(ext); ew <= 8 && w >= ew+3 {
			return ansi.Truncate(name[:dot], w-ew-1, "") + ellipsis + ext
		}
	}
	return End(name, w)
}

// PadTo pads s with blanks on the right to w cells. Text that is already wider is
// kept whole.
func PadTo(s string, w int) string {
	if pad := w - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// Wrap breaks s into rows of at most w cells, at most maxRows of them. When the text
// needs more rows, the last row ends with "…" in place of the rest. Words are kept
// whole where they fit, and a longer word is broken.
func Wrap(s string, w, maxRows int) []string {
	if w <= 0 || maxRows <= 0 {
		return nil
	}
	rows := strings.Split(ansi.Wrap(s, w, ""), "\n")
	if len(rows) <= maxRows {
		return rows
	}
	rows = rows[:maxRows]
	last := rows[maxRows-1]
	if ansi.StringWidth(last) >= w {
		last = ansi.Truncate(last, w-1, "")
	}
	rows[maxRows-1] = last + ellipsis
	return rows
}

// Window returns the part of the one-line text value that a field room cells wide
// shows with its caret before rune pos, and the caret's column in that part. A value
// that fits, with a cell to spare for a caret at its end, is returned whole. A longer
// one is cut at both ends, around the caret: it keeps up to a quarter of the room to
// the right of the caret and gives the rest to the text before it, so the end of
// what was just typed is always visible and the caret never leaves the room.
func Window(value string, pos, room int) (shown string, caret int) {
	runes := []rune(value)
	pos = min(max(pos, 0), len(runes))
	if room <= 0 {
		return "", 0
	}
	cellOf := func(r rune) int { return ansi.StringWidth(string(r)) }
	total, before := 0, 0
	for i, r := range runes {
		if i < pos {
			before += cellOf(r)
		}
		total += cellOf(r)
	}
	if total+1 <= room {
		return value, before
	}
	// right is the cells kept after the caret, left the cells before it.
	right, end := 0, pos
	for end < len(runes) && right+cellOf(runes[end]) <= (room-1)/4 {
		right += cellOf(runes[end])
		end++
	}
	left, start := 0, pos
	for start > 0 && left+cellOf(runes[start-1]) <= room-1-right {
		left += cellOf(runes[start-1])
		start--
	}
	return string(runes[start:end]), left
}
