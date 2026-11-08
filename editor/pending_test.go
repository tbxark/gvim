package editor

import (
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// stepUntil runs index jobs until done reports true or the index is complete, and
// returns the number of jobs that ran.
func stepUntil(t *testing.T, e *Editor, done func() bool) int {
	t.Helper()
	steps := 0
	for !done() {
		ran, more := indexStep(e)
		if ran {
			steps++
		}
		if !more {
			break
		}
	}
	return steps
}

func TestGotoBeyondTheIndexedPrefixWaits(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	start := d.Cursor
	if err := e.GotoLine(300, 3); err != nil {
		t.Fatalf("GotoLine: %v", err)
	}
	if d.Cursor != start {
		t.Fatalf("the caret moved to %v before the index got there", d.Cursor)
	}
	if d.LargeState().Pending == nil || !strings.HasPrefix(e.Status(), "Scanning... ") {
		t.Fatalf("pending = %v, status = %q", d.LargeState().Pending, e.Status())
	}
	if len(e.extras.jumps.entries) != 0 {
		t.Fatal("a jump is recorded before the caret moves")
	}
	steps := stepUntil(t, e, func() bool { return d.LargeState().Pending == nil })
	if want := (buffer.Pos{Line: 299, Col: 2}); d.Cursor != want {
		t.Fatalf("the caret is at %v, want %v", d.Cursor, want)
	}
	if d.Index().Complete {
		t.Fatalf("the goto waited for the whole index (%d jobs)", steps)
	}
	if d.Top > 299 || d.Top+e.rows <= 299 {
		t.Fatalf("Top = %d: the caret is not on screen", d.Top)
	}
	if got := len(e.extras.jumps.entries); got != 1 || e.extras.jumps.entries[0].Pos != start {
		t.Fatalf("jump list = %+v, want the place the goto started from", e.extras.jumps.entries)
	}
	run(t, e, "navigate_back")
	if d.Cursor != start {
		t.Fatalf("navigate_back went to %v", d.Cursor)
	}
}

func TestGotoPastTheEndWaitsForTheEndAndClamps(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	if err := e.GotoLine(99999, 99); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, e, func() bool { return false })
	if !d.Index().Complete || d.LargeState().Pending != nil {
		t.Fatalf("complete = %v, pending = %v", d.Index().Complete, d.LargeState().Pending)
	}
	if want := (buffer.Pos{Line: 400}); d.Cursor != want {
		t.Fatalf("the caret is at %v, want the last line %v", d.Cursor, want)
	}
}

func TestGotoInsideThePrefixIsImmediate(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	if err := e.GotoLine(2, 4); err != nil {
		t.Fatal(err)
	}
	if want := (buffer.Pos{Line: 1, Col: 3}); d.Cursor != want || d.LargeState().Pending != nil {
		t.Fatalf("caret = %v, pending = %v", d.Cursor, d.LargeState().Pending)
	}
}

func TestGotoReplacesThePendingGoto(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	if err := e.GotoLine(300, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.GotoLine(2, 1); err != nil {
		t.Fatal(err)
	}
	if d.LargeState().Pending != nil {
		t.Fatal("the first goto still waits")
	}
	indexAll(t, e)
	if want := (buffer.Pos{Line: 1}); d.Cursor != want {
		t.Fatalf("caret = %v, want %v", d.Cursor, want)
	}
	if err := e.GotoLine(0, 1); !errors.Is(err, ErrBadLine) {
		t.Fatalf("GotoLine(0) = %v", err)
	}
}

func TestPendingGotoSurvivesEdit(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	if err := e.GotoLine(300, 2); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "xy")
	if !d.Buffer.Modified() || d.LargeState().Pending == nil {
		t.Fatalf("modified = %v, pending = %v", d.Buffer.Modified(), d.LargeState().Pending)
	}
	indexAll(t, e)
	if want := (buffer.Pos{Line: 299, Col: 1}); d.Cursor != want {
		t.Fatalf("caret = %v, want %v", d.Cursor, want)
	}
	if got := d.Buffer.Line(0); got != "xyline 0000" {
		t.Fatalf("Line(0) = %q", got)
	}
}

