package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// Column map for the drag tests: "alpha beta gamma" has alpha at 0-4, beta at
// 6-9 and gamma at 11-15. "next line" is on line 1.
const dragText = "alpha beta gamma\nnext line"

func TestExtendSelection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(e *Editor)
		p          buffer.Pos
		u          Unit
		wantAnchor buffer.Pos
		wantHead   buffer.Pos
	}{
		{
			name:       "char drag from caret",
			setup:      func(e *Editor) { e.SetCursor(buffer.Pos{Col: 0}, false) },
			p:          buffer.Pos{Col: 5},
			u:          UnitChar,
			wantAnchor: buffer.Pos{Col: 0},
			wantHead:   buffer.Pos{Col: 5},
		},
		{
			name:       "word drag forward reaches end of word under pointer",
			setup:      func(e *Editor) { e.SelectRange(buffer.Pos{Col: 6}, buffer.Pos{Col: 10}) },
			p:          buffer.Pos{Col: 13},
			u:          UnitWord,
			wantAnchor: buffer.Pos{Col: 6},
			wantHead:   buffer.Pos{Col: 16},
		},
		{
			name:       "word drag backward reaches start of word under pointer",
			setup:      func(e *Editor) { e.SelectRange(buffer.Pos{Col: 6}, buffer.Pos{Col: 10}) },
			p:          buffer.Pos{Col: 1},
			u:          UnitWord,
			wantAnchor: buffer.Pos{Col: 10},
			wantHead:   buffer.Pos{Col: 0},
		},
		{
			name:       "word drag inside the starting word keeps it selected",
			setup:      func(e *Editor) { e.SelectRange(buffer.Pos{Col: 6}, buffer.Pos{Col: 10}) },
			p:          buffer.Pos{Col: 8},
			u:          UnitWord,
			wantAnchor: buffer.Pos{Col: 6},
			wantHead:   buffer.Pos{Col: 10},
		},
		{
			name:       "word drag with no selection starts at caret",
			setup:      func(e *Editor) { e.SetCursor(buffer.Pos{Col: 8}, false) },
			p:          buffer.Pos{Col: 13},
			u:          UnitWord,
			wantAnchor: buffer.Pos{Col: 8},
			wantHead:   buffer.Pos{Col: 16},
		},
		{
			name:       "line drag forward reaches start of next line",
			setup:      func(e *Editor) { e.SelectRange(buffer.LineBounds(e.Active().Buffer, 0)) },
			p:          buffer.Pos{Line: 1, Col: 3},
			u:          UnitLine,
			wantAnchor: buffer.Pos{Line: 0, Col: 0},
			wantHead:   buffer.Pos{Line: 1, Col: 9},
		},
		{
			name:       "line drag backward reaches start of pointer line",
			setup:      func(e *Editor) { e.SelectRange(buffer.LineBounds(e.Active().Buffer, 1)) },
			p:          buffer.Pos{Line: 0, Col: 3},
			u:          UnitLine,
			wantAnchor: buffer.Pos{Line: 1, Col: 9},
			wantHead:   buffer.Pos{Line: 0, Col: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(dragText)
			tt.setup(e)
			e.ExtendSelection(tt.p, tt.u)
			d := e.Active()
			if d.Sel == nil {
				t.Fatal("ExtendSelection left no selection")
			}
			if d.Sel.Anchor != tt.wantAnchor || d.Sel.Head != tt.wantHead {
				t.Fatalf("Sel = %+v, want anchor %+v head %+v", *d.Sel, tt.wantAnchor, tt.wantHead)
			}
			if d.Cursor != tt.wantHead {
				t.Fatalf("Cursor = %+v, want the head %+v", d.Cursor, tt.wantHead)
			}
		})
	}
}

func TestExtendSelectionCharWithNoMovementHasNoSelection(t *testing.T) {
	t.Parallel()
	e := withDoc(dragText)
	e.SetCursor(buffer.Pos{Col: 3}, false)
	e.ExtendSelection(buffer.Pos{Col: 3}, UnitChar)
	if e.Active().HasSelection() {
		t.Fatal("a drag that does not move should select nothing")
	}
}

