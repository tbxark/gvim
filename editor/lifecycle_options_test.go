package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfirmQuit(t *testing.T) {
	t.Parallel()
	t.Run("off quits a clean session", func(t *testing.T) {
		e := New(Options{})
		if e.QuitNeedsConfirm() {
			t.Fatal("QuitNeedsConfirm = true by default")
		}
		if r := e.Do("quit", nil); r.Err != nil || !r.Quit {
			t.Fatalf("quit = %+v", r)
		}
	})
	t.Run("action asks first", func(t *testing.T) {
		e := New(Options{ConfirmQuit: true})
		if !e.QuitNeedsConfirm() {
			t.Fatal("QuitNeedsConfirm = false with ConfirmQuit")
		}
		if r := e.Do("quit", nil); !errors.Is(r.Err, ErrQuitNeedsConfirm) || r.Quit || e.Quitting() {
			t.Fatalf("quit = %+v, quitting = %v; want ErrQuitNeedsConfirm", r, e.Quitting())
		}
		if r := e.Do("quit", map[string]any{"force": true}); r.Err != nil || !r.Quit {
			t.Fatalf("forced quit = %+v", r)
		}
	})
	t.Run("Quit waits for the answer", func(t *testing.T) {
		e := New(Options{ConfirmQuit: true})
		if dirty := e.Quit(false); dirty != nil || e.Quitting() {
			t.Fatalf("Quit(false) = %v, quitting = %v; want a pending confirmation", dirty, e.Quitting())
		}
		e.Quit(true)
		if !e.Quitting() {
			t.Fatal("Quit(true) did not quit")
		}
	})
	t.Run("unsaved tabs are listed first", func(t *testing.T) {
		e := New(Options{ConfirmQuit: true})
		typeText(t, e, "dirty")
		if r := e.Do("quit", nil); !errors.Is(r.Err, ErrUnsaved) {
			t.Fatalf("quit = %+v, want ErrUnsaved", r)
		}
		if dirty := e.Quit(false); len(dirty) != 1 {
			t.Fatalf("Quit(false) = %v, want the dirty tab", dirty)
		}
	})
}

func TestLastTabClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	closers := map[string]func(e *Editor) error{
		"CloseTab":    func(e *Editor) error { return e.CloseTab(0, false) },
		"CloseByPath": func(e *Editor) error { return e.CloseByPath(path, false) },
	}
	for name, closeIt := range closers {
		for _, auto := range []bool{false, true} {
			t.Run(name, func(t *testing.T) {
				e := New(Options{AutoCreateEmptyBufferOnLastBufferClose: auto})
				if err := e.OpenFile(path); err != nil {
					t.Fatal(err)
				}
				if err := closeIt(e); err != nil {
					t.Fatal(err)
				}
				if e.Empty() == auto {
					t.Fatalf("auto = %v: Empty() = %v", auto, e.Empty())
				}
				if auto {
					d := e.Active()
					if d == nil || d.Path != "" || d.Modified() || len(e.Tabs()) != 1 {
						t.Fatalf("active = %+v, tabs = %d; want one pristine untitled tab", d, len(e.Tabs()))
					}
				}
			})
		}
	}
}
