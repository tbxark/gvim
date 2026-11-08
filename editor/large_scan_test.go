package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// lineCalls returns how many base lines d has read from its source so far.
func lineCalls(d *Document) int64 { return d.Buffer.Stats().LineCalls }

func TestScanBudget(t *testing.T) {
	t.Parallel()
	small := buffer.New(strings.Repeat("line\n", 50))
	if got := newScanBudget(small); got != nil {
		t.Fatalf("budget of a memory buffer = %+v, want none", got)
	}
	var none *scanBudget
	if !none.take(small, 0) || none.exhausted() {
		t.Fatal("a nil budget must allow every line")
	}

	_, d := openIndexed(t, largeOptions(), strings.Repeat("0123456789\n", 200))
	b := newScanBudget(d.Buffer)
	if b.lines != ScanLinesCap || b.bytes != ScanBytesBudget {
		t.Fatalf("budget = %+v", b)
	}
	b = &scanBudget{lines: 3, bytes: 1 << 20}
	for i := range 3 {
		if !b.take(d.Buffer, i) {
			t.Fatalf("line %d refused with lines left", i)
		}
	}
	if b.take(d.Buffer, 3) || !b.exhausted() {
		t.Fatal("the fourth line must be refused")
	}
	b = &scanBudget{lines: 100, bytes: 25}
	if !b.take(d.Buffer, 0) || !b.take(d.Buffer, 1) || b.take(d.Buffer, 2) {
		t.Fatal("a budget of 25 bytes holds two lines of 10")
	}
	if b.take(d.Buffer, 3) {
		t.Fatal("a spent budget must stay spent")
	}
}

func TestBracketMatchStopsAtTheLineCap(t *testing.T) {
	t.Parallel()
	const width = 64
	e, d, _ := openVirtual(t, 20_000*width, width)
	indexAll(t, e)
	if err := e.GotoLine(15_000, 3); err != nil {
		t.Fatal(err)
	}
	at, version, before := d.Cursor, d.Buffer.Version(), lineCalls(d)
	r := e.Do("goto_matching_bracket", nil)
	if !errors.Is(r.Err, ErrScanLimit) {
		t.Fatalf("goto_matching_bracket = %v, want ErrScanLimit", r.Err)
	}
	if got := lineCalls(d) - before; got > ScanLinesCap+10 {
		t.Errorf("the scan read %d lines, want at most %d", got, ScanLinesCap)
	}
	if d.Cursor != at || d.Buffer.Version() != version {
		t.Errorf("caret %v -> %v: a stopped scan must not move", at, d.Cursor)
	}
	if st := d.Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes != 0 {
		t.Errorf("stats = %+v", st)
	}
}

func TestBracketMatchStopsAtTheByteBudget(t *testing.T) {
	t.Parallel()
	const width = 100 << 10
	e, d, _ := openVirtual(t, 200*width, width)
	indexAll(t, e)
	if err := e.GotoLine(150, 3); err != nil {
		t.Fatal(err)
	}
	before := d.Buffer.Stats()
	if r := e.Do("goto_matching_bracket", nil); !errors.Is(r.Err, ErrScanLimit) {
		t.Fatalf("goto_matching_bracket = %v, want ErrScanLimit", r.Err)
	}
	after := d.Buffer.Stats()
	if got, want := after.LineCalls-before.LineCalls, int64(ScanBytesBudget/width+3); got > want {
		t.Errorf("the scan read %d lines of %d KiB, want at most %d", got, width>>10, want)
	}
	if got := after.ReadBytes - before.ReadBytes; got > 2*ScanBytesBudget {
		t.Errorf("the scan read %d bytes from the source, want about %d", got, ScanBytesBudget)
	}
}

func TestBracketMatchStillFindsPairsInALargeDocument(t *testing.T) {
	t.Parallel()
	content := "start (\n" + strings.Repeat("filler\n", 300) + "end )\n"
	e, d := openIndexed(t, largeOptions(), content)
	d.Cursor = buffer.Pos{Line: 0, Col: 6}
	run(t, e, "goto_matching_bracket")
	if want := (buffer.Pos{Line: 301, Col: 4}); d.Cursor != want {
		t.Fatalf("caret = %v, want %v", d.Cursor, want)
	}
	run(t, e, "goto_matching_bracket")
	if want := (buffer.Pos{Line: 0, Col: 6}); d.Cursor != want {
		t.Fatalf("caret = %v, want %v", d.Cursor, want)
	}
}

