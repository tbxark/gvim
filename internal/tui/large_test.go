package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/search"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// largeEditor is a fake editor whose documents are the real documents of a real
// editor, with the large file ports the app adapter has. The files the tests open
// are a few MiB, so the line index of one is not complete when it opens: the editor
// scans the first 4 MiB at once and leaves the rest to the index commands.
type largeEditor struct {
	*fakeEditor
	ed *editor.Editor

	savedCalls int           // calls of SavedLines
	snapshots  int           // snapshots taken for scans and F3
	rowReads   int           // lines read for the visible highlights
	lineCalls  map[int]int   // calls of Line, by line
	cancelled  bool          // a write saw its context cancelled
	cancelWork time.Duration // how long a write takes to clean up after it was cancelled
	nextBudget int64         // the read budget of FindNext; 0 is the default
}

var (
	_ indexer        = (*largeEditor)(nil)
	_ streamSearcher = (*largeEditor)(nil)
	_ fullLoader     = (*largeEditor)(nil)
	_ ctxSaver       = (*largeEditor)(nil)
	_ asyncSaver     = (*largeEditor)(nil)
	_ fileEditor     = (*largeEditor)(nil)
	_ tabEditor      = (*largeEditor)(nil)
)

func newLargeEditor(t *testing.T, paths ...string) *largeEditor {
	t.Helper()
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	le := &largeEditor{fakeEditor: newFakeEditor("x"), ed: ed, lineCalls: map[int]int{}}
	for _, p := range paths {
		if err := ed.OpenFile(p); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(ed.Close)
	return le
}

func (e *largeEditor) SetViewport(rows, cols int) { e.ed.SetViewport(rows, cols) }
func (e *largeEditor) Do(a editor.Action, args map[string]any) editor.Result {
	return e.ed.Do(a, args)
}
func (e *largeEditor) Status() string { return e.ed.Status() }
func (e *largeEditor) Tabs() []TabInfo {
	tabs := e.ed.Tabs()
	out := make([]TabInfo, len(tabs))
	for i, t := range tabs {
		out[i] = TabInfo{Name: t.Name, Path: t.Path, Modified: t.Modified, Active: t.Active}
	}
	return out
}
func (e *largeEditor) SetCursor(p buffer.Pos, extend bool) { e.ed.SetCursor(p, extend) }
func (e *largeEditor) ScrollBy(rows int)                   { e.ed.ScrollBy(rows) }
func (e *largeEditor) ScrollTo(top int)                    { e.ed.ScrollTo(top) }
func (e *largeEditor) OpenFile(path string) error          { return e.ed.OpenFile(path) }
func (e *largeEditor) OpenPreview(path string) error       { return e.ed.OpenPreview(path) }
func (e *largeEditor) GotoLine(line, col int) error        { return e.ed.GotoLine(line, col) }
func (e *largeEditor) OpenLoaded(d *editor.Document)       { e.ed.OpenLoaded(d) }
func (e *largeEditor) ActivateTab(i int)                   { e.ed.ActivateTab(i) }
func (e *largeEditor) CloseTab(i int, force bool) error    { return e.ed.CloseTab(i, force) }
func (e *largeEditor) CloseNeedsConfirm(i int) (bool, error) {
	return e.ed.CloseNeedsConfirm(i)
}
func (e *largeEditor) Quit(force bool) []editor.Dirty { return e.ed.Quit(force) }
func (e *largeEditor) LoadFullFunc(path string) func() (*editor.Document, error) {
	return e.ed.LoadFullFunc(path)
}

func (e *largeEditor) Active() Document {
	d := e.ed.Active()
	if d == nil {
		return nil
	}
	return &largeView{d: d, e: e}
}

func (e *largeEditor) PlanIndex() (editor.IndexJob, bool)   { return e.ed.PlanIndex() }
func (e *largeEditor) ApplyIndex(r editor.IndexResult) bool { return e.ed.ApplyIndex(r) }

func (e *largeEditor) PlanSave() (editor.SavePlan, error) { return e.ed.PlanSave() }
func (e *largeEditor) PlanSaveAs(path string, force bool) (editor.SavePlan, error) {
	return e.ed.PlanSaveAs(path, force)
}
func (e *largeEditor) PlanSaveAll() (editor.SavePlan, error) { return e.ed.PlanSaveAll() }
func (e *largeEditor) WriteSave(p editor.SavePlan) []editor.SaveResult {
	return p.Run()
}
func (e *largeEditor) WriteSaveContext(ctx context.Context, p editor.SavePlan) []editor.SaveResult {
	<-ctx.Done() // the write waits to be cancelled, so a test can quit while it runs
	time.Sleep(e.cancelWork)
	e.cancelled = true
	return p.RunContext(ctx)
}
func (e *largeEditor) ApplySavePlan(p editor.SavePlan, results []editor.SaveResult) error {
	return e.ed.ApplySavePlan(p, results)
}

func (e *largeEditor) SearchKey() (any, uint64, bool, bool) {
	d := e.ed.Active()
	if d == nil || !d.Large() {
		return nil, 0, false, false
	}
	return d, d.Buffer.Version(), d.Index().Complete, true
}

func (e *largeEditor) StartScan(q search.Query, startLine int) (*search.Scan, error) {
	e.snapshots++
	snap := e.ed.Active().Buffer.Snapshot()
	sc, err := search.NewScan(snap, q, startLine)
	if err != nil {
		snap.Release()
	}
	return sc, err
}

func (e *largeEditor) FindNext(q search.Query, from buffer.Pos, backward bool, at search.Cursor) (search.Match, bool, bool, search.Cursor, error) {
	e.snapshots++
	snap := e.ed.Active().Buffer.Snapshot()
	defer snap.Release()
	dir := search.Forward
	if backward {
		dir = search.Backward
	}
	return search.FindNextAt(snap, q, from, dir, e.nextBudget, at)
}

func (e *largeEditor) VisibleMatches(q search.Query, first, last int) ([]search.Match, error) {
	b := e.ed.Active().Buffer
	rows := func(i int) string {
		if i < 0 || i >= b.Lines() || b.LineBytes(i) > editorpane.LongLineRunes {
			return ""
		}
		e.rowReads++
		return b.Line(i)
	}
	return search.FindVisible(rows, first, last, q)
}

// largeView is the Document of a real document, as the app's documentAdapter shows it.
type largeView struct {
	d *editor.Document
	e *largeEditor
}

func (v *largeView) Name() string       { return v.d.Name() }
func (v *largeView) Top() int           { return v.d.Top }
func (v *largeView) Encoding() string   { return v.d.Encoding() }
func (v *largeView) LineEnding() string { return v.d.LineEnding() }
func (v *largeView) Modified() bool     { return v.d.Modified() }
func (v *largeView) LineCount() int     { return v.d.Buffer.Lines() }
func (v *largeView) Line(i int) string {
	v.e.lineCalls[i]++
	return v.d.Buffer.Line(i)
}
func (v *largeView) SavedLines() []string {
	v.e.savedCalls++
	return nil
}
func (v *largeView) Cursor() buffer.Pos          { return v.d.Cursor }
func (v *largeView) LineSpans(int) []syntax.Span { return nil }
func (v *largeView) Selection() (anchor, head buffer.Pos, ok bool) {
	if v.d.Sel == nil {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	return v.d.Sel.Anchor, v.d.Sel.Head, true
}
func (v *largeView) Large() bool                  { return v.d.Large() }
func (v *largeView) Index() buffer.IndexInfo      { return v.d.Index() }
func (v *largeView) ApproxLineCount() int         { return v.d.Index().ApproxLines }
func (v *largeView) LineDirty(i int) bool         { return v.d.Buffer.LineDirty(i) }
func (v *largeView) LineLen(i int) int            { return v.d.Buffer.LineLen(i) }
func (v *largeView) LineBytes(i int) int          { return v.d.Buffer.LineBytes(i) }
func (v *largeView) LineSlice(i, c, n int) string { return v.d.Buffer.LineSlice(i, c, n) }
func (v *largeView) ReadOnly() bool               { return v.d.ReadOnly() }
func (v *largeView) FilePath() string             { return v.d.Path }
func (v *largeView) HasPath() bool                { return v.d.Path != "" }

// writeLines writes a file of at least size bytes made of lines of width bytes: the
// line number, zero padded, and a newline. A line whose number is a multiple of every
// has "needle" in place of its padding.
func writeLines(t *testing.T, dir, name string, size, width, every int) string {
	t.Helper()
	var sb strings.Builder
	for i := 0; sb.Len() < size; i++ {
		line := fmt.Sprintf("%0*d", width-1, i)
		if every > 0 && i%every == 0 {
			line = fmt.Sprintf("%d needle", i)
			line += strings.Repeat("-", max(width-1-len(line), 0))
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// bigFile is a file of 5.5 MiB, over the 4 MiB the editor indexes when it opens it.
const bigFile = 5<<20 + 512<<10

// newLargeModel returns a model over a largeEditor with the files open, sized 100 by 30.
func newLargeModel(t *testing.T, paths ...string) (*Model, *largeEditor) {
	t.Helper()
	le := newLargeEditor(t, paths...)
	km, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: le, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock})
	return m, le
}

// start sizes the model and returns the command of the first update.
func start(m *Model) tea.Cmd {
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return cmd
}

// ofType returns the messages of type T among msgs.
func ofType[T any](msgs []tea.Msg) []T {
	var out []T
	for _, msg := range msgs {
		if v, ok := msg.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// feedAll runs cmd, feeds its messages to m, and goes on with the commands they start,
// until none is left or rounds run out. It returns every message it saw.
func feedAll(t *testing.T, m *Model, cmd tea.Cmd, rounds int) []tea.Msg {
	t.Helper()
	var seen []tea.Msg
	pending := []tea.Cmd{cmd}
	for range rounds {
		if len(pending) == 0 {
			return seen
		}
		next := pending
		pending = nil
		for _, c := range next {
			for _, msg := range runCmdWithin(t, c, 20_000_000_000) {
				seen = append(seen, msg)
				_, c2 := m.Update(msg)
				pending = append(pending, c2)
			}
		}
	}
	t.Fatalf("the commands did not end within %d rounds", rounds)
	return seen
}

func TestOpeningALargeFileStartsTheIndexChainAndItEndsAtCompletion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := writeLines(t, dir, "a.txt", bigFile, 40, 0)
	b := writeLines(t, dir, "b.txt", bigFile, 40, 0)
	m, le := newLargeModel(t, a, b)
	for _, d := range []int{0, 1} {
		if le.ed.DocAt(d).Index().Complete {
			t.Fatalf("document %d: the index is complete before any command ran", d)
		}
	}
	cmd := start(m)
	seen := feedAll(t, m, cmd, 20)
	steps := ofType[indexMsg](seen)
	if len(steps) < 2 {
		t.Fatalf("%d index steps ran, want one for each of the two files at least", len(steps))
	}
	for _, d := range []int{0, 1} {
		info := le.ed.DocAt(d).Index()
		if !info.Complete || info.Lines == 0 {
			t.Errorf("document %d: index %+v, want it complete", d, info)
		}
	}
	if m.large.indexBusy {
		t.Errorf("the chain is marked busy after it ended")
	}
	if _, more := le.PlanIndex(); more {
		t.Errorf("PlanIndex still has a step to plan")
	}
	if got := le.ed.Status(); !strings.HasPrefix(got, "Line index built:") {
		t.Errorf("status = %q, want the line index message", got)
	}
}

func TestIndexResultAfterTheTabClosedIsDropped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 0))
	cmd := start(m)
	msgs := runCmd(cmd)
	steps := ofType[indexMsg](msgs)
	if len(steps) != 1 || !m.large.indexBusy {
		t.Fatalf("got %d index steps, busy %v; want the chain started", len(steps), m.large.indexBusy)
	}
	if err := le.ed.CloseTab(0, true); err != nil {
		t.Fatal(err)
	}
	status := le.ed.Status()
	_, cmd = m.Update(steps[0])
	if cmd != nil {
		t.Errorf("a result for a closed tab started another command")
	}
	if m.large.indexBusy {
		t.Errorf("the chain is still marked busy")
	}
	if got := le.ed.Status(); got != status {
		t.Errorf("status changed to %q: the stale result was applied", got)
	}
}

func TestIndexResultFromBeforeCloseIsDropped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 0))
	steps := ofType[indexMsg](runCmd(start(m)))
	if len(steps) != 1 {
		t.Fatalf("got %d index steps", len(steps))
	}
	m.Close()
	m.Update(steps[0])
	if le.ed.DocAt(0).Index().Complete {
		t.Errorf("the result of a stopped chain was applied")
	}
}

