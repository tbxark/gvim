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
	"time"
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

	undo []*group
	redo []*group
	// open reports whether the newest undo group may absorb the next edit.
	open bool
	// depth counts the Group calls in progress. txn collects the edits made
	// inside the outermost one and is nil when depth is zero.
	depth int
	txn   *group

	// clock stamps each edit for undo grouping. See SetClock.
	clock func() time.Time

	ver   uint64 // version of the current content
	saved uint64 // version at the last MarkSaved call
	seq   uint64 // source of fresh version numbers
}

// New returns a Buffer containing text. Lines are split on '\n'; a trailing
// newline produces a final empty line, so Text round-trips exactly.
func New(text string) *Buffer {
	b := &Buffer{clock: time.Now}
	b.setText(text)
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
}

// Lines returns the number of lines. An empty buffer has one line.
func (b *Buffer) Lines() int {
	return len(b.lines)
}

// Line returns line i without its newline. Out-of-range indexes are clamped.
func (b *Buffer) Line(i int) string {
	return string(b.lines[b.clampLine(i)])
}

// LineLen returns the number of runes on line i. Out-of-range indexes are clamped.
func (b *Buffer) LineLen(i int) int {
	return len(b.lines[b.clampLine(i)])
}

// RuneAt returns the rune at p and reports whether p addresses a character.
// It returns false at the end of a line, where the position holds a newline or
// the end of the document.
func (b *Buffer) RuneAt(p Pos) (rune, bool) {
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
	p.Line = b.clampLine(p.Line)
	p.Col = min(max(p.Col, 0), len(b.lines[p.Line]))
	return p
}

// End returns the position after the last rune of the document.
func (b *Buffer) End() Pos {
	last := len(b.lines) - 1
	return Pos{last, len(b.lines[last])}
}

// Insert inserts s at p and returns the position just after the inserted text.
// p is clamped first. Inserting the empty string changes nothing.
func (b *Buffer) Insert(p Pos, s string) Pos {
	p = b.Clamp(p)
	if s == "" {
		return p
	}
	b.record(opInsert, p, s)
	return b.insertRaw(p, s)
}

// Delete removes the text between from and to, in either order, and returns
// the removed text. Both positions are clamped.
func (b *Buffer) Delete(from, to Pos) string {
	from, to = b.Clamp(from), b.Clamp(to)
	if to.Less(from) {
		from, to = to, from
	}
	if from == to {
		return ""
	}
	text := b.Range(from, to)
	b.record(opDelete, from, text)
	b.deleteRaw(from, to)
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
}

func (b *Buffer) clampLine(i int) int {
	return min(max(i, 0), len(b.lines)-1)
}

// insertRaw applies an insertion without touching history and returns the
// position after the inserted text. p must already be clamped.
func (b *Buffer) insertRaw(p Pos, s string) Pos {
	end := advance(p, s)
	parts := strings.Split(s, "\n")
	line := b.lines[p.Line]
	head := slices.Clone(line[:p.Col])
	tail := slices.Clone(line[p.Col:])

	if len(parts) == 1 {
		b.lines[p.Line] = slices.Concat(head, []rune(s), tail)
		return end
	}
	inserted := make([][]rune, 0, len(parts))
	inserted = append(inserted, slices.Concat(head, []rune(parts[0])))
	for _, part := range parts[1 : len(parts)-1] {
		inserted = append(inserted, []rune(part))
	}
	inserted = append(inserted, slices.Concat([]rune(parts[len(parts)-1]), tail))
	b.lines = slices.Replace(b.lines, p.Line, p.Line+1, inserted...)
	return end
}

// deleteRaw removes the range from..to without touching history. Both
// positions must be clamped and ordered.
func (b *Buffer) deleteRaw(from, to Pos) {
	head := b.lines[from.Line][:from.Col]
	tail := b.lines[to.Line][to.Col:]
	joined := slices.Concat(head, tail)
	b.lines = slices.Replace(b.lines, from.Line, to.Line+1, joined)
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
