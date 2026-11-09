package highlight

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// openLocalLargeDoc writes content to a Go file and opens it in a local large-file
// editor with quote auto-close enabled.
func openLocalLargeDoc(t *testing.T, content string) *editor.Editor {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big.go")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{
		Empty:                   true,
		LargeFileMode:           true,
		LargeFileThresholdBytes: 1024,
		AutoClose:               true,
	})
	if err := ed.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	t.Cleanup(ed.Close)
	doc := ed.Active()
	if doc == nil || !doc.Large() || doc.AsyncLarge() {
		t.Fatalf("document = %v, large %v, async %v; want a local large file", doc, doc != nil && doc.Large(), doc != nil && doc.AsyncLarge())
	}
	return ed
}

// localLargeQuoteContent is a local large-file body of numbered lines with a string
// and a comment.
func localLargeQuoteContent(lines int) string {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&sb, "var x%05d = \"hello\" // c\n", i)
	}
	return sb.String()
}

// quoteLineInside returns the rune column inside hello on a line of
// localLargeQuoteContent, and the column before its opening quote (code).
func quoteLineInside(line string) (inside, code int) {
	start := strings.Index(line, "hello")
	quote := strings.Index(line, "\"")
	return start + 1, quote - 1
}

// quoteInsideOnLine returns the rune column inside the "hello" on a normal test line.
func quoteInsideOnLine(line string) int {
	return strings.Index(line, "hello") + 1
}

// TestStringContextLocalLargeIsBounded: a local large document now gets string and
// comment context, reading only a bounded window through the buffer's page cache (not
// the whole file), and the window/line bounds hold.
func TestStringContextLocalLargeIsBounded(t *testing.T) {
	t.Parallel()
	// The file is several MiB so the bounded window is a small fraction of it; the
	// target line is inside the head the large-file loader indexed at open.
	const lines, target = 300000, 100000
	ed := openLocalLargeDoc(t, localLargeQuoteContent(lines))
	doc := ed.Active()
	line := doc.Buffer.Line(target)
	inside, _ := quoteLineInside(line)
	if inside < 0 {
		t.Fatalf("test line has no string: %q", line)
	}

	before := doc.Buffer.Stats()
	c := &stringCache{}
	if !c.inString(doc, buffer.Pos{Line: target, Col: inside}) {
		t.Fatalf("a loaded local large line inside a string was not detected: %v", c.lines)
	}
	if c.inString(doc, buffer.Pos{Line: target, Col: 1}) {
		t.Fatal("code on a local large line was reported inside a string")
	}
	after := doc.Buffer.Stats()
	if got := c.end - c.start; got > 2*stringReach+1 {
		t.Fatalf("context window = %d lines, want at most %d", got, 2*stringReach+1)
	}
	var bytes int
	for _, l := range c.lines {
		bytes += len(l) + 1
	}
	if bytes > stringMaxBytes+maxLexedLineBytes {
		t.Fatalf("context window = %d bytes, want at most %d", bytes, stringMaxBytes+maxLexedLineBytes)
	}
	// The read is bounded by the window, not by the file.
	read := after.ReadBytes - before.ReadBytes
	lineCalls := after.LineCalls - before.LineCalls
	// Page-granular reads can exceed the text budget, but they are bounded by the
	// window (page granularity and two index probes per line), not by the file size.
	if read > 1<<20 || read > after.TotalBytes/4 || lineCalls > 4*(2*stringReach+1) {
		t.Fatalf("query read %d bytes/%d lines of a %d-byte local large file; want a bounded window", read, lineCalls, after.TotalBytes)
	}

	// The non-code mask marks the string and the comment on a local large line.
	mask := NewNonCodeContext()(doc, target)
	var masked int
	for _, m := range mask {
		if m {
			masked++
		}
	}
	if len(mask) != len([]rune(line)) || masked < 5 {
		t.Fatalf("non-code mask = %v for %q, want the string and comment masked", mask, line)
	}
}

