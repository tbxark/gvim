package buffer

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// seamCorpus is shared by the table tests: empty, one line, multi-line,
// multibyte, empty lines and a trailing newline.
var seamCorpus = []struct {
	name string
	text string
}{
	{"empty", ""},
	{"one line", "hello"},
	{"multi", "alpha\nbeta\ngamma"},
	{"multibyte", "héllo\n日本語\n😀 x"},
	{"empty lines", "\n\n\n"},
	{"trailing newline", "a\nb\n"},
}

func TestSeamsOnMemoryBuffer(t *testing.T) {
	t.Parallel()
	for _, c := range seamCorpus {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := New(c.text)
			if b.Large() {
				t.Error("Large() = true")
			}
			if err := b.Refusal(); err != nil {
				t.Errorf("Refusal() = %v", err)
			}
			if b.LineDirty(0) || b.LineDirty(b.Lines()-1) || b.LineDirty(-1) {
				t.Error("LineDirty reported true")
			}
			if b.SourceErr() != nil || b.CheckSource() != nil || b.SourceGrew() {
				t.Error("source state not clean")
			}
			if err1, err2 := b.Close(), b.Close(); err1 != nil || err2 != nil {
				t.Error("Close must be a repeatable no-op")
			}
			if got := b.Stats(); got != (Stats{}) {
				t.Errorf("Stats() = %+v, want zero", got)
			}
			want := IndexInfo{Lines: b.Lines(), ApproxLines: b.Lines(), Complete: true}
			if got := b.Index(80); got != want {
				t.Errorf("Index = %+v, want %+v", got, want)
			}
			if got, want := b.Empty(), c.text == ""; got != want {
				t.Errorf("Empty() = %v, want %v", got, want)
			}
		})
	}
}

func TestEmptyFollowsEdits(t *testing.T) {
	t.Parallel()
	b := New("")
	b.Insert(Pos{}, "x")
	if b.Empty() {
		t.Error("Empty() after insert")
	}
	b.Delete(Pos{}, b.End())
	if !b.Empty() {
		t.Error("not Empty() after deleting everything")
	}
	b.Insert(Pos{}, "\n")
	if b.Empty() {
		t.Error("a lone newline is not empty")
	}
}

func TestVersionFollowsEditsUndoAndRedo(t *testing.T) {
	t.Parallel()
	b := New("abc")
	v0 := b.Version()
	b.Insert(Pos{0, 3}, "d")
	v1 := b.Version()
	if v1 == v0 {
		t.Fatal("edit did not change Version")
	}
	b.Break()
	b.Insert(Pos{0, 0}, "z")
	v2 := b.Version()
	if v2 == v1 || v2 == v0 {
		t.Fatalf("versions not distinct: %d %d %d", v0, v1, v2)
	}
	b.Undo()
	if got := b.Version(); got != v1 {
		t.Errorf("after Undo Version = %d, want %d", got, v1)
	}
	b.Undo()
	if got := b.Version(); got != v0 {
		t.Errorf("after second Undo Version = %d, want %d", got, v0)
	}
	b.Redo()
	b.Redo()
	if got := b.Version(); got != v2 {
		t.Errorf("after Redo Version = %d, want %d", got, v2)
	}
	b.MarkSaved()
	if got := b.Version(); got != v2 {
		t.Errorf("MarkSaved changed Version to %d", got)
	}
}

func TestTextPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		max  int
		want string
	}{
		{"zero", "abc", 0, ""},
		{"negative", "abc", -5, ""},
		{"cut inside line", "abcdef", 3, "abc"},
		{"whole", "abc", 10, "abc"},
		{"exact", "abc", 3, "abc"},
		{"across newline", "ab\ncd", 4, "ab\nc"},
		{"stops at newline", "ab\ncd", 2, "ab"},
		{"newline fits", "ab\ncd", 3, "ab\n"},
		{"multibyte cut back", "aé日", 2, "a"},
		{"multibyte exact", "aé日", 3, "aé"},
		{"four byte cut back", "x😀", 4, "x"},
		{"empty buffer", "", 8, ""},
		{"empty lines", "\n\n\n", 2, "\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := New(tt.text).TextPrefix(tt.max)
			if got != tt.want {
				t.Errorf("TextPrefix(%d) = %q, want %q", tt.max, got, tt.want)
			}
		})
	}
	// Property: the prefix is a rune-aligned prefix of Text, never longer
	// than the limit, and as long as possible.
	for _, c := range seamCorpus {
		text := New(c.text).Text()
		for max := 0; max <= len(text)+2; max++ {
			got := New(c.text).TextPrefix(max)
			if !strings.HasPrefix(text, got) || len(got) > max || !utf8.ValidString(got) {
				t.Fatalf("%s: TextPrefix(%d) = %q is not a valid prefix of %q", c.name, max, got, text)
			}
			if rest := text[len(got):]; rest != "" {
				if _, n := utf8.DecodeRuneInString(rest); len(got)+n <= max {
					t.Fatalf("%s: TextPrefix(%d) = %q stops early", c.name, max, got)
				}
			}
		}
	}
}

