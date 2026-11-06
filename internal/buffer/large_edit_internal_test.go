package buffer

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// termsOf lists the terminator flags of every document line.
func termsOf(b *Buffer) []uint8 {
	out := make([]uint8, b.Lines())
	for i := range out {
		out[i] = b.big.termAt(i)
	}
	return out
}

func equalTerms(got, want []uint8) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func openIndexed(t *testing.T, data string) *Buffer {
	t.Helper()
	b := tinyOpen(t, data)
	indexFully(t, b)
	b.SetClock(func() time.Time { return time.Unix(1_700_000_000, 0) })
	return b
}

func TestHugePasteThenEditSharesBackingArray(t *testing.T) {
	b := openIndexed(t, "first\nsecond\nthird\n")
	b.Insert(Pos{Line: 1}, strings.Repeat("pasted\n", 99)+"pasted")
	p, _, ok := at(b.big.st.root, 1)
	if !ok || p.isRun() || len(p.lit) != 100 {
		t.Fatalf("paste is not one literal piece of 100 lines: %+v", p)
	}
	b.Break()
	b.Insert(Pos{Line: 51, Col: 2}, "EDIT")

	left, _, _ := at(b.big.st.root, 1)
	right, _, _ := at(b.big.st.root, 52)
	if len(left.lit) != 50 || &left.lit[0] != &p.lit[0] {
		t.Errorf("left half has %d lines and does not alias the pasted array", len(left.lit))
	}
	if len(right.lit) != 49 || &right.lit[0] != &p.lit[51] {
		t.Errorf("right half has %d lines and does not alias the pasted array", len(right.lit))
	}
	// Appending to a half must never reach into the other half.
	if cap(left.lit) != len(left.lit) {
		t.Errorf("left half has spare capacity %d", cap(left.lit)-len(left.lit))
	}
	if got := b.Line(51); got != "paEDITsted" {
		t.Errorf("Line(51) = %q", got)
	}
	// The pasted lines were not copied: the overlay grew by one line.
	if got, limit := b.Stats().OverlayBytes, int64(100*(6*runeCost+lineCost))+int64(10*runeCost+lineCost); got > limit {
		t.Errorf("OverlayBytes = %d, want at most %d", got, limit)
	}
}

func TestEnterInCRLFRegionOfLFHeadInheritsCRLF(t *testing.T) {
	const (
		lf   = eolLF
		crlf = eolCRLF
	)
	b := openIndexed(t, "lf1\nlf2\ncr1\r\ncr2\r\ncr3\r\ntail")
	if got, want := termsOf(b), []uint8{lf, lf, crlf, crlf, crlf, eolDefault}; !equalTerms(got, want) {
		t.Fatalf("terminators of the source: %v, want %v", got, want)
	}

	b.Insert(Pos{Line: 3, Col: 1}, "\n")
	if got, want := termsOf(b), []uint8{lf, lf, crlf, crlf, crlf, crlf, eolDefault}; !equalTerms(got, want) {
		t.Errorf("Enter in a CRLF line: %v, want %v", got, want)
	}
	if b.Line(3) != "c" || b.Line(4) != "r2" {
		t.Errorf("split lines: %q %q", b.Line(3), b.Line(4))
	}

	b.Break()
	b.Insert(Pos{Line: 1, Col: 1}, "\n")
	if got, want := termsOf(b), []uint8{lf, lf, lf, crlf, crlf, crlf, crlf, eolDefault}; !equalTerms(got, want) {
		t.Errorf("Enter in an LF line: %v, want %v", got, want)
	}

	// Undo brings the original terminators back with the original lines.
	b.Undo()
	b.Undo()
	if got, want := termsOf(b), []uint8{lf, lf, crlf, crlf, crlf, eolDefault}; !equalTerms(got, want) {
		t.Errorf("after undo: %v, want %v", got, want)
	}
}

func TestJoinLinesTakesTailTerminator(t *testing.T) {
	const (
		lf   = eolLF
		crlf = eolCRLF
	)
	tests := []struct {
		name     string
		from, to Pos
		want     []uint8
		joined   string
	}{
		{"LF head, CRLF tail", Pos{Line: 1, Col: 3}, Pos{Line: 2}, []uint8{lf, crlf, lf, eolDefault}, "lf2cr1"},
		{"CRLF head, LF tail", Pos{Line: 2, Col: 3}, Pos{Line: 3}, []uint8{lf, lf, lf, eolDefault}, "cr1lf3"},
		{"join with the unterminated last line", Pos{Line: 3, Col: 3}, Pos{Line: 4}, []uint8{lf, lf, crlf, eolDefault}, "lf3last"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := openIndexed(t, "lf1\nlf2\ncr1\r\nlf3\nlast")
			b.Delete(tt.from, tt.to)
			if got := termsOf(b); !equalTerms(got, tt.want) {
				t.Errorf("terminators %v, want %v", got, tt.want)
			}
			if got := b.Line(tt.from.Line); got != tt.joined {
				t.Errorf("joined line = %q, want %q", got, tt.joined)
			}
		})
	}
}

