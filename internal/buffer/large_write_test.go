package buffer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/buffer/buffertest"
)

var errBoom = errors.New("boom")

// writeSnap streams a snapshot of b to memory.
func writeSnap(t testing.TB, b *buffer.Buffer, o buffer.WriteOptions) ([]byte, buffer.WriteResult, error) {
	t.Helper()
	s := b.Snapshot()
	defer s.Release()
	var out bytes.Buffer
	res, err := s.WriteTo(t.Context(), &out, o)
	return out.Bytes(), res, err
}

// checkWriteResult verifies the byte count and per-chunk newline counts of a
// write against the bytes themselves.
func checkWriteResult(t testing.TB, out []byte, res buffer.WriteResult, prefixLen int) {
	t.Helper()
	if res.Bytes != int64(len(out)) {
		t.Fatalf("Bytes = %d, wrote %d", res.Bytes, len(out))
	}
	if res.Chunk != tChunk {
		t.Fatalf("Chunk = %d, want %d", res.Chunk, tChunk)
	}
	text := out[prefixLen:]
	var want []int32
	for len(text) > 0 {
		n := min(len(text), tChunk)
		want = append(want, int32(bytes.Count(text[:n], []byte{'\n'})))
		text = text[n:]
	}
	if !slices.Equal(res.Counts, want) {
		t.Fatalf("Counts = %v, want %v", res.Counts, want)
	}
}

// uniformEOL reports the line ending of data when every terminator is the
// same, and no lone CR exists.
func uniformEOL(data []byte) (string, bool) {
	crlf, lf, cr := bytes.Count(data, []byte("\r\n")), bytes.Count(data, []byte("\n")), bytes.Count(data, []byte("\r"))
	switch {
	case cr != crlf:
		return "", false
	case crlf == 0:
		return "\n", true
	case crlf == lf:
		return "\r\n", true
	}
	return "", false
}

func TestLargeSnapshotReadsMatchMemory(t *testing.T) {
	for _, c := range corpora() {
		t.Run(c.name, func(t *testing.T) {
			b, _ := openTiny(t, c.data, c.skip)
			indexAll(t, b)
			want := wantLines(c.data, int(c.skip), tMaxLine)
			ms := buffer.New(strings.Join(want, "\n")).Snapshot()
			s := b.Snapshot()
			defer s.Release()
			if s.Lines() != len(want) || s.Version() != b.Version() {
				t.Fatalf("Lines/Version = %d/%d, want %d/%d", s.Lines(), s.Version(), len(want), b.Version())
			}
			for i := -1; i <= len(want); i++ {
				if got, w := s.Line(i), ms.Line(i); got != w {
					t.Fatalf("Line(%d) = %q, want %q", i, got, w)
				}
			}
			for _, maxBytes := range []int{1, 7, 20, 64, 1 << 20} {
				var parts []string
				for from := 0; ; {
					first, n, text := s.Chunk(from, maxBytes)
					mf, mn, mt := ms.Chunk(from, maxBytes)
					if first != mf || n != mn || text != mt {
						t.Fatalf("Chunk(%d, %d) = %d %d %q, memory %d %d %q", from, maxBytes, first, n, text, mf, mn, mt)
					}
					if n == 0 {
						break
					}
					parts = append(parts, text)
					from = first + n
				}
				if got := strings.Join(parts, "\n"); got != strings.Join(want, "\n") {
					t.Fatalf("Chunk windows (%d) join to %q", maxBytes, got)
				}
				parts = nil
				for to := len(want); ; {
					first, n, text := s.ChunkBefore(to, maxBytes)
					mf, mn, mt := ms.ChunkBefore(to, maxBytes)
					if first != mf || n != mn || text != mt {
						t.Fatalf("ChunkBefore(%d, %d) = %d %d %q, memory %d %d %q", to, maxBytes, first, n, text, mf, mn, mt)
					}
					if n == 0 {
						break
					}
					parts = append([]string{text}, parts...)
					to = first
				}
				if got := strings.Join(parts, "\n"); got != strings.Join(want, "\n") {
					t.Fatalf("ChunkBefore windows (%d) join to %q", maxBytes, got)
				}
			}
			if s.Err() != nil {
				t.Fatalf("Err = %v", s.Err())
			}
		})
	}
}

