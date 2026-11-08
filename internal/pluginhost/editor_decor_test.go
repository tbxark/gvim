package pluginhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/search"
)

// decorSpans returns the byte spans (start, end) of the highlights the store holds for the
// line of the document.
func decorSpans(st *decorations.Store, id pluginapi.BufferID, line int) [][2]int {
	var out [][2]int
	for _, s := range st.Line(uint64(id), line).Spans {
		out = append(out, [2]int{s.Start, s.End})
	}
	return out
}

func hlItem(fl, fc, tl, tc int) decorations.Item {
	return decorations.Item{Kind: decorations.Highlight, FromLine: fl, FromCol: fc, ToLine: tl, ToCol: tc}
}

func TestDecorShiftedByListenerBeforeEditReturns(t *testing.T) {
	pe, ed, ids := pluginEditorOver(t, "hello\nworld\nthird")
	id := ids[0]
	st := pe.DecorStore()
	apptest.Must(t, st.Set(uint64(id), "p", "a", []decorations.Item{
		hlItem(1, 1, 1, 4), // "orl"
		{Kind: decorations.Gutter, FromLine: 2, Text: "+"},
	}))

	tests := []struct {
		name   string
		edit   func()
		span   [][2]int // spans of the line the highlight is on afterwards
		onLine int
		gutter int // the line the gutter mark is on afterwards
	}{
		{"insert above through the plugin port", func() {
			apptest.Must(t, pe.Edit(id, []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "ab\n"}}, pluginapi.EditOptions{}))
		}, [][2]int{{1, 4}}, 2, 3},
		{"typing on the highlight's line, through the buffer", func() {
			ed.Active().Buffer.Insert(buffer.Pos{Line: 2, Col: 0}, "ZZ")
		}, [][2]int{{3, 6}}, 2, 3},
		{"undo moves it back", func() {
			if res := ed.Do("undo", nil); res.Err != nil {
				t.Fatal(res.Err)
			}
		}, [][2]int{{1, 4}}, 2, 3},
		{"delete the line above", func() {
			ed.Active().Buffer.Delete(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 1, Col: 0})
		}, [][2]int{{1, 4}}, 1, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.edit()
			// No Update has run: the anchors moved while the edit was running.
			if got := decorSpans(st, id, tc.onLine); len(got) != 1 || got[0] != tc.span[0] {
				t.Fatalf("spans on line %d = %v, want %v", tc.onLine, got, tc.span)
			}
			if st.Line(uint64(id), tc.gutter).Gutter == nil {
				t.Fatalf("the gutter mark is not on line %d", tc.gutter)
			}
		})
	}
}

func TestDecorByteColumnsOfMultibyteText(t *testing.T) {
	pe, ed, ids := pluginEditorOver(t, "éabc")
	id := ids[0]
	st := pe.DecorStore()
	// "ab" is bytes 2 to 4 of the line: é is two bytes.
	apptest.Must(t, st.Set(uint64(id), "p", "a", []decorations.Item{hlItem(0, 2, 0, 4)}))
	ed.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "é世") // 2 + 3 bytes
	if got := decorSpans(st, id, 0); len(got) != 1 || got[0] != [2]int{7, 9} {
		t.Fatalf("spans = %v, want [[7 9]]", got)
	}
	// The text is now "é世éabc". Typing at the start of the range (before the a) moves it,
	// typing at its end (before the c) leaves it, and typing inside grows it.
	buf := ed.Active().Buffer
	tests := []struct {
		col  int // rune column of the insert
		want [2]int
	}{{3, [2]int{8, 10}}, {6, [2]int{8, 10}}, {5, [2]int{8, 11}}}
	for _, tc := range tests {
		buf.Insert(buffer.Pos{Line: 0, Col: tc.col}, string(rune('x'+tc.col%3)))
		if got := decorSpans(st, id, 0); len(got) != 1 || got[0] != tc.want {
			t.Fatalf("after an insert at rune column %d: spans = %v, want %v (text %q)", tc.col, got, tc.want, buf.Line(0))
		}
	}
}

func TestDecorListenerRunsBeforeLaterListeners(t *testing.T) {
	pe, ed, ids := pluginEditorOver(t, "one\ntwo")
	id := ids[0]
	st := pe.DecorStore()
	apptest.Must(t, st.Set(uint64(id), "p", "a", []decorations.Item{{Kind: decorations.EOL, FromLine: 1, Text: "t"}}))
	var sawLine = -1
	ed.AddChangeListener("later", func(doc editor.DocID, c buffer.Change) {
		for l := range 5 {
			if len(st.Line(uint64(doc), l).EOL) == 1 {
				sawLine = l
			}
		}
	})
	ed.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "\n\n")
	if sawLine != 3 {
		t.Fatalf("a later listener saw the item on line %d, want 3 (already moved)", sawLine)
	}
}

