package buffer_test

import (
	"encoding/json/v2"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/buffer/buffertest"
)

// indexOneChunk counts and applies the next chunk of b's index.
func indexOneChunk(t testing.TB, b *buffer.Buffer) {
	t.Helper()
	sc := b.BeginIndexScan()
	if sc == nil {
		t.Fatalf("BeginIndexScan returned nil with an incomplete index (err %v)", b.SourceErr())
	}
	batch, err := sc.Next(t.Context(), 1)
	sc.Close()
	if err != nil {
		t.Fatal(err)
	}
	b.ApplyIndex(batch)
}

// restoreWhenIndexed restores o, indexing one chunk at a time while the buffer says
// the index is not far enough, as the editor does after each batch. It returns how
// many times the restore was pending.
func restoreWhenIndexed(t testing.TB, b *buffer.Buffer, o buffer.OverlayState) (pending int) {
	t.Helper()
	for {
		err := b.RestoreOverlay(o)
		if err == nil {
			return pending
		}
		if !errors.Is(err, buffer.ErrIndexPending) {
			t.Fatalf("RestoreOverlay: %v", err)
		}
		if b.Modified() {
			t.Fatalf("a pending restore left the buffer modified")
		}
		pending++
		indexOneChunk(t, b)
	}
}

// overlayData is a file of many short lines: with the tiny geometry its index needs
// dozens of chunks.
func overlayData(eol string) []byte {
	var sb strings.Builder
	for i := range 60 {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%7))
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString(eol)
	}
	sb.WriteString("last")
	return []byte(sb.String())
}

func TestOverlayIncompleteIndexRoundTrip(t *testing.T) {
	data := overlayData("\n")
	b, _ := openTiny(t, data, 0)
	b.SetClock(fixedClock)
	ref := buffer.New(string(data))
	ref.SetClock(fixedClock)

	// Index about half of the file, edit inside it, and stash while the rest is unknown.
	for b.Index(0).Indexed < b.Index(0).Size/2 {
		indexOneChunk(t, b)
	}
	if b.Index(0).Complete {
		t.Fatal("the index is complete: the test needs a partial one")
	}
	edit := func(f func(*buffer.Buffer)) { f(b); f(ref) }
	edit(func(x *buffer.Buffer) { x.Break(); ins(x, 1, 2, "ONE") })
	edit(func(x *buffer.Buffer) { x.Break(); x.Delete(buffer.Pos{Line: 3, Col: 1}, buffer.Pos{Line: 5, Col: 2}) })
	edit(func(x *buffer.Buffer) { x.Break(); ins(x, 6, 0, "new\nlines\n") })
	state, ok := b.Overlay()
	if !ok || state.Empty() {
		t.Fatalf("Overlay() = %+v, %v: want the edits", state, ok)
	}

	// Through JSON, as the session keeps it.
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var back buffer.OverlayState
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}

	fresh, _ := openTiny(t, data, 0)
	pending := restoreWhenIndexed(t, fresh, back)
	if pending == 0 {
		t.Errorf("the restore was never pending: the test did not start from a shorter index")
	}
	if !fresh.Modified() {
		t.Errorf("a restored buffer is not modified")
	}
	indexAll(t, fresh)
	if got, want := fresh.Text(), ref.Text(); got != want {
		t.Fatalf("Text() = %q\nwant %q", got, want)
	}
	if got, want := fresh.Lines(), ref.Lines(); got != want {
		t.Errorf("Lines() = %d, want %d", got, want)
	}

	// The restore is one undo step that goes back to the file, and redo brings it again.
	if _, ok := fresh.Undo(); !ok {
		t.Fatal("Undo() after a restore has nothing to undo")
	}
	if fresh.Text() != string(data) || fresh.Modified() {
		t.Errorf("Undo() did not restore the file (modified %v)", fresh.Modified())
	}
	if _, ok := fresh.Redo(); !ok || fresh.Text() != ref.Text() {
		t.Errorf("Redo() did not bring the restored text back")
	}
}