func TestLargeSnapshotIsIsolatedAndCheap(t *testing.T) {
	data := lines("line %02d\n", 30)
	b, _ := openTiny(t, []byte(data), 0)
	b.SetClock(fixedClock)
	indexAll(t, b)
	read := b.Stats().ReadBytes
	s := b.Snapshot()
	defer s.Release()
	if got := b.Stats().ReadBytes; got != read || b.Stats().MaterializedBytes != 0 {
		t.Fatalf("Snapshot read the source: %d bytes, materialized %d", got-read, b.Stats().MaterializedBytes)
	}
	before := b.Text()
	ver := b.Version()
	b.Insert(buffer.Pos{Line: 2, Col: 1}, "XX\nYY")
	b.Break()
	b.Delete(buffer.Pos{Line: 5}, buffer.Pos{Line: 9, Col: 3})
	b.Undo()
	b.Undo()
	b.Insert(buffer.Pos{}, "again")
	if s.Version() != ver {
		t.Fatalf("snapshot Version = %d, want %d", s.Version(), ver)
	}
	var parts []string
	for from := 0; ; {
		first, n, text := s.Chunk(from, 50)
		if n == 0 {
			break
		}
		parts = append(parts, text)
		from = first + n
	}
	if got := strings.Join(parts, "\n"); got != before {
		t.Fatalf("snapshot changed after edits:\n%q\nwant\n%q", got, before)
	}
}

func TestLargeWriteUneditedIsByteIdentical(t *testing.T) {
	for _, c := range corpora() {
		t.Run(c.name, func(t *testing.T) {
			b, _ := openTiny(t, c.data, c.skip)
			indexAll(t, b)
			read := b.Stats().ReadBytes
			prefix := c.data[:c.skip]
			out, res, err := writeSnap(t, b, buffer.WriteOptions{EOL: "\n", Prefix: prefix})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, c.data) {
				t.Fatalf("output %q, want %q", out, c.data)
			}
			checkWriteResult(t, out, res, len(prefix))
			// The runs are copied once; the offset lookups add a few pages.
			if got := b.Stats().ReadBytes - read; got > int64(len(c.data)+4*tChunk) {
				t.Fatalf("write read %d bytes of a %d byte file", got, len(c.data))
			}
			if b.Modified() {
				t.Fatal("WriteTo marked the buffer modified")
			}
		})
	}
}

func TestLargeWriteMatchesMemoryAfterEdits(t *testing.T) {
	var cs []corpus
	for _, c := range editCorpora() {
		if _, ok := uniformEOL(c.data); ok && c.skip == 0 {
			cs = append(cs, c)
		}
	}
	for seq := range 120 {
		c := cs[seq%len(cs)]
		t.Run(fmt.Sprintf("%03d %s", seq, c.name), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(uint64(seq), 7))
			eol, _ := uniformEOL(c.data)
			b, _ := openTiny(t, c.data, 0)
			b.SetClock(fixedClock)
			indexAll(t, b)
			m := buffer.New(strings.Join(wantLines(c.data, 0, tMaxLine), "\n"))
			m.SetClock(fixedClock)
			pos := func() buffer.Pos {
				line := rng.IntN(m.Lines())
				return buffer.Pos{Line: line, Col: rng.IntN(m.LineLen(line) + 2)}
			}
			both := func(f func(*buffer.Buffer)) { f(b); f(m) }
			for step := range 60 {
				switch r := rng.IntN(100); {
				case r < 35:
					p, s := pos(), diffTexts[rng.IntN(len(diffTexts))]
					both(func(x *buffer.Buffer) { x.Insert(p, s) })
				case r < 60:
					p, q := pos(), pos()
					both(func(x *buffer.Buffer) { x.Delete(p, q) })
				case r < 70:
					p, s, q := pos(), diffTexts[rng.IntN(len(diffTexts))], pos()
					both(func(x *buffer.Buffer) {
						x.Group(func() {
							x.Delete(p, q)
							x.Insert(x.Clamp(p), s)
						})
					})
				case r < 80:
					both(func(x *buffer.Buffer) { x.Undo() })
				case r < 90:
					both(func(x *buffer.Buffer) { x.Redo() })
				default:
					both(func(x *buffer.Buffer) { x.Break() })
				}
				out, res, err := writeSnap(t, b, buffer.WriteOptions{EOL: eol})
				if err != nil {
					t.Fatalf("step %d: %v", step, err)
				}
				if want := strings.ReplaceAll(m.Text(), "\n", eol); string(out) != want {
					t.Fatalf("step %d: output %q, want %q", step, out, want)
				}
				checkWriteResult(t, out, res, 0)
			}
		})
	}
}

// edit helpers for the mixed-ending tests.
func ins(b *buffer.Buffer, line, col int, s string) { b.Insert(buffer.Pos{Line: line, Col: col}, s) }

