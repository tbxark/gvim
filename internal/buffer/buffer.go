// Package buffer implements the text model behind the editor: a list of lines
// addressed by rune columns, with edits that can be undone and redone.
//
// A Buffer is not safe for concurrent use. Every method works on positions
// that are clamped into the document instead of failing, so callers never
// need to pre-validate cursor coordinates.
package buffer

import (
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Pos is a location in a Buffer. Line and Col are both 0-based. Col counts
// runes, not bytes, so Pos{0, 2} is after the second character of the first line.
type Pos struct {
	Line, Col int
}

// Less reports whether p comes before q in document order.
func (p Pos) Less(q Pos) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Col < q.Col
}

// Selection is a range of text between an Anchor, where the selection began,
// and a Head, where the cursor is now. Anchor may come after Head.
type Selection struct {
	Anchor, Head Pos
}

// Ordered returns the selection bounds in document order.
func (s Selection) Ordered() (from, to Pos) {
	if s.Head.Less(s.Anchor) {
		return s.Head, s.Anchor
	}
	return s.Anchor, s.Head
}

// Buffer holds a document as lines of runes. Create one with New; the zero
// value has no lines and must not be used.
type Buffer struct {
	lines [][]rune
	// big is set for a large buffer, which reads its text from a file source
	// and leaves lines nil. See OpenLarge.
	big *bigStore

	undo []*group
	redo []*group
	// open reports whether the newest undo group may absorb the next edit.
	open bool
	// depth counts the Group calls in progress. txn collects the edits made
	// inside the outermost one and is nil when depth is zero.
	depth int
	txn   *group
	// txnRefused records that an edit inside the outermost Group was refused.
	// Only large buffers refuse edits.
	txnRefused bool

	// clock stamps each edit for undo grouping. See SetClock.
	clock func() time.Time

	ver   uint64 // version of the current content
	saved uint64 // version at the last MarkSaved call
	seq   uint64 // source of fresh version numbers

	// nonASCII counts the runes above 0x7f in the text. insertRaw and deleteRaw keep it,
	// so NonASCII is O(1). A large buffer counts the text typed into it, and never
	// subtracts text it cannot see.
	nonASCII int

	// obs is the change observer. See SetObserver.
	obs func(Change)
	// starts[i] is the offset in Text where line i starts, valid for the first
	// startsValid lines. See ByteOffset.
	starts      []int
	startsValid int
	// span is the part of the text that edits touched since it was saved. See EditedLines.
	span editSpan
	// serial counts the edits, and journal keeps the last of them. See EditedSince.
	serial  uint64
	journal [journalSize]editNote
	// snaps counts the memory snapshots taken and not released. They share the lines, so
	// while one is alive the lines are not edited in place.
	snaps atomic.Int32
}

// New returns a Buffer containing text. Lines are split on '\n'; a trailing
// newline produces a final empty line, so Text round-trips exactly.
func New(text string) *Buffer {
	b := &Buffer{clock: time.Now}
	b.setText(text)
	b.resetSpan()
	return b
}

// NewAfter returns a Buffer containing text whose version numbers all differ from every
// one prev has issued, so a cache keyed on Buffer.Version alone cannot take the new text
// for the content of prev. Its history is empty and it is not modified.
func NewAfter(text string, prev *Buffer) *Buffer {
	b := New(text)
	if prev != nil {
		b.seq = max(prev.seq, prev.ver) + 1
		b.ver, b.saved = b.seq, b.seq
	}
	return b
}

// SetClock replaces the function that stamps edits for undo grouping. Edits
// stamped in the same one-second wall-clock bucket undo together. A nil
// clock restores time.Now. Tests inject a fixed or stepped clock to make
// grouping deterministic.
func (b *Buffer) SetClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	b.clock = now
}

// now returns the current time from the injected clock.
func (b *Buffer) now() time.Time {
	if b.clock == nil {
		return time.Now()
	}
	return b.clock()
}

// setText replaces the whole content without recording history.
func (b *Buffer) setText(text string) {
	parts := strings.Split(text, "\n")
	b.lines = make([][]rune, len(parts))
	for i, p := range parts {
		b.lines[i] = []rune(p)
	}
	b.nonASCII = countNonASCII(text)
}

// NonASCII reports whether the text has a rune above 0x7f, which is any character that
// ASCII does not hold. Typed, pasted and deleted text is counted as it changes, so the
// answer is current without a scan.
func (b *Buffer) NonASCII() bool { return b.nonASCII > 0 }

// countNonASCII is the number of runes above 0x7f in s.
func countNonASCII(s string) int {
	n := 0
	for _, r := range s {
		if r >= utf8.RuneSelf {
			n++
		}
	}
	return n
}