func TestBracketForwardScanStopsAtTheKnownIndex(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), "open (\n"+lineText(400)+") close\n")
	if d.Index().Complete {
		t.Fatal("the index is complete")
	}
	d.Cursor = buffer.Pos{Line: 0, Col: 5}
	if r := e.Do("goto_matching_bracket", nil); !errors.Is(r.Err, ErrScanLimit) {
		t.Fatalf("goto_matching_bracket = %v, want ErrScanLimit: the match may be past the index", r.Err)
	}
}

func TestParagraphMotionNeverReadsALine(t *testing.T) {
	t.Parallel()
	const width = 64
	e, d, _ := openVirtual(t, 20_000*width, width)
	indexAll(t, e)
	for _, tt := range []struct {
		name   Action
		line   int
		target int
	}{
		{name: "move_to_paragraph_down", line: 100},
		{name: "select_to_paragraph_down", line: 100},
		{name: "move_to_paragraph_up", line: 15_000},
		{name: "select_to_paragraph_up", line: 15_000},
	} {
		if err := e.GotoLine(tt.line, 1); err != nil {
			t.Fatal(err)
		}
		at, before := d.Cursor, d.Buffer.Stats()
		if r := e.Do(tt.name, nil); !errors.Is(r.Err, ErrScanLimit) {
			t.Errorf("Do(%s) = %v, want ErrScanLimit", tt.name, r.Err)
		}
		after := d.Buffer.Stats()
		if d.Cursor != at || d.HasSelection() {
			t.Errorf("%s moved the caret %v -> %v", tt.name, at, d.Cursor)
		}
		if after.LineCalls != before.LineCalls || after.DecodedRunes != before.DecodedRunes || after.MaterializedBytes != 0 {
			t.Errorf("%s read text: %+v -> %+v", tt.name, before, after)
		}
	}
}

func TestParagraphMotionFindsBlankLines(t *testing.T) {
	t.Parallel()
	content := "a\nb\n\nc\nd\n\n" + lineText(200)
	e, d := openIndexed(t, largeOptions(), content)
	run(t, e, "move_to_paragraph_down")
	if d.Cursor != (buffer.Pos{Line: 2}) {
		t.Fatalf("down: caret = %v, want line 2", d.Cursor)
	}
	run(t, e, "move_to_paragraph_down")
	if d.Cursor != (buffer.Pos{Line: 5}) {
		t.Fatalf("down again: caret = %v, want line 5", d.Cursor)
	}
	run(t, e, "move_to_paragraph_up")
	if d.Cursor != (buffer.Pos{Line: 2}) {
		t.Fatalf("up: caret = %v, want line 2", d.Cursor)
	}
}

func TestParagraphMotionWaitsForTheIndex(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	if d.Index().Complete {
		t.Fatal("the index is complete")
	}
	if r := e.Do("move_to_paragraph_down", nil); !errors.Is(r.Err, ErrScanLimit) || d.Cursor != (buffer.Pos{}) {
		t.Fatalf("move_to_paragraph_down = %v, caret %v: it must not jump to the end of the known text", r.Err, d.Cursor)
	}
}

func TestDabbrevStopsAtTheLineCap(t *testing.T) {
	t.Parallel()
	const width = 64
	e, d, _ := openVirtual(t, 20_000*width, width)
	indexAll(t, e)
	if err := e.GotoLine(19_000, 1); err != nil {
		t.Fatal(err)
	}
	run(t, e, "move_line_end")
	before, version := lineCalls(d), d.Buffer.Version()
	r := e.Do("dabbrev_expand", nil)
	if !errors.Is(r.Err, ErrScanLimit) {
		t.Fatalf("dabbrev_expand = %v, want ErrScanLimit", r.Err)
	}
	if got := lineCalls(d) - before; got > ScanLinesCap+10 {
		t.Errorf("the scan read %d lines, want at most %d", got, ScanLinesCap)
	}
	if d.Buffer.Version() != version {
		t.Error("a failed expansion changed the text")
	}
}

// openBlank opens a large document of blank lines with the smart typing rules on.
func openBlank(t *testing.T, lines int) (*Editor, *Document) {
	t.Helper()
	opts := largeOptions()
	opts.AutoIndent, opts.TabSize = true, 4
	e, d := openLarge(t, opts, "x\n"+strings.Repeat("\n", lines))
	indexAll(t, e)
	return e, d
}

