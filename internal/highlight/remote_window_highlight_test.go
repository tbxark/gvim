package highlight

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/syntax"
)

// remoteHighlightAccess is a FileAccess + RangeAccess over synthetic remote bytes.
// A whole-file read is refused: a windowed remote large file must never use one.
type remoteHighlightAccess struct {
	data  []byte
	whole int
	mu    sync.Mutex
	reads int
	bytes int64
}

func (a *remoteHighlightAccess) ReadFile(string, int64) ([]byte, error) {
	a.mu.Lock()
	a.whole++
	a.mu.Unlock()
	return nil, errors.New("test: whole-file read not allowed")
}
func (a *remoteHighlightAccess) Stat(string) (document.AccessStat, error) {
	return document.AccessStat{Exists: true, Size: int64(len(a.data)), Mtime: time.Unix(1, 0)}, nil
}
func (a *remoteHighlightAccess) WriteFileAtomic(string, []byte) error {
	return errors.New("test: no write")
}
func (a *remoteHighlightAccess) ReadRange(_ context.Context, _ string, off, n int64) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads++
	if off < 0 || off > int64(len(a.data)) {
		return nil, io.EOF
	}
	end := min(off+n, int64(len(a.data)))
	a.bytes += end - off
	out := append([]byte(nil), a.data[off:end]...)
	if end < off+n {
		return out, io.EOF
	}
	return out, nil
}
func (a *remoteHighlightAccess) counts() (reads int, bytes int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reads, a.bytes
}