// countNonASCIIIn is the number of runes above 0x7f between from and to, which are
// clamped and ordered.
func (b *Buffer) countNonASCIIIn(from, to Pos) int {
	n := 0
	for i := from.Line; i <= to.Line; i++ {
		line := b.lines[i]
		lo, hi := 0, len(line)
		if i == from.Line {
			lo = from.Col
		}
		if i == to.Line {
			hi = to.Col
		}
		for _, r := range line[lo:hi] {
			if r >= utf8.RuneSelf {
				n++
			}
		}
	}
	return n
}

// Lines returns the number of lines. An empty buffer has one line.
func (b *Buffer) Lines() int {
	if b.big != nil {
		return b.big.lines()
	}
	return len(b.lines)
}

// Line returns line i without its newline. Out-of-range indexes are clamped.
func (b *Buffer) Line(i int) string {
	if b.big != nil {
		return b.big.line(i)
	}
	return string(b.lines[b.clampLine(i)])
}

// LineEquals reports whether line i is text, without building the line as a string.
// Out-of-range indexes are clamped.
func (b *Buffer) LineEquals(i int, text string) bool {
	if b.big != nil {
		return b.big.line(i) == text
	}
	line := b.lines[b.clampLine(i)]
	if len(line) > len(text) {
		return false // a rune takes at least one byte
	}
	n := 0
	for _, r := range text {
		if n >= len(line) || line[n] != r {
			return false
		}
		n++
	}
	return n == len(line)
}

// LineIndent returns the display width of the leading spaces and tabs of line i, with
// tabWidth as the width of a tab, and whether the line is blank (white space only). It
// does not build the line as a string. Out-of-range indexes are clamped.
func (b *Buffer) LineIndent(i, tabWidth int) (width int, blank bool) {
	tabWidth = max(tabWidth, 1)
	var line []rune
	if b.big != nil {
		line = []rune(b.big.line(i))
	} else {
		line = b.lines[b.clampLine(i)]
	}
	counting := true
	for _, r := range line {
		switch {
		case counting && r == ' ':
			width++
		case counting && r == '\t':
			width = (width/tabWidth + 1) * tabWidth
		case unicode.IsSpace(r):
			counting = false
		default:
			return width, false
		}
	}
	return width, true
}

// LineLen returns the number of runes on line i. Out-of-range indexes are clamped.
func (b *Buffer) LineLen(i int) int {
	if b.big != nil {
		return b.big.lineLen(i)
	}
	return len(b.lines[b.clampLine(i)])
}

// RuneAt returns the rune at p and reports whether p addresses a character.
// It returns false at the end of a line, where the position holds a newline or
// the end of the document.
func (b *Buffer) RuneAt(p Pos) (rune, bool) {
	if b.big != nil {
		return b.big.runeAt(p)
	}
	if p.Line < 0 || p.Line >= len(b.lines) {
		return 0, false
	}
	line := b.lines[p.Line]
	if p.Col < 0 || p.Col >= len(line) {
		return 0, false
	}
	return line[p.Col], true
}

// Text returns the whole document joined with '\n'.
func (b *Buffer) Text() string {
	if b.big != nil {
		return b.bigText(b.big.text())
	}
	var sb strings.Builder
	for i, l := range b.lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(l))
	}
	return sb.String()
}