func TestLargeWritePerLineEOL(t *testing.T) {
	tests := []struct {
		name string
		data string
		eol  string
		edit func(b *buffer.Buffer)
		want string
	}{
		{"unedited", "a\nb\r\nc\nd", "\n", func(*buffer.Buffer) {}, "a\nb\r\nc\nd"},
		{"edit a CRLF line in an LF head", "a\nb\nc\r\nd\r\ne\r\nf", "\n",
			func(b *buffer.Buffer) { ins(b, 3, 1, "X") }, "a\nb\nc\r\ndX\r\ne\r\nf"},
		{"edit an LF line in a CRLF head", "a\r\nb\r\nc\nd\ne", "\r\n",
			func(b *buffer.Buffer) { ins(b, 3, 1, "X") }, "a\r\nb\r\nc\ndX\ne"},
		{"enter in a CRLF region", "a\nb\nc\r\nd\r\ne", "\n",
			func(b *buffer.Buffer) { ins(b, 2, 1, "\nnew") }, "a\nb\nc\r\nnew\r\nd\r\ne"},
		{"enter in an LF region", "a\r\nb\nc\nd", "\r\n",
			func(b *buffer.Buffer) { ins(b, 1, 1, "\nnew") }, "a\r\nb\nnew\nc\nd"},
		{"enter at the unterminated last line takes the previous terminator", "a\r\nb", "\n",
			func(b *buffer.Buffer) { ins(b, 1, 1, "\nc") }, "a\r\nb\r\nc"},
		{"single line gets the document EOL", "x", "\r\n",
			func(b *buffer.Buffer) { ins(b, 0, 1, "\ny") }, "x\r\ny"},
		{"single line gets the document EOL (LF)", "x", "\n",
			func(b *buffer.Buffer) { ins(b, 0, 1, "\ny") }, "x\ny"},
		{"join keeps the tail terminator", "a\nb\r\nc", "\n",
			func(b *buffer.Buffer) { b.Delete(buffer.Pos{Line: 0, Col: 1}, buffer.Pos{Line: 1, Col: 0}) }, "ab\r\nc"},
		{"delete the last line break", "a\nb\r\nc", "\n",
			func(b *buffer.Buffer) { b.Delete(buffer.Pos{Line: 1, Col: 1}, buffer.Pos{Line: 2, Col: 0}) }, "a\nbc"},
		{"lone CR and invalid bytes survive around an edit", "x\ry\n\xff\xfe\r\nlast", "\n",
			func(b *buffer.Buffer) { ins(b, 2, 4, "!") }, "x\ry\n\xff\xfe\r\nlast!"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := openTiny(t, []byte(tc.data), 0)
			indexAll(t, b)
			tc.edit(b)
			out, res, err := writeSnap(t, b, buffer.WriteOptions{EOL: tc.eol})
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.want {
				t.Fatalf("output %q, want %q", out, tc.want)
			}
			checkWriteResult(t, out, res, 0)
		})
	}
}