func TestStatusRowShowsTheLargeChipAndTheScanPercent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 0))
	cmd := start(m)
	s := m.statusView()
	if !strings.HasPrefix(s.large, "LARGE Scanning... ") || !strings.HasSuffix(s.large, "%") {
		t.Fatalf("status chip while indexing = %q, want LARGE with the percent", s.large)
	}
	info := le.ed.DocAt(0).Index()
	want := fmt.Sprintf("LARGE Scanning... %d%%", info.Indexed*100/info.Size)
	if s.large != want {
		t.Errorf("chip = %q, want %q", s.large, want)
	}
	if !strings.Contains(strings.Join(screenRows(m), "\n"), want) {
		t.Errorf("the chip is not on the status row")
	}
	feedAll(t, m, cmd, 10)
	if got := m.statusView().large; got != "LARGE" {
		t.Errorf("chip after the index = %q, want LARGE", got)
	}
}

func TestGutterAgreesBetweenLayoutAndPaneAndStaysAcrossABatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Lines of 42 bytes: the 4 MiB that open indexes hold about 99,800 lines, the
	// whole file about 137,000.
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 42, 0))
	cmd := start(m)
	doc := le.Active()
	exact, approx := doc.LineCount(), displayLines(doc)
	if exact >= 100_000 || approx < 100_000 {
		t.Fatalf("lines %d, estimate %d: the fixture must cross a digit boundary", exact, approx)
	}
	before := m.layout().gutter
	if want := editorpane.GutterWidth(approx); before != want {
		t.Fatalf("layout gutter = %d, want the estimate's %d", before, want)
	}
	if before == editorpane.GutterWidth(exact) {
		t.Fatalf("the gutter ignores the estimate")
	}
	// The pane paints the separator where the layout says the gutter ends.
	rows := screenRows(m)
	x := strings.Index(rows[m.layout().bodyTop], "│")
	if x < 0 || x != m.layout().textX-2 {
		t.Errorf("separator at column %d, layout text starts at %d", x, m.layout().textX)
	}
	feedAll(t, m, cmd, 10)
	if got := m.layout().gutter; got != before {
		t.Errorf("gutter changed from %d to %d when the index completed", before, got)
	}
	if exact, approx = le.Active().LineCount(), displayLines(le.Active()); exact != approx {
		t.Errorf("complete index: %d lines, estimate %d", exact, approx)
	}
}

