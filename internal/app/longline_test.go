package app

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
)

// openLong opens a file with the content in a runtime of the real model, with the index of a
// large file, if it is one, built to the end.
func openLong(tb testing.TB, name, content string) *runtime {
	tb.Helper()
	dir := tb.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
	rt, err := buildRuntime(tb.Context(), Options{Root: dir, Files: []FileTarget{{Path: path}}}, false)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { rt.close(); rt.ed.Close() })
	_, cmd := rt.model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	for i := 0; cmd != nil && i < 1_000_000; i++ {
		_, cmd = rt.model.Update(cmd())
	}
	rt.model.View()
	return rt
}

// key sends a key press and paints the frame, as the program does.
func key(rt *runtime, k tea.KeyPressMsg) string {
	_, _ = rt.model.Update(k)
	return ansi.Strip(rt.model.View().Content)
}

var (
	ctrlEnd = tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl}
	ctrlHom = tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl}
	keyEnd  = tea.KeyPressMsg{Code: tea.KeyEnd}
)

// words is text of n runes of words, ending in the marker.
func longText(n int, marker string) string {
	var b strings.Builder
	for k := 0; b.Len() < n; k++ {
		b.WriteString(`{"k":"v`)
		b.WriteString(strings.Repeat("a", k%7))
		b.WriteString(`"}, `)
	}
	return b.String()[:n] + marker
}