func TestOpenPreviewLabelsAndReplacesPreview(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b, c, d := writeFile(t, dir, "a.txt"), writeFile(t, dir, "b.txt"), writeFile(t, dir, "c.txt"), writeFile(t, dir, "d.txt")
	e := New(Options{})

	if err := e.OpenPreview(a); err != nil {
		t.Fatalf("OpenPreview(a): %v", err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"a.txt (preview)"}) {
		t.Fatalf("first preview: tabs = %v", got)
	}
	if tab := e.Tabs()[0]; !tab.Preview || !tab.Active {
		t.Fatalf("preview tab flags = %+v", tab)
	}

	if err := e.OpenPreview(b); err != nil {
		t.Fatalf("OpenPreview(b): %v", err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"b.txt (preview)"}) {
		t.Fatalf("second preview should replace the first: tabs = %v", got)
	}

	if err := e.OpenFile(c); err != nil {
		t.Fatalf("OpenFile(c): %v", err)
	}
	if err := e.OpenPreview(d); err != nil {
		t.Fatalf("OpenPreview(d): %v", err)
	}
	// The preview keeps its position: b was first, c is permanent after it.
	if got := tabNames(e); !equalStrings(got, []string{"d.txt (preview)", "c.txt"}) {
		t.Fatalf("preview beside a permanent tab: tabs = %v", got)
	}
	if e.Tabs()[1].Preview {
		t.Fatal("OpenFile tab must not be a preview")
	}
}

func TestPreviewBecomesPermanentWhenEdited(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := writeFile(t, dir, "a.txt"), writeFile(t, dir, "b.txt")
	e := New(Options{})
	if err := e.OpenPreview(a); err != nil {
		t.Fatalf("OpenPreview: %v", err)
	}
	if r := e.Do("insert_char", map[string]any{"text": "x"}); r.Err != nil {
		t.Fatalf("insert_char: %v", r.Err)
	}
	if tab := e.Tabs()[0]; tab.Preview || tab.Name != "a.txt" || !tab.Modified {
		t.Fatalf("edited preview should be permanent: %+v", tab)
	}
	if err := e.OpenPreview(b); err != nil {
		t.Fatalf("OpenPreview(b): %v", err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"a.txt", "b.txt (preview)"}) {
		t.Fatalf("an edited preview must not be replaced: tabs = %v", got)
	}
}

func TestOpenFilePromotesMatchingPreview(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := writeFile(t, dir, "a.txt")
	e := New(Options{})
	if err := e.OpenPreview(a); err != nil {
		t.Fatalf("OpenPreview: %v", err)
	}
	if err := e.OpenFile(a); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"a.txt"}) || e.Tabs()[0].Preview {
		t.Fatalf("OpenFile on a previewed path should promote it, not add a tab: %+v", e.Tabs())
	}
}

func TestOpenPreviewOfOpenPathActivatesExistingTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := writeFile(t, dir, "a.txt"), writeFile(t, dir, "b.txt")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatalf("OpenFile(a): %v", err)
	}
	if err := e.OpenFile(b); err != nil {
		t.Fatalf("OpenFile(b): %v", err)
	}
	if err := e.OpenPreview(a); err != nil {
		t.Fatalf("OpenPreview(a): %v", err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"a.txt", "b.txt"}) {
		t.Fatalf("an open path must not gain a preview tab: tabs = %v", got)
	}
	if e.Active().Name() != "a.txt" {
		t.Fatalf("active = %q, want a.txt", e.Active().Name())
	}
}

func TestOpenPreviewErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.bin")
	if err := os.WriteFile(bad, []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	err := e.OpenPreview(bad)
	if !errors.Is(err, ErrNotUTF8) {
		t.Fatalf("OpenPreview(binary) = %v, want ErrNotUTF8", err)
	}
	if e.Status() != err.Error() {
		t.Fatalf("status = %q, want %q", e.Status(), err.Error())
	}
	if got := tabNames(e); !equalStrings(got, []string{"[No Name]"}) {
		t.Fatalf("failed preview changed tabs: %v", got)
	}
}

// writeFile creates name in dir with a line of text and returns its path.
func writeFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("text of "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