// findLargeBar opens the find bar over the model and types text into it.
func findLargeBar(m *Model, text string) tea.Cmd {
	press(m, ctrl('f'))
	var cmds []tea.Cmd
	for _, r := range text {
		cmds = append(cmds, press(m, typed(string(r))))
	}
	return tea.Batch(cmds...)
}

func TestFindBarCountsALargeDocumentInASlicedScan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A line has needle in it when its number is a multiple of 1000.
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 1000))
	feedAll(t, m, start(m), 10) // finish the index, so the scan is not partial
	cmd := findLargeBar(m, "needle")
	seen := feedAll(t, m, cmd, 400)
	if len(ofType[searchStepMsg](seen)) == 0 {
		t.Fatalf("no scan step ran")
	}
	total := le.ed.Active().Buffer.Lines()
	want := (total-1)/1000 + 1
	rows := strings.Join(screenRows(m), "\n")
	if wantText := fmt.Sprintf("Match 1 of %d", want); !strings.Contains(rows, wantText) && !strings.Contains(rows, fmt.Sprintf("%d matches", want)) {
		t.Errorf("the find bar does not show %d matches:\n%s", want, rows)
	}
	if st := le.ed.Active().Buffer.Stats(); st.OverCap != 0 {
		t.Errorf("the search read the whole text: %+v", st)
	}
	if m.large.stream == nil || !m.large.stream.scan.Done() {
		t.Errorf("the scan did not end")
	}
	if got := len(m.large.stream.scan.Matches()); got != want {
		t.Errorf("scan found %d matches, want %d", got, want)
	}
}

func TestANewQueryCancelsTheScanBeforeIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 1000))
	feedAll(t, m, start(m), 10)
	press(m, ctrl('f'))
	first := runCmd(press(m, typed("n")))
	steps := ofType[searchStepMsg](first)
	if len(steps) != 1 {
		t.Fatalf("got %d scan steps for the first query", len(steps))
	}
	old := steps[0].run
	// The second letter arrives while the first step's message is on its way.
	second := press(m, typed("e"))
	if !old.dead {
		t.Fatalf("the first scan was not cancelled by the new query")
	}
	if m.large.stream == nil || m.large.stream == old || m.large.stream.key.q.Text != "ne" {
		t.Fatalf("the running scan is not the one for the new query")
	}
	// The old scan is closed only when its step has ended, so that no command still
	// reads it.
	m.Update(steps[0])
	if !old.scan.Done() {
		t.Errorf("the cancelled scan was not closed when its step ended")
	}
	feedAll(t, m, second, 400)
	if !m.large.stream.scan.Done() {
		t.Errorf("the scan of the new query did not end")
	}
	if got := len(m.large.stream.scan.Matches()); got != 0 {
		// "ne" is not in the lines but in "needle": the lines with needle.
		total := le.ed.Active().Buffer.Lines()
		if want := (total-1)/1000 + 1; got != want {
			t.Errorf("matches of ne = %d, want %d", got, want)
		}
	}
}

func TestAnEditCancelsAndRestartsTheScan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 1000))
	feedAll(t, m, start(m), 10)
	feedAll(t, m, findLargeBar(m, "needle"), 400)
	run := m.large.stream
	if run == nil {
		t.Fatal("no scan ran")
	}
	// An edit made through the editor changes the text the scan searched.
	le.ed.Do("insert_char", map[string]any{"text": "Z"})
	cmd := m.searchSync()
	if cmd == nil || m.large.stream == run {
		t.Fatalf("the scan was not restarted for the edited text")
	}
	feedAll(t, m, cmd, 400)
	if !m.large.stream.scan.Done() {
		t.Errorf("the restarted scan did not end")
	}
}

func TestF3FindsTheNextMatchOfALargeDocumentWithoutAList(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 1000))
	feedAll(t, m, start(m), 10)
	feedAll(t, m, findLargeBar(m, "needle"), 400)
	press(m, special(tea.KeyEnter)) // close the bar with the first match selected
	before := le.ed.Active().Buffer.Stats()
	press(m, special(tea.KeyF3))
	a, h, ok := le.Active().Selection()
	if !ok || a.Line != 1000 || h.Line != 1000 {
		t.Fatalf("selection after F3 = %+v..%+v (%v), want the match on line 1000", a, h, ok)
	}
	press(m, special(tea.KeyF3))
	if a, _, _ := le.Active().Selection(); a.Line != 2000 {
		t.Errorf("second F3 went to line %d, want 2000", a.Line)
	}
	if st := le.ed.Active().Buffer.Stats(); st.OverCap != 0 || st.ReadBytes-before.ReadBytes > 8<<20 {
		t.Errorf("F3 read %d bytes and went over the cap %d times: it must not read the file", st.ReadBytes-before.ReadBytes, st.OverCap)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyF3, Mod: tea.ModShift})
	if a, _, _ := le.Active().Selection(); a.Line != 1000 {
		t.Errorf("Shift+F3 went to line %d, want 1000", a.Line)
	}
}