// A line of megabytes in a document in memory wraps, and the end of it can be reached and typed
// at. The caret goes to the real end, the frame shows it, and a key typed there is on the line.
func TestVeryLongLineInMemoryWrapsAndReachesItsEnd(t *testing.T) {
	isolateConfig(t)
	const n = 3_000_000
	rt := openLong(t, "a.json", longText(n, "THE-END")+"\nnext\n")
	d := rt.ed.Active()
	if d.Large() {
		t.Fatal("the file is large")
	}
	// End goes to the end of the row, then of the next one: the second row ends where the first
	// does not, and the caret is on the first rows of the line.
	key(rt, keyEnd)
	first := d.Cursor
	key(rt, keyEnd)
	if first.Line != 0 || first.Col == 0 || d.Cursor.Col <= first.Col || d.Cursor.Col > 4*first.Col {
		t.Fatalf("End twice: caret %+v then %+v, want the ends of the first two rows", first, d.Cursor)
	}
	// From near the end of the line, End on each row reaches the real end of the line, and the
	// frame follows the caret there.
	rt.ed.SetCursor(buffer.Pos{Col: d.Buffer.LineLen(0) - 500}, false)
	var screen string
	for range 40 {
		screen = key(rt, keyEnd)
		if d.Cursor.Col == d.Buffer.LineLen(0) {
			break
		}
	}
	if got, want := d.Cursor.Col, d.Buffer.LineLen(0); got != want || d.Cursor.Line != 0 {
		t.Fatalf("the caret is at %+v after End on each row, want the end %d of the line", d.Cursor, want)
	}
	if !strings.Contains(screen, "THE-END") {
		t.Errorf("the frame does not show the end of the line:\n%s", screen)
	}
	screen = key(rt, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !strings.Contains(screen, "THE-ENDx") {
		t.Errorf("the typed character is not at the end of the line:\n%s", screen)
	}
	if got := d.Buffer.Line(0); !strings.HasSuffix(got, "THE-ENDx") {
		t.Errorf("the line ends with %q", got[len(got)-12:])
	}
}

// Typing at the end of a very long line costs a bounded amount of work: the memory it allocates
// does not grow with the line. A frame that built the line as text or runes would allocate
// megabytes for each key.
func TestTypingAtTheEndOfAVeryLongLineAllocatesLittle(t *testing.T) {
	isolateConfig(t)
	const n = 2_000_000
	rt := openLong(t, "a.json", longText(n, "")+"\n")
	d := rt.ed.Active()
	rt.ed.SetCursor(buffer.Pos{Col: d.Buffer.LineLen(0)}, false)
	key(rt, tea.KeyPressMsg{Code: tea.KeyRight})
	typed := func() uint64 {
		var before, after goruntime.MemStats
		goruntime.ReadMemStats(&before)
		key(rt, tea.KeyPressMsg{Code: 'x', Text: "x"})
		goruntime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	typed() // the first key sets up what the rest reuse
	var worst uint64
	for range 10 {
		worst = max(worst, typed())
	}
	t.Logf("a key typed at the end of the line allocated at most %d bytes", worst)
	if worst > 4<<20 {
		t.Errorf("a key typed at the end of a %d byte line allocated %d bytes, want a few segments' worth", n, worst)
	}
}

// A file of more than the large file threshold with one line of tens of megabytes is read to its
// end: Ctrl+End puts the caret at the real end of the line, the frame shows it, the characters
// before the caret are selected and copied, and the line, which is not editable, says why.
func TestHugeLineInALargeFileIsReachedAndCopied(t *testing.T) {
	isolateConfig(t)
	const n = 12 << 20
	content := longText(n, "THE-TAIL-MARK")
	rt := openLong(t, "big.json", content)
	d := rt.ed.Active()
	if !d.Large() {
		t.Fatalf("%d bytes did not open in large file mode", len(content))
	}
	if got := d.Buffer.LineLen(0); got != len(content) {
		t.Fatalf("the line has %d runes, want %d", got, len(content))
	}
	screen := key(rt, ctrlEnd)
	if got := d.Cursor; got.Line != 0 || got.Col != len(content) {
		t.Fatalf("ctrl+end: caret %+v, want column %d, the real end of the line", got, len(content))
	}
	if !strings.Contains(screen, "THE-TAIL-MARK") {
		t.Errorf("the frame does not show the end of the line:\n%s", screen)
	}
	for range 4 {
		key(rt, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	}
	if r := rt.ed.Do("copy", nil); r.Err != nil || r.Text != "MARK" {
		t.Errorf("copy = %q, %v; want the 4 runes before the caret", r.Text, r.Err)
	}
	// Home goes to the start of the line, and the first row shows it.
	screen = key(rt, ctrlHom)
	if !strings.Contains(screen, `{"k":"v`) {
		t.Errorf("the frame does not show the start of the line:\n%s", screen)
	}
	// An edit at the start of the line is made: the line is read whole and the key goes in there.
	key(rt, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := d.Buffer.LineSlice(0, 0, 2); got != "x{" {
		t.Errorf("after typing x at the start, the line begins %q, want %q", got, "x{")
	}
	if got := d.Buffer.LineLen(0); got != len(content)+1 {
		t.Errorf("the line has %d runes after the edit, want %d", got, len(content)+1)
	}
}

func BenchmarkTypeAtTheEndOfAVeryLongLine(b *testing.B) {
	rt := openLong(b, "a.json", longText(5_000_000, "")+"\n")
	d := rt.ed.Active()
	rt.ed.SetCursor(buffer.Pos{Col: d.Buffer.LineLen(0)}, false)
	key(rt, tea.KeyPressMsg{Code: tea.KeyRight})
	x := tea.KeyPressMsg{Code: 'x', Text: "x"}
	b.ResetTimer()
	for range b.N {
		key(rt, x)
	}
}

func BenchmarkMoveInAVeryLongLine(b *testing.B) {
	rt := openLong(b, "a.json", longText(5_000_000, "")+"\n")
	left, right := tea.KeyPressMsg{Code: tea.KeyLeft}, tea.KeyPressMsg{Code: tea.KeyRight}
	b.ResetTimer()
	for i := range b.N {
		if i%2 == 0 {
			key(rt, right)
		} else {
			key(rt, left)
		}
	}
}

func BenchmarkTypeInA9MBFile(b *testing.B) {
	var sb strings.Builder
	for i := 0; sb.Len() < 9_300_000; i++ {
		sb.WriteString("func f() { return 1 } // comment text here\n")
	}
	rt := openLong(b, "a.go", sb.String())
	x := tea.KeyPressMsg{Code: 'x', Text: "x"}
	b.ResetTimer()
	for range b.N {
		key(rt, x)
	}
}

func BenchmarkTypeInALongWrappedParagraph(b *testing.B) {
	rt := openLong(b, "a.md", strings.Repeat("lorem ipsum dolor sit amet ", 2500)+"\n")
	d := rt.ed.Active()
	rt.ed.SetCursor(buffer.Pos{Col: d.Buffer.LineLen(0)}, false)
	key(rt, tea.KeyPressMsg{Code: tea.KeyRight})
	x := tea.KeyPressMsg{Code: 'x', Text: "x"}
	b.ResetTimer()
	for range b.N {
		key(rt, x)
	}
}