func TestTextCheckedMemoryNeverErrors(t *testing.T) {
	t.Parallel()
	for _, c := range seamCorpus {
		b := New(c.text)
		got, err := b.TextChecked()
		if err != nil || got != b.Text() {
			t.Errorf("%s: TextChecked = %q, %v; want %q, nil", c.name, got, err, b.Text())
		}
		last := b.Lines() - 1
		for _, r := range []struct{ from, to Pos }{
			{Pos{}, b.End()},
			{b.End(), Pos{}},
			{Pos{0, 1}, Pos{last, 1}},
			{Pos{-3, -3}, Pos{99, 99}},
		} {
			got, err := b.RangeChecked(r.from, r.to)
			if err != nil || got != b.Range(r.from, r.to) {
				t.Errorf("%s: RangeChecked(%v, %v) = %q, %v; want %q, nil", c.name, r.from, r.to, got, err, b.Range(r.from, r.to))
			}
		}
	}
}

func TestLineSliceMatchesLineRunes(t *testing.T) {
	t.Parallel()
	for _, c := range seamCorpus {
		b := New(c.text)
		for i := -1; i <= b.Lines(); i++ {
			runes := []rune(b.Line(i))
			for col := -2; col <= len(runes)+2; col++ {
				for n := -1; n <= len(runes)+2; n++ {
					lo := min(max(col, 0), len(runes))
					hi := min(lo+max(n, 0), len(runes))
					want := string(runes[lo:hi])
					if got := b.LineSlice(i, col, n); got != want {
						t.Fatalf("%s: LineSlice(%d, %d, %d) = %q, want %q", c.name, i, col, n, got, want)
					}
				}
			}
		}
	}
	b := New("abc")
	if got := b.LineSlice(0, 1, int(^uint(0)>>1)); got != "bc" {
		t.Errorf("LineSlice with huge n = %q, want %q", got, "bc")
	}
}