func TestLargeWriteHugeLineStaysVerbatim(t *testing.T) {
	huge := strings.Repeat("h", 200)
	data := "first\n" + huge + "\r\nlast\r\n"
	b, _ := openTiny(t, []byte(data), 0)
	indexAll(t, b)
	ins(b, 0, 5, "!")
	ins(b, 2, 0, ">")
	out, _, err := writeSnap(t, b, buffer.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "first!\n" + huge + "\r\n>last\r\n"; string(out) != want {
		t.Fatalf("output %q, want %q", out, want)
	}
}

func TestLargeWriteNeedsCompleteIndex(t *testing.T) {
	b, _ := openTiny(t, []byte(lines("alpha line %02d\n", 24)), 0)
	if b.Index(0).Complete {
		t.Fatal("test source indexed at open")
	}
	out, _, err := writeSnap(t, b, buffer.WriteOptions{Prefix: []byte("BOM")})
	if !errors.Is(err, buffer.ErrIndexPending) || len(out) != 0 {
		t.Fatalf("WriteTo = %q, %v; want nothing and ErrIndexPending", out, err)
	}
	indexAll(t, b)
	if _, _, err := writeSnap(t, b, buffer.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestMemorySnapshotWriteToRefuses(t *testing.T) {
	s := buffer.New("a\nb").Snapshot()
	var out bytes.Buffer
	if _, err := s.WriteTo(t.Context(), &out, buffer.WriteOptions{}); err == nil || out.Len() != 0 {
		t.Fatalf("WriteTo = %v, wrote %q; want an error", err, out.Bytes())
	}
}

// failWriter fails after n bytes; short makes it report a short write instead.
type failWriter struct {
	n     int
	short bool
	wrote int
}

func (w *failWriter) Write(p []byte) (int, error) {
	if w.wrote+len(p) <= w.n {
		w.wrote += len(p)
		return len(p), nil
	}
	k := w.n - w.wrote
	w.wrote = w.n
	if w.short {
		return k, nil
	}
	return k, errBoom
}

func TestLargeWriteFailingWriter(t *testing.T) {
	data := lines("alpha line %02d\n", 24)
	for _, tc := range []struct {
		name    string
		n       int
		short   bool
		edit    bool
		prefix  string
		wantErr error
	}{
		{"first write", 0, false, false, "", errBoom},
		{"inside a run", 100, false, false, "", errBoom},
		{"short write inside a run", 100, true, false, "", io.ErrShortWrite},
		{"inside literal lines", 40, false, true, "", errBoom},
		{"on the prefix", 1, false, false, "BOM", errBoom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := openTiny(t, []byte(data), 0)
			indexAll(t, b)
			if tc.edit {
				b.Delete(buffer.Pos{}, b.End())
				b.Insert(buffer.Pos{}, data+data)
			}
			modified := b.Modified()
			s := b.Snapshot()
			defer s.Release()
			_, err := s.WriteTo(t.Context(), &failWriter{n: tc.n, short: tc.short}, buffer.WriteOptions{Prefix: []byte(tc.prefix)})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("WriteTo error = %v, want %v", err, tc.wantErr)
			}
			if b.Modified() != modified || b.Refusal() != nil {
				t.Fatal("a failed write changed the buffer")
			}
			// The snapshot is still good.
			if _, err := s.WriteTo(t.Context(), io.Discard, buffer.WriteOptions{}); err != nil {
				t.Fatalf("second WriteTo: %v", err)
			}
		})
	}
}

// hookWriter calls hook on its first Write.
type hookWriter struct {
	bytes.Buffer
	hook func()
}

func (w *hookWriter) Write(p []byte) (int, error) {
	if w.hook != nil {
		w.hook()
		w.hook = nil
	}
	return w.Buffer.Write(p)
}

func TestLargeWriteChecksTheSource(t *testing.T) {
	data := lines("alpha line %02d\n", 24)
	open := func(t *testing.T) (*buffer.Buffer, *buffertest.MemSource) {
		b, src := openTiny(t, []byte(data), 0)
		indexAll(t, b)
		return b, src
	}
	t.Run("changed before", func(t *testing.T) {
		b, src := open(t)
		src.Touch()
		out, _, err := writeSnap(t, b, buffer.WriteOptions{Prefix: []byte("BOM")})
		if !errors.Is(err, buffer.ErrSourceChanged) || len(out) != 0 {
			t.Fatalf("WriteTo = %q, %v", out, err)
		}
	})
	t.Run("grown", func(t *testing.T) {
		b, src := open(t)
		src.Grow([]byte("appended\n"))
		out, _, err := writeSnap(t, b, buffer.WriteOptions{})
		if !errors.Is(err, buffer.ErrSourceGrew) || len(out) != 0 {
			t.Fatalf("WriteTo = %q, %v", out, err)
		}
		out, res, err := writeSnap(t, b, buffer.WriteOptions{AllowGrown: true})
		if err != nil || string(out) != data {
			t.Fatalf("AllowGrown WriteTo = %q, %v; want the first %d bytes", out, err, len(data))
		}
		checkWriteResult(t, out, res, 0)
	})
	t.Run("changed during", func(t *testing.T) {
		b, src := open(t)
		s := b.Snapshot()
		defer s.Release()
		w := &hookWriter{hook: src.Touch}
		if _, err := s.WriteTo(t.Context(), w, buffer.WriteOptions{}); !errors.Is(err, buffer.ErrSourceChanged) {
			t.Fatalf("WriteTo error = %v, want ErrSourceChanged", err)
		}
	})
	t.Run("truncated during", func(t *testing.T) {
		b, src := open(t)
		s := b.Snapshot()
		defer s.Release()
		w := &hookWriter{hook: func() { src.Truncate(40) }}
		if _, err := s.WriteTo(t.Context(), w, buffer.WriteOptions{}); !errors.Is(err, buffer.ErrSourceChanged) {
			t.Fatalf("WriteTo error = %v, want ErrSourceChanged", err)
		}
	})
	t.Run("frozen buffer", func(t *testing.T) {
		b, src := open(t)
		src.Touch()
		if err := b.CheckSource(); !errors.Is(err, buffer.ErrSourceChanged) {
			t.Fatalf("CheckSource = %v", err)
		}
		if _, _, err := writeSnap(t, b, buffer.WriteOptions{}); !errors.Is(err, buffer.ErrSourceChanged) {
			t.Fatalf("WriteTo error = %v, want ErrSourceChanged", err)
		}
	})
}

// cancelWriter cancels a context once n bytes were written.
type cancelWriter struct {
	bytes.Buffer
	n      int
	cancel context.CancelFunc
}

func (w *cancelWriter) Write(p []byte) (int, error) {
	if w.Len() >= w.n {
		w.cancel()
	}
	return w.Buffer.Write(p)
}

func TestLargeWriteCancelMidStream(t *testing.T) {
	buffer.SetWriteStep(t, 32)
	data := lines("alpha line %02d\n", 200)
	for _, edited := range []bool{false, true} {
		t.Run(fmt.Sprintf("edited=%v", edited), func(t *testing.T) {
			b, _ := openTiny(t, []byte(data), 0)
			indexAll(t, b)
			if edited {
				b.Delete(buffer.Pos{}, b.End())
				b.Insert(buffer.Pos{}, data)
			}
			s := b.Snapshot()
			defer s.Release()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w := &cancelWriter{n: 100, cancel: cancel}
			_, err := s.WriteTo(ctx, w, buffer.WriteOptions{})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("WriteTo error = %v, want context.Canceled", err)
			}
			if w.Len() > 100+3*32 {
				t.Fatalf("wrote %d bytes after the cancel at 100", w.Len())
			}
			// A context that is already canceled writes nothing.
			var out bytes.Buffer
			if _, err := s.WriteTo(ctx, &out, buffer.WriteOptions{}); !errors.Is(err, context.Canceled) || out.Len() != 0 {
				t.Fatalf("WriteTo on a canceled context = %d bytes, %v", out.Len(), err)
			}
		})
	}
}

