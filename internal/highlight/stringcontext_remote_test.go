package highlight

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// remoteQuoteContent is a UTF-8 Go-looking remote file whose lines carry a string
// literal and a comment.
func remoteQuoteContent(lines int) []byte {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&sb, "var x%08d = \"hello\" // c\n", i)
	}
	return []byte(sb.String())
}

// openRemoteQuoteDoc opens content as a windowed remote large document and completes
// its index, leaving the source cache holding the last windows (the first window is
// evicted, like a document scrolled away from the top).
func openRemoteQuoteDoc(t *testing.T) (*editor.Editor, *remoteHighlightAccess) {
	t.Helper()
	access := &remoteHighlightAccess{data: remoteQuoteContent(400000)} // ~13 MiB
	ed := editor.New(editor.Options{
		Empty:                   true,
		LargeFileMode:           true,
		LargeFileThresholdBytes: 1024,
		AutoClose:               true,
		FileAccess:              access,
	})
	t.Cleanup(ed.Close)
	if err := ed.OpenFile("/remote/quote.go"); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	doc := ed.Active()
	if doc == nil || !doc.AsyncLarge() {
		t.Fatalf("document = %v, async %v; want a windowed remote large file", doc, doc != nil && doc.AsyncLarge())
	}
	completeRemoteIndex(t, doc)
	for {
		if _, ok := doc.TakePendingLargeWindow(); !ok {
			break
		}
	}
	return ed, access
}

// loadRemoteLine simulates the paint reading line and the root loading the window it
// missed.
func loadRemoteLine(t *testing.T, doc *editor.Document, line int) {
	t.Helper()
	_ = doc.Buffer.Line(line)
	if w, ok := doc.TakePendingLargeWindow(); ok {
		if err := doc.LoadLargeWindow(context.Background(), w); err != nil {
			t.Fatalf("LoadLargeWindow: %v", err)
		}
	}
}

// TestStringContextRemoteLargeUnloadedWindowIsSafe: a context query on a line whose
// remote window has not arrived answers false/nil without reading or requesting the
// window; after the paint loads it, the loaded window gives the real string/comment
// context, and a later window arrival re-lexes the cached window.
func TestStringContextRemoteLargeUnloadedWindowIsSafe(t *testing.T) {
	ed, access := openRemoteQuoteDoc(t)
	doc := ed.Active()
	inside := buffer.Pos{Line: 0, Col: 18} // inside "hello"
	if doc.LineLoaded(0) {
		t.Fatal("line 0 is loaded before the paint; the test needs an evicted first window")
	}
	reads, bytes := access.counts()
	c := &stringCache{}
	if c.inString(doc, inside) {
		t.Fatal("an unloaded remote line is reported inside a string")
	}
	if c.nonCode(doc, 0) != nil {
		t.Fatal("an unloaded remote line got a non-code mask")
	}
	if r, b := access.counts(); r != reads || b != bytes {
		t.Fatalf("a context query on an unloaded window performed remote I/O: reads %d->%d bytes %d->%d", reads, r, bytes, b)
	}
	if _, ok := doc.TakePendingLargeWindow(); ok {
		t.Fatal("a context query on an unloaded window recorded a remote window miss")
	}

	// The paint's own text read and the root's load bring the window in; the next
	// context query uses it and performs no extra remote reads.
	loadRemoteLine(t, doc, 0)
	if !doc.LineLoaded(0) {
		t.Fatal("line 0 is still unloaded after its window arrived")
	}
	loadedReads, loadedBytes := access.counts()
	if !c.inString(doc, inside) {
		t.Fatalf("a loaded line inside a string is not reported inside it: %v", c.spans)
	}
	if c.inString(doc, buffer.Pos{Line: 0, Col: 1}) {
		t.Fatal("code on the loaded line is reported inside a string")
	}
	if got, want := c.end-c.start, len(c.lines); got != want {
		t.Fatalf("window = [%d, %d), %d lines", c.start, c.end, got)
	}
	if c.end-c.start > 2*stringReach+1 {
		t.Fatalf("window has %d lines, want a bounded window", c.end-c.start)
	}
	if r, b := access.counts(); r != loadedReads || b != loadedBytes {
		t.Fatalf("a context query on a loaded window performed remote I/O: reads %d->%d bytes %d->%d", loadedReads, r, loadedBytes, b)
	}
	if _, ok := doc.TakePendingLargeWindow(); ok {
		t.Fatal("a context query on a loaded window recorded a window miss")
	}
	mask := NewNonCodeContext()(doc, 0)
	if len(mask) == 0 || !mask[17] || !mask[24] {
		t.Fatalf("non-code mask = %v; want the string at 17 and the comment at 24 masked", mask)
	}

	// A later, unrelated window arrival changes the source generation without a text
	// version change; the cache must re-lex, not serve spans from before the arrival.
	lexed := c.lexed
	target := 3 * (1 << 20) / 34 // a line in an evicted middle window
	if target >= doc.Buffer.Lines() {
		t.Fatalf("target line %d past the %d lines", target, doc.Buffer.Lines())
	}
	loadRemoteLine(t, doc, target)
	if !doc.LineLoaded(0) {
		t.Fatalf("loading the target window evicted line 0; choose another target")
	}
	if got := c.inString(doc, inside); !got {
		t.Fatal("inside-string context was lost after another window arrived")
	}
	if c.lexed <= lexed {
		t.Fatal("the context cache was not re-lexed after the window generation changed")
	}
}