func TestDocumentEndActionsWaitAndReplay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action  Action
		wantSel bool
	}{
		{action: "move_document_end"},
		{action: "select_document_end", wantSel: true},
		{action: "select_all", wantSel: true},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			t.Parallel()
			e, d := openLarge(t, largeOptions(), lineText(400))
			start := d.Cursor
			r := e.Do(tt.action, nil)
			if r.Err != nil {
				t.Fatalf("Do: %v", r.Err)
			}
			if d.Cursor != start || d.Sel != nil {
				t.Fatalf("the action ran on the frontier: caret %v, selection %v", d.Cursor, d.Sel)
			}
			if d.LargeState().Pending == nil || !strings.HasPrefix(e.Status(), "Scanning... ") {
				t.Fatalf("pending = %v, status = %q", d.LargeState().Pending, e.Status())
			}
			indexAll(t, e)
			end := d.Buffer.End()
			if want := (buffer.Pos{Line: 400}); end != want || d.Cursor != end {
				t.Fatalf("End() = %v, caret = %v", end, d.Cursor)
			}
			if (d.Sel != nil) != tt.wantSel {
				t.Fatalf("selection = %v, want one: %v", d.Sel, tt.wantSel)
			}
			if tt.action == "select_all" && *d.Sel != (buffer.Selection{Head: end}) {
				t.Fatalf("selection = %+v", *d.Sel)
			}
			if d.LargeState().Pending != nil {
				t.Fatal("still pending")
			}
		})
	}
}

func TestDocumentEndActionsRunAtOnceWhenTheIndexIsComplete(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	indexAll(t, e)
	run(t, e, "move_document_end")
	if want := (buffer.Pos{Line: 400}); d.Cursor != want || d.LargeState().Pending != nil {
		t.Fatalf("caret = %v, pending = %v", d.Cursor, d.LargeState().Pending)
	}
	m := docWith(Options{}, "a\nb\nc")
	run(t, m, "select_all")
	if m.Active().Cursor != (buffer.Pos{Line: 2, Col: 1}) {
		t.Fatalf("select_all in a small document: %v", m.Active().Cursor)
	}
}

func TestEditingActionNotQueuedWhileIndexing(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	version := d.Buffer.Version()
	for _, a := range []Action{"sort_lines", "ensure_final_newline"} {
		r := e.Do(a, nil)
		if !errors.Is(r.Err, ErrIndexing) {
			t.Errorf("Do(%s) = %v, want ErrIndexing", a, r.Err)
		}
		if !strings.HasPrefix(e.Status(), string(a)+": ") {
			t.Errorf("status after %s = %q", a, e.Status())
		}
	}
	// A large document never does these, so waiting for the index would not help.
	for _, a := range []Action{"trim_trailing_whitespace", "format_buffer"} {
		r := e.Do(a, nil)
		if !errors.Is(r.Err, ErrLargeFile) {
			t.Errorf("Do(%s) = %v, want ErrLargeFile", a, r.Err)
		}
	}
	if d.LargeState().Pending != nil || d.Buffer.Version() != version {
		t.Fatalf("pending = %v, version %d -> %d", d.LargeState().Pending, version, d.Buffer.Version())
	}
	// With a selection, sort works on that selection alone.
	d.Sel = &buffer.Selection{Anchor: buffer.Pos{Line: 1}, Head: buffer.Pos{Line: 3}}
	d.Cursor = d.Sel.Head
	if r := e.Do("sort_lines", nil); errors.Is(r.Err, ErrIndexing) {
		t.Fatalf("sort_lines of a selection = %v", r.Err)
	}
	// Ordinary editing and movement are not held back.
	run(t, e, "move_down", "move_page_down")
	typeText(t, e, "z")
}

func TestPendingActionDroppedAfterEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name string
		move func(t *testing.T, e *Editor)
	}{
		{Name: "text edited", move: func(t *testing.T, e *Editor) { typeText(t, e, "q") }},
		{Name: "caret moved", move: func(t *testing.T, e *Editor) { run(t, e, "move_down") }},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, d := openLarge(t, largeOptions(), lineText(400))
			run(t, e, "move_document_end")
			tt.move(t, e)
			at := d.Cursor
			indexAll(t, e)
			if d.Status != "Jump cancelled" {
				t.Fatalf("status = %q", d.Status)
			}
			if d.Cursor != at || d.LargeState().Pending != nil {
				t.Fatalf("caret = %v, want %v; pending = %v", d.Cursor, at, d.LargeState().Pending)
			}
		})
	}
}