func TestEditedLastLineEolDefault(t *testing.T) {
	b := openIndexed(t, "a\r\nb\r\nlast")
	b.Insert(Pos{Line: 2, Col: 4}, "!")
	if got, want := termsOf(b), []uint8{eolCRLF, eolCRLF, eolDefault}; !equalTerms(got, want) {
		t.Fatalf("editing the last line: %v, want %v", got, want)
	}
	// Enter at the end gives the old last line the terminator of its neighbour.
	b.Break()
	b.Insert(Pos{Line: 2, Col: 5}, "\nnext")
	if got, want := termsOf(b), []uint8{eolCRLF, eolCRLF, eolCRLF, eolDefault}; !equalTerms(got, want) {
		t.Errorf("Enter at the end: %v, want %v", got, want)
	}
	// With no terminated line before it, a default stays a default.
	one := openIndexed(t, "only")
	one.Insert(Pos{Line: 0, Col: 4}, "\nmore")
	if got, want := termsOf(one), []uint8{eolDefault, eolDefault}; !equalTerms(got, want) {
		t.Errorf("Enter in a one-line document: %v, want %v", got, want)
	}
}

func TestBigOpsStoreNoTextAndNeverMerge(t *testing.T) {
	b := openIndexed(t, "abc\ndef\n")
	paste := strings.Repeat("xy\n", InlineOpMax/3+10)
	end := b.Insert(Pos{Line: 0, Col: 1}, paste)
	want := advance(Pos{Line: 0, Col: 1}, paste)
	if end != want {
		t.Fatalf("Insert returned %v, want %v", end, want)
	}
	// Typing right after the paste is the same undo step but a separate op.
	b.Insert(end, "z")
	if len(b.undo) != 1 || len(b.undo[0].ops) != 2 {
		t.Fatalf("undo groups %d, ops %d; want one group of two ops", len(b.undo), len(b.undo[0].ops))
	}
	if o := b.undo[0].ops[0]; !o.big || o.text != "" || o.end != want {
		t.Errorf("paste op: big %v, text %d bytes, end %v", o.big, len(o.text), o.end)
	}
	b.Break()
	if got := b.Delete(Pos{Line: 0, Col: 1}, Pos{Line: want.Line, Col: want.Col + 1}); got != "" {
		t.Errorf("Delete of %d bytes returned text", len(paste))
	}
	if b.Text() != "abc\ndef\n" {
		t.Errorf("Text after deleting the paste = %q", b.Text())
	}
	if cost := b.undo[0].cost; cost < int64(len(paste)) {
		t.Errorf("group cost %d does not cover the paste", cost)
	}
	// Undo and Redo restore the big edit without its text.
	b.Break()
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo failed")
	}
	if b.Lines() != 3+InlineOpMax/3+10 {
		t.Errorf("Undo of the delete: %d lines", b.Lines())
	}
	b.Undo()
	if b.Text() != "abc\ndef\n" || b.Modified() {
		t.Errorf("Undo of the paste: modified %v", b.Modified())
	}
	if at, ok := b.Redo(); !ok || at != (Pos{Line: want.Line, Col: want.Col + 1}) {
		t.Errorf("Redo = %v %v, want the position after the typed z", at, ok)
	}
}

func TestMergedTypingCostsOneLine(t *testing.T) {
	b := openIndexed(t, "short\n")
	b.big.maxHistory = 1 << 30
	b.Insert(Pos{Line: 0, Col: 5}, strings.Repeat("0123456789", 6))
	b.Break()
	col := 65
	for range 40 {
		b.Insert(Pos{Line: 0, Col: col}, "x")
		col++
	}
	if len(b.undo) != 2 || len(b.undo[1].ops) != 1 {
		t.Fatalf("typing did not merge: %d groups, %d ops in the last", len(b.undo), len(b.undo[1].ops))
	}
	// One 105-rune line is about 450 bytes; 40 of them would be 18000.
	if got := b.undo[1].cost; got > 2*(105*runeCost+lineCost+40) {
		t.Errorf("merged typing cost %d, want about one line", got)
	}
}