// TestNoMatchInAPartlyIndexedDocumentSaysSo is the regression test for "No matches"
// being the answer to a search that only read the part of the file the line index
// has reached.
func TestNoMatchInAPartlyIndexedDocumentSaysSo(t *testing.T) {
	t.Parallel()
	const partial = "line index not complete"
	for _, complete := range []bool{false, true} {
		name := map[bool]string{false: "index incomplete", true: "index complete"}[complete]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m, le := newLargeModel(t, writeLines(t, t.TempDir(), "a.txt", bigFile, 40, 0))
			cmd := start(m)
			if complete {
				feedAll(t, m, cmd, 10)
			}
			if le.ed.Active().Index().Complete != complete {
				t.Fatalf("index complete = %v, want %v", !complete, complete)
			}
			// The bar's own answer to Enter.
			findLargeBar(m, "no such text")
			press(m, special(tea.KeyEnter))
			rows := strings.Join(screenRows(m), "\n")
			if !strings.Contains(rows, "No matches") || strings.Contains(rows, partial) != !complete {
				t.Errorf("the find bar after Enter (index complete %v):\n%s", complete, rows)
			}
			// F3 with the bar closed, and Shift+F3.
			press(m, special(tea.KeyEscape))
			for _, key := range []tea.KeyPressMsg{special(tea.KeyF3), {Code: tea.KeyF3, Mod: tea.ModShift}} {
				m.notice = ""
				press(m, key)
				if !strings.HasPrefix(m.notice, "No matches") || strings.Contains(m.notice, partial) != !complete {
					t.Errorf("notice after %v with the index complete %v = %q", key, complete, m.notice)
				}
			}
		})
	}
}

func TestHighlightsOfALargeDocumentCoverTheVisibleRowsOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 7))
	feedAll(t, m, start(m), 10)
	feedAll(t, m, findLargeBar(m, "needle"), 400)
	press(m, special(tea.KeyEnter))
	before := le.ed.Active().Buffer.Stats()
	fn := m.findLineHighlights()
	top := le.Active().Top()
	if hs := fn(top + 7 - top%7); len(hs) == 0 && top%7 != 0 {
		// The row with a multiple of seven is highlighted.
		t.Errorf("no highlight on line %d", top+7-top%7)
	}
	if hs := fn(top + 100_000); hs != nil {
		t.Errorf("a row far from the window has highlights: %v", hs)
	}
	after := le.ed.Active().Buffer.Stats()
	if got := after.MaterializedBytes - before.MaterializedBytes; got > 64<<10 {
		t.Errorf("the highlights read %d bytes of text, want only the rows of a frame", got)
	}
}

// latin1File writes a file of at least size bytes that is not UTF-8.
func latin1File(t *testing.T, dir string, size int) string {
	t.Helper()
	line := []byte("caf\xe9 au lait\n")
	data := make([]byte, 0, size+len(line))
	for len(data) < size {
		data = append(data, line...)
	}
	path := filepath.Join(dir, "latin.txt")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestALargeFileInAnotherEncodingAsksBeforeItLoadsWhole(t *testing.T) {
	t.Parallel()
	path := latin1File(t, t.TempDir(), 3000)
	m, le := newLargeModel(t)
	start(m)
	m.openFile(path, false)
	if len(m.overlays) != 1 {
		t.Fatalf("overlay stack has %d entries after the open, want the question", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*prompt.Confirm); !ok {
		t.Fatalf("top overlay = %T, want a confirm", m.overlays[0])
	}
	if !strings.Contains(m.notice, "requires full load") {
		t.Errorf("notice = %q, want the reason", m.notice)
	}
	if len(le.ed.Tabs()) != 0 {
		t.Fatalf("the file opened before the answer")
	}
	// Yes sends the answer, the answer starts the load, and the result opens the file.
	var loaded []fullLoadedMsg
	for _, msg := range runCmd(press(m, typed("y"))) {
		_, cmd := m.Update(msg)
		loaded = append(loaded, ofType[fullLoadedMsg](runCmd(cmd))...)
	}
	if len(loaded) != 1 {
		t.Fatalf("got %d loads, want 1", len(loaded))
	}
	m.Update(loaded[0])
	if len(le.ed.Tabs()) != 1 || le.ed.Active().Large() {
		t.Errorf("tabs %d, large %v: want the file open as a small document", len(le.ed.Tabs()), le.ed.Active() != nil && le.ed.Active().Large())
	}
	if m.notice != "" {
		t.Errorf("notice = %q after the load", m.notice)
	}
}

func TestDecliningTheFullLoadLeavesTheFileClosed(t *testing.T) {
	t.Parallel()
	path := latin1File(t, t.TempDir(), 3000)
	m, le := newLargeModel(t)
	start(m)
	msg := fullLoadPromptMsg{err: editor.ErrLargeNeedsFullLoad{Path: path, Size: 3000, Codec: "iso-8859-1"}}
	m.Update(msg)
	if len(m.overlays) != 1 {
		t.Fatalf("the prompt message did not ask")
	}
	press(m, typed("n"))
	if len(le.ed.Tabs()) != 0 {
		t.Errorf("a declined file opened")
	}
}

func TestSavingShowsTheNoticeAndAQuitStopsTheWriteAndWaitsForIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeLines(t, dir, "a.txt", bigFile, 40, 0)
	m, le := newLargeModel(t, path)
	feedAll(t, m, start(m), 10)
	le.ed.Do("insert_char", map[string]any{"text": "Z"})
	cmd := press(m, ctrl('s'))
	if m.notice != savingNotice {
		t.Fatalf("notice = %q, want %q", m.notice, savingNotice)
	}
	if len(m.large.saves) != 1 {
		t.Fatalf("%d writes in flight, want 1", len(m.large.saves))
	}
	// The write runs on its own goroutine and waits to be cancelled.
	done := make(chan []tea.Msg, 1)
	go func() { done <- runCmd(cmd) }()
	quit := m.quit()
	if quit == nil || !m.large.quitting {
		t.Fatalf("a quit with a write in flight must wait")
	}
	msgs := <-done
	saved := ofType[saveDoneMsg](msgs)
	if len(saved) != 1 || !le.cancelled {
		t.Fatalf("the write was not stopped: %d results, cancelled %v", len(saved), le.cancelled)
	}
	_, cmd = m.Update(saved[0])
	var quitMsg bool
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			quitMsg = true
		}
	}
	if !quitMsg {
		t.Errorf("the program did not quit when the cancelled write ended")
	}
	if m.notice == savingNotice {
		t.Errorf("the saving notice stayed")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".a.txt.gvim-*"))
	if len(left) != 0 {
		t.Errorf("the cancelled write left %v", left)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.HasPrefix(string(data), "Z") {
		t.Errorf("the cancelled write changed the file (err %v)", err)
	}
}

