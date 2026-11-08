package diff

import (
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Kind is the kind of a Hunk.
type Kind int

const (
	Insert Kind = iota // new lines only
	Delete             // old lines only
	Change             // old lines replaced by new lines
)

// Hunk is a run of changed lines. Line indexes are zero-based. OldStart and
// NewStart are the first line of the run on each side. For Insert, OldLines is
// 0 and OldStart is the old line the new lines go before. For Delete, NewLines
// is 0 and NewStart is the new line the old lines were removed before.
type Hunk struct {
	Kind     Kind
	OldStart int
	OldLines int
	NewStart int
	NewLines int
}

// Lines returns the line hunks that turn a into b, in order. Lines are compared
// as whole strings, so a line that differs only in its indentation is changed.
// The diff is a valid diff, but on very large inputs it may not be minimal,
// because the diffmatchpatch default timeout applies.
func Lines(a, b []string) []Hunk {
	ids := make(map[string]rune)
	x := symbols(a, ids)
	y := symbols(b, ids)
	diffs := diffmatchpatch.New().DiffMainRunes(x, y, false)

	var hunks []Hunk
	var cur Hunk
	open := false
	oldPos, newPos := 0, 0
	for _, d := range diffs {
		// Each rune of d.Text is one line. Runes that are not valid code points
		// are written as U+FFFD, one per rune, so the count is still exact.
		n := utf8.RuneCountInString(d.Text)
		if n == 0 {
			continue
		}
		if d.Type == diffmatchpatch.DiffEqual {
			if open {
				hunks = append(hunks, withKind(cur))
				open = false
			}
			oldPos += n
			newPos += n
			continue
		}
		if !open {
			cur = Hunk{OldStart: oldPos, NewStart: newPos}
			open = true
		}
		if d.Type == diffmatchpatch.DiffDelete {
			cur.OldLines += n
			oldPos += n
		} else {
			cur.NewLines += n
			newPos += n
		}
	}
	if open {
		hunks = append(hunks, withKind(cur))
	}
	return hunks
}

// symbols maps each line to a rune, assigning new runes to new lines through
// ids, so that equal lines get equal runes in both texts.
func symbols(lines []string, ids map[string]rune) []rune {
	out := make([]rune, len(lines))
	for i, line := range lines {
		r, ok := ids[line]
		if !ok {
			r = symbol(len(ids))
			ids[line] = r
		}
		out[i] = r
	}
	return out
}

// symbol returns the rune for the n-th distinct line. Surrogates are skipped, so
// the rune is a valid code point for every n that fits in the code space.
func symbol(n int) rune {
	if n >= 0xd800 {
		n += 0x800
	}
	return rune(n)
}

// withKind sets the Kind of h from the number of lines on each side.
func withKind(h Hunk) Hunk {
	switch {
	case h.OldLines > 0 && h.NewLines > 0:
		h.Kind = Change
	case h.OldLines > 0:
		h.Kind = Delete
	default:
		h.Kind = Insert
	}
	return h
}

// NextHunk returns the NewStart of the first hunk that starts after line, and
// false when there is none. It does not wrap around. The caller clamps the line
// to the text, since a Delete hunk can start at the end of the text.
func NextHunk(hunks []Hunk, line int) (int, bool) {
	best, found := 0, false
	for _, h := range hunks {
		if h.NewStart > line && (!found || h.NewStart < best) {
			best, found = h.NewStart, true
		}
	}
	return best, found
}

// PrevHunk returns the NewStart of the last hunk that starts before line, and
// false when there is none. A hunk that contains line is included, so from
// inside a hunk PrevHunk returns that hunk's start. It does not wrap around.
func PrevHunk(hunks []Hunk, line int) (int, bool) {
	best, found := 0, false
	for _, h := range hunks {
		if h.NewStart < line && (!found || h.NewStart > best) {
			best, found = h.NewStart, true
		}
	}
	return best, found
}