func TestPendingDroppedOnTabSwitch(t *testing.T) {
	t.Parallel()
	e, a := openLarge(t, largeOptions(), lineText(400))
	run(t, e, "move_document_end")
	start := a.Cursor
	if err := e.OpenFile(writeLargeFile(t, "other.txt", "small\n")); err != nil {
		t.Fatal(err)
	}
	if e.Active() == a {
		t.Fatal("no tab switch")
	}
	indexAll(t, e)
	if a.Status != "Jump cancelled" || a.Cursor != start {
		t.Fatalf("status = %q, caret = %v", a.Status, a.Cursor)
	}
}

func TestPendingReplayTargetsOwningDoc(t *testing.T) {
	t.Parallel()
	e, a := openLarge(t, largeOptions(), lineText(400))
	if err := e.GotoLine(300, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(writeLargeFile(t, "other.txt", "one\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	b := e.Active()
	b.Cursor = buffer.Pos{Line: 1, Col: 2}
	indexAll(t, e)
	if want := (buffer.Pos{Line: 299}); a.Cursor != want {
		t.Fatalf("the owning document's caret = %v, want %v", a.Cursor, want)
	}
	if b.Cursor != (buffer.Pos{Line: 1, Col: 2}) || e.Active() != b {
		t.Fatalf("the active document was disturbed: caret %v", b.Cursor)
	}
}

func TestPendingDroppedWhenTheIndexFails(t *testing.T) {
	t.Parallel()
	e, d, src := openMem(t, largeOptions(), lineText(400))
	if err := e.GotoLine(300, 1); err != nil {
		t.Fatal(err)
	}
	src.FailNext(1, errBoom)
	indexAll(t, e)
	if d.LargeState().Pending != nil || !strings.HasPrefix(d.Status, "Index failed: ") {
		t.Fatalf("pending = %v, status = %q", d.LargeState().Pending, d.Status)
	}
	if d.Cursor != (buffer.Pos{}) {
		t.Fatalf("caret moved to %v", d.Cursor)
	}
}

func TestPendingProgressShowsInTheStatus(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	if err := e.GotoLine(99999, 1); err != nil {
		t.Fatal(err)
	}
	if _, more := indexStep(e); !more {
		t.Fatal("complete after one job")
	}
	if !strings.HasPrefix(d.Status, "Scanning... ") || strings.Contains(d.Status, "built") {
		t.Fatalf("status = %q", d.Status)
	}
}

// TestEditAfterAQueuedSelectionWaitsForTheIndex is the regression test for the actions
// that follow a select_all that is still queued running on the caret: copy copied one
// line, cut removed it, and typing inserted a character, while the user believed the
// whole document was selected.
func TestEditAfterAQueuedSelectionWaitsForTheIndex(t *testing.T) {
	t.Parallel()
	for _, queued := range []Action{"select_all", "select_document_end"} {
		for _, a := range []Action{"copy", "cut", "paste", "insert_char", "delete_backward", "delete_line", "undo"} {
			t.Run(string(queued)+" then "+string(a), func(t *testing.T) {
				t.Parallel()
				e, d := openLarge(t, largeOptions(), lineText(400))
				run(t, e, "move_down")
				run(t, e, queued)
				if d.LargeState().Pending == nil {
					t.Fatalf("%s was not queued", queued)
				}
				version, cursor := d.Buffer.Version(), d.Cursor
				e.clipboard = "kept"
				r := e.Do(a, map[string]any{"text": "x"})
				if !errors.Is(r.Err, ErrIndexing) {
					t.Fatalf("Do(%s) = %v, want ErrIndexing", a, r.Err)
				}
				if d.Buffer.Version() != version || d.Cursor != cursor || e.clipboard != "kept" {
					t.Errorf("%s changed the document: version %d -> %d, caret %v -> %v, clipboard %q",
						a, version, d.Buffer.Version(), cursor, d.Cursor, e.clipboard)
				}
				if d.LargeState().Pending == nil {
					t.Errorf("%s dropped the queued %s", a, queued)
				}
				// Movement is not held back, and it cancels the queued selection as before.
				run(t, e, "move_down")
				indexAll(t, e)
				if d.Status != "Jump cancelled" {
					t.Errorf("status after the index = %q", d.Status)
				}
			})
		}
	}
}