func TestOverlayRoundTripKeepsTheBytesOnSave(t *testing.T) {
	tests := []struct {
		name string
		data string
		edit func(b *buffer.Buffer)
	}{
		{"enter in a CRLF region of an LF head", "a\nb\nc\r\nd\r\ne", func(b *buffer.Buffer) { ins(b, 2, 1, "\nnew") }},
		{"edit in an LF region of a CRLF head", "a\r\nb\r\nc\nd\ne", func(b *buffer.Buffer) { ins(b, 3, 1, "X") }},
		{"unicode and a line break at the end", "α\nβ\nγ\n", func(b *buffer.Buffer) { ins(b, 1, 1, "日本\n😀") }},
		{"edit the last unterminated line", "one\ntwo\nthree", func(b *buffer.Buffer) { ins(b, 2, 5, "\nfour") }},
		{"delete across lines", "a\r\nb\nc\r\nd\ne", func(b *buffer.Buffer) { b.Delete(buffer.Pos{Line: 0, Col: 1}, buffer.Pos{Line: 3, Col: 0}) }},
		{"delete everything", "a\nb\nc\n", func(b *buffer.Buffer) { b.Delete(buffer.Pos{}, b.End()) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := openTiny(t, []byte(tc.data), 0)
			indexAll(t, b)
			tc.edit(b)
			want, _, err := writeSnap(t, b, buffer.WriteOptions{EOL: "\n"})
			if err != nil {
				t.Fatal(err)
			}
			state, _ := b.Overlay()
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var back buffer.OverlayState
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatal(err)
			}
			fresh, _ := openTiny(t, []byte(tc.data), 0)
			restoreWhenIndexed(t, fresh, back)
			indexAll(t, fresh)
			got, _, err := writeSnap(t, fresh, buffer.WriteOptions{EOL: "\n"})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("the saved bytes are %q, want %q", got, want)
			}
			if fresh.Text() != b.Text() {
				t.Errorf("Text() = %q, want %q", fresh.Text(), b.Text())
			}
		})
	}
}

func TestRestoreOverlayBeforeIndexReturnsPending(t *testing.T) {
	data := overlayData("\n")
	b, _ := openTiny(t, data, 0)
	indexAll(t, b)
	ins(b, 40, 0, "deep ")
	state, _ := b.Overlay()
	if state.TailFirst < 41 {
		t.Fatalf("TailFirst = %d, want it past line 40", state.TailFirst)
	}

	fresh, src := openTiny(t, data, 0)
	read := src.BytesRead()
	if err := fresh.RestoreOverlay(state); !errors.Is(err, buffer.ErrIndexPending) {
		t.Fatalf("RestoreOverlay() = %v, want ErrIndexPending", err)
	}
	if src.BytesRead() != read {
		t.Errorf("a pending restore read the source")
	}
	if text := fresh.Text(); fresh.Modified() || text != string(data[:len(text)]) {
		t.Errorf("a pending restore changed the buffer")
	}
	if n := restoreWhenIndexed(t, fresh, state); n == 0 {
		t.Errorf("the restore was not pending")
	}
	if !strings.Contains(fresh.Line(40), "deep ") {
		t.Errorf("Line(40) = %q after the restore", fresh.Line(40))
	}
}

func TestRestoreOverlayDoesNotNeedAWholeIndexForARunAtTheStart(t *testing.T) {
	data := overlayData("\n")
	b, _ := openTiny(t, data, 0)
	ins(b, 0, 0, ">") // the first line is known from the start
	state, _ := b.Overlay()

	fresh, _ := openTiny(t, data, 0)
	if err := fresh.RestoreOverlay(state); err != nil {
		t.Fatalf("RestoreOverlay() = %v with the first lines indexed", err)
	}
	if fresh.Line(0) != ">"+b.Line(0)[1:] {
		t.Errorf("Line(0) = %q", fresh.Line(0))
	}
}