func TestLargeWriteInSmallSteps(t *testing.T) {
	buffer.SetWriteStep(t, 5)
	data := "a\r\nbb\nccc\r\n" + strings.Repeat("d", 40) + "\n"
	b, _ := openTiny(t, []byte(data), 0)
	indexAll(t, b)
	ins(b, 1, 1, "é\nx")
	out, res, err := writeSnap(t, b, buffer.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a\r\nbé\nxb\nccc\r\n" + strings.Repeat("d", 40) + "\n"; string(out) != want {
		t.Fatalf("output %q, want %q", out, want)
	}
	checkWriteResult(t, out, res, 0)
}

// rebased is a buffer that was edited, written and rebased onto the output.
type rebased struct {
	b      *buffer.Buffer
	src    *buffertest.MemSource // the old source
	newSrc *buffertest.MemSource
	out    []byte
	res    buffer.WriteResult
	ver    uint64
	prefix []byte
	read0  int64
}

func prepareRebase(t *testing.T, data []byte, skip int64, edit func(*buffer.Buffer)) rebased {
	t.Helper()
	b, src := openTiny(t, data, skip)
	b.SetClock(fixedClock)
	indexAll(t, b)
	edit(b)
	prefix := data[:skip]
	out, res, err := writeSnap(t, b, buffer.WriteOptions{Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	return rebased{
		b: b, src: src, out: out, res: res, ver: b.Version(), prefix: prefix,
		newSrc: buffertest.NewMemSource(out), read0: b.Stats().ReadBytes,
	}
}

func TestLargeRebase(t *testing.T) {
	tests := []struct {
		name string
		data string
		skip int64
		edit func(b *buffer.Buffer)
	}{
		{"edited", lines("alpha line %02d\n", 24), 0, func(b *buffer.Buffer) {
			ins(b, 3, 2, "XY\nZ")
			b.Break()
			b.Delete(buffer.Pos{Line: 10}, buffer.Pos{Line: 14, Col: 3})
		}},
		{"crlf", lines("crlf line %02d\r\n", 12), 0, func(b *buffer.Buffer) { ins(b, 4, 0, "new\nlines\n") }},
		{"bom", "\xef\xbb\xbf" + lines("bom line %02d\n", 12), 3, func(b *buffer.Buffer) { ins(b, 1, 3, "é日") }},
		{"deleted everything", lines("alpha line %02d\n", 6), 0, func(b *buffer.Buffer) { b.Delete(buffer.Pos{}, b.End()) }},
		{"pasted a very long line", lines("short %02d\n", 6), 0, func(b *buffer.Buffer) { ins(b, 2, 0, strings.Repeat("L", 150)) }},
		{"no trailing newline", "one\ntwo\nthree", 0, func(b *buffer.Buffer) { ins(b, 2, 5, "!") }},
		{"unedited", lines("alpha line %02d\n", 24), 0, func(*buffer.Buffer) {}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := prepareRebase(t, []byte(tc.data), int64(tc.skip), tc.edit)
			b := r.b
			want := b.Text()
			r.read0 = b.Stats().ReadBytes
			if err := b.Rebase(r.newSrc, r.res, r.ver, int64(len(r.prefix))); err != nil {
				t.Fatal(err)
			}
			if got := b.Stats().ReadBytes - r.read0; got > 3*tChunk {
				t.Fatalf("Rebase read %d bytes, want at most 3 chunks", got)
			}
			// The same grid as a scan of the written bytes.
			fresh, _ := openTiny(t, r.out, int64(len(r.prefix)))
			indexAll(t, fresh)
			if !slices.Equal(b.GridNL(), fresh.GridNL()) || b.GridSkip() != fresh.GridSkip() {
				t.Fatalf("grid %v skip %d, fresh scan %v skip %d", b.GridNL(), b.GridSkip(), fresh.GridNL(), fresh.GridSkip())
			}
			if got, w := b.Index(0), fresh.Index(0); got != w {
				t.Fatalf("Index = %+v, fresh scan %+v", got, w)
			}
			// Text is what the written bytes read as; a line pasted over MaxLine
			// is cut when read back.
			if got := b.Text(); got != fresh.Text() || got != want && !fresh.Index(0).HugeLine {
				t.Fatalf("Text = %q, before %q, fresh %q", got, want, fresh.Text())
			}
			if tc.name == "pasted a very long line" && !b.Index(0).HugeLine {
				t.Fatal("a pasted line over MaxLine is not reported as huge after Rebase")
			}
			if b.Modified() || b.Version() != r.ver || b.Stats().Pieces != 0 || b.Stats().HistoryGroups != 0 {
				t.Fatalf("after Rebase: Modified %v, Version %d (want %d), %+v", b.Modified(), b.Version(), r.ver, b.Stats())
			}
			if _, ok := b.Undo(); ok {
				t.Fatal("Undo has history after Rebase")
			}
			if _, ok := b.Redo(); ok {
				t.Fatal("Redo has history after Rebase")
			}
			if !r.src.Closed() || r.newSrc.Closed() {
				t.Fatalf("old source closed %v, new source closed %v", r.src.Closed(), r.newSrc.Closed())
			}
			// A second save needs no rescan and gives the same bytes.
			read := b.Stats().ReadBytes
			out2, res2, err := writeSnap(t, b, buffer.WriteOptions{Prefix: r.prefix})
			if err != nil || !bytes.Equal(out2, r.out) {
				t.Fatalf("second write = %q, %v; want %q", out2, err, r.out)
			}
			checkWriteResult(t, out2, res2, len(r.prefix))
			if got := b.Stats().ReadBytes - read; got > int64(len(r.out)+4*tChunk) {
				t.Fatalf("second write read %d bytes of a %d byte file", got, len(r.out))
			}
			// Editing goes on from the new file, and undo reaches only that.
			want = b.Text()
			ins(b, 0, 0, "Z")
			if !b.Modified() || b.Text() != "Z"+want {
				t.Fatalf("edit after Rebase: %q", b.Text())
			}
			if _, ok := b.Undo(); !ok || b.Text() != want || b.Modified() {
				t.Fatalf("undo after Rebase: %q modified %v", b.Text(), b.Modified())
			}
			if _, ok := b.Undo(); ok {
				t.Fatal("undo went past the Rebase")
			}
		})
	}
}

func TestLargeRebaseRefusals(t *testing.T) {
	data := lines("alpha line %02d\n", 24)
	edit := func(b *buffer.Buffer) { ins(b, 3, 2, "XY\nZ") }
	type fixture struct {
		r    rebased
		src  buffer.Source
		res  buffer.WriteResult
		ver  uint64
		plen int64
	}
	tests := []struct {
		name  string
		setup func(f *fixture)
		// wantErr: nil means any error that is not ErrRebaseMismatch.
		wantErr error
	}{
		{"stale version", func(f *fixture) { ins(f.r.b, 0, 0, "late") }, buffer.ErrRebaseMismatch},
		{"size", func(f *fixture) { f.src = buffertest.NewMemSource(append(slices.Clone(f.r.out), 'x')) }, buffer.ErrRebaseMismatch},
		{"missing counts", func(f *fixture) { f.res.Counts = f.res.Counts[1:] }, buffer.ErrRebaseMismatch},
		{"bad chunk", func(f *fixture) { f.res.Chunk = 32 }, buffer.ErrRebaseMismatch},
		{"prefix longer than the file", func(f *fixture) { f.plen = f.res.Bytes + 1 }, buffer.ErrRebaseMismatch},
		{"wrong count in the first chunk", func(f *fixture) { f.res.Counts[0]++ }, buffer.ErrRebaseMismatch},
		{"wrong count in the middle chunk", func(f *fixture) { f.res.Counts[len(f.res.Counts)/2]-- }, buffer.ErrRebaseMismatch},
		{"wrong count in the last chunk", func(f *fixture) { f.res.Counts[len(f.res.Counts)-1]++ }, buffer.ErrRebaseMismatch},
		{"a different file of equal size", func(f *fixture) {
			other := bytes.ReplaceAll(f.r.out, []byte("alpha"), []byte("a\nph\n"))
			f.src = buffertest.NewMemSource(other)
		}, buffer.ErrRebaseMismatch},
		{"unreadable file", func(f *fixture) {
			m := buffertest.NewMemSource(f.r.out)
			m.FailNext(1, errBoom)
			f.src = m
		}, errBoom},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := prepareRebase(t, []byte(data), 0, edit)
			f := fixture{r: r, src: r.newSrc, res: r.res, ver: r.ver}
			f.res.Counts = slices.Clone(r.res.Counts)
			tc.setup(&f)
			b := r.b
			text, modified, stats := b.Text(), b.Modified(), b.Stats()
			err := b.Rebase(f.src, f.res, f.ver, f.plen)
			if err == nil || !errors.Is(err, tc.wantErr) {
				t.Fatalf("Rebase error = %v, want %v", err, tc.wantErr)
			}
			if r.src.Closed() {
				t.Fatal("a refused Rebase closed the old source")
			}
			if c, ok := f.src.(*buffertest.MemSource); ok && c.Closed() {
				t.Fatal("a refused Rebase closed the new source")
			}
			after := b.Stats()
			if b.Text() != text || b.Modified() != modified && tc.name != "stale version" ||
				after.Pieces != stats.Pieces || after.HistoryGroups != stats.HistoryGroups {
				t.Fatalf("a refused Rebase changed the buffer: %+v -> %+v", stats, after)
			}
			// The old source and the history still work.
			if _, ok := b.Undo(); !ok {
				t.Fatal("history lost")
			}
			if _, _, err := writeSnap(t, b, buffer.WriteOptions{}); err != nil {
				t.Fatalf("write after a refused Rebase: %v", err)
			}
		})
	}
}

