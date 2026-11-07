package buffer_test

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// recorder collects the changes a buffer reports.
type recorder struct{ got []buffer.Change }

func (r *recorder) observe(b *buffer.Buffer) {
	b.SetObserver(func(c buffer.Change) { r.got = append(r.got, c) })
}

func (r *recorder) take() []buffer.Change {
	out := r.got
	r.got = nil
	return out
}

func TestObserverReportsExactChanges(t *testing.T) {
	t.Parallel()
	steps := []struct {
		name string
		do   func(b *buffer.Buffer)
		want []buffer.Change
	}{
		{
			name: "insert multibyte",
			do:   func(b *buffer.Buffer) { b.Insert(buffer.Pos{Line: 1, Col: 2}, "é日") },
			want: []buffer.Change{{
				From: buffer.Pos{Line: 1, Col: 2}, To: buffer.Pos{Line: 1, Col: 2},
				FromByte: 4, ToByte: 4, Text: "é日",
			}},
		},
		{
			name: "delete across lines",
			do:   func(b *buffer.Buffer) { b.Delete(buffer.Pos{Line: 1, Col: 1}, buffer.Pos{Line: 2, Col: 1}) },
			want: []buffer.Change{{
				From: buffer.Pos{Line: 1, Col: 1}, To: buffer.Pos{Line: 2, Col: 1},
				FromByte: 2, ToByte: 1, Removed: "éé\nx",
			}},
		},
		{
			name: "replace is delete then insert",
			do: func(b *buffer.Buffer) {
				b.Group(func() {
					b.Delete(buffer.Pos{Col: 1}, buffer.Pos{Col: 3})
					b.Insert(buffer.Pos{Col: 1}, "Z")
				})
			},
			want: []buffer.Change{
				{From: buffer.Pos{Col: 1}, To: buffer.Pos{Col: 3}, FromByte: 1, ToByte: 3, Removed: "bc"},
				{From: buffer.Pos{Col: 1}, To: buffer.Pos{Col: 1}, FromByte: 1, ToByte: 1, Text: "Z"},
			},
		},
		{
			name: "undo of a group runs newest first",
			do: func(b *buffer.Buffer) {
				b.Group(func() {
					b.Delete(buffer.Pos{Col: 1}, buffer.Pos{Col: 3})
					b.Insert(buffer.Pos{Col: 1}, "Z")
				})
				b.Undo()
			},
			want: []buffer.Change{
				{From: buffer.Pos{Col: 1}, To: buffer.Pos{Col: 3}, FromByte: 1, ToByte: 3, Removed: "bc"},
				{From: buffer.Pos{Col: 1}, To: buffer.Pos{Col: 1}, FromByte: 1, ToByte: 1, Text: "Z"},
				{From: buffer.Pos{Col: 1}, To: buffer.Pos{Col: 2}, FromByte: 1, ToByte: 2, Removed: "Z", Undo: true},
				{From: buffer.Pos{Col: 1}, To: buffer.Pos{Col: 1}, FromByte: 1, ToByte: 1, Text: "bc", Undo: true},
			},
		},
	}
	for _, s := range steps {
		b := buffer.New("abcd\nééé\nxy")
		var r recorder
		r.observe(b)
		s.do(b)
		got := r.take()
		for i := range got {
			got[i].Version = 0
		}
		if !slices.Equal(got, s.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", s.name, got, s.want)
		}
	}
}

func TestObserverVersionAndRedo(t *testing.T) {
	t.Parallel()
	b := buffer.New("ab")
	var r recorder
	r.observe(b)
	b.Insert(buffer.Pos{Col: 1}, "X")
	ver := b.Version()
	if len(r.got) != 1 || r.got[0].Version != ver {
		t.Fatalf("insert version = %+v, want %d", r.got, ver)
	}
	r.take()
	b.Undo()
	if len(r.got) != 1 || !r.got[0].Undo || r.got[0].Version != b.Version() {
		t.Fatalf("undo = %+v, buffer version %d", r.got, b.Version())
	}
	r.take()
	b.Redo()
	want := buffer.Change{From: buffer.Pos{Col: 1}, To: buffer.Pos{Col: 1}, FromByte: 1, ToByte: 1, Text: "X", Undo: true, Version: ver}
	if len(r.got) != 1 || r.got[0] != want {
		t.Fatalf("redo = %+v, want %+v", r.got, want)
	}
}

func TestObserverRemovedCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n    int
		keep bool
	}{{buffer.MaxRemoved, true}, {buffer.MaxRemoved + 1, false}} {
		b := buffer.New(strings.Repeat("x", tc.n))
		var r recorder
		r.observe(b)
		b.Delete(buffer.Pos{}, buffer.Pos{Col: tc.n})
		if len(r.got) != 1 || (r.got[0].Removed != "") != tc.keep {
			t.Errorf("delete of %d bytes: Removed has %d bytes, keep=%v", tc.n, len(r.got[0].Removed), tc.keep)
		}
	}
}

func TestObserverOffIsInert(t *testing.T) {
	b := buffer.New("abc")
	var r recorder
	r.observe(b)
	b.SetObserver(nil)
	b.Insert(buffer.Pos{}, "x")
	b.Undo()
	if len(r.got) != 0 {
		t.Fatalf("observer off still saw %d changes", len(r.got))
	}
	// Without an observer an edit builds no Change: it allocates no more than
	// the same edit does with an observer that keeps nothing.
	run := func(b *buffer.Buffer) float64 {
		return testing.AllocsPerRun(50, func() {
			b.Insert(buffer.Pos{Col: 1}, "q")
			b.Delete(buffer.Pos{Col: 1}, buffer.Pos{Col: 2})
		})
	}
	plain := buffer.New("abc")
	kept := buffer.New("abc")
	var sink []buffer.Change
	kept.SetObserver(func(c buffer.Change) { sink = append(sink[:0], c) })
	if a, b := run(plain), run(kept); a > b {
		t.Errorf("allocs without observer = %v, with = %v; want no more without", a, b)
	}
}

// mirror is a text kept in step with a buffer by applying its changes.
type mirror [][]rune

func newMirror(text string) mirror {
	var m mirror
	for _, l := range strings.Split(text, "\n") {
		m = append(m, []rune(l))
	}
	return m
}

func (m mirror) text() string {
	parts := make([]string, len(m))
	for i, l := range m {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// apply replaces c.From..c.To by c.Text, checking that the byte columns of the
// change agree with the mirror before it changes.
func (m *mirror) apply(t *testing.T, c buffer.Change) {
	t.Helper()
	col := func(p buffer.Pos) int {
		if p.Line >= len(*m) || p.Col > len((*m)[p.Line]) {
			t.Fatalf("change %+v is outside the mirror", c)
		}
		return len(string((*m)[p.Line][:p.Col]))
	}
	if col(c.From) != c.FromByte || col(c.To) != c.ToByte {
		t.Fatalf("change %+v: byte columns %d,%d, mirror says %d,%d", c, c.FromByte, c.ToByte, col(c.From), col(c.To))
	}
	if !utf8.ValidString(c.Text) {
		t.Fatalf("change %+v: Text is not valid UTF-8, the buffer holds runes", c)
	}
	// Text between From and To, to compare with Removed.
	cur := *m
	var sb strings.Builder
	for l := c.From.Line; l <= c.To.Line; l++ {
		lo, hi := 0, len(cur[l])
		if l == c.From.Line {
			lo = c.From.Col
		}
		if l == c.To.Line {
			hi = c.To.Col
		}
		sb.WriteString(string(cur[l][lo:hi]))
		if l < c.To.Line {
			sb.WriteByte('\n')
		}
	}
	if c.Removed != "" && c.Removed != sb.String() {
		t.Fatalf("change %+v: removed %q, mirror says %q", c, c.Removed, sb.String())
	}
	head := slices.Clone(cur[c.From.Line][:c.From.Col])
	tail := slices.Clone(cur[c.To.Line][c.To.Col:])
	ins := newMirror(c.Text)
	ins[0] = append(head, ins[0]...)
	ins[len(ins)-1] = append(ins[len(ins)-1], tail...)
	*m = slices.Concat(cur[:c.From.Line], ins, cur[c.To.Line+1:])
}

func TestChangeRecordsAreSequential(t *testing.T) {
	t.Parallel()
	pieces := []string{"a", "é", "\n", "日本", "xy\nz", "😀", "\n\n", " ", "word\ntail", "\xff", "a\xe6\x97\nb"}
	for seed := uint64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewPCG(seed, 99))
		text := "héllo\nwörld 日本\n\nlast line"
		b := buffer.New(text)
		m := newMirror(text)
		var r recorder
		r.observe(b)
		randPos := func() buffer.Pos {
			l := rng.IntN(b.Lines())
			return buffer.Pos{Line: l, Col: rng.IntN(b.LineLen(l) + 1)}
		}
		for step := 0; step < 120; step++ {
			switch rng.IntN(7) {
			case 0, 1:
				b.Insert(randPos(), pieces[rng.IntN(len(pieces))])
			case 2:
				b.Delete(randPos(), randPos())
			case 3:
				b.Undo()
			case 4:
				b.Redo()
			case 5:
				b.Group(func() {
					from := randPos()
					b.Delete(from, randPos())
					b.Insert(b.Clamp(from), pieces[rng.IntN(len(pieces))])
				})
			case 6:
				b.Break()
			}
			for _, c := range r.take() {
				m.apply(t, c)
			}
			if got, want := m.text(), b.Text(); got != want {
				t.Fatalf("seed %d step %d: mirror %q, buffer %q", seed, step, got, want)
			}
		}
	}
}

