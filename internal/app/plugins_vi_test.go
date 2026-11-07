package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/uitest"
)

// viSession starts the real app with the bundled vi_mode enabled by the configuration
// (it is off by default) over a file with text.
func viSession(t *testing.T, text string) *pluginSession {
	t.Helper()
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins": {"vi_mode": {"enabled": true}}}`)
	s := newSession(t, Options{})
	s.openFile("a.txt", text)
	s.waitStatus("vi_mode", "loaded")
	s.until("normal mode", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
	return s
}

func (s *pluginSession) enter() { s.press(tea.KeyPressMsg{Code: tea.KeyEnter}) }
func (s *pluginSession) esc()   { s.press(tea.KeyPressMsg{Code: tea.KeyEscape}) }

// TestViModeIsOffByDefault: the bundled plugin ships disabled, and a plain editor types.
func TestViModeIsOffByDefault(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.openFile("a.txt", "hello\n")
	s.until("the plugin list", func() bool { return len(s.plug.Plugins()) > 0 })
	for _, p := range s.plug.Plugins() {
		if p.Name == "vi_mode" && p.Status == "loaded" {
			t.Fatalf("vi_mode is %s without being enabled", p.Status)
		}
	}
	s.typeText("x")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "xhello\n" {
		t.Fatalf("text = %q: typing must reach the editor", got)
	}
}

// TestViModeEndToEnd plays a vi session on the real editor: normal mode swallows letters,
// operators, counts, insert mode, the : prompt, visual mode, search, undo, and the way
// out through the panic chord, three Esc presses, and the toggle command.
func TestViModeEndToEnd(t *testing.T) {
	s := viSession(t, "hello world\nsecond line\nthird\n")

	// a letter is a command, not text
	s.typeText("x")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "ello world\nsecond line\nthird\n" {
		t.Fatalf("after x: %q", got)
	}
	// operators, counts and motions
	s.typeText("dw")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world\nsecond line\nthird\n" {
		t.Fatalf("after dw: %q", got)
	}
	s.typeText("jddk")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world\nthird\n" {
		t.Fatalf("after jddk: %q", got)
	}
	s.typeText("u")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world\nsecond line\nthird\n" {
		t.Fatalf("after u: %q", got)
	}
	// redo, and an arrow key that moves like l
	s.press(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world\nthird\n" {
		t.Fatalf("after ctrl+r: %q", got)
	}
	s.typeText("u")
	s.typeText("gg0")
	s.press(tea.KeyPressMsg{Code: tea.KeyRight})
	s.typeText("x")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "wrld\nsecond line\nthird\n" {
		t.Fatalf("after Right and x: %q", got)
	}
	s.typeText("u")
	s.settle(20 * time.Millisecond)
	// insert mode types, Esc comes back and steps left
	s.typeText("ggA")
	s.until("the insert label", func() bool { return strings.Contains(s.statusRow(), "-- INSERT --") })
	s.typeText("!!")
	s.esc()
	s.until("normal again", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
	if got := s.text(); got != "world!!\nsecond line\nthird\n" {
		t.Fatalf("after A!!: %q", got)
	}
	// a pending operator shows in the status row
	s.typeText("2d")
	s.until("the pending keys", func() bool { return strings.Contains(s.statusRow(), "2d") })
	s.esc()
	// dot repeat
	s.typeText("jA?")
	s.esc()
	s.typeText("j.")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world!!\nsecond line?\nthird?\n" {
		t.Fatalf("after dot repeat: %q", got)
	}
	// the : prompt
	s.typeText(":")
	s.until("the prompt", func() bool { return s.screenHas(":") })
	s.typeText("%s/line/LINE/")
	s.enter()
	s.until("the substitution", func() bool { return strings.Contains(s.text(), "second LINE?") })
	// search and delete to the match
	s.typeText("gg/")
	s.typeText("third")
	s.enter()
	s.settle(20 * time.Millisecond)
	s.typeText("dd")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world!!\nsecond LINE?\n" {
		t.Fatalf("after /third and dd: %q", got)
	}
	// a vi pattern with word boundaries goes through the editor's search
	s.typeText("gg/")
	s.typeText("\\<sec")
	s.enter()
	s.settle(20 * time.Millisecond)
	s.typeText("x")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world!!\necond LINE?\n" {
		t.Fatalf("after /\\<sec and x: %q", got)
	}
	s.typeText("u")
	s.settle(20 * time.Millisecond)
	// visual line mode
	s.typeText("ggVj")
	s.until("the visual label", func() bool { return strings.Contains(s.statusRow(), "-- VISUAL LINE --") })
	s.typeText("d")
	s.until("normal after the operator", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
	if got := s.text(); got != "" && got != "\n" {
		t.Fatalf("after Vjd: %q", got)
	}
	s.typeText("u")
	s.settle(20 * time.Millisecond)

	// the panic chord ends vi, whatever mode it is in
	s.typeText("i")
	s.until("insert", func() bool { return strings.Contains(s.statusRow(), "-- INSERT --") })
	s.press(tea.KeyPressMsg{Code: '^', Mod: tea.ModCtrl})
	s.until("vi to end", func() bool { return !strings.Contains(s.statusRow(), "-- INSERT --") })
	before := s.text()
	s.typeText("Q")
	s.settle(20 * time.Millisecond)
	if got := s.text(); !strings.HasPrefix(got, "Q") || got != "Q"+before {
		t.Fatalf("after the panic chord a key must type: %q (was %q)", got, before)
	}
	// Toggle brings vi back, in normal mode
	s.runCommand("Vi Mode: Toggle")
	s.until("vi on again", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
	s.typeText("x")
	s.settle(20 * time.Millisecond)
	// the caret stood after the typed Q, on the w
	if got := s.text(); got != "Q"+before[1:] {
		t.Fatalf("x after the toggle: %q, want %q", got, "Q"+before[1:])
	}

	// three Esc presses within 600 ms clear the input mode too
	s.esc()
	s.esc()
	s.esc()
	s.until("vi to end after three Esc", func() bool { return !strings.Contains(s.statusRow(), "-- NORMAL --") })
	s.typeText("z")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "Qz"+before[1:] {
		t.Fatalf("after three Esc a key must type: %q", got)
	}
	// and the toggle command turns it on again
	s.runCommand("Vi Mode: Toggle")
	s.until("vi on once more", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
}

// TestViModeCells checks the painted cells: the mode label on the status row, the text,
// and that normal mode does not print what is typed.
func TestViModeCells(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins": {"vi_mode": {"enabled": true}}}`)
	t.Setenv("GVIM_PLUGIN_DEBUG", "1")
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello world\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := buildRuntime(t.Context(), Options{Root: t.TempDir(), Keymap: "macos", Files: []FileTarget{{Path: path}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.close)
	s := uitest.Start(t, rt.model, 100, 30)

	label := func(sc uitest.Screen, text string) (int, int, bool) { return sc.Find(text) }
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("-- NORMAL --") && sc.Contains("hello world") })
	x, y, ok := label(sc, "-- NORMAL --")
	if !ok || y != sc.Height()-1 {
		t.Fatalf("the label is at %d,%d (found %v), want the status row %d:\n%s", x, y, ok, sc.Height()-1, sc)
	}
	for i, want := range "-- NORMAL --" {
		if got := sc.Cell(x+i, y).Char; got != string(want) {
			t.Fatalf("status cell %d = %q, want %q", x+i, got, string(want))
		}
	}

	s.Type("w")
	s.Type("D")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return !sc.Contains("hello world") })
	tx, ty, ok := sc.Find("hello ")
	if !ok {
		t.Fatalf("the first line lost its beginning:\n%s", sc)
	}
	for i, want := range "hello" {
		if got := sc.Cell(tx+i, ty).Char; got != string(want) {
			t.Fatalf("text cell %d of the first row = %q, want %q", i, got, string(want))
		}
	}
	if sc.Contains("world") {
		t.Fatalf("D left the rest of the line:\n%s", sc)
	}

	s.Type("A")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("-- INSERT --") })
	if sc.Contains("-- NORMAL --") {
		t.Fatalf("both labels are showing:\n%s", sc)
	}
	s.Type("ok")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("hello ok") })
	_ = sc
	s.Key("esc")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("-- NORMAL --") && !sc.Contains("-- INSERT --") })

	// a visual selection and a visual block are painted: the cells under them take the
	// selection colour, the cells next to them do not
	s.Type("gg0")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("hello ok") })
	tx, ty, _ = sc.Find("hello ok")
	plain := sc.Cell(tx+5, ty).Bg // a cell nothing is drawn on
	s.Type("vl")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("-- VISUAL --") })
	sel := sc.Cell(tx, ty).Bg
	if sel == plain {
		t.Errorf("the first selected cell keeps the background %v", sel)
	}
	if got := sc.Cell(tx+1, ty).Bg; got != sel {
		t.Errorf("the second selected cell has the background %v, want the selection's %v", got, sel)
	}
	if got := sc.Cell(tx+3, ty).Bg; got != plain {
		t.Errorf("a cell outside the selection has the background %v, want %v", got, plain)
	}
	s.Key("esc")
	s.Type("0")
	s.Key("ctrl+v")
	s.Type("jl")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("-- VISUAL BLOCK --") })
	sx, sy, ok := sc.Find("second")
	if !ok {
		t.Fatalf("the second line is not on the screen:\n%s", sc)
	}
	// the caret is drawn on the head, (1,1); the other three cells of the block are painted
	for _, c := range [][2]int{{tx, ty}, {tx + 1, ty}, {sx, sy}} {
		if got := sc.Cell(c[0], c[1]).Bg; got != sel {
			t.Errorf("block cell (%d,%d) has the background %v, want %v:\n%s", c[0], c[1], got, sel, sc)
		}
	}
	for _, c := range [][2]int{{tx + 3, ty}, {sx + 3, sy}} {
		if got := sc.Cell(c[0], c[1]).Bg; got == sel {
			t.Errorf("cell (%d,%d) outside the block is painted as selected", c[0], c[1])
		}
	}
	s.Key("esc")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("-- NORMAL --") })
}