func TestLargeRebaseInsideGroupIsRefused(t *testing.T) {
	r := prepareRebase(t, []byte(lines("alpha line %02d\n", 24)), 0, func(b *buffer.Buffer) { ins(b, 0, 0, "x") })
	var err error
	r.b.Group(func() { err = r.b.Rebase(r.newSrc, r.res, r.ver, 0) })
	if !errors.Is(err, buffer.ErrRebaseMismatch) {
		t.Fatalf("Rebase inside Group = %v", err)
	}
}

func TestLargeRebaseOfAMemoryBufferFails(t *testing.T) {
	b := buffer.New("a")
	if err := b.Rebase(buffertest.NewMemSource([]byte("a")), buffer.WriteResult{}, b.Version(), 0); err == nil {
		t.Fatal("Rebase succeeded on a memory buffer")
	}
}

func TestLargeSnapshotOutlivesRebaseAndClose(t *testing.T) {
	r := prepareRebase(t, []byte(lines("alpha line %02d\n", 24)), 0, func(b *buffer.Buffer) { ins(b, 3, 2, "XY\nZ") })
	b := r.b
	// The snapshot was taken before the rebase and keeps the old source open.
	old := b.Snapshot()
	oldText := b.Text()
	// Unrelated pages are cached and the snapshot reads through them.
	if _, n, _ := old.Chunk(0, 40); n == 0 {
		t.Fatal("empty chunk")
	}
	if err := b.Rebase(r.newSrc, r.res, r.ver, 0); err != nil {
		t.Fatal(err)
	}
	if r.src.Closed() {
		t.Fatal("old source closed while a snapshot holds it")
	}
	var parts []string
	for from := 0; ; {
		first, n, text := old.Chunk(from, 30)
		if n == 0 {
			break
		}
		parts = append(parts, text)
		from = first + n
	}
	if got := strings.Join(parts, "\n"); got != oldText || old.Err() != nil {
		t.Fatalf("snapshot after Rebase = %q (err %v), want %q", got, old.Err(), oldText)
	}
	if _, _, err := func() ([]byte, buffer.WriteResult, error) {
		var out bytes.Buffer
		res, err := old.WriteTo(t.Context(), &out, buffer.WriteOptions{})
		return out.Bytes(), res, err
	}(); err != nil {
		t.Fatal(err)
	}
	old.Release()
	old.Release() // idempotent
	if !r.src.Closed() {
		t.Fatal("old source still open after the last release")
	}
	if st := b.Stats(); st.CacheBytes > int64(b.PageCap())*tChunk {
		t.Fatalf("retired pages stay counted: %d cache bytes, %d pages allowed", st.CacheBytes, b.PageCap())
	}

	// Closing the buffer keeps a snapshot's source open too.
	snap := b.Snapshot()
	_ = b.Close()
	if r.newSrc.Closed() {
		t.Fatal("source closed while a snapshot holds it")
	}
	if got := snap.Line(0); got != "alpha line 00" || snap.Err() != nil {
		t.Fatalf("snapshot after Close: Line(0) = %q, err %v", got, snap.Err())
	}
	snap.Release()
	if !r.newSrc.Closed() {
		t.Fatal("source still open after the last release")
	}

	// A snapshot of a closed buffer cannot read.
	late := b.Snapshot()
	defer late.Release()
	// Pages the closed buffer still had cached keep reading; others do not.
	if late.Line(23) != "" || late.Err() == nil {
		t.Fatalf("snapshot of a closed buffer: Line(23) = %q, err %v", late.Line(23), late.Err())
	}
	if _, err := late.WriteTo(t.Context(), io.Discard, buffer.WriteOptions{}); err == nil {
		t.Fatal("WriteTo on a snapshot of a closed buffer succeeded")
	}
}