func TestDecorDroppedWhenTheBufferIsReplaced(t *testing.T) {
	apptest.IsolateConfig(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("one\ntwo\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	pe := newPluginEditor(ed, func(*editor.Document) {})
	id := pe.ActiveBuffer()
	st := pe.DecorStore()
	apptest.Must(t, st.Set(uint64(id), "p", "a", []decorations.Item{hlItem(0, 0, 0, 2), {Kind: decorations.Gutter, FromLine: 1, Text: "+"}}))
	apptest.Must(t, os.WriteFile(path, []byte("uno\ndos\n"), 0o644))
	apptest.Must(t, ed.Revert(true))
	if got := ed.Active().Buffer.Line(0); got != "uno" {
		t.Fatalf("the revert did not happen: %q", got)
	}
	if st.Count(uint64(id)) != 0 {
		t.Fatalf("%d decorations survive a revert; the plugins set them again on the next event", st.Count(uint64(id)))
	}
}

func TestDecorOnALargeDocumentIsDroppedByAnEdit(t *testing.T) {
	// An edit of a large document is reported as a change of unknown extent, so the
	// anchors cannot follow it: the decorations go, and the plugins set them again.
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	apptest.Must(t, os.WriteFile(path, []byte(strings.Repeat("some line of text\n", 250)), 0o644))
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	apptest.Must(t, ed.OpenFile(path))
	if !ed.Active().Large() {
		t.Fatal("the document is not large")
	}
	pe := newPluginEditor(ed, func(*editor.Document) {})
	id := pe.ActiveBuffer()
	st := pe.DecorStore()
	apptest.Must(t, st.Set(uint64(id), "p", "a", []decorations.Item{hlItem(200, 1, 200, 4), {Kind: decorations.Gutter, FromLine: 100, Text: "+"}}))
	// Reading a line far down touches no decoration but its own.
	if got := decorSpans(st, id, 200); len(got) != 1 || got[0] != [2]int{1, 4} {
		t.Fatalf("spans on line 200 = %v", got)
	}
	ed.Do("insert_char", map[string]any{"text": "Z"})
	if st.Count(uint64(id)) != 0 {
		t.Fatalf("%d decorations survive an edit of a large document", st.Count(uint64(id)))
	}
}

// TestDecorationsEndToEnd runs a plugin through the real runtime, editor and ui: it puts
// decorations on a line, an edit moves them with the text, and the frame shows them.

// bulkReplace replaces every "foo" of a 50 000 line file, one in every tenth line, by two
// lines, with the given number of line decorations in each of three namespaces. It returns
// the time ReplaceMatches took and the editor, with the store.
func bulkReplace(t *testing.T, perNS int) (time.Duration, *pluginEditor) {
	t.Helper()
	var sb strings.Builder
	for i := range 50_000 {
		if i%10 == 0 {
			sb.WriteString("foo\n")
		} else {
			sb.WriteString("x\n")
		}
	}
	pe, ed, ids := pluginEditorOver(t, sb.String())
	st := pe.DecorStore()
	for _, ns := range []string{"a", "b", "c"} {
		items := make([]decorations.Item, perNS)
		for i := range items {
			items[i] = decorations.Item{Kind: decorations.Gutter, FromLine: i * 3, Text: "+"}
		}
		apptest.Must(t, st.Set(uint64(ids[0]), "p", ns, items))
	}
	start := time.Now()
	n, err := ed.ReplaceMatches(search.Query{Text: "foo", CaseSensitive: true}, "a\nb")
	took := time.Since(start)
	if n != 5000 || err != nil {
		t.Fatalf("ReplaceMatches = %d, %v", n, err)
	}
	return took, pe
}

// TestReplaceAllMovesDecorationsInOnePass runs Replace All with a line break in the
// replacement, 5000 changes, over 48 000 decorations. The anchors move once for the whole
// operation: it must not cost the changes times the decorations, which was ten times the
// operation without decorations.
func TestReplaceAllMovesDecorationsInOnePass(t *testing.T) {
	base, _ := bulkReplace(t, 0)
	took, pe := bulkReplace(t, 16_000)
	if took > 3*base+150*time.Millisecond {
		t.Errorf("Replace All took %v with 48000 decorations, %v with none", took, base)
	}
	id := pe.ListBuffers()[0].ID
	items := pe.DecorStore().Items(uint64(id), "p", "c")
	if len(items) != 16_000 {
		t.Fatalf("%d decorations left, want 16000", len(items))
	}
	// The matches are on the lines that are multiples of 10, and each one adds a line. A match
	// at the start of the decorated line pushes the decoration down with the line's own text.
	for _, i := range []int{0, 1, 4, 5000, 15999} {
		line := 3 * i
		want := line + line/10 + 1
		if got := items[i].FromLine; got != want {
			t.Errorf("decoration %d is on line %d, want %d", i, got, want)
		}
	}
}