// TestStringContextLocalLargeLongLineBoundary: a line over the 4096-byte lexer limit
// has no context, is not materialized, and bounds the window of the nearby lines
// instead of being lexed or read whole.
func TestStringContextLocalLargeLongLineBoundary(t *testing.T) {
	t.Parallel()
	huge := "var huge = \"" + strings.Repeat("x", 10000) + "\""
	lines := make([]string, 0, 102)
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("var before%02d = \"hello\" // c", i))
	}
	lines = append(lines, huge)
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("var after%02d = \"hello\" // c", i))
	}
	ed := openLocalLargeDoc(t, strings.Join(lines, "\n")+"\n")
	doc := ed.Active()
	if !doc.Large() {
		t.Fatal("the test file is not large")
	}

	before := doc.Buffer.Stats()
	c := &stringCache{}
	if c.inString(doc, buffer.Pos{Line: 50, Col: 12}) {
		t.Fatal("a line over the lexer limit got string context")
	}
	if c.nonCode(doc, 50) != nil {
		t.Fatal("a line over the lexer limit got a non-code mask")
	}
	if c.lexed != 0 {
		t.Fatal("a too-long query line was lexed")
	}
	if after := doc.Buffer.Stats(); after.MaterializedBytes != before.MaterializedBytes {
		t.Fatalf("the long line was materialized (%d->%d bytes)", before.MaterializedBytes, after.MaterializedBytes)
	}

	// A normal line before the huge one still gets context, and the huge line bounds
	// the forward window instead of being read or lexed.
	if !c.inString(doc, buffer.Pos{Line: 49, Col: quoteInsideOnLine(lines[49])}) {
		t.Fatal("the normal line before the long line lost its string context")
	}
	if c.end > 50 {
		t.Fatalf("window [%d, %d) includes the huge line at 50", c.start, c.end)
	}
	for _, l := range c.lines {
		if len(l) > maxLexedLineBytes {
			t.Fatalf("the window includes a %d-byte line", len(l))
		}
	}
	// A normal line after the huge one gets context starting after it.
	if !c.inString(doc, buffer.Pos{Line: 51, Col: quoteInsideOnLine(lines[51])}) {
		t.Fatal("the normal line after the long line lost its string context")
	}
	if c.start < 51 {
		t.Fatalf("window [%d, %d) reaches back over the huge line", c.start, c.end)
	}
}

// TestStringContextLocalLargeEditing: the context works while editing a local large
// file: a quote inside the loaded window's string is not paired, the same quote in
// code is paired, and the document stays editable.
func TestStringContextLocalLargeEditing(t *testing.T) {
	t.Parallel()
	const lines, target = 20000, 10000
	ed := openLocalLargeDoc(t, localLargeQuoteContent(lines))
	doc := ed.Active()
	ed.SetStringContext(NewStringContext())
	ed.SetNonCodeContext(NewNonCodeContext())
	if doc.ReadOnly() {
		t.Fatal("the local large document is read-only")
	}
	line := doc.Buffer.Line(target)
	inside, code := quoteLineInside(line)

	doc.Cursor, doc.Sel = buffer.Pos{Line: target, Col: inside}, nil
	if res := ed.Do("insert_char", map[string]any{"text": "\""}); res.Err != nil {
		t.Fatalf("typing inside the string: %v", res.Err)
	}
	line = doc.Buffer.Line(target)
	if got := strings.Count(line, "\""); got != 3 {
		t.Fatalf("quotes after typing inside the string = %d in %q, want 3 (no pair)", got, line)
	}

	if res := ed.Do("undo", nil); res.Err != nil {
		t.Fatalf("undo: %v", res.Err)
	}
	doc.Cursor, doc.Sel = buffer.Pos{Line: target, Col: code}, nil
	if res := ed.Do("insert_char", map[string]any{"text": "\""}); res.Err != nil {
		t.Fatalf("typing outside the string: %v", res.Err)
	}
	line = doc.Buffer.Line(target)
	if got := strings.Count(line, "\""); got != 4 {
		t.Fatalf("quotes after typing in code = %d in %q, want 4 (auto-closed pair)", got, line)
	}
	if !doc.Modified() {
		t.Fatal("editing a local large document did not mark it modified")
	}
}