func TestRestoreOverlayValidates(t *testing.T) {
	data := overlayData("\n")
	lit := func(lines ...string) buffer.OverlayPiece { return buffer.OverlayPiece{Lines: lines} }
	run := func(first, count int) buffer.OverlayPiece {
		return buffer.OverlayPiece{Run: true, First: first, Count: count}
	}
	tests := []struct {
		name string
		st   buffer.OverlayState
	}{
		{"negative tail", buffer.OverlayState{TailFirst: -1}},
		{"run past the tail", buffer.OverlayState{Pieces: []buffer.OverlayPiece{run(0, 5)}, TailFirst: 4}},
		{"runs out of order", buffer.OverlayState{Pieces: []buffer.OverlayPiece{run(2, 2), run(0, 2)}, TailFirst: 6}},
		{"runs overlap", buffer.OverlayState{Pieces: []buffer.OverlayPiece{run(0, 3), run(2, 2)}, TailFirst: 6}},
		{"empty run", buffer.OverlayState{Pieces: []buffer.OverlayPiece{run(1, 0)}, TailFirst: 3}},
		{"negative run", buffer.OverlayState{Pieces: []buffer.OverlayPiece{run(-1, 2)}, TailFirst: 3}},
		{"run with text", buffer.OverlayState{Pieces: []buffer.OverlayPiece{{Run: true, Count: 1, Lines: []string{"x"}}}, TailFirst: 1}},
		{"empty literal piece", buffer.OverlayState{Pieces: []buffer.OverlayPiece{lit()}, TailFirst: 1}},
		{"line break in a line", buffer.OverlayState{Pieces: []buffer.OverlayPiece{lit("a\nb")}, TailFirst: 1}},
		{"terminators of the wrong length", buffer.OverlayState{Pieces: []buffer.OverlayPiece{{Lines: []string{"a", "b"}, EOL: "n"}}, TailFirst: 1}},
		{"unknown terminator", buffer.OverlayState{Pieces: []buffer.OverlayPiece{{Lines: []string{"a"}, EOL: "?"}}, TailFirst: 1}},
		{"literal with a run range", buffer.OverlayState{Pieces: []buffer.OverlayPiece{{Lines: []string{"a"}, Count: 2}}, TailFirst: 1}},
		{"past the end of a complete file", buffer.OverlayState{Pieces: []buffer.OverlayPiece{lit("a")}, TailFirst: 1000}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := openTiny(t, data, 0)
			indexAll(t, b)
			before := b.Text()
			err := b.RestoreOverlay(tc.st)
			if !errors.Is(err, buffer.ErrOverlayInvalid) {
				t.Fatalf("RestoreOverlay() = %v, want ErrOverlayInvalid", err)
			}
			if b.Modified() || b.Text() != before {
				t.Errorf("a refused restore changed the buffer")
			}
		})
	}
}

