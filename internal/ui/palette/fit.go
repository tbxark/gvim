package palette

import (
	"slices"
	"strconv"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/space"
)

// ellipsisRune is the cut mark the shortening in internal/ui/space leaves.
const ellipsisRune = '…'

// fitName returns the name of r shortened to at most width cells, and the rune
// offsets in the result that are matched. A file path keeps its base name and gives
// up its middle, a grep row keeps the match in view, and any other name is cut at
// its end.
func fitName(r row, width int) (string, []int) {
	switch r.kind {
	case rowGrep:
		return fitGrep(r, width)
	case rowFile:
		return fitMapped(r.name, r.hl, width, space.Path)
	}
	return fitMapped(r.name, r.hl, width, space.End)
}

// fitMapped shortens name with cut and moves the matched offsets hl along with the
// runes that stay. cut keeps a prefix and a suffix of the name around a single "…",
// as space.End (an empty suffix) and space.Path do.
func fitMapped(name string, hl []int, width int, cut func(string, int) string) (string, []int) {
	shown := cut(name, width)
	if shown == name || len(hl) == 0 {
		return shown, hl
	}
	orig, sr := []rune(name), []rune(shown)
	np := slices.Index(sr, ellipsisRune)
	if np < 0 {
		return shown, nil
	}
	ns := len(sr) - np - 1
	var out []int
	for _, i := range hl {
		switch {
		case i < np:
			out = append(out, i)
		case i >= len(orig)-ns:
			out = append(out, i-(len(orig)-ns)+np+1)
		}
	}
	return shown, out
}

// grepMinText and grepMinPath are the cells a grep row keeps for the line text and
// for the path at least, when the row is too long for both.
const (
	grepMinText = 12
	grepMinPath = 12
)

// fitGrep builds the label of a grep row, "path:line: text", in width cells. The
// path is shortened in the middle, keeping its base name, and the line text is
// windowed so the match stays visible: "…" marks what is cut on either side. The
// offsets returned are the matched runes of the label.
func fitGrep(r row, width int) (string, []int) {
	loc := ":" + strconv.Itoa(r.line) + ": "
	text := r.text
	full := r.rel + loc + text
	if ansi.StringWidth(full) <= width {
		return full, matchOffsets(len([]rune(r.rel+loc)), r.mcol, r.qlen, len([]rune(text)))
	}
	locW := ansi.StringWidth(loc)
	// The path takes what the line text does not need, and leaves it half the row.
	minText := min(ansi.StringWidth(text), max(width/2, grepMinText))
	pw := min(ansi.StringWidth(r.rel), max(width-locW-minText, grepMinPath))
	room := width - locW - pw
	if room < 4 {
		pw = max(width-locW-4, 1)
		room = width - locW - pw
	}
	if room <= 0 {
		return space.End(full, width), nil
	}
	path := space.Path(r.rel, pw)
	body, off := windowAround(text, r.mcol, r.qlen, room)
	label := path + loc + body
	prefix := len([]rune(path + loc))
	return label, matchOffsets(prefix, off, r.qlen, len([]rune(body)))
}

// windowAround shows text in at most room cells so that the match at rune offset mcol
// with qlen runes is in view. It returns the shown text and the rune offset of the
// match in it. A text that fits is returned whole. The start of the text is kept
// when the match fits in the first cells, and otherwise the window starts a quarter
// of the room before the match, with "…" in front.
func windowAround(text string, mcol, qlen, room int) (string, int) {
	if ansi.StringWidth(text) <= room {
		return text, mcol
	}
	runes := []rune(text)
	mcol = min(max(mcol, 0), len(runes))
	end := min(mcol+max(qlen, 1), len(runes))
	if ansi.StringWidth(string(runes[:end])) <= room-1 {
		return space.End(text, room), mcol
	}
	// The quarter of the room before the match is counted in cells, so a wide character
	// does not take two of them away from the match.
	start, before := mcol, 0
	for start > 1 {
		cw := ansi.StringWidth(string(runes[start-1]))
		if before+cw > room/4 {
			break
		}
		before += cw
		start--
	}
	rest := space.End(string(runes[start:]), room-1)
	return string(ellipsisRune) + rest, mcol - start + 1
}

// matchOffsets lists the rune offsets prefix+off ... prefix+off+qlen-1 that fall
// inside a body of bodyLen runes. A match cut off by the end of the body, or one
// whose last rune is the closing "…", loses those runes.
func matchOffsets(prefix, off, qlen, bodyLen int) []int {
	var out []int
	for j := range qlen {
		if i := off + j; i >= 0 && i < bodyLen {
			out = append(out, prefix+i)
		}
	}
	return out
}