// Range returns the text between from and to, in document order, with
// newlines included. Positions are clamped.
func (b *Buffer) Range(from, to Pos) string {
	if b.big != nil {
		return b.bigText(b.big.rangeText(from, to))
	}
	from, to = b.Clamp(from), b.Clamp(to)
	if to.Less(from) {
		from, to = to, from
	}
	if from == to {
		return ""
	}
	if from.Line == to.Line {
		return string(b.lines[from.Line][from.Col:to.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(b.lines[from.Line][from.Col:]))
	for i := from.Line + 1; i < to.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(string(b.lines[i]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(b.lines[to.Line][:to.Col]))
	return sb.String()
}

// Clamp returns p moved into the document: the line is kept within the
// buffer and the column within the line.
func (b *Buffer) Clamp(p Pos) Pos {
	if b.big != nil {
		return b.big.clamp(p)
	}
	p.Line = b.clampLine(p.Line)
	p.Col = min(max(p.Col, 0), len(b.lines[p.Line]))
	return p
}

// End returns the position after the last rune of the document.
func (b *Buffer) End() Pos {
	if b.big != nil {
		return b.big.end()
	}
	last := len(b.lines) - 1
	return Pos{last, len(b.lines[last])}
}

// Insert inserts s at p and returns the position just after the inserted text.
// p is clamped first. Inserting the empty string changes nothing. A large
// buffer can refuse an edit: Insert then does nothing and returns p, and
// Refusal says why.
func (b *Buffer) Insert(p Pos, s string) Pos {
	p = b.Clamp(p)
	if s == "" {
		return p
	}
	if b.big != nil {
		return b.bigInsert(p, s)
	}
	var c Change
	if b.obs != nil {
		c = b.changeOf(p, p, s, "")
	}
	b.record(opInsert, p, s)
	end := b.insertRaw(p, s)
	if b.obs != nil {
		b.notify(c)
	}
	return end
}

// Delete removes the text between from and to, in either order, and returns
// the removed text. Both positions are clamped. A large buffer returns "" for
// a removed span over InlineOpMax, and can refuse an edit: Delete then does
// nothing and returns "", and Refusal says why.
func (b *Buffer) Delete(from, to Pos) string {
	from, to = b.Clamp(from), b.Clamp(to)
	if to.Less(from) {
		from, to = to, from
	}
	if from == to {
		return ""
	}
	if b.big != nil {
		return b.bigDelete(from, to)
	}
	text := b.Range(from, to)
	var c Change
	if b.obs != nil {
		c = b.changeOf(from, to, "", text)
	}
	b.record(opDelete, from, text)
	b.deleteRaw(from, to)
	if b.obs != nil {
		b.notify(c)
	}
	return text
}

// Modified reports whether the content differs from the last MarkSaved call.
// A buffer that was never saved is modified once any edit has been applied.
// Undoing back to the saved content is not modified, and an edit after an
// undo to the saved point is modified again.
func (b *Buffer) Modified() bool {
	return b.ver != b.saved
}

// MarkSaved records the current content as the saved state. It is also a
// save boundary: the next edit starts a new undo group, even within the same
// wall-clock bucket.
func (b *Buffer) MarkSaved() {
	b.saved = b.ver
	b.open = false
	b.resetSpan()
}

func (b *Buffer) clampLine(i int) int {
	if b.big != nil {
		return b.big.clampLine(i)
	}
	return min(max(i, 0), len(b.lines)-1)
}

// insertRaw applies an insertion without touching history and returns the
// position after the inserted text. p must already be clamped.
func (b *Buffer) insertRaw(p Pos, s string) Pos {
	b.forgetStarts(p.Line)
	b.nonASCII += countNonASCII(s)
	end := advance(p, s)
	line := b.lines[p.Line]
	if !strings.Contains(s, "\n") {
		// Text on one line goes into the line. With no snapshot of the text alive, that is
		// done where the line is, and takes the move of the text after the caret, none when
		// typing at the end of the line, which grows it in place. A snapshot shares the
		// lines, so with one the edit makes a new line, as it always did.
		switch {
		case b.snaps.Load() == 0:
			b.lines[p.Line] = slices.Insert(line, p.Col, []rune(s)...)
		default:
			b.lines[p.Line] = slices.Concat(line[:p.Col], []rune(s), line[p.Col:])
		}
		b.noteEdit(p.Line, p.Line, 0)
		return end
	}
	parts := strings.Split(s, "\n")
	head := slices.Clone(line[:p.Col])
	tail := slices.Clone(line[p.Col:])
	inserted := make([][]rune, 0, len(parts))
	inserted = append(inserted, slices.Concat(head, []rune(parts[0])))
	for _, part := range parts[1 : len(parts)-1] {
		inserted = append(inserted, []rune(part))
	}
	inserted = append(inserted, slices.Concat([]rune(parts[len(parts)-1]), tail))
	b.lines = slices.Replace(b.lines, p.Line, p.Line+1, inserted...)
	b.noteEdit(p.Line, end.Line, len(parts)-1)
	return end
}

// deleteRaw removes the range from..to without touching history. Both
// positions must be clamped and ordered.
func (b *Buffer) deleteRaw(from, to Pos) {
	b.forgetStarts(from.Line)
	b.nonASCII -= b.countNonASCIIIn(from, to)
	if from.Line == to.Line {
		// Text on one line comes out of the line, where it is when no snapshot shares it.
		line := b.lines[from.Line]
		switch {
		case b.snaps.Load() == 0:
			b.lines[from.Line] = slices.Delete(line, from.Col, to.Col)
		default:
			b.lines[from.Line] = slices.Concat(line[:from.Col], line[to.Col:])
		}
		b.noteEdit(from.Line, from.Line, 0)
		return
	}
	head := b.lines[from.Line][:from.Col]
	tail := b.lines[to.Line][to.Col:]
	joined := slices.Concat(head, tail)
	b.lines = slices.Replace(b.lines, from.Line, to.Line+1, joined)
	b.noteEdit(from.Line, from.Line, from.Line-to.Line)
}

// advance returns the position reached after typing s starting at p.
func advance(p Pos, s string) Pos {
	nl := strings.Count(s, "\n")
	if nl == 0 {
		return Pos{p.Line, p.Col + utf8.RuneCountInString(s)}
	}
	last := s[strings.LastIndexByte(s, '\n')+1:]
	return Pos{p.Line + nl, utf8.RuneCountInString(last)}
}
