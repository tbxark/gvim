package editor

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// run applies a sequence of actions with no arguments and fails on any error.
func run(t *testing.T, e *Editor, actions ...Action) {
	t.Helper()
	for _, a := range actions {
		if r := e.Do(a, nil); r.Err != nil {
			t.Fatalf("Do(%s): %v", a, r.Err)
		}
	}
}

// typeText sends each rune of s as insert_char.
func typeText(t *testing.T, e *Editor, s string) {
	t.Helper()
	for _, r := range s {
		if r := e.Do("insert_char", map[string]any{"text": string(r)}); r.Err != nil {
			t.Fatalf("insert_char: %v", r.Err)
		}
	}
}

func text(e *Editor) string { return e.Active().Buffer.Text() }

func TestNewStartsWithUnnamedDocument(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	tabs := e.Tabs()
	if len(tabs) != 1 || tabs[0].Name != "[No Name]" || !tabs[0].Active {
		t.Fatalf("Tabs() = %+v", tabs)
	}
	if e.Status() != "" {
		t.Fatalf("Status() = %q, want empty", e.Status())
	}
}

func TestTypingAndDeletion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		script  func(t *testing.T, e *Editor)
		want    string
		wantCur buffer.Pos
	}{
		{
			name: "type then newline",
			script: func(t *testing.T, e *Editor) {
				typeText(t, e, "ab")
				run(t, e, "insert_newline")
				typeText(t, e, "c")
			},
			want:    "ab\nc",
			wantCur: buffer.Pos{Line: 1, Col: 1},
		},
		{
			name: "backspace joins lines",
			script: func(t *testing.T, e *Editor) {
				typeText(t, e, "ab")
				run(t, e, "insert_newline")
				typeText(t, e, "cd")
				run(t, e, "move_line_start", "delete_backward")
			},
			want:    "abcd",
			wantCur: buffer.Pos{Line: 0, Col: 2},
		},
		{
			name: "forward delete joins lines",
			script: func(t *testing.T, e *Editor) {
				typeText(t, e, "ab")
				run(t, e, "insert_newline", "move_up", "move_line_end", "delete_forward")
			},
			want:    "ab",
			wantCur: buffer.Pos{Line: 0, Col: 2},
		},
		{
			name: "tab inserts a tab rune",
			opts: Options{UseTabs: true},
			script: func(t *testing.T, e *Editor) {
				run(t, e, "insert_tab")
				typeText(t, e, "x")
			},
			want:    "\tx",
			wantCur: buffer.Pos{Line: 0, Col: 2},
		},
		{
			name: "delete word backward",
			script: func(t *testing.T, e *Editor) {
				typeText(t, e, "foo bar")
				run(t, e, "delete_word_backward")
			},
			want:    "foo ",
			wantCur: buffer.Pos{Line: 0, Col: 4},
		},
		{
			name: "delete to line end",
			script: func(t *testing.T, e *Editor) {
				typeText(t, e, "hello")
				run(t, e, "move_left", "move_left", "delete_to_line_end")
			},
			want:    "hel",
			wantCur: buffer.Pos{Line: 0, Col: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(tt.opts)
			tt.script(t, e)
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != tt.wantCur {
				t.Fatalf("cursor = %+v, want %+v", got, tt.wantCur)
			}
		})
	}
}

func TestSelectionCopyCutPaste(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "one two")
	run(t, e, "select_word_left")
	if !e.Active().HasSelection() {
		t.Fatal("select_word_left should create a selection")
	}
	run(t, e, "copy")
	run(t, e, "move_document_end", "paste")
	if got := text(e); got != "one twotwo" {
		t.Fatalf("after paste = %q", got)
	}

	run(t, e, "select_all", "cut")
	if got := text(e); got != "" {
		t.Fatalf("after cut-all = %q", got)
	}
	run(t, e, "paste")
	if got := text(e); got != "one twotwo" {
		t.Fatalf("paste from cut = %q", got)
	}
}

func TestCopyWithoutSelectionFails(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	r := e.Do("copy", nil)
	if !errors.Is(r.Err, ErrNothingSelected) {
		t.Fatalf("copy err = %v, want ErrNothingSelected", r.Err)
	}
	if e.Status() != ErrNothingSelected.Error() {
		t.Fatalf("status = %q", e.Status())
	}
}

func TestSelectAllTypeReplaces(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "abc")
	run(t, e, "select_all")
	typeText(t, e, "x")
	if got := text(e); got != "x" {
		t.Fatalf("typing over selection = %q", got)
	}
}

func TestUndoRedoFromEditor(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "hi")
	run(t, e, "move_left")
	typeText(t, e, "!")
	run(t, e, "undo")
	if got := text(e); got != "hi" {
		t.Fatalf("undo = %q, want %q", got, "hi")
	}
	run(t, e, "redo")
	if got := text(e); got != "h!i" {
		t.Fatalf("redo = %q, want %q", got, "h!i")
	}
}

func TestMoveLineSwapsLines(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "one")
	run(t, e, "insert_newline")
	typeText(t, e, "two")
	run(t, e, "move_line_up")
	if got := text(e); got != "two\none" {
		t.Fatalf("move_line_up = %q", got)
	}
	if e.Active().Cursor.Line != 0 {
		t.Fatalf("cursor line = %d, want 0", e.Active().Cursor.Line)
	}
	run(t, e, "move_line_up") // already first: no change
	if got := text(e); got != "two\none" {
		t.Fatalf("move past top changed text: %q", got)
	}
}

