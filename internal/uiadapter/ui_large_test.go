package uiadapter

import (
	"context"
	"fmt"
	"github.com/tbxark/gvim/internal/apptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/highlight"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/search"
	"github.com/tbxark/gvim/theme"
)

// bigLines writes a file of 5.5 MiB, which is over the 4 MiB that opening a large file
// indexes at once. Every 1000th line holds "needle".
func bigLines(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	for i := 0; sb.Len() < 5<<20+512<<10; i++ {
		if i%1000 == 0 {
			fmt.Fprintf(&sb, "%d needle %030d\n", i, 0)
			continue
		}
		fmt.Fprintf(&sb, "%039d\n", i)
	}
	path := filepath.Join(dir, "big.txt")
	apptest.Must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	return path
}

// openBig opens the file at path in an editor with large file mode on at 1 KiB.
func openBig(t *testing.T, path string) *editor.Editor {
	t.Helper()
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	apptest.Must(t, ed.OpenFile(path))
	t.Cleanup(ed.Close)
	if !ed.Active().Large() {
		t.Fatal("the document is not large")
	}
	return ed
}

// finishIndex runs the line index of the editor's documents to the end through the
// adapter, as the root does.
func finishIndex(t *testing.T, a *Editor) int {
	t.Helper()
	steps := 0
	for {
		job, ok := a.PlanIndex()
		if !ok {
			return steps
		}
		a.ApplyIndex(job.Run(context.Background()))
		steps++
		if steps > 100 {
			t.Fatal("the index did not end")
		}
	}
}

func TestDocumentAdapterOfALargeDocumentHoldsNoSavedTextAndNoSpans(t *testing.T) {
	t.Parallel()
	ed := openBig(t, bigLines(t, t.TempDir()))
	a := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	doc := a.Active()
	ld, ok := doc.(tui.LargeDoc)
	if !ok || !ld.Large() {
		t.Fatalf("the adapter's document does not report large file mode: %T", doc)
	}
	if info := ld.Index(); info.Complete || info.Lines != doc.LineCount() {
		t.Errorf("Index = %+v, want an incomplete index with %d lines", info, doc.LineCount())
	}
	ed.Do("insert_char", map[string]any{"text": "Z"})
	if got := doc.SavedLines(); got != nil {
		t.Errorf("SavedLines has %d lines; a large document holds none", len(got))
	}
	for _, i := range []int{0, 5, 100} {
		if got := doc.LineSpans(i); got != nil {
			t.Errorf("LineSpans(%d) = %v, want none: the lexer would read the whole file", i, got)
		}
	}
	if st := ed.Active().Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes > 1<<20 {
		t.Errorf("the adapter read the text: %+v", st)
	}
}

func TestDocumentAdapterOfALargeDocumentAnswersTheLargeViews(t *testing.T) {
	t.Parallel()
	ed := openBig(t, bigLines(t, t.TempDir()))
	a := &Editor{ed: ed}
	da, ok := a.Active().(documentAdapter)
	if !ok {
		t.Fatalf("Active is %T", a.Active())
	}
	if got := da.ApproxLineCount(); got < da.LineCount() {
		t.Errorf("ApproxLineCount = %d below the %d lines known", got, da.LineCount())
	}
	if da.LineDirty(0) {
		t.Errorf("an unmodified document has a dirty line")
	}
	ed.Do("insert_char", map[string]any{"text": "Z"})
	if !da.LineDirty(0) || da.LineDirty(5) {
		t.Errorf("LineDirty(0) = %v, LineDirty(5) = %v after typing on line 0", da.LineDirty(0), da.LineDirty(5))
	}
	line := da.Line(3)
	if got, want := da.LineSlice(3, 4, 10), string([]rune(line)[4:14]); got != want {
		t.Errorf("LineSlice = %q, want %q", got, want)
	}
	if got := da.LineBytes(3); got != len(line) {
		t.Errorf("LineBytes = %d, want %d", got, len(line))
	}
	if got := da.LineLen(3); got != len([]rune(line)) {
		t.Errorf("LineLen = %d, want %d", got, len([]rune(line)))
	}
}

func TestAdapterRunsTheIndexSteps(t *testing.T) {
	t.Parallel()
	ed := openBig(t, bigLines(t, t.TempDir()))
	a := &Editor{ed: ed}
	if steps := finishIndex(t, a); steps == 0 {
		t.Fatalf("no index step was planned for an incomplete index")
	}
	info := ed.Active().Index()
	if !info.Complete || info.Lines < 100_000 {
		t.Errorf("Index after the steps = %+v", info)
	}
	if _, ok := a.PlanIndex(); ok {
		t.Errorf("a step was planned for a complete index")
	}
}