// remoteHighlightContent is a UTF-8 Go-looking file of lines of about lineWidth bytes.
func remoteHighlightContent(lines, lineWidth int) []byte {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		text := fmt.Sprintf("var x%08d = 1", i)
		if len(text) < lineWidth {
			text += strings.Repeat("x", lineWidth-len(text))
		}
		sb.WriteString(text)
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

func hasKeywordSpans(spans []syntax.Span) bool {
	for _, sp := range spans {
		if sp.Kind == syntax.KindKeyword {
			return true
		}
	}
	return false
}

// completeRemoteIndex finishes the line index the way the root's commands do:
// scan batches, loading every window the scan misses.
func completeRemoteIndex(t *testing.T, doc *editor.Document) {
	t.Helper()
	ctx := context.Background()
	for range 400 {
		scan := doc.Buffer.BeginIndexScan()
		if scan == nil {
			break
		}
		batch, err := scan.Next(ctx, 1<<20)
		scan.Close()
		doc.Buffer.ApplyIndex(batch)
		if errors.Is(err, buffer.ErrSourcePending) {
			w, ok := doc.TakePendingLargeWindow()
			if !ok {
				t.Fatal("index scan missed a window without recording it")
			}
			if err := doc.LoadLargeWindow(ctx, w); err != nil {
				t.Fatalf("LoadLargeWindow: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("index scan: %v", err)
		}
	}
	if !doc.Index().Complete {
		t.Fatalf("index not complete: %+v", doc.Index())
	}
}

// TestRemoteLargeDocumentWindowHighlighting: a windowed remote large document is
// highlighted through bounded windows. The lines of a window that arrives after the
// first paint are lexed on the repaint, scrolling loads only bounded windows, and the
// document stays editable (not read-only) with no blocking or whole-file read.
func TestRemoteLargeDocumentWindowHighlighting(t *testing.T) {
	t.Parallel()
	// 12 MiB in 1 MiB windows, more than the 8-window source cache: completing the
	// index evicts the first windows, so the first paint misses them.
	access := &remoteHighlightAccess{data: remoteHighlightContent(600000, 21)}
	ed := editor.New(editor.Options{
		Empty:                   true,
		LargeFileMode:           true,
		LargeFileThresholdBytes: 1024,
		FileAccess:              access,
	})
	t.Cleanup(ed.Close)
	if err := ed.OpenFile("/remote/big.go"); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	doc := ed.Active()
	if doc == nil || !doc.AsyncLarge() || doc.ReadOnly() {
		t.Fatalf("document = %v, async %v, read-only %v; want an editable windowed remote large file", doc, doc != nil && doc.AsyncLarge(), doc != nil && doc.ReadOnly())
	}
	if access.whole != 0 {
		t.Fatalf("a whole-file read was made %d time(s)", access.whole)
	}
	completeRemoteIndex(t, doc)
	// The index scan may leave its last miss recorded; a paint-driven check starts
	// from a clean slate.
	for {
		if _, ok := doc.TakePendingLargeWindow(); !ok {
			break
		}
	}
	readsBefore, bytesBefore := access.counts()

	cache := New()
	// With no context the target window has the same first line and length before and
	// after its window arrives, so the highlighter's own window cache must be dropped
	// on the generation change for the loaded text to be lexed.
	cache.SetContext(0)
	// Highlighting an unloaded line must not read it: a context read would record a
	// window miss that the paint did not ask for. Only the paint's own text read may
	// request the window.
	if hasKeywordSpans(cache.LineSpans(doc, 0)) {
		t.Fatal("line 0 was already highlighted before its window was loaded")
	}
	if _, ok := doc.TakePendingLargeWindow(); ok {
		t.Fatal("highlighting an unloaded line recorded a remote window miss")
	}
	if reads, bytes := access.counts(); reads != readsBefore || bytes != bytesBefore {
		t.Fatalf("highlighting an unloaded line performed remote I/O: reads %d->%d bytes %d->%d",
			readsBefore, reads, bytesBefore, bytes)
	}

	// The paint reads the visible line's text (it is blank until the window arrives),
	// and the root loads the window the paint missed.
	_ = doc.Buffer.Line(0)
	if w, ok := doc.TakePendingLargeWindow(); !ok {
		t.Fatal("the paint's text read recorded no pending window")
	} else if err := doc.LoadLargeWindow(context.Background(), w); err != nil {
		t.Fatalf("LoadLargeWindow: %v", err)
	}
	// The repaint re-lexes: the generation changed even though the text version did
	// not, so the cached blank spans must be replaced by the loaded text's spans.
	if !doc.LineLoaded(0) {
		t.Fatal("line 0 is still reported unloaded after its window arrived")
	}
	if !hasKeywordSpans(cache.LineSpans(doc, 0)) {
		t.Fatalf("line 0 is still not highlighted after its window loaded: %v", cache.LineSpans(doc, 0))
	}
	if doc.WindowGeneration() == 0 {
		t.Fatal("the remote window generation did not change")
	}
	// A repaint of the loaded window, and scrolling inside it, lex and read nothing
	// remote: the cache and the loaded window serve the plain frame.
	readsLexed, bytesLexed := access.counts()
	for line := 0; line < 40; line++ {
		cache.LineSpans(doc, line)
	}
	if reads, bytes := access.counts(); reads != readsLexed || bytes != bytesLexed {
		t.Fatalf("repainting a loaded window performed remote I/O: reads %d->%d bytes %d->%d",
			readsLexed, reads, bytesLexed, bytes)
	}
	if len(cache.large.wins) > maxSpanWindows {
		t.Fatalf("highlight windows = %d, over the bound %d", len(cache.large.wins), maxSpanWindows)
	}

	// Scroll to a line in an evicted middle window. Highlighting it requests no window;
	// only the paint's text read does, and the next repaint lexes that bounded window.
	target := 3 * (1 << 20) / 21
	if target >= doc.Buffer.Lines() {
		t.Fatalf("target line %d is past the %d lines", target, doc.Buffer.Lines())
	}
	if hasKeywordSpans(cache.LineSpans(doc, target)) {
		t.Fatal("the scrolled-to line is highlighted before its window loaded")
	}
	if _, ok := doc.TakePendingLargeWindow(); ok {
		t.Fatal("scrolling highlight recorded a remote window miss")
	}
	_ = doc.Buffer.Line(target)
	if w, ok := doc.TakePendingLargeWindow(); !ok {
		t.Fatal("the paint's scrolled text read recorded no pending window")
	} else if err := doc.LoadLargeWindow(context.Background(), w); err != nil {
		t.Fatalf("LoadLargeWindow(scrolled): %v", err)
	}
	if !hasKeywordSpans(cache.LineSpans(doc, target)) {
		t.Fatalf("scrolled-to line %d is not highlighted after its window loaded: %v", target, cache.LineSpans(doc, target))
	}
	_, bytesAfter := access.counts()
	if added := bytesAfter - bytesBefore; added > 3<<20 {
		t.Fatalf("highlighting/inspecting two point windows read %d extra bytes of a 12 MiB file; want bounded windows", added)
	}
	if access.whole != 0 {
		t.Fatalf("highlighting made a whole-file read %d time(s)", access.whole)
	}

	// Editing the loaded window still works: the highlight path reads only.
	if res := ed.Do("insert_char", map[string]any{"text": "z"}); res.Err != nil {
		t.Fatalf("edit after highlighting: %v", res.Err)
	}
	if !doc.Modified() {
		t.Fatal("the edit did not mark the remote large document modified")
	}
}

// TestRemoteLargeBinaryIsAReadOnlySummary: a binary remote large file is not a
// windowed text document at all. It opens as the read-only summary, so there is no
// line index to highlight, no window to load and no whole-file read; the bytes are
// read only through the bounded hex range.
func TestRemoteLargeBinaryIsAReadOnlySummary(t *testing.T) {
	t.Parallel()
	data := append([]byte("binary\x00content\n"), remoteHighlightContent(300000, 21)...)
	access := &remoteHighlightAccess{data: data}
	ed := editor.New(editor.Options{
		Empty:                   true,
		LargeFileMode:           true,
		LargeFileThresholdBytes: 1024,
		FileAccess:              access,
	})
	t.Cleanup(ed.Close)
	if err := ed.OpenFile("/remote/blob.bin"); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	doc := ed.Active()
	if doc == nil || doc.AsyncLarge() || doc.Large() || !doc.Binary() || !doc.ReadOnly() {
		t.Fatalf("document = %v, large %v, async %v, binary %v, read-only %v; want a binary summary",
			doc, doc != nil && doc.Large(), doc != nil && doc.AsyncLarge(), doc != nil && doc.Binary(), doc != nil && doc.ReadOnly())
	}
	if _, ok := doc.TakePendingLargeWindow(); ok {
		t.Fatal("a binary summary recorded a large-file window")
	}
	reads, bytes := access.counts()
	// The open probes the head through the same bounded RangeAccess the text path
	// uses; it must not read the whole file (or a full remote window).
	if bytes > document.LargeSampleSize || bytes >= int64(len(data)) || access.whole != 0 {
		t.Fatalf("the binary open read ranges %d/%d bytes (whole %d), want at most the %d-byte sample",
			reads, bytes, access.whole, document.LargeSampleSize)
	}
	cache := New()
	_ = cache.LineSpans(doc, 0)
	if r, b := access.counts(); r != reads || b != bytes {
		t.Fatalf("highlighting a binary summary performed remote I/O: reads %d->%d bytes %d->%d",
			reads, r, bytes, b)
	}
	if !doc.ReadOnly() || access.whole != 0 {
		t.Fatalf("read-only %v, whole-file reads %d; want read-only true and no whole-file read", doc.ReadOnly(), access.whole)
	}
	got, err := doc.ReadBinaryRange(context.Background(), 0, 4)
	if err != nil || string(got) != "bina" {
		t.Fatalf("ReadBinaryRange = %q, %v; want the bounded remote bytes", got, err)
	}
	if readsAfter, bytesAfter := access.counts(); readsAfter != reads+1 || bytesAfter != bytes+4 || access.whole != 0 {
		t.Fatalf("hex read added ranges %d/%d bytes (whole %d); want one 4-byte range",
			readsAfter-reads, bytesAfter-bytes, access.whole)
	}
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err == nil {
		t.Fatal("an edit of the binary summary succeeded")
	}
}
