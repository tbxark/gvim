package buffer

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// bytesSource is a minimal Source for tests that need package internals; the
// richer buffertest sources cannot be imported from package buffer.
type bytesSource struct{ data []byte }

func (s *bytesSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (s *bytesSource) Size() int64  { return int64(len(s.data)) }
func (s *bytesSource) Check() error { return nil }
func (s *bytesSource) Close() error { return nil }

func tinyOpen(t *testing.T, data string) *Buffer {
	t.Helper()
	b, err := OpenLarge(&bytesSource{data: []byte(data)}, LargeOptions{
		Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32, Budget: NewPageBudget(1 << 20),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func indexFully(t *testing.T, b *Buffer) {
	t.Helper()
	for !b.Index(0).Complete {
		sc := b.BeginIndexScan()
		batch, err := sc.Next(context.Background(), 1<<20)
		sc.Close()
		if err != nil {
			t.Fatal(err)
		}
		b.ApplyIndex(batch)
	}
}

func TestLineRangeTerminators(t *testing.T) {
	data := "ab\r\n\r\ncd\nef\rgh\n" + strings.Repeat("x", 14) + "\r\n" + "tail"
	b := tinyOpen(t, data)
	indexFully(t, b)
	want := []struct {
		text string
		term uint8
	}{
		{"ab", eolCRLF}, {"", eolCRLF}, {"cd", eolLF}, {"ef\rgh", eolLF},
		{strings.Repeat("x", 14), eolCRLF}, {"tail", eolDefault},
	}
	if got := b.Lines(); got != len(want) {
		t.Fatalf("Lines = %d, want %d", got, len(want))
	}
	for i, w := range want {
		start, end, term, err := b.big.lineRange(i)
		if err != nil {
			t.Fatalf("lineRange(%d): %v", i, err)
		}
		if got := data[start:end]; got != w.text || term != w.term {
			t.Errorf("lineRange(%d) = %q, term %d; want %q, term %d", i, got, term, w.text, w.term)
		}
	}
}

// A copy of the reader taken at one point of the index may be used on another
// goroutine while the buffer goes on indexing: it only reads cells of the
// index that are never written again. Run with -race.
func TestBaseReaderConcurrentWithApplyIndex(t *testing.T) {
	var sb strings.Builder
	for i := range 400 {
		fmt.Fprintf(&sb, "line %04d\n", i)
	}
	data := sb.String()
	b := tinyOpen(t, data)
	r := b.big.baseReader
	known := r.g.baseLines()
	if known < 1 || known >= 400 {
		t.Fatalf("test needs a partial index, got %d lines", known)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 30 {
			for i := range known {
				got, _, err := r.lineBytes(i)
				if want := fmt.Sprintf("line %04d", i); err != nil || string(got) != want {
					t.Errorf("lineBytes(%d) = %q, %v; want %q", i, got, err, want)
					return
				}
			}
		}
	}()
	for !b.Index(0).Complete {
		sc := b.BeginIndexScan()
		batch, err := sc.Next(context.Background(), 32)
		sc.Close()
		if err != nil {
			t.Fatal(err)
		}
		b.ApplyIndex(batch)
		_ = b.Line(b.Lines() - 1)
		_ = b.Stats()
	}
	<-done
	if b.Lines() != 401 {
		t.Errorf("Lines = %d, want 401", b.Lines())
	}
}

// The scan runs on a worker goroutine while the buffer goroutine applies its
// batches, reads lines and polls the counters.
func TestIndexScanConcurrentWithReads(t *testing.T) {
	var sb strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&sb, "line %05d\n", i)
	}
	b := tinyOpen(t, sb.String())
	sc := b.BeginIndexScan()
	batches := make(chan IndexBatch)
	errc := make(chan error, 1)
	go func() {
		defer close(batches)
		for {
			batch, err := sc.Next(context.Background(), 64)
			if err != nil {
				errc <- err
				return
			}
			batches <- batch
			if batch.Done {
				return
			}
		}
	}()
	prev := 0
	for batch := range batches {
		b.ApplyIndex(batch)
		n := b.Lines()
		if n < prev {
			t.Fatalf("Lines shrank from %d to %d", prev, n)
		}
		prev = n
		if got, want := b.Line(n-2), fmt.Sprintf("line %05d", n-2); n >= 2 && got != want {
			t.Fatalf("Line(%d) = %q, want %q", n-2, got, want)
		}
		_ = b.Stats()
	}
	select {
	case err := <-errc:
		t.Fatal(err)
	default:
	}
	sc.Close()
	if !b.Index(0).Complete || b.Lines() != 3001 {
		t.Errorf("Index = %+v", b.Index(0))
	}
}

// Edits, undo and redo on the buffer goroutine while the scan runs on another:
// the edits only touch the overlay, never the index the scan feeds.
func TestEditsConcurrentWithIndexScan(t *testing.T) {
	var sb strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&sb, "line %05d\n", i)
	}
	b := tinyOpen(t, sb.String())
	sc := b.BeginIndexScan()
	batches := make(chan IndexBatch)
	go func() {
		defer close(batches)
		for {
			batch, err := sc.Next(context.Background(), 64)
			if err != nil {
				return
			}
			batches <- batch
			if batch.Done {
				return
			}
		}
	}()
	applied := 0
	for batch := range batches {
		b.ApplyIndex(batch)
		applied++
		last := b.Lines() - 1
		b.Break()
		b.Insert(Pos{Line: last, Col: 0}, "x")
		b.Delete(Pos{Line: 0}, Pos{Line: 1})
		b.Undo()
		b.Redo()
		_ = b.Stats()
	}
	sc.Close()
	if !b.Index(0).Complete {
		t.Fatalf("Index = %+v", b.Index(0))
	}
	// Each round removed the first line, and its edits never reached the index.
	if want := 3001 - applied; !b.Modified() || b.Lines() != want || b.Line(0) != fmt.Sprintf("line %05d", applied) {
		t.Errorf("Modified %v, %d lines (want %d), Line(0) = %q", b.Modified(), b.Lines(), want, b.Line(0))
	}
}