// A signal ends the program without a quit: Run returns and the caller closes the model.
// The write in flight must be stopped and must have removed its temporary file before
// Close returns, or the process exits with a half-written copy next to the file.
func TestCloseStopsTheWriteInFlightAndWaitsForIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeLines(t, dir, "a.txt", bigFile, 40, 0)
	m, le := newLargeModel(t, path)
	le.cancelWork = 100 * time.Millisecond
	feedAll(t, m, start(m), 10)
	le.ed.Do("insert_char", map[string]any{"text": "Z"})
	cmd := press(m, ctrl('s'))
	done := make(chan []tea.Msg, 1)
	go func() { done <- runCmd(cmd) }()
	m.Close()
	if !le.cancelled {
		t.Fatalf("Close returned while the write was still running")
	}
	if len(ofType[saveDoneMsg](<-done)) != 1 {
		t.Fatalf("the write did not end")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".a.txt.gvim-*"))
	if len(left) != 0 {
		t.Errorf("the write left %v", left)
	}
}

func TestQuitWaitEndsAtItsTimeout(t *testing.T) {
	t.Parallel()
	m, _ := newLargeModel(t)
	m.large.quitting = true
	_, cmd := m.Update(quitWaitMsg{})
	if cmd == nil {
		t.Fatalf("the timeout did not end the wait")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("the timeout did not quit")
	}
}

func TestAQuitWithNoWriteInFlightQuitsAtOnce(t *testing.T) {
	t.Parallel()
	m, _ := newLargeModel(t)
	if _, ok := m.quit()().(tea.QuitMsg); !ok {
		t.Errorf("quit with nothing to wait for did not quit")
	}
}

func TestAFailedIndexShowsItsStatusAndStopsTheChain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeLines(t, dir, "a.txt", bigFile, 40, 0)
	m, le := newLargeModel(t, path)
	cmd := start(m)
	// The file shrinks under the scan: the read of the next chunks comes up short.
	if err := os.Truncate(path, 4<<20+100); err != nil {
		t.Fatal(err)
	}
	seen := feedAll(t, m, cmd, 10)
	if len(ofType[indexMsg](seen)) != 1 {
		t.Fatalf("the chain ran %d steps, want it to stop after the failure", len(ofType[indexMsg](seen)))
	}
	if got := le.ed.Status(); !strings.HasPrefix(got, "Index failed:") {
		t.Errorf("status = %q, want the failure", got)
	}
	if le.ed.DocAt(0).Index().Complete {
		t.Errorf("a failed index is complete")
	}
	if _, ok := le.PlanIndex(); ok {
		t.Errorf("PlanIndex plans a step for a document whose index failed")
	}
}

func TestPaneOfALargeDocumentNeverReadsTheSavedLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 0))
	start(m)
	le.ed.Do("insert_char", map[string]any{"text": "Z"})
	le.savedCalls, le.lineCalls = 0, map[int]int{}
	_ = m.paint()
	if le.savedCalls != 0 {
		t.Errorf("SavedLines called %d times for a large document", le.savedCalls)
	}
	rows := m.bodyRows()
	for i := range le.lineCalls {
		if i > rows+2 {
			t.Errorf("Line(%d) was read for a frame of %d rows", i, rows)
		}
	}
	// The edited line shows its change bar.
	rowsText := screenRows(m)
	if !strings.Contains(rowsText[m.layout().bodyTop], "│") {
		t.Errorf("no gutter on the first row: %q", rowsText[m.layout().bodyTop])
	}
}

// A large document wraps as the setting says, as any other does: its long lines are laid out in
// segments, so the wrap costs nothing in proportion to their length.
func TestLargeDocumentWrapsWhenTheSettingSaysSo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 400, 0))
	start(m)
	if p := m.viewPane(editorpane.Pane{}, le.Active()); !p.Wrap {
		t.Errorf("a large document is painted unwrapped while line wrap is on")
	}
	m.view.lineWrap = false
	if p := m.viewPane(editorpane.Pane{}, le.Active()); p.Wrap {
		t.Errorf("a large document is painted wrapped while line wrap is off")
	}
}