func TestAdapterSearchesALargeDocumentWithoutAList(t *testing.T) {
	t.Parallel()
	ed := openBig(t, bigLines(t, t.TempDir()))
	a := &Editor{ed: ed}
	finishIndex(t, a)
	doc, ver, complete, ok := a.SearchKey()
	if !ok || doc != ed.Active() || ver != ed.Active().Buffer.Version() || !complete {
		t.Fatalf("SearchKey = %v %d %v %v", doc, ver, complete, ok)
	}
	q := search.Query{Text: "needle"}
	if _, err := a.FindMatches(q); err == nil {
		t.Errorf("FindMatches lists the matches of a large document")
	}
	m, found, scanned, _, err := a.FindNext(q, buffer.Pos{Line: 1}, false, search.Cursor{})
	if err != nil || !found || !scanned || m.Start.Line != 1000 {
		t.Fatalf("FindNext = %+v %v %v %v, want the match on line 1000", m, found, scanned, err)
	}
	m, found, _, _, err = a.FindNext(q, buffer.Pos{Line: 1000}, true, search.Cursor{})
	if err != nil || !found || m.Start.Line != 0 {
		t.Fatalf("FindNext backward = %+v %v %v, want the match on line 0", m, found, err)
	}
	visible, err := a.VisibleMatches(q, 995, 1005)
	if err != nil || len(visible) != 1 || visible[0].Start.Line != 1000 {
		t.Fatalf("VisibleMatches = %v %v, want only line 1000", visible, err)
	}
	sc, err := a.StartScan(q, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	for done := false; !done; {
		done, err = sc.Step(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if want := (ed.Active().Buffer.Lines()-1)/1000 + 1; len(sc.Matches()) != want {
		t.Errorf("the scan found %d matches, want %d", len(sc.Matches()), want)
	}
	if _, err := a.StartScan(search.Query{Text: "("}, 0); err != nil {
		// A plain query that is not a regex compiles.
		t.Errorf("StartScan of a literal paren: %v", err)
	}
	if _, err := a.StartScan(search.Query{Text: "(", Regex: true}, 0); err == nil {
		t.Errorf("StartScan accepted a pattern that does not compile")
	}
	if st := ed.Active().Buffer.Stats(); st.OverCap != 0 {
		t.Errorf("the search went over the cap: %+v", st)
	}
}

func TestAdapterWriteStopsWhenItsContextIsCancelled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := bigLines(t, dir)
	original, err := os.ReadFile(path)
	apptest.Must(t, err)
	ed := openBig(t, path)
	a := &Editor{ed: ed}
	finishIndex(t, a)
	ed.Do("insert_char", map[string]any{"text": "Z"})
	plan, err := a.PlanSave()
	apptest.Must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := a.WriteSaveContext(ctx, plan)
	if len(results) != 1 || results[0].Err() == nil {
		t.Fatalf("results = %+v, want the cancelled write to report an error", results)
	}
	data, err := os.ReadFile(path)
	apptest.Must(t, err)
	if string(data) != string(original) {
		t.Errorf("the cancelled write changed the file")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".big.txt.gvim-*")); len(left) != 0 {
		t.Errorf("the cancelled write left %v", left)
	}
	_ = a.ApplySavePlan(plan, results)
	if !ed.Active().Modified() {
		t.Errorf("a cancelled save marked the document saved")
	}
}

// TestStringContextOfALargeDocumentReadsABoundedWindow is the readable-side contract
// for a local large file: the query may read its bounded context window (task-238 made
// large files editable-context-capable), but it must never read the whole file. The old
// zero-read contract predates local large-file context; a regression that reads far past
// the window or materializes the file must fail here, not be hidden by a loose bound.
func TestStringContextOfALargeDocumentReadsABoundedWindow(t *testing.T) {
	t.Parallel()
	ed := openBig(t, bigLines(t, t.TempDir()))
	doc := ed.Active()
	before := doc.Buffer.Stats()
	if highlight.NewStringContext()(doc, buffer.Pos{Line: 2, Col: 3}) {
		t.Errorf("a position of a large document with no string is reported inside a string")
	}
	after := doc.Buffer.Stats()
	linesRead := after.LineCalls - before.LineCalls
	// The context reads at most the 300 lines above and below the query, with two
	// index probes per line (size and text); anything past this is out of contract.
	const reach = 300
	if maxLines := int64(2 * (2*reach + 1)); linesRead <= 0 || linesRead > maxLines {
		t.Fatalf("stringContext read %d lines of a large document, want the bounded window (0, %d]", linesRead, maxLines)
	}
	if total := int64(doc.Buffer.Lines()); linesRead >= total/4 {
		t.Fatalf("stringContext read %d of %d lines; a bounded window must be far below the file", linesRead, total)
	}
	// Page-granular reads may pull a few pages around the window; they must stay small
	// and far below the whole file.
	readBytes := after.ReadBytes - before.ReadBytes
	if readBytes < 0 || readBytes > 1<<20 || readBytes >= after.TotalBytes/4 {
		t.Fatalf("stringContext read %d of %d bytes; a bounded window must not read the file", readBytes, after.TotalBytes)
	}
}

// TestRootDrivesALargeFileFromOpenToIndexedStatus runs the real adapter under the real
// root model: the first update starts the index, its steps are fed back, and the status
// row shows the chip and then the finished index.
func TestRootDrivesALargeFileFromOpenToIndexedStatus(t *testing.T) {
	t.Parallel()
	ed := openBig(t, bigLines(t, t.TempDir()))
	m := tui.New(NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "LARGE Scanning... ") {
		t.Errorf("the first frame has no scan progress on the status row:\n%s", frame)
	}
	for range 20 {
		if cmd == nil {
			break
		}
		msg := cmd()
		_, cmd = m.Update(msg)
	}
	if info := ed.Active().Index(); !info.Complete {
		t.Fatalf("the index is not complete after the commands: %+v", info)
	}
	frame = ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Line index built:") || strings.Contains(frame, "Scanning...") || !strings.Contains(frame, " LARGE ") {
		t.Errorf("the frame after the index lacks the LARGE chip or the built message:\n%s", frame)
	}
}