func TestIndentScansStopAtTheLineCap(t *testing.T) {
	t.Parallel()
	const lines = 20_000
	e, d := openBlank(t, lines)
	if err := e.GotoLine(19_000, 1); err != nil {
		t.Fatal(err)
	}
	before := lineCalls(d)
	run(t, e, "insert_newline")
	// Without a cap the scan up reads 19 000 lines and the scan down 1 000.
	if got := lineCalls(d) - before; got > ScanLinesCap+1_000+10 {
		t.Errorf("Enter read %d lines, want at most %d", got, ScanLinesCap+1_000)
	}
	if d.Cursor.Line != 19_000 {
		t.Errorf("caret line = %d: Enter must start a line below", d.Cursor.Line)
	}

	typeText(t, e, "    ")
	before = lineCalls(d)
	typeText(t, e, "}")
	if got := lineCalls(d) - before; got > ScanLinesCap+10 {
		t.Errorf("typing a closer read %d lines, want at most %d", got, ScanLinesCap)
	}
	if st := d.Buffer.Stats(); st.OverCap != 0 {
		t.Errorf("OverCap = %d", st.OverCap)
	}
}

func TestEditorSmokeVirtual8GB(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("short mode")
	}
	const width = 64
	e, d, src := openVirtual(t, 8<<30, width)
	if !d.Large() || d.Index().Complete {
		t.Fatalf("large %v, complete %v", d.Large(), d.Index().Complete)
	}
	st := d.Buffer.Stats()
	if st.TotalBytes != 8<<30 || src.BytesRead() > buffer.DefaultMaxLine+8<<20 {
		t.Fatalf("open: total %d, read %d bytes", st.TotalBytes, src.BytesRead())
	}
	baseline := st.ReadBytes
	for range 50 {
		if r := e.Do("move_page_down", nil); r.Err != nil {
			t.Fatalf("move_page_down: %v", r.Err)
		}
	}
	callsAfterScroll := lineCalls(d)
	if callsAfterScroll > 50*int64(e.rows+10) {
		t.Errorf("50 page downs read %d lines", callsAfterScroll)
	}
	typeText(t, e, "hello")
	if got := d.Buffer.Line(d.Cursor.Line); !strings.Contains(got, "hello") {
		t.Fatalf("typed line = %q", got)
	}
	if err := e.GotoLine(1000, 1); err != nil {
		t.Fatal(err)
	}
	if d.Cursor.Line != 999 {
		t.Fatalf("goto 1000: caret line %d", d.Cursor.Line)
	}
	before := lineCalls(d)
	run(t, e, "move_down", "move_line_end", "move_word_left")
	if got := lineCalls(d) - before; got > 40 {
		t.Errorf("three moves read %d lines", got)
	}
	st = d.Buffer.Stats()
	if st.OverCap != 0 || st.MaterializedBytes != 0 {
		t.Errorf("stats = %+v", st)
	}
	if st.ReadBytes-baseline > 16<<20 {
		t.Errorf("scrolling and editing read %d bytes of 8 GiB", st.ReadBytes-baseline)
	}
	// An action that needs the end of the document waits for the index instead of
	// reading to it.
	run(t, e, "move_document_end")
	if d.large.pending == nil {
		t.Error("move_document_end did not wait for the index")
	}
}

// TestEveryActionOnALargeDocument runs every action on a large document, with and
// without a selection. None may panic, read more text than the cap, or leave a refusal
// for the next action, and the work it does must be bounded by the size of the document.
func TestEveryActionOnALargeDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := lineText(200)
	for _, a := range Actions() {
		t.Run(string(a), func(t *testing.T) {
			t.Parallel()
			for _, selected := range []bool{false, true} {
				path := filepath.Join(dir, fmt.Sprintf("%s-%t.txt", a, selected))
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				e := New(largeOptions())
				if err := e.OpenFile(path); err != nil {
					t.Fatal(err)
				}
				indexAll(t, e)
				d := e.Active()
				if !d.Large() {
					t.Fatal("the document is not large")
				}
				if err := e.GotoLine(20, 3); err != nil {
					t.Fatal(err)
				}
				if selected {
					selectRange(d, buffer.Pos{Line: 18, Col: 2}, buffer.Pos{Line: 22, Col: 4})
				}
				e.clipboard = "Q"
				args := map[string]any{
					"text":    "x",
					"name":    "utf-8",
					"value":   "lf",
					"command": "cat",
					"path":    filepath.Join(dir, fmt.Sprintf("%s-%t.saved", a, selected)),
					"force":   true,
				}
				e.Do(a, args)
				st := d.Buffer.Stats()
				if st.OverCap != 0 {
					t.Errorf("%s (selected %t): OverCap = %d", a, selected, st.OverCap)
				}
				if err := d.Buffer.Refusal(); err != nil {
					t.Errorf("%s (selected %t): refusal left behind: %v", a, selected, err)
				}
				if d.planErr != nil {
					t.Errorf("%s (selected %t): plan error left behind: %v", a, selected, d.planErr)
				}
				if bound := int64(20 * d.Buffer.Lines()); st.LineCalls > bound {
					t.Errorf("%s (selected %t): read %d lines, want at most %d", a, selected, st.LineCalls, bound)
				}
			}
		})
	}
}