// TestStringContextRemoteLargeEditing: with the loaded window's context installed,
// typing a quote inside a string does not auto-close, while the same quote in code
// still pairs; both edits stay in the loaded window and make no extra remote reads.
func TestStringContextRemoteLargeEditing(t *testing.T) {
	ed, access := openRemoteQuoteDoc(t)
	doc := ed.Active()
	loadRemoteLine(t, doc, 0)
	if !doc.LineLoaded(0) {
		t.Fatal("line 0 did not load")
	}
	ed.SetStringContext(NewStringContext())
	ed.SetNonCodeContext(NewNonCodeContext())
	reads, bytes := access.counts()

	// Inside "hello": the quote is inserted as text, not paired.
	doc.Cursor, doc.Sel = buffer.Pos{Line: 0, Col: 18}, nil
	if res := ed.Do("insert_char", map[string]any{"text": "\""}); res.Err != nil {
		t.Fatalf("typing inside the string: %v", res.Err)
	}
	line := doc.Buffer.Line(0)
	if got := strings.Count(line, "\""); got != 3 {
		t.Fatalf("quotes after typing inside the string = %d in %q, want 3 (no pair)", got, line)
	}
	if r, b := access.counts(); r != reads || b != bytes {
		t.Fatalf("typing inside a loaded window performed remote I/O: reads %d->%d bytes %d->%d", reads, r, bytes, b)
	}

	// Undo, then type the same quote in code (the space before the opening quote):
	// the context says code, so auto-close pairs it.
	if res := ed.Do("undo", nil); res.Err != nil {
		t.Fatalf("undo: %v", res.Err)
	}
	doc.Cursor, doc.Sel = buffer.Pos{Line: 0, Col: 15}, nil
	if res := ed.Do("insert_char", map[string]any{"text": "\""}); res.Err != nil {
		t.Fatalf("typing outside the string: %v", res.Err)
	}
	line = doc.Buffer.Line(0)
	if got := strings.Count(line, "\""); got != 4 {
		t.Fatalf("quotes after typing in code = %d in %q, want 4 (auto-closed pair)", got, line)
	}
	if r, b := access.counts(); r != reads || b != bytes {
		t.Fatalf("editing the loaded window performed remote I/O: reads %d->%d bytes %d->%d", reads, r, bytes, b)
	}
}