func TestVerticalMotionKeepsWantedColumn(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "longer line")
	run(t, e, "insert_newline")
	typeText(t, e, "ab")
	run(t, e, "move_up")
	if c := e.Active().Cursor.Col; c != 2 {
		t.Fatalf("col after up = %d, want 2", c)
	}
	run(t, e, "move_down", "move_down")
	if c := e.Active().Cursor.Col; c != 2 {
		t.Fatalf("col after returning to short line = %d, want 2", c)
	}
}

func TestSelectMotionsExtendSelection(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "abcdef")
	run(t, e, "move_line_start", "select_right", "select_right")
	sel := e.Active().Sel
	if sel == nil || sel.Anchor != (buffer.Pos{}) || sel.Head != (buffer.Pos{Col: 2}) {
		t.Fatalf("selection = %+v", sel)
	}
	run(t, e, "move_right")
	if e.Active().Sel != nil {
		t.Fatal("plain motion should clear selection")
	}
}

func TestNoopIsSilentWithAndWithoutDocument(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if r := e.Do("noop", nil); r.Err != nil {
		t.Fatalf("noop with no document: %v", r.Err)
	}
	e.newUntitled()
	if r := e.Do("noop", nil); r.Err != nil {
		t.Fatalf("noop with a document: %v", r.Err)
	}
}

func TestUnknownActionAndBadArguments(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if r := e.Do("no_such_action", nil); !errors.Is(r.Err, ErrUnknownAction) {
		t.Fatalf("unknown action err = %v", r.Err)
	}
	if r := e.Do("insert_char", nil); r.Err == nil {
		t.Fatal("insert_char without text should fail")
	}
}

func TestMultipleTabsAndCloseRules(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "x")
	run(t, e, "new")
	if len(e.Tabs()) != 2 || e.Tabs()[1].Active != true {
		t.Fatalf("new tab not active: %+v", e.Tabs())
	}
	run(t, e, "prev_buffer")
	if !e.Tabs()[0].Active {
		t.Fatal("prev_buffer should activate the first tab")
	}
	// The first tab is modified, so closing it needs force.
	r := e.Do("close_tab", nil)
	if !errors.Is(r.Err, ErrUnsaved) {
		t.Fatalf("close modified tab err = %v", r.Err)
	}
	// The second tab is unmodified, so it closes without force.
	run(t, e, "next_buffer", "close_tab")
	if n := len(e.Tabs()); n != 1 || e.Tabs()[0].Modified != true {
		t.Fatalf("tabs after closing clean tab = %+v", e.Tabs())
	}
	if r := e.Do("close_tab", map[string]any{"force": true}); r.Err != nil {
		t.Fatalf("forced close: %v", r.Err)
	}
	if !e.Empty() || len(e.Tabs()) != 0 || e.Active() != nil {
		t.Fatalf("closing the last tab should leave the editor empty: %+v", e.Tabs())
	}
}

func TestQuitRequiresSavedBuffers(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "dirty")
	r := e.Do("quit", nil)
	if !errors.Is(r.Err, ErrUnsaved) || r.Quit {
		t.Fatalf("quit with unsaved edits: %+v", r)
	}
	if r := e.Do("quit", map[string]any{"force": true}); !r.Quit {
		t.Fatal("forced quit should set Quit")
	}
}

func TestOpenSaveRoundTripBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	original := "héllo\r\nworld\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if e.Status() != "Opened a.txt" {
		t.Fatalf("status = %q", e.Status())
	}
	if e.Active().Modified() {
		t.Fatal("freshly opened file should not be modified")
	}
	run(t, e, "move_document_end", "insert_newline")
	typeText(t, e, "x")
	run(t, e, "save")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The input has one CRLF and one LF break, a tie, so the file is LF. The
	// first line keeps its CRLF and the new lines take LF. See
	// TestMixedLineEndingsAreKeptOnSave.
	if want := "héllo\r\nworld\n\nx"; string(got) != want {
		t.Fatalf("saved bytes = %q, want %q", got, want)
	}
	if e.Active().Modified() {
		t.Fatal("save should clear modified")
	}
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bin")
	// A UTF-16LE byte-order mark, then a lone high surrogate: no text decodes.
	if err := os.WriteFile(bad, []byte{0xff, 0xfe, 0x00, 0xd8}, 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(bad); !errors.Is(err, ErrNotDecodable) {
		t.Fatalf("binary open err = %v, want ErrNotDecodable", err)
	}
	if len(e.Tabs()) != 1 {
		t.Fatal("failed open must not add a tab")
	}
}

func TestSaveWithoutPathFails(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if r := e.Do("save", nil); !errors.Is(r.Err, ErrNoPath) {
		t.Fatalf("save unnamed err = %v", r.Err)
	}
}

func TestMissingFileOpensEmptyAndSavesCreates(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "new.txt")
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile missing: %v", err)
	}
	typeText(t, e, "made")
	run(t, e, "save")
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "made" {
		t.Fatalf("created file = %q, %v", got, err)
	}
}

func TestViewportScrollsToCursor(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 3, Cols: 10})
	for range 10 {
		run(t, e, "insert_newline")
	}
	d := e.Active()
	if d.Cursor.Line != 10 || d.Top != 8 {
		t.Fatalf("cursor line %d, top %d; want 10 and 8", d.Cursor.Line, d.Top)
	}
	run(t, e, "move_document_start")
	if d.Top != 0 {
		t.Fatalf("top after doc start = %d, want 0", d.Top)
	}
}

func TestActionsListIsSortedAndComplete(t *testing.T) {
	t.Parallel()
	names := Actions()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("Actions not sorted at %d: %q >= %q", i, names[i-1], names[i])
		}
	}
	for _, want := range []Action{"move_left", "select_left", "move_word_right", "smart_home", "save", "undo"} {
		found := slices.Contains(names, want)
		if !found {
			t.Fatalf("Actions() missing %q", want)
		}
	}
}