func TestLargeSnapshotRecordsReadErrors(t *testing.T) {
	b, src := openTiny(t, []byte(lines("alpha line %02d\n", 24)), 0)
	indexAll(t, b)
	s := b.Snapshot()
	defer s.Release()
	src.FailNext(1, errBoom)
	if got := s.Line(20); got != "" {
		t.Fatalf("Line on a failing source = %q", got)
	}
	if !errors.Is(s.Err(), errBoom) {
		t.Fatalf("Err = %v, want boom", s.Err())
	}
	if _, n, _ := s.Chunk(20, 100); n != 1 {
		t.Fatalf("Chunk after a failure returned %d lines, want just the one that was asked for", n)
	}
	if _, err := s.WriteTo(t.Context(), io.Discard, buffer.WriteOptions{}); !errors.Is(err, errBoom) {
		t.Fatalf("WriteTo = %v, want the recorded error", err)
	}
}

// TestSnapshotChunkConcurrentWithEditsAndIndex runs under -race: a snapshot
// is read on another goroutine while the buffer is edited and its index grows.
func TestSnapshotChunkConcurrentWithEditsAndIndex(t *testing.T) {
	data := lines("concurrent line %04d with é\n", 600)
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete=%v", complete), func(t *testing.T) {
			b, _ := openTiny(t, []byte(data), 0)
			b.SetClock(fixedClock)
			if complete {
				indexAll(t, b)
			}
			snap := b.Snapshot()
			defer snap.Release()
			var wantLines []string
			for i := range snap.Lines() {
				wantLines = append(wantLines, b.Line(i))
			}
			var wantBytes []byte
			if complete {
				wantBytes, _, _ = writeSnap(t, b, buffer.WriteOptions{})
			}

			var gotLines []string
			var gotBytes bytes.Buffer
			var gotErr error
			var wg sync.WaitGroup
			wg.Go(func() {
				for round := range 3 {
					gotLines = gotLines[:0]
					for from := 0; ; {
						first, n, text := snap.Chunk(from, 150)
						if n == 0 {
							break
						}
						gotLines = append(gotLines, strings.Split(text, "\n")...)
						from = first + n
					}
					if complete && round == 0 {
						_, gotErr = snap.WriteTo(context.Background(), &gotBytes, buffer.WriteOptions{})
					}
				}
			})

			rng := rand.New(rand.NewPCG(5, 6))
			for i := range 400 {
				line := rng.IntN(b.Lines())
				switch i % 5 {
				case 0, 1:
					ins(b, line, 2, "typed\ntext")
				case 2:
					b.Delete(buffer.Pos{Line: line}, buffer.Pos{Line: min(line+3, b.Lines()-1), Col: 4})
				case 3:
					b.Undo()
				default:
					if sc := b.BeginIndexScan(); sc != nil {
						batch, err := sc.Next(context.Background(), 64)
						sc.Close()
						if err != nil {
							t.Error(err)
						}
						b.ApplyIndex(batch)
					}
				}
				_ = b.Line(rng.IntN(b.Lines()))
			}
			wg.Wait()
			if !slices.Equal(gotLines, wantLines) {
				t.Fatalf("snapshot lines changed under edits: %d lines, want %d", len(gotLines), len(wantLines))
			}
			if complete && (gotErr != nil || !bytes.Equal(gotBytes.Bytes(), wantBytes)) {
				t.Fatalf("snapshot WriteTo under edits: err %v, %d bytes, want %d", gotErr, gotBytes.Len(), len(wantBytes))
			}
		})
	}
}