func TestRestoreOverlayRefusals(t *testing.T) {
	data := overlayData("\n")
	good := buffer.OverlayState{Pieces: []buffer.OverlayPiece{{Lines: []string{"replaced"}}}, TailFirst: 1}

	t.Run("a buffer with edits", func(t *testing.T) {
		b, _ := openTiny(t, data, 0)
		indexAll(t, b)
		ins(b, 0, 0, "x")
		before := b.Text()
		if err := b.RestoreOverlay(good); err == nil {
			t.Fatal("RestoreOverlay() onto an edited buffer succeeded")
		}
		if b.Text() != before {
			t.Errorf("the refused restore changed the text")
		}
	})
	t.Run("inside a group", func(t *testing.T) {
		b, _ := openTiny(t, data, 0)
		var err error
		b.Group(func() { err = b.RestoreOverlay(good) })
		if err == nil || b.Modified() {
			t.Errorf("RestoreOverlay() inside Group = %v, modified %v", err, b.Modified())
		}
	})
	t.Run("a memory buffer", func(t *testing.T) {
		m := buffer.New("a\nb")
		if _, ok := m.Overlay(); ok {
			t.Errorf("Overlay() of a memory buffer is ok")
		}
		if err := m.RestoreOverlay(good); err == nil {
			t.Errorf("RestoreOverlay() onto a memory buffer succeeded")
		}
	})
	t.Run("a closed buffer", func(t *testing.T) {
		b, _ := openTiny(t, data, 0)
		_ = b.Close()
		if err := b.RestoreOverlay(good); err == nil {
			t.Errorf("RestoreOverlay() onto a closed buffer succeeded")
		}
	})
	t.Run("over the cap on unsaved bytes", func(t *testing.T) {
		opts := tinyGeom(0)
		opts.MaxOverlayBytes = 64
		b, err := buffer.OpenLarge(buffertest.NewMemSource(data), opts)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Close() })
		big := buffer.OverlayState{Pieces: []buffer.OverlayPiece{{Lines: []string{strings.Repeat("y", 100)}}}, TailFirst: 1}
		if err := b.RestoreOverlay(big); !errors.Is(err, buffer.ErrOverlayFull) {
			t.Errorf("RestoreOverlay() = %v, want ErrOverlayFull", err)
		}
		if b.Modified() {
			t.Errorf("a refused restore left the buffer modified")
		}
	})
}

func TestOverlayOfAnUneditedBufferIsEmpty(t *testing.T) {
	b, _ := openTiny(t, overlayData("\n"), 0)
	state, ok := b.Overlay()
	if !ok || !state.Empty() {
		t.Fatalf("Overlay() = %+v, %v: want an empty state", state, ok)
	}
	if err := b.RestoreOverlay(state); err != nil || b.Modified() {
		t.Errorf("restoring an empty state: err %v, modified %v", err, b.Modified())
	}
	ins(b, 0, 0, "x")
	b.Undo()
	if state, _ := b.Overlay(); !state.Empty() {
		t.Errorf("Overlay() after undoing every edit = %+v, want empty", state)
	}
}

// newlineSource is size bytes of '\n': one empty line per byte.
type newlineSource struct{ size int64 }

func (s newlineSource) ReadAt(p []byte, off int64) (int, error) {
	n := int(min(int64(len(p)), s.size-off))
	if n <= 0 {
		return 0, io.EOF
	}
	for i := range n {
		p[i] = '\n'
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (s newlineSource) Size() int64  { return s.size }
func (s newlineSource) Check() error { return nil }
func (s newlineSource) Close() error { return nil }

// A file with more than 2^31 lines is handled by the buffer, so its saved
// overlay must restore too.
func TestRestoreOverlayOverInt32Lines(t *testing.T) {
	const (
		chunk    = 1 << 20
		chunks   = 3072
		size     = int64(chunks)*chunk + 0
		perChunk = chunk
	)
	open := func() *buffer.Buffer {
		b, err := buffer.OpenLarge(newlineSource{size: size}, buffer.LargeOptions{Chunk: chunk, Budget: buffer.NewPageBudget(64 << 20)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Close() })
		b.JumpScanned(chunks, perChunk)
		return b
	}
	b := open()
	if b.Lines() <= math.MaxInt32 {
		t.Fatalf("Lines = %d, want over MaxInt32", b.Lines())
	}
	at := b.Lines() - 6
	b.Insert(buffer.Pos{Line: at}, "hello\nworld")
	ov, ok := b.Overlay()
	if !ok {
		t.Fatal("Overlay() = not ok")
	}
	fresh := open()
	if err := fresh.RestoreOverlay(ov); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	if fresh.Lines() != b.Lines() || fresh.Line(at) != "hello" || fresh.Line(at+1) != "world" {
		t.Errorf("restored lines = %d (%q, %q), want %d and hello, world", fresh.Lines(), fresh.Line(at), fresh.Line(at+1), b.Lines())
	}
}
