package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	expanel "github.com/tbxark/gvim/internal/tui/explorer"
)

const conflictFile = "head\n" +
	"<<<<<<< HEAD\n" +
	"our line\n" +
	"=======\n" +
	"their line\n" +
	">>>>>>> other\n" +
	"middle\n" +
	"<<<<<<< HEAD\n" +
	"our second\n" +
	"=======\n" +
	"their second\n" +
	">>>>>>> other\n" +
	"tail\n"

var bgSGR = regexp.MustCompile(`48;2;\d+;\d+;\d+|48;5;\d+`)

// rowBackgrounds returns the background colors painted on the row of the screen that has
// text in it.
func (s *pluginSession) rowBackgrounds(text string) map[string]bool {
	s.t.Helper()
	out := map[string]bool{}
	raw := strings.Split(s.m.View().Content, "\n")
	for _, r := range raw {
		if !strings.Contains(stripANSI(r), text) {
			continue
		}
		for _, m := range bgSGR.FindAllString(r, -1) {
			out[m] = true
		}
	}
	return out
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;:]*[A-Za-z]")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// conflictSession opens the conflict file in the real app with the bundled merge_conflict
// plugin (on by default) and waits for its scan.
func conflictSession(t *testing.T) *pluginSession {
	t.Helper()
	isolateConfig(t)
	s := newSession(t, Options{})
	s.waitStatus("merge_conflict", "loaded")
	// The subscriptions exist now; the next message turns the change tracking on.
	s.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	path := filepath.Join(t.TempDir(), "c.txt")
	must(t, os.WriteFile(path, []byte(conflictFile), 0o644))
	s.send(expanel.OpenFileMsg{Path: path})
	s.until("the conflicts to be painted", func() bool {
		s.settle(10 * time.Millisecond)
		return len(s.rowBackgrounds("our line")) > len(s.rowBackgrounds("middle"))
	})
	return s
}

func TestMergeConflictPaintsAndNavigates(t *testing.T) {
	s := conflictSession(t)
	d := s.rt.ed.Active()

	// The sides have the diff colors, and the plain text around them does not.
	ours, theirs, plain := s.rowBackgrounds("our line"), s.rowBackgrounds("their line"), s.rowBackgrounds("middle")
	for bg := range plain {
		delete(ours, bg)
		delete(theirs, bg)
	}
	if len(ours) == 0 || len(theirs) == 0 {
		t.Fatalf("the sides are not painted: ours %v theirs %v plain %v", ours, theirs, plain)
	}
	if len(s.rowBackgrounds("<<<<<<<")) == len(plain) {
		t.Errorf("the marker line is not painted")
	}

	s.runCommand("Merge: Next Conflict")
	if d.Cursor.Line != 1 || !strings.Contains(s.statusRow(), "Conflict 1 of 2") {
		t.Fatalf("after Next: cursor %+v, status %q", d.Cursor, s.statusRow())
	}
	s.runCommand("Merge: Next Conflict")
	if d.Cursor.Line != 7 || !strings.Contains(s.statusRow(), "Conflict 2 of 2") {
		t.Fatalf("after Next again: cursor %+v, status %q", d.Cursor, s.statusRow())
	}
	s.runCommand("Merge: Previous Conflict")
	if d.Cursor.Line != 1 {
		t.Fatalf("after Previous: cursor %+v", d.Cursor)
	}
}

func TestMergeConflictResolvesInOneUndoStep(t *testing.T) {
	s := conflictSession(t)
	s.runCommand("Merge: Next Conflict") // caret on the first start marker
	s.runCommand("Merge: Take Theirs")
	want := "head\ntheir line\nmiddle\n<<<<<<< HEAD\nour second\n=======\ntheir second\n>>>>>>> other\ntail\n"
	if got := s.text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	s.until("the first conflict to be unpainted and the second painted", func() bool {
		s.settle(10 * time.Millisecond)
		return !s.screenHas("<<<<<<< HEAD") || len(s.rowBackgrounds("our second")) > len(s.rowBackgrounds("middle"))
	})
	// The caret is on the conflict that is left (line 3) and the notice counts it.
	if d := s.rt.ed.Active(); d.Cursor.Line != 3 || !strings.Contains(s.statusRow(), "1 conflict remaining") {
		t.Fatalf("cursor %+v, status %q", d.Cursor, s.statusRow())
	}

	s.runCommand("Merge: Use Both")
	if got := s.text(); got != "head\ntheir line\nmiddle\nour second\ntheir second\ntail\n" {
		t.Fatalf("text after Use Both = %q", got)
	}
	if !strings.Contains(s.statusRow(), "all conflicts resolved") {
		t.Fatalf("status = %q", s.statusRow())
	}
	s.until("the paint to go", func() bool {
		s.settle(10 * time.Millisecond)
		return len(s.rowBackgrounds("their second")) == len(s.rowBackgrounds("tail"))
	})

	// Each resolution is one undo step.
	s.press(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := s.text(); got != want {
		t.Fatalf("text after one undo = %q, want %q", got, want)
	}
	s.press(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := s.text(); got != conflictFile {
		t.Fatalf("text after two undos = %q, want the original", got)
	}
}

// TestMergeConflictKeyMode: Start Resolution takes single keys; letters do not type; the
// mode ends when the last conflict is resolved.
func TestMergeConflictKeyMode(t *testing.T) {
	s := conflictSession(t)
	s.runCommand("Merge: Start Resolution")
	s.until("the mode label", func() bool { return strings.Contains(s.statusRow(), "-- MERGE --") || s.screenHas("-- MERGE --") })

	s.key('x') // swallowed
	if got := s.text(); got != conflictFile {
		t.Fatalf("a letter typed in the mode: %q", got)
	}
	s.key('u') // ours of the first conflict
	s.key('t') // theirs of the second
	want := "head\nour line\nmiddle\ntheir second\ntail\n"
	if got := s.text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	s.until("the mode to end", func() bool { return !s.screenHas("-- MERGE --") })
	s.key('x')
	if got := s.text(); strings.Count(got, "x") != 1 || len(got) != len(want)+1 {
		t.Fatalf("typing did not come back: %q", got)
	}
}

// TestMergeConflictFollowsTyping: typing above a conflict moves it, and spoiling a marker
// ends it, once the plugin has seen the buffer_changed event (it waits 300 ms for quiet).
func TestMergeConflictFollowsTyping(t *testing.T) {
	s := conflictSession(t)
	painted := func(text string) bool { return len(s.rowBackgrounds(text)) > len(s.rowBackgrounds("tail")) }

	// A new first line: both conflicts move down one row and stay painted.
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the conflicts to stay painted", func() bool {
		s.settle(20 * time.Millisecond)
		return painted("our line") && painted("their second")
	})

	// Spoil the first separator: that conflict is no longer one, the other still is.
	for range 3 {
		s.press(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	s.press(tea.KeyPressMsg{Code: tea.KeyEnd})
	s.typeText("x")
	if got := strings.Split(s.text(), "\n")[4]; got != "=======x" {
		t.Fatalf("the line typed on is %q", got)
	}
	s.until("the first conflict to be unpainted", func() bool {
		s.settle(20 * time.Millisecond)
		return !painted("their line") && painted("their second")
	})
}
