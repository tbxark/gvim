package buffer

import "math"

// editSpan is the part of a memory buffer's text that edits touched since the buffer was
// made or last marked saved. Lines above lo and the lines below the last touched line,
// counted from the end, are the lines of the saved text. It lets a caller that compares
// the text with the saved text look at the edited lines only, which for typing in a big
// document is one line, and not at all of them.
type editSpan struct {
	// lo is the first line an edit touched, or MaxInt when none did.
	lo int
	// fromEnd is the fewest lines that stood below the last line an edit touched, or
	// MaxInt when none did.
	fromEnd int
}

// editNote is one edit in the journal: the lines it touched, in the text after it, and by
// how many lines it changed the line count.
type editNote struct {
	first, last, delta int
}

// journalSize is how many edits the journal keeps.
const journalSize = 256

// resetSpan forgets the edits: the text is the saved text.
func (b *Buffer) resetSpan() { b.span = editSpan{lo: math.MaxInt, fromEnd: math.MaxInt} }

// noteEdit records an edit that touched lines first through last of the text after the
// edit, so it is called once the edit is made, and changed the number of lines by delta. The
// lines of an edit that took out lines count as the one it joined them in.
func (b *Buffer) noteEdit(first, last, delta int) {
	b.span.lo = min(b.span.lo, first)
	b.span.fromEnd = min(b.span.fromEnd, len(b.lines)-1-last)
	b.journal[b.serial%journalSize] = editNote{first, last, delta}
	b.serial++
}

// EditSerial numbers the edits the buffer has made, undo and redo included. It only grows.
func (b *Buffer) EditSerial() uint64 { return b.serial }

// EditedSince returns the lines [lo, hi] that the edits made after serial touched, when none of
// them changed the line count, so a line has the number it had at serial. ok is false when one
// did, when too many edits passed for the journal to hold them, and for a large buffer. serial
// is a value EditSerial returned.
func (b *Buffer) EditedSince(serial uint64) (lo, hi int, ok bool) {
	if b.big != nil || serial > b.serial || b.serial-serial > journalSize {
		return 0, 0, false
	}
	lo, hi = math.MaxInt, -1
	for k := serial + 1; k <= b.serial; k++ {
		note := b.journal[(k-1)%journalSize]
		if note.delta != 0 {
			return 0, 0, false
		}
		lo, hi = min(lo, note.first), max(hi, note.last)
	}
	return lo, hi, true
}

// EditedLines returns the lines [lo, hi) that may differ from the text at the last
// MarkSaved call, or from the text the buffer was made with. Every line before lo and
// every line from hi on is the same as the saved line at the same distance from the
// start, and from the end, of the text. ok is false for a large buffer, which keeps no
// saved text. The range is an upper bound: an edit that was undone is still in it.
func (b *Buffer) EditedLines() (lo, hi int, ok bool) {
	if b.big != nil {
		return 0, 0, false
	}
	n := len(b.lines)
	lo = min(b.span.lo, n)
	hi = max(n-min(b.span.fromEnd, n), lo)
	return lo, hi, true
}