func TestChangeTextIsWhatTheBufferHolds(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"valid", "é日", "é日"},
		{"lone byte", "\xff", "\uFFFD"},
		{"each bad byte is one rune", "a\xff\xfeb", "a\uFFFD\uFFFDb"},
		{"truncated sequence", "\xe6\x97", "\uFFFD\uFFFD"},
	}
	for _, tt := range tests {
		b := buffer.New("ab\n")
		var r recorder
		r.observe(b)
		b.Insert(buffer.Pos{Col: 1}, tt.in)
		b.Break()
		b.Delete(buffer.Pos{Col: 1}, buffer.Pos{Col: 1 + len([]rune(tt.in))})
		b.Undo()
		got := r.take()
		if len(got) != 3 {
			t.Fatalf("%s: %d changes, want 3", tt.name, len(got))
		}
		if got[0].Text != tt.want {
			t.Errorf("%s: insert Text = %q, want %q", tt.name, got[0].Text, tt.want)
		}
		if got[1].Removed != tt.want {
			t.Errorf("%s: delete Removed = %q, want %q", tt.name, got[1].Removed, tt.want)
		}
		if got[2].Text != tt.want {
			t.Errorf("%s: undo Text = %q, want %q", tt.name, got[2].Text, tt.want)
		}
		if line := b.Line(0); got[2].ToByte != 1 || len(line) != 1+len(tt.want)+1 {
			t.Errorf("%s: line %q, undo change %+v", tt.name, line, got[2])
		}
	}
}

func TestLargeBufferReportsCoarseChanges(t *testing.T) {
	t.Parallel()
	b, _ := openTiny(t, []byte("one\ntwo\nthree\nfour\n"), 0)
	indexAll(t, b)
	var r recorder
	r.observe(b)
	b.Insert(buffer.Pos{Col: 1}, "X")
	b.Delete(buffer.Pos{Line: 1}, buffer.Pos{Line: 1, Col: 2})
	b.Undo()
	b.Redo()
	got := r.take()
	if len(got) != 4 {
		t.Fatalf("got %d changes, want 4: %+v", len(got), got)
	}
	for i, c := range got {
		want := buffer.Change{Coarse: true, Undo: i >= 2, Version: c.Version}
		if c != want {
			t.Errorf("change %d = %+v, want %+v", i, c, want)
		}
	}
	// Both edits are one undo group: the undo goes back to version 0 and the
	// redo to the version after the second edit.
	if got[0].Version == 0 || got[2].Version != 0 || got[3].Version != got[1].Version || got[3].Version != b.Version() {
		t.Errorf("versions = %d %d %d %d, buffer at %d", got[0].Version, got[1].Version, got[2].Version, got[3].Version, b.Version())
	}
}