// The toggle of the wrap of a large document says what it did, and nothing else.
func TestWrapToggleOfALargeDocumentIsPlain(t *testing.T) {
	t.Parallel()
	m, _ := newLargeModel(t, writeLines(t, t.TempDir(), "a.txt", bigFile, 40, 0))
	start(m)
	for _, want := range []string{"Line wrap: off", "Line wrap: on"} {
		if err := m.Run("toggle_line_wrap"); err != nil {
			t.Fatal(err)
		}
		if m.notice != want {
			t.Errorf("notice = %q, want %q", m.notice, want)
		}
	}
}

func TestLSPIsNotGivenALargeDocumentsText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.go", bigFile, 40, 0))
	start(m)
	m.lsp.port = newFakeLSP("go")
	le.lineCalls = map[int]int{}
	m.lspReconcile()
	if len(m.lsp.tracks) != 0 {
		t.Errorf("a large document is tracked for the language server")
	}
	if n := len(le.lineCalls); n != 0 {
		t.Errorf("the server sync read %d lines of a large document", n)
	}
}

func TestFindBarFrameWorkIsBoundedByTheRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 7))
	feedAll(t, m, start(m), 10)
	feedAll(t, m, findLargeBar(m, "needle"), 400)
	press(m, special(tea.KeyEnter)) // the bar closes and the search stays highlighted
	rows := m.bodyRows()
	le.snapshots, le.rowReads, le.lineCalls = 0, 0, map[int]int{}
	const frames = 3
	for range frames {
		_ = m.paint()
	}
	if le.snapshots != 0 {
		t.Errorf("%d snapshots were taken while painting: a frame must not start a search", le.snapshots)
	}
	if limit := frames * 3 * rows; le.rowReads > limit+3*frames {
		t.Errorf("the highlights read %d lines in %d frames of %d rows, want at most %d", le.rowReads, frames, rows, limit)
	}
	calls := 0
	for _, n := range le.lineCalls {
		calls += n
	}
	// A row is laid out for the width of the bar, for the painting and for the caret.
	if limit := frames * 4 * rows; calls > limit {
		t.Errorf("the document's Line was called %d times in %d frames of %d rows, want at most %d", calls, frames, rows, limit)
	}
}

func TestClosingATabStopsItsWriteAndNamesTheCancel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeLines(t, dir, "a.txt", bigFile, 40, 0)
	other := writeLines(t, dir, "b.txt", 2048, 40, 0)
	m, le := newLargeModel(t, path, other)
	feedAll(t, m, start(m), 10)
	le.ed.ActivateTab(0)
	le.ed.Do("insert_char", map[string]any{"text": "Z"})
	cmd := press(m, ctrl('s'))
	if len(m.large.saves) != 1 {
		t.Fatalf("%d writes in flight, want 1", len(m.large.saves))
	}
	done := make(chan []tea.Msg, 1)
	go func() { done <- runCmd(cmd) }()
	// Closing the other tab does not touch the write of this one.
	if err := m.closeTab(m.tabPort(), 1, true); err != nil {
		t.Fatal(err)
	}
	for _, run := range m.large.saves {
		if run.ctxDone() {
			t.Fatalf("closing another tab stopped the write")
		}
	}
	if err := m.closeTab(m.tabPort(), 0, true); err != nil {
		t.Fatal(err)
	}
	saved := ofType[saveDoneMsg](<-done)
	if len(saved) != 1 || !le.cancelled {
		t.Fatalf("the write was not stopped: %d results, cancelled %v", len(saved), le.cancelled)
	}
	_, cmd = m.Update(saved[0])
	if cmd != nil || m.notice != "Save cancelled" {
		t.Errorf("notice = %q, cmd %v; want the cancel named and the flow ended", m.notice, cmd != nil)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".a.txt.gvim-*"))
	if len(left) != 0 {
		t.Errorf("the cancelled write left %v", left)
	}
}

func TestSwitchingTabCancelsAndRestartsTheScanOnTheOtherDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := writeLines(t, dir, "a.txt", bigFile, 40, 1000)
	b := writeLines(t, dir, "b.txt", bigFile, 40, 500)
	m, le := newLargeModel(t, a, b)
	feedAll(t, m, start(m), 20)
	le.ed.ActivateTab(0)
	feedAll(t, m, findLargeBar(m, "needle"), 400)
	first := m.large.stream
	if first == nil || !first.scan.Done() {
		t.Fatalf("the scan of the first document did not end")
	}
	le.ed.ActivateTab(1)
	cmd := m.searchSync()
	second := m.large.stream
	if second == nil || second == first || second.key.doc == first.key.doc {
		t.Fatalf("the scan was not restarted for the other document")
	}
	feedAll(t, m, cmd, 400)
	total := le.ed.Active().Buffer.Lines()
	if want := (total-1)/500 + 1; len(m.large.stream.scan.Matches()) != want {
		t.Errorf("the second document has %d matches, want %d", len(m.large.stream.scan.Matches()), want)
	}
}

