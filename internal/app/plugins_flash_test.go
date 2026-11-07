package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// flashSession starts the app with the bundled flash plugin turned on (it is off by
// default) over a buffer with the text.
func flashSession(t *testing.T, text string) *pluginSession {
	t.Helper()
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins": {"flash": {"enabled": true}}}`)
	s := newSession(t, Options{})
	s.openFile("a.txt", text)
	s.waitStatus("flash", "loaded")
	s.settle(30 * time.Millisecond)
	return s
}

func (s *pluginSession) key(code rune) {
	s.t.Helper()
	k := tea.KeyPressMsg{Code: code}
	if code >= ' ' && code < 0x7f {
		k.Text = string(code)
	}
	s.press(k)
}

func TestFlashOffByDefault(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.waitStatus("merge_conflict", "loaded")
	s.settle(30 * time.Millisecond)
	if st := s.status("flash"); st == "loaded" {
		t.Fatal("flash is loaded without being enabled")
	}
	if s.paletteHas("Flash: Jump") {
		t.Fatal("the palette lists Flash: Jump while the plugin is off")
	}
}

// TestFlashJumpsWithLabels drives the real runtime: the command starts a key capture, the
// labels are drawn over the text (the buffer keeps its text), a label key moves the caret,
// and nothing of it reaches the document.
func TestFlashJumpsWithLabels(t *testing.T) {
	const text = "alpha beta\ngamma delta\n"
	s := flashSession(t, text)

	s.runCommand("Flash: Jump")
	s.until("the Flash banner", func() bool { return strings.Contains(s.statusRow(), "Flash[]") })
	// Every word start has a label over its second letter. a, b, g and d start words, so
	// they are never labels; the others go to the nearest words first: s f h j.
	for _, want := range []string{"aspha bfta", "ghmma djlta"} {
		if !s.screenHas(want) {
			t.Fatalf("the screen lacks %q:\n%s", want, strings.Join(s.rows(), "\n"))
		}
	}
	if got := s.text(); got != text {
		t.Fatalf("text = %q: labels must be decorations", got)
	}

	// Typing a pattern narrows the matches: the t of "beta" and of "delta", each labelled
	// over the letter after it, nearest first (s then d; a is the letter after both).
	s.key('t')
	s.until("the pattern in the banner", func() bool { return strings.Contains(s.statusRow(), "Flash[t]") })
	if !s.screenHas("alpha bets") || !s.screenHas("gamma deltd") {
		t.Fatalf("matches of t:\n%s", strings.Join(s.rows(), "\n"))
	}

	// A label jumps.
	s.key('s')
	s.until("the banner to go", func() bool { return !strings.Contains(s.statusRow(), "Flash[") })
	d := s.rt.ed.Active()
	if d.Cursor.Line != 0 || d.Cursor.Col != 8 {
		t.Fatalf("cursor = %+v, want 0:8 (the t of beta)", d.Cursor)
	}
	if got := s.text(); got != text {
		t.Fatalf("text = %q: the keys of the session reached the document", got)
	}
	if s.screenHas("bets") || s.screenHas("deltd") {
		t.Fatalf("labels are still drawn after the jump:\n%s", strings.Join(s.rows(), "\n"))
	}
	// The editor takes keys again.
	s.key('x')
	if got := s.text(); got != "alpha bexta\ngamma delta\n" {
		t.Fatalf("text after typing = %q", got)
	}
}

func TestFlashAcrossSplits(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins": {"flash": {"enabled": true}}}`)
	s := newSession(t, Options{})
	s.openFile("a.txt", "alpha beta\n")
	s.openFile("b.txt", "gamma delta\n")
	s.waitStatus("flash", "loaded")
	s.runCommand("Split Vertical")
	// The focused (right) pane now shows a.txt, the left one b.txt.
	s.press(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModCtrl})
	s.settle(30 * time.Millisecond)
	if got := s.text(); got != "alpha beta\n" {
		t.Fatalf("focused text = %q", got)
	}
	s.runCommand("Flash: Jump")
	s.until("the banner", func() bool { return strings.Contains(s.statusRow(), "Flash[]") })
	// The focused pane's words get the nearest labels (s, f), the other pane's the next ones
	// (h, j; g and d start words and a is a label over an a, which looks the same).
	var row string
	for _, r := range s.rows() {
		if strings.Contains(r, "ghmma djlta") {
			row = r
		}
	}
	if !strings.Contains(row, "aspha bfta") {
		t.Fatalf("labels in both panes expected on one row, got\n%s", strings.Join(s.rows(), "\n"))
	}
	// A label in the other pane focuses it and moves its caret.
	s.key('j')
	s.until("the banner to go", func() bool { return !strings.Contains(s.statusRow(), "Flash[") })
	if got := s.text(); got != "gamma delta\n" {
		t.Fatalf("focused text after the jump = %q, want the other pane's buffer", got)
	}
	if d := s.rt.ed.Active(); d.Cursor.Line != 0 || d.Cursor.Col != 6 {
		t.Fatalf("cursor = %+v, want 0:6", d.Cursor)
	}
}

func TestFlashEscapeAndEnter(t *testing.T) {
	const text = "alpha beta\ngamma delta\n"
	s := flashSession(t, text)
	d := s.rt.ed.Active()

	s.runCommand("Flash: Jump")
	s.until("the banner", func() bool { return strings.Contains(s.statusRow(), "Flash[]") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.until("the banner to go", func() bool { return !strings.Contains(s.statusRow(), "Flash[") })
	if d.Cursor.Line != 0 || d.Cursor.Col != 0 {
		t.Fatalf("cursor after Escape = %+v", d.Cursor)
	}
	if s.screenHas("aspha") {
		t.Fatal("labels are still drawn after Escape")
	}

	// Enter jumps to the nearest match: with the caret at the end of the second line, the
	// nearest "a" is the last letter of "delta".
	s.press(tea.KeyPressMsg{Code: tea.KeyDown})
	s.press(tea.KeyPressMsg{Code: tea.KeyEnd})
	if d.Cursor.Line != 1 || d.Cursor.Col != 11 {
		t.Fatalf("cursor before the jump = %+v", d.Cursor)
	}
	s.runCommand("Flash: Jump")
	s.until("the banner", func() bool { return strings.Contains(s.statusRow(), "Flash[]") })
	s.key('a') // not a label: "alpha", "beta", "gamma" and "delta" all contain it
	s.until("the pattern", func() bool { return strings.Contains(s.statusRow(), "Flash[a]") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the banner to go", func() bool { return !strings.Contains(s.statusRow(), "Flash[") })
	if d.Cursor.Line != 1 || d.Cursor.Col != 10 {
		t.Fatalf("cursor after Enter = %+v, want 1:10", d.Cursor)
	}
}