func TestLargeRefusedGroupReportsCoarse(t *testing.T) {
	t.Parallel()
	// Line 1 holds bytes that are not UTF-8, so editing it is refused and the
	// group is dropped whole, including the edit on line 0 that was made.
	b, _ := openTiny(t, []byte("ok\n\xff\xfebad\nend\n"), 0)
	indexAll(t, b)
	var r recorder
	r.observe(b)
	before := b.Version()
	b.Group(func() {
		b.Insert(buffer.Pos{}, "X")
		b.Insert(buffer.Pos{Line: 1}, "Y")
	})
	if b.Version() != before || b.Line(0) != "ok" {
		t.Fatalf("group was not dropped: version %d->%d, line 0 %q", before, b.Version(), b.Line(0))
	}
	got := r.take()
	if len(got) < 2 || !got[len(got)-1].Coarse || got[len(got)-1].Version != before {
		t.Fatalf("changes = %+v, want a final coarse change at version %d", got, before)
	}
}

func TestByteColAndPosAtByteCol(t *testing.T) {
	t.Parallel()
	b := buffer.New("aé日😀z\n\nplain")
	rows := []struct {
		p       buffer.Pos
		byteCol int
	}{
		{buffer.Pos{Line: 0, Col: 0}, 0},
		{buffer.Pos{Line: 0, Col: 1}, 1},
		{buffer.Pos{Line: 0, Col: 2}, 3},
		{buffer.Pos{Line: 0, Col: 3}, 6},
		{buffer.Pos{Line: 0, Col: 4}, 10},
		{buffer.Pos{Line: 0, Col: 5}, 11},
		{buffer.Pos{Line: 1, Col: 0}, 0},
		{buffer.Pos{Line: 2, Col: 5}, 5},
	}
	for _, r := range rows {
		if got := b.ByteCol(r.p); got != r.byteCol {
			t.Errorf("ByteCol(%v) = %d, want %d", r.p, got, r.byteCol)
		}
		if got := b.PosAtByteCol(r.p.Line, r.byteCol); got != r.p {
			t.Errorf("PosAtByteCol(%d,%d) = %v, want %v", r.p.Line, r.byteCol, got, r.p)
		}
	}
	rounding := []struct {
		line, byteCol int
		want          buffer.Pos
	}{
		{0, 2, buffer.Pos{Line: 0, Col: 1}},   // inside é
		{0, 5, buffer.Pos{Line: 0, Col: 2}},   // inside 日
		{0, 8, buffer.Pos{Line: 0, Col: 3}},   // inside 😀
		{0, 99, buffer.Pos{Line: 0, Col: 5}},  // past the end
		{0, -4, buffer.Pos{Line: 0, Col: 0}},  // before the start
		{99, 3, buffer.Pos{Line: 2, Col: 3}},  // line clamps
		{-1, 99, buffer.Pos{Line: 0, Col: 5}}, // line clamps up
	}
	for _, r := range rounding {
		if got := b.PosAtByteCol(r.line, r.byteCol); got != r.want {
			t.Errorf("PosAtByteCol(%d,%d) = %v, want %v", r.line, r.byteCol, got, r.want)
		}
	}
	if got := b.ByteCol(buffer.Pos{Line: 0, Col: 99}); got != 11 {
		t.Errorf("ByteCol past the end = %d, want 11", got)
	}
}