// TestViModeRegressions plays the sessions of S22 on the real editor, where the wanted
// column and the wrapped lines are the editor's own.
func TestViModeRegressions(t *testing.T) {
	const holes = "abcdef ghi\nxy\n\n0123456789 abc\nlast line here"
	tests := []struct {
		name string
		text string
		keys []any // strings are typed, tea.KeyPressMsg are pressed
		want string
	}{
		{"D on an empty line", "abc\n\ndef\nghi", []any{"jD"}, "abc\n\ndef\nghi"},
		{"C on an empty line", "abc\n\ndef\nghi", []any{"jCx", tea.KeyPressMsg{Code: tea.KeyEscape}}, "abc\nx\ndef\nghi"},
		{"d$ on an empty line", "abc\n\ndef\nghi", []any{"jd$"}, "abc\n\ndef\nghi"},
		{"y$ on an empty line yanks nothing", "abc\n\ndef\nghi", []any{"jy$P"}, "abc\n\ndef\nghi"},
		{"visual j keeps the wanted column", holes, []any{"3lvjjjd"}, "abc456789 abc\nlast line here"},
		{"visual j then k", holes, []any{"3lvjkd"}, "abcef ghi\nxy\n\n0123456789 abc\nlast line here"},
		{"block I over short lines", holes, []any{"3l", tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}, "jjjIZ", tea.KeyPressMsg{Code: tea.KeyEscape}},
			"abcZdef ghi\nxy\n\n012Z3456789 abc\nlast line here"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := viSession(t, tt.text)
			for _, k := range tt.keys {
				switch k := k.(type) {
				case string:
					s.typeText(k)
				case tea.KeyPressMsg:
					s.press(k)
				}
				s.settle(20 * time.Millisecond)
			}
			if got := s.text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestViModeVerticalOnAWrappedLine: j and k go by display rows, also on the first and the
// last logical line, so j j k k comes back (S22-4).
func TestViModeVerticalOnAWrappedLine(t *testing.T) {
	s := viSession(t, strings.Repeat("abcdefghij", 33))
	s.typeText("jj")
	s.settle(20 * time.Millisecond)
	down := s.rt.ed.Active().Cursor
	if down.Col == 0 {
		t.Fatalf("jj did not move on a wrapped single line: %v", down)
	}
	s.typeText("kk")
	s.settle(20 * time.Millisecond)
	if got := s.rt.ed.Active().Cursor; got.Line != 0 || got.Col != 0 {
		t.Fatalf("after jjkk the caret is at %v, want 0:0 (was at %v after jj)", got, down)
	}
}

// TestViModeSearchOnALargeFile: the search of the editor works on a large file but b:find
// does not, so vi says that plainly instead of "E486" (which is Vim's "pattern not found")
// and the raw error code.
func TestViModeSearchOnALargeFile(t *testing.T) {
	for _, keys := range []string{"/FIXME", "*", ":g/FIXME/d"} {
		t.Run(keys, func(t *testing.T) {
			isolateConfig(t)
			writeUserFile(t, "config.json", `{"editor": {"large_file_threshold_bytes": 4096},
				"plugins": {"vi_mode": {"enabled": true}}}`)
			s := newSession(t, Options{})
			s.openFile("big.txt", strings.Repeat("some FIXME line\n", 1000))
			if !s.rt.ed.Active().Large() {
				t.Fatal("the document is not large")
			}
			s.waitStatus("vi_mode", "loaded")
			s.until("normal mode", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
			s.typeText(keys)
			if keys[0] != '*' {
				s.enter()
			}
			s.until("the large file message", func() bool { return strings.Contains(s.statusRow(), "large-file") })
			row := s.statusRow()
			if strings.Contains(row, "E486") || strings.Contains(row, "too_large") {
				t.Fatalf("status = %q: the error code leaks", row)
			}
		})
	}
}