func TestVersionsFollowEditsUndoRedoAndRefusals(t *testing.T) {
	b := openIndexed(t, strings.Repeat("l", 70)+"\nshort\n") // first line is cut: not editable
	if b.Version() != 0 {
		t.Fatalf("fresh version %d", b.Version())
	}
	b.Insert(Pos{Line: 1}, "a")
	v1 := b.Version()
	b.Break()
	b.Insert(Pos{Line: 1}, "b")
	v2 := b.Version()
	if v1 != 1 || v2 != 2 {
		t.Fatalf("versions %d %d, want 1 2", v1, v2)
	}
	b.Insert(Pos{Line: 0, Col: 1}, "refused")
	if b.Version() != v2 || b.Refusal() == nil {
		t.Fatalf("refused edit: version %d", b.Version())
	}
	b.Undo()
	if b.Version() != v1 {
		t.Errorf("Undo version %d, want %d", b.Version(), v1)
	}
	b.Redo()
	if b.Version() != v2 {
		t.Errorf("Redo version %d, want %d", b.Version(), v2)
	}
	b.Break()
	b.Insert(Pos{Line: 1}, "c")
	if b.Version() != v2+1 {
		t.Errorf("next edit version %d, want %d (the refusal used none)", b.Version(), v2+1)
	}
}

func TestFirstLineProvisionalIsNotEditable(t *testing.T) {
	// 64 bytes of first line and no newline yet: the scan stops at MaxLine.
	data := strings.Repeat("d", 64) + "\nnext\n"
	b := tinyOpen(t, data)
	if !b.big.g.provisional() {
		t.Fatalf("test needs a provisional first line (indexed %d)", b.Index(0).Indexed)
	}
	if got := b.Insert(Pos{}, "X"); got != (Pos{}) || b.Refusal() == nil || b.Modified() {
		t.Errorf("edit of the provisional line: returned %v, modified %v", got, b.Modified())
	}
	if got := b.Delete(Pos{}, b.End()); got != "" || b.Refusal() == nil || b.Modified() {
		t.Errorf("delete of the provisional line: returned %q, modified %v", got, b.Modified())
	}
	indexFully(t, b)
	b.Insert(Pos{}, "X")
	if err := b.Refusal(); err != nil || b.Line(0) != "X"+data[:64] || b.Line(1) != "next" {
		t.Errorf("after indexing: refusal %v, lines %q %q", err, b.Line(0), b.Line(1))
	}
}

func TestEditInEmptySource(t *testing.T) {
	b := openIndexed(t, "")
	b.Insert(Pos{}, "hi\nthere")
	if b.Text() != "hi\nthere" || b.Empty() {
		t.Fatalf("Text = %q", b.Text())
	}
	b.Undo()
	if b.Text() != "" || !b.Empty() || b.Modified() {
		t.Errorf("after undo: %q empty %v modified %v", b.Text(), b.Empty(), b.Modified())
	}
	b.Redo()
	b.Delete(Pos{}, b.End())
	if b.Text() != "" || !b.Empty() || b.Lines() != 1 {
		t.Errorf("after delete all: %q empty %v lines %d", b.Text(), b.Empty(), b.Lines())
	}
}

func TestEditsKeepWorkingAsTheIndexGrows(t *testing.T) {
	var sb strings.Builder
	for i := range 40 {
		fmt.Fprintf(&sb, "row %02d\n", i)
	}
	b := tinyOpen(t, sb.String())
	if b.Index(0).Complete {
		t.Fatal("test needs a partial index")
	}
	seen := b.Lines()
	b.Insert(Pos{Line: seen - 1, Col: 0}, ">>")
	b.Delete(Pos{Line: 0}, Pos{Line: 1})
	indexFully(t, b)
	if b.Lines() != 40-1+1 {
		t.Errorf("Lines = %d after indexing", b.Lines())
	}
	if got := b.Line(seen - 2); got != ">>row "+fmt.Sprintf("%02d", seen-1) {
		t.Errorf("edited line = %q", got)
	}
	if got := b.Line(b.Lines() - 2); got != "row 39" {
		t.Errorf("last terminated line = %q", got)
	}
}

func TestProvisionalLineIsDecodedAgainWhenFound(t *testing.T) {
	// 63 characters and a CRLF: the provisional line, cut at MaxLine, still
	// holds the carriage return.
	line := strings.Repeat("c", 63)
	b := tinyOpen(t, line+"\r\nnext\r\n")
	if !b.big.g.provisional() {
		t.Fatalf("test needs a provisional first line (indexed %d)", b.Index(0).Indexed)
	}
	if got := b.Line(0); got != line+"\r" {
		t.Fatalf("provisional Line(0) = %q", got)
	}
	_ = b.LineLen(0) // decode and cache it
	indexFully(t, b)
	if got := b.Line(0); got != line || b.LineLen(0) != 63 {
		t.Errorf("Line(0) after the newline was found = %q (%d runes)", got, b.LineLen(0))
	}
}