func TestByteColOnInvalidUTF8Large(t *testing.T) {
	t.Parallel()
	// The second source line is "\xff\xfebad": each bad byte shows as U+FFFD
	// (3 bytes), and byte columns count that decoded text.
	b, _ := openTiny(t, []byte("ok\n\xff\xfebad\nend\n"), 0)
	indexAll(t, b)
	line := b.Line(1)
	if line != "\uFFFD\uFFFDbad" {
		t.Fatalf("line 1 = %q", line)
	}
	rows := []struct {
		col, byteCol int
	}{{0, 0}, {1, 3}, {2, 6}, {5, 9}}
	for _, r := range rows {
		if got := b.ByteCol(buffer.Pos{Line: 1, Col: r.col}); got != r.byteCol {
			t.Errorf("ByteCol(1,%d) = %d, want %d", r.col, got, r.byteCol)
		}
		if got := b.PosAtByteCol(1, r.byteCol); got != (buffer.Pos{Line: 1, Col: r.col}) {
			t.Errorf("PosAtByteCol(1,%d) = %v, want col %d", r.byteCol, got, r.col)
		}
	}
	if got := b.PosAtByteCol(1, 4); got != (buffer.Pos{Line: 1, Col: 1}) {
		t.Errorf("PosAtByteCol inside U+FFFD = %v, want col 1", got)
	}
	if _, err := b.ByteOffset(buffer.Pos{}); err != buffer.ErrLargeBuffer {
		t.Errorf("ByteOffset on a large buffer: err = %v, want ErrLargeBuffer", err)
	}
	if _, err := b.PosAtByteOffset(3); err != buffer.ErrLargeBuffer {
		t.Errorf("PosAtByteOffset on a large buffer: err = %v, want ErrLargeBuffer", err)
	}
}

func TestByteOffsetRoundTrip(t *testing.T) {
	t.Parallel()
	for seed := uint64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewPCG(seed, 5))
		pieces := []string{"a", "é", "\n", "日本語", "😀", "xyz", "\n\n"}
		var sb strings.Builder
		for range 60 {
			sb.WriteString(pieces[rng.IntN(len(pieces))])
		}
		b := buffer.New(sb.String())
		text := b.Text()
		for l := 0; l < b.Lines(); l++ {
			for c := 0; c <= b.LineLen(l); c++ {
				p := buffer.Pos{Line: l, Col: c}
				off, err := b.ByteOffset(p)
				if err != nil {
					t.Fatal(err)
				}
				if got := text[:off]; strings.Count(got, "\n") != l || !utf8.ValidString(got) {
					t.Fatalf("offset %d of %v is not at that position of %q", off, p, text)
				}
				back, err := b.PosAtByteOffset(off)
				if err != nil || back != p {
					t.Fatalf("PosAtByteOffset(ByteOffset(%v)) = %v, %v", p, back, err)
				}
			}
		}
		if p, _ := b.PosAtByteOffset(len(text) + 50); p != b.End() {
			t.Errorf("offset past the end = %v, want %v", p, b.End())
		}
		if p, _ := b.PosAtByteOffset(-5); p != (buffer.Pos{}) {
			t.Errorf("negative offset = %v, want the start", p)
		}
	}
}

func TestByteOffsetIncremental(t *testing.T) {
	t.Parallel()
	b := buffer.New(strings.Repeat("line é\n", 200) + "end")
	// Learn every start offset, then edit near the bottom and compare with a
	// fresh buffer, which has no cache, for every line.
	for l := 0; l < b.Lines(); l++ {
		if _, err := b.ByteOffset(buffer.Pos{Line: l}); err != nil {
			t.Fatal(err)
		}
	}
	edits := []func(){
		func() { b.Insert(buffer.Pos{Line: 150, Col: 2}, "日本\nxx") },
		func() { b.Delete(buffer.Pos{Line: 10, Col: 1}, buffer.Pos{Line: 12, Col: 3}) },
		func() { b.Undo() },
		func() { b.Break(); b.Insert(buffer.Pos{Line: 199, Col: 0}, "q") },
		func() { b.Undo(); b.Undo() },
		func() { b.Redo() },
	}
	for i, edit := range edits {
		edit()
		fresh := buffer.New(b.Text())
		for l := 0; l < b.Lines(); l += 7 {
			got, _ := b.ByteOffset(buffer.Pos{Line: l, Col: 1})
			want, _ := fresh.ByteOffset(buffer.Pos{Line: l, Col: 1})
			if got != want {
				t.Fatalf("after edit %d: offset of line %d = %d, want %d", i, l, got, want)
			}
		}
	}
}
