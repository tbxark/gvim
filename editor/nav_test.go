package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// navDo runs one action with no arguments and fails the test on error.
func navDo(t *testing.T, e *Editor, a Action) {
	t.Helper()
	if r := e.Do(a, nil); r.Err != nil {
		t.Fatalf("Do(%s): %v", a, r.Err)
	}
}

// navGoto moves the caret to a 0-based line, the way the go-to-line prompt does.
func navGoto(t *testing.T, e *Editor, line int) {
	t.Helper()
	if err := e.GotoLine(line+1, 1); err != nil {
		t.Fatalf("GotoLine(%d): %v", line+1, err)
	}
}

// navLine returns the caret line of the active document.
func navLine(e *Editor) int { return e.Active().Cursor.Line }

func TestNavigateBackAndForwardFollowJumps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		steps    func(t *testing.T, e *Editor)
		wantLine int
	}{
		{
			Name:     "navigate_back returns from a go-to-line",
			steps:    func(t *testing.T, e *Editor) { navGoto(t, e, 2); navDo(t, e, "navigate_back") },
			wantLine: 0,
		},
		{
			Name: "navigate_forward returns after a back",
			steps: func(t *testing.T, e *Editor) {
				navGoto(t, e, 2)
				navDo(t, e, "navigate_back")
				navDo(t, e, "navigate_forward")
			},
			wantLine: 2,
		},
		{
			Name:     "navigate_back with no history does nothing",
			steps:    func(t *testing.T, e *Editor) { navDo(t, e, "navigate_back") },
			wantLine: 0,
		},
		{
			Name:     "navigate_forward at the end does nothing",
			steps:    func(t *testing.T, e *Editor) { navGoto(t, e, 2); navDo(t, e, "navigate_forward") },
			wantLine: 2,
		},
		{
			Name: "a new jump drops the forward history",
			steps: func(t *testing.T, e *Editor) {
				navGoto(t, e, 3)
				navGoto(t, e, 6)
				navDo(t, e, "navigate_back")
				navDo(t, e, "navigate_back")
				navGoto(t, e, 9)
				navDo(t, e, "navigate_forward")
			},
			wantLine: 9,
		},
		{
			Name: "two backs return to the first place",
			steps: func(t *testing.T, e *Editor) {
				navGoto(t, e, 2)
				navGoto(t, e, 5)
				navDo(t, e, "navigate_back")
				navDo(t, e, "navigate_back")
			},
			wantLine: 0,
		},
		{
			Name: "navigate_back undoes a bookmark jump",
			steps: func(t *testing.T, e *Editor) {
				navGoto(t, e, 5)
				if r := e.Do("set_bookmark", slotArgs("1")); r.Err != nil {
					t.Fatalf("set_bookmark: %v", r.Err)
				}
				navGoto(t, e, 0)
				if r := e.Do("jump_to_bookmark", slotArgs("1")); r.Err != nil {
					t.Fatalf("jump_to_bookmark: %v", r.Err)
				}
				navDo(t, e, "navigate_back")
			},
			wantLine: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(numbered(10))
			tt.steps(t, e)
			if got := navLine(e); got != tt.wantLine {
				t.Fatalf("caret line = %d, want %d", got, tt.wantLine)
			}
		})
	}
}

func TestNavigateBackReturnsToEarlierFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("a1\na2\na3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b1\nb2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatalf("OpenFile(a): %v", err)
	}
	marksCaret(e, 2, 0)
	if err := e.OpenFile(b); err != nil {
		t.Fatalf("OpenFile(b): %v", err)
	}
	navDo(t, e, "navigate_back")
	if got := e.Active().Path; got != a {
		t.Fatalf("active path after back = %q, want %q", got, a)
	}
	if got := navLine(e); got != 2 {
		t.Fatalf("caret line after back = %d, want 2", got)
	}
	navDo(t, e, "navigate_forward")
	if got := e.Active().Path; got != b {
		t.Fatalf("active path after forward = %q, want %q", got, b)
	}
}

func TestNavigationHistoryKeepsTheLatestHundredPlaces(t *testing.T) {
	t.Parallel()
	e := withDoc(numbered(3))
	// Each jump moves between lines 0 and 1, so every origin differs from the
	// one before it and none is dropped as a duplicate.
	for i := range 150 {
		navGoto(t, e, (i+1)%2)
	}
	backs := 0
	for range 200 {
		before := navLine(e)
		navDo(t, e, "navigate_back")
		if navLine(e) == before {
			break
		}
		backs++
	}
	// The history holds 100 places. The place the caret was on when the first
	// back ran takes one of them, which leaves 99 places to go back to.
	if backs != 99 {
		t.Fatalf("successful navigate_back calls = %d, want 99", backs)
	}
}

func TestNavigationSkipsAPlaceWhoseTabIsClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths := map[string]string{}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x\ny\nz\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = p
	}
	// The history holds a at line 1, then b at line 0, then c is active. Closing
	// b leaves a place with no tab between the open tabs a and c.
	e := New(Options{})
	if err := e.OpenFile(paths["a.txt"]); err != nil {
		t.Fatal(err)
	}
	navGoto(t, e, 1)
	if err := e.OpenFile(paths["b.txt"]); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(paths["c.txt"]); err != nil {
		t.Fatal(err)
	}
	if err := e.CloseTab(1, true); err != nil {
		t.Fatalf("CloseTab(b): %v", err)
	}
	navDo(t, e, "navigate_back")
	if got := filepath.Base(e.Active().Path); got != "a.txt" {
		t.Fatalf("active file after navigate_back = %q, want a.txt", got)
	}
	if got := navLine(e); got != 1 {
		t.Fatalf("caret line after navigate_back = %d, want 1", got)
	}
	navDo(t, e, "navigate_forward")
	if got := filepath.Base(e.Active().Path); got != "c.txt" {
		t.Fatalf("active file after navigate_forward = %q, want c.txt", got)
	}
}