func TestClosingTheBarEndsTheScan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, _ := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 1000))
	feedAll(t, m, start(m), 10)
	press(m, ctrl('f'))
	runs := ofType[searchStepMsg](runCmd(press(m, typed("n"))))
	if len(runs) != 1 || m.large.stream == nil {
		t.Fatalf("no scan runs for the open bar")
	}
	press(m, special(tea.KeyEscape))
	if m.large.stream != nil {
		t.Errorf("the scan outlived the bar")
	}
	m.Update(runs[0])
	if !runs[0].run.scan.Done() {
		t.Errorf("the scan was not closed when its last step ended")
	}
}

// TestRepeatingF3GoesOnWhereTheLastSearchStopped is the regression test for F3 on a
// document whose next match is farther than one search budget: every press used to
// start again at the caret and read the same first budget, so the match was never
// reached.
func TestRepeatingF3GoesOnWhereTheLastSearchStopped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Only line 0 has the needle, so F3 from it must go around the whole file.
	m, le := newLargeModel(t, writeLines(t, dir, "a.txt", bigFile, 40, 1<<30))
	feedAll(t, m, start(m), 10)
	feedAll(t, m, findLargeBar(m, "needle"), 400)
	press(m, special(tea.KeyEnter)) // close the bar with the match on line 0 selected
	le.nextBudget = 1 << 20

	presses := 0
	for ; presses < 20; presses++ {
		press(m, special(tea.KeyF3))
		if m.notice != find.SearchLimitText {
			break
		}
	}
	if presses == 0 {
		t.Fatalf("the first F3 did not run out of its %d byte budget, so the test checks nothing", le.nextBudget)
	}
	if m.notice != "Search wrapped" {
		t.Fatalf("notice after %d F3 presses = %q, want %q", presses+1, m.notice, "Search wrapped")
	}
	if a, _, ok := le.Active().Selection(); !ok || a.Line != 0 {
		t.Errorf("selection after wrapping = %+v (%v), want the match on line 0", a, ok)
	}
}

// TestF3DoesNotResumeAnotherSearch checks that the position a limited search stopped
// at is used only by the same search: a different position starts at the beginning.
func TestF3DoesNotResumeAnotherSearch(t *testing.T) {
	t.Parallel()
	m := &Model{}
	q := search.Query{Text: "x"}
	snap := buffer.New("a\nb").Snapshot()
	defer snap.Release()
	_, _, _, cur, _ := search.FindNextAt(snap, q, buffer.Pos{}, search.Forward, 1, search.Cursor{})
	if cur == (search.Cursor{}) {
		t.Fatal("a search with a budget of one byte did not stop part way")
	}
	ss := &nextStub{cur: cur}
	_, _, _, _ = m.findNext(ss, q, buffer.Pos{Line: 1}, false)
	_, _, _, _ = m.findNext(ss, q, buffer.Pos{Line: 2}, false)
	_, _, _, _ = m.findNext(ss, q, buffer.Pos{Line: 2}, true)
	_, _, _, _ = m.findNext(ss, q, buffer.Pos{Line: 2}, true)
	for i, want := range []bool{false, false, false, true} {
		if got := ss.ats[i] != (search.Cursor{}); got != want {
			t.Errorf("call %d resumed = %v, want %v", i, got, want)
		}
	}
}

// nextStub is a streamSearcher whose FindNext always runs out of budget, with a
// cursor that is not the zero one.
type nextStub struct {
	streamSearcher
	cur search.Cursor
	ats []search.Cursor
}

func (s *nextStub) SearchKey() (any, uint64, bool, bool) { return nil, 1, true, true }
func (s *nextStub) FindNext(_ search.Query, _ buffer.Pos, _ bool, at search.Cursor) (search.Match, bool, bool, search.Cursor, error) {
	s.ats = append(s.ats, at)
	return search.Match{}, false, false, s.cur, nil
}

// A file named on the command line that is large and not UTF-8 is not read before the
// first frame: the app hands it over, and the root asks whether to load it once running.
func TestInitAsksAboutLargeFilesThatAreNotUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prompts []editor.ErrLargeNeedsFullLoad
	}{
		{"none", nil},
		{"one", []editor.ErrLargeNeedsFullLoad{{Path: "a.log", Size: 1 << 30, Codec: "iso-8859-1"}}},
		{"two", []editor.ErrLargeNeedsFullLoad{{Path: "a.log"}, {Path: "b.log"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			le := newLargeEditor(t)
			km, err := keymap.ByName("default")
			if err != nil {
				t.Fatal(err)
			}
			m := New(Deps{Editor: le, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock,
				Root: RootPorts{FullLoadPrompts: tt.prompts}})
			start(m)
			prompts := ofType[fullLoadPromptMsg](runCmd(m.Init()))
			if len(prompts) != len(tt.prompts) {
				t.Fatalf("Init sent %d prompts, want %d", len(prompts), len(tt.prompts))
			}
			for i, p := range prompts {
				if p.err != tt.prompts[i] {
					t.Errorf("prompt %d = %+v, want %+v", i, p.err, tt.prompts[i])
				}
				m.Update(p)
			}
			if len(m.overlays) != len(tt.prompts) {
				t.Errorf("%d overlays, want one question per file (%d)", len(m.overlays), len(tt.prompts))
			}
		})
	}
}