func TestLineBytesUTF8(t *testing.T) {
	t.Parallel()
	for _, c := range seamCorpus {
		b := New(c.text)
		for i := -1; i <= b.Lines(); i++ {
			if got, want := b.LineBytes(i), len(b.Line(i)); got != want {
				t.Errorf("%s: LineBytes(%d) = %d, want %d", c.name, i, got, want)
			}
		}
	}
	tests := []struct {
		line string
		want int
	}{{"", 0}, {"abc", 3}, {"é", 2}, {"日本", 6}, {"😀", 4}}
	for _, tt := range tests {
		if got := New(tt.line).LineBytes(0); got != tt.want {
			t.Errorf("LineBytes(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
	// A rune that is not encodable (a lone surrogate) is stored as U+FFFD.
	b := New("")
	b.lines[0] = []rune{'a', 0xD800}
	if got, want := b.LineBytes(0), len(b.Line(0)); got != want {
		t.Errorf("LineBytes with surrogate = %d, want %d", got, want)
	}
}

func TestSnapshotBasics(t *testing.T) {
	t.Parallel()
	for _, c := range seamCorpus {
		b := New(c.text)
		b.Insert(b.End(), "!")
		s := b.Snapshot()
		if s.Version() != b.Version() {
			t.Errorf("%s: Version = %d, want %d", c.name, s.Version(), b.Version())
		}
		if s.Lines() != b.Lines() {
			t.Fatalf("%s: Lines = %d, want %d", c.name, s.Lines(), b.Lines())
		}
		for i := -1; i <= b.Lines(); i++ {
			if s.Line(i) != b.Line(i) {
				t.Errorf("%s: Line(%d) = %q, want %q", c.name, i, s.Line(i), b.Line(i))
			}
		}
		s.Release()
		s.Release() // idempotent
	}
}

func TestMemorySnapshotIsolatedFromEdits(t *testing.T) {
	t.Parallel()
	b := New("one\ntwo\nthree")
	s := b.Snapshot()
	want := b.Text()

	b.Insert(Pos{1, 1}, "X\nY\nZ")       // splits a line, shifting the line headers
	b.Delete(Pos{0, 1}, Pos{2, 2})       // joins lines in place
	b.Insert(b.End(), "\nlast")          // grows at the end
	b.Insert(Pos{0, 0}, "first\nsecond") // grows at the start
	b.Undo()
	b.Undo()
	b.Redo()

	_, n, got := s.Chunk(0, 1<<20)
	if n != 3 || got != want || s.Lines() != 3 {
		t.Fatalf("snapshot changed: Lines=%d n=%d text=%q, want %q", s.Lines(), n, got, want)
	}
	if b.Version() == s.Version() {
		t.Error("buffer and snapshot versions should differ after edits")
	}
}

func TestChunk(t *testing.T) {
	t.Parallel()
	s := New("aa\nbbb\nc\n日本").Snapshot()
	tests := []struct {
		name      string
		from, max int
		first, n  int
		text      string
	}{
		{"all", 0, 100, 0, 4, "aa\nbbb\nc\n日本"},
		{"first line only fits", 0, 2, 0, 1, "aa"},
		{"joined size counts newline", 0, 5, 0, 1, "aa"},
		{"exact fit", 0, 6, 0, 2, "aa\nbbb"},
		{"first line always whole", 1, 1, 1, 1, "bbb"},
		{"zero max still one line", 0, 0, 0, 1, "aa"},
		{"negative max still one line", 2, -4, 2, 1, "c"},
		{"from middle", 2, 100, 2, 2, "c\n日本"},
		{"last line", 3, 100, 3, 1, "日本"},
		{"multibyte counted in bytes", 2, 7, 2, 1, "c"},
		{"multibyte fits", 2, 8, 2, 2, "c\n日本"},
		{"at end", 4, 100, 4, 0, ""},
		{"past end", 9, 100, 4, 0, ""},
		{"negative from", -3, 100, 0, 4, "aa\nbbb\nc\n日本"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			first, n, text := s.Chunk(tt.from, tt.max)
			if first != tt.first || n != tt.n || text != tt.text {
				t.Errorf("Chunk(%d, %d) = %d, %d, %q; want %d, %d, %q",
					tt.from, tt.max, first, n, text, tt.first, tt.n, tt.text)
			}
		})
	}
}

func TestChunkWindowsConcatenateToText(t *testing.T) {
	t.Parallel()
	for _, c := range seamCorpus {
		b := New(c.text)
		s := b.Snapshot()
		for _, max := range []int{0, 1, 3, 7, 16, 1 << 20} {
			var parts []string
			for from := 0; ; {
				first, n, text := s.Chunk(from, max)
				if n == 0 {
					if first != s.Lines() {
						t.Fatalf("%s max=%d: end chunk first = %d, want %d", c.name, max, first, s.Lines())
					}
					break
				}
				if first != from {
					t.Fatalf("%s max=%d: first = %d, want %d", c.name, max, first, from)
				}
				if n > 1 && len(text) > max {
					t.Fatalf("%s max=%d: %d lines in %d bytes", c.name, max, n, len(text))
				}
				if got := strings.Count(text, "\n"); got != n-1 {
					t.Fatalf("%s max=%d: %d lines but %d newlines", c.name, max, n, got)
				}
				parts = append(parts, text)
				from += n
			}
			if got := strings.Join(parts, "\n"); got != b.Text() {
				t.Errorf("%s max=%d: windows join to %q, want %q", c.name, max, got, b.Text())
			}
		}
	}
}

func TestChunkBefore(t *testing.T) {
	t.Parallel()
	s := New("aa\nbbb\nc\n日本").Snapshot()
	tests := []struct {
		name     string
		to, max  int
		first, n int
		text     string
	}{
		{"all", 4, 100, 0, 4, "aa\nbbb\nc\n日本"},
		{"last line only", 4, 6, 3, 1, "日本"},
		{"two lines fit", 3, 6, 1, 2, "bbb\nc"},
		{"line always whole", 2, 1, 1, 1, "bbb"},
		{"zero max one line", 3, 0, 2, 1, "c"},
		{"to clamps to end", 99, 100, 0, 4, "aa\nbbb\nc\n日本"},
		{"to one", 1, 100, 0, 1, "aa"},
		{"to zero", 0, 100, 0, 0, ""},
		{"to negative", -2, 100, 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			first, n, text := s.ChunkBefore(tt.to, tt.max)
			if first != tt.first || n != tt.n || text != tt.text {
				t.Errorf("ChunkBefore(%d, %d) = %d, %d, %q; want %d, %d, %q",
					tt.to, tt.max, first, n, text, tt.first, tt.n, tt.text)
			}
		})
	}
}

func TestChunkBeforeMirrorsChunk(t *testing.T) {
	t.Parallel()
	for _, c := range seamCorpus {
		b := New(c.text)
		s := b.Snapshot()
		for _, max := range []int{0, 1, 3, 7, 16, 1 << 20} {
			var parts []string
			for to := s.Lines(); ; {
				first, n, text := s.ChunkBefore(to, max)
				if n == 0 {
					if to > 0 {
						t.Fatalf("%s max=%d: empty chunk before line %d", c.name, max, to)
					}
					break
				}
				if first+n != to {
					t.Fatalf("%s max=%d: chunk [%d,+%d) does not end at %d", c.name, max, first, n, to)
				}
				if n > 1 && len(text) > max {
					t.Fatalf("%s max=%d: %d lines in %d bytes", c.name, max, n, len(text))
				}
				parts = append([]string{text}, parts...)
				to = first
			}
			if got := strings.Join(parts, "\n"); got != b.Text() {
				t.Errorf("%s max=%d: backward windows join to %q, want %q", c.name, max, got, b.Text())
			}
		}
	}
}

// wordBoundsRef is WordBounds as it was before it read the line once: it asks
// the buffer for the class of every character.
func wordBoundsRef(b *Buffer, p Pos) (start, end Pos) {
	p = b.Clamp(p)
	if _, ok := b.RuneAt(p); !ok {
		if p.Col == 0 {
			return p, p
		}
		p.Col--
	}
	cls := b.ClassAt(p)
	start, end = p, Pos{Line: p.Line, Col: p.Col + 1}
	for start.Col > 0 && b.ClassAt(Pos{Line: p.Line, Col: start.Col - 1}) == cls {
		start.Col--
	}
	for end.Col < b.LineLen(p.Line) && b.ClassAt(end) == cls {
		end.Col++
	}
	return start, end
}

func TestWordBoundsMatchesPerRuneReference(t *testing.T) {
	t.Parallel()
	b := New("foo_bar  baz.qux\n\n  日本語 é1\n\t(x)\nend")
	for line := -1; line <= b.Lines(); line++ {
		for col := -1; col <= b.LineLen(line)+2; col++ {
			p := Pos{line, col}
			gs, ge := WordBounds(b, p)
			ws, we := wordBoundsRef(b, p)
			if gs != ws || ge != we {
				t.Fatalf("WordBounds(%v) = %v, %v; want %v, %v", p, gs, ge, ws, we)
			}
		}
	}
}

func TestMergeableRefusesBigOps(t *testing.T) {
	t.Parallel()
	small := op{kind: opInsert, from: Pos{0, 0}, text: "ab"}
	next := op{kind: opInsert, from: Pos{0, 2}, text: "c"}
	if !mergeable(&small, &next) {
		t.Fatal("plain adjacent inserts must merge")
	}
	big := next
	big.big = true
	if mergeable(&small, &big) {
		t.Error("a big next op merged")
	}
	prevBig := small
	prevBig.big = true
	if mergeable(&prevBig, &next) {
		t.Error("a big previous op merged")
	}
}

func TestSeamErrorsAreDistinct(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrOverCap, ErrSourceChanged, ErrSourceGrew, ErrLineTooLong,
		ErrInvalidUTF8, ErrOverlayFull, ErrIndexPending, ErrUnsupportedPlatform,
	}
	for i, a := range errs {
		if a == nil || a.Error() == "" {
			t.Fatalf("error %d is empty", i)
		}
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Errorf("errors %d and %d are not distinct", i, j)
			}
		}
	}
	if MaxMaterialize != 16<<20 || InlineOpMax != 256<<10 {
		t.Errorf("limits changed: MaxMaterialize=%d InlineOpMax=%d", MaxMaterialize, InlineOpMax)
	}
}
