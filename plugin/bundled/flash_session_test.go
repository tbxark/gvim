package bundled_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// ended checks that a session left nothing behind: no decoration, no key wait, a cleared
// status row.
func (e *flashEnv) ended(t *testing.T) {
	t.Helper()
	if n := len(e.h.Host.Decor().Entries); n != 0 {
		t.Errorf("%d decorations left after the session", n)
	}
	if e.h.Host.Keys().Pending != nil {
		t.Error("a key wait is still pending")
	}
	if got := e.status(); got != "" {
		t.Errorf("status = %q, want it cleared", got)
	}
	if res := e.leaked(); len(res) != 0 {
		t.Errorf("resources left: %v", res)
	}
}

// leaked lists the resources of the host other than the registered commands.
func (e *flashEnv) leaked() []string {
	var out []string
	for _, r := range e.h.Host.Resources() {
		if !strings.HasPrefix(r, "command:") {
			out = append(out, r)
		}
	}
	return out
}

func TestFlashSessionEnds(t *testing.T) {
	t.Parallel()
	// Word starts: abc bar baz abz qux. They begin with a, b and q, which are never labels,
	// so those keys type; the labels are s d f g h by distance from the caret.
	const text = "abc bar\nbaz abz\nqux"
	p := func(line, col int) pluginapi.Pos { return pluginapi.Pos{Line: line, Col: col} }
	ch := func(s string) pluginapi.Key { return pluginapi.Key{Key: s, Text: s} }
	tests := []struct {
		name   string
		cursor pluginapi.Pos
		keys   []pluginapi.Key
		fail   string // the key wait ends with this error after the keys
		want   pluginapi.Pos
	}{
		{"a label jumps", p(0, 0), []pluginapi.Key{ch("f")}, "", p(1, 0)},
		{"a typed pattern then a label", p(0, 0), []pluginapi.Key{ch("q"), ch("h")}, "", p(2, 0)},
		{"escape leaves the caret", p(1, 2), []pluginapi.Key{{Key: "escape"}}, "", p(1, 2)},
		{"enter jumps to the nearest match", p(1, 5), []pluginapi.Key{ch("a"), {Key: "enter"}}, "", p(1, 4)},
		{"enter without a match stays", p(1, 5), []pluginapi.Key{ch("y"), {Key: "enter"}}, "", p(1, 5)},
		{"an arrow ends it", p(1, 2), []pluginapi.Key{{Key: "left"}}, "", p(1, 2)},
		{"a control chord ends it", p(1, 2), []pluginapi.Key{{Key: "f", Text: "f", Ctrl: true}}, "", p(1, 2)},
		{"a timeout ends it", p(1, 2), nil, "timeout: no key", p(1, 2)},
		{"the panic chord ends it", p(1, 2), []pluginapi.Key{ch("y")}, "cancelled: cleared", p(1, 2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newFlash(t, text, nil)
			if err := e.h.Host.SetCursor(e.buf, tt.cursor, false, false, false); err != nil {
				t.Fatal(err)
			}
			e.jump(t)
			for _, k := range tt.keys {
				if e.h.Host.Keys().Pending == nil {
					t.Fatalf("the session ended before key %+v", k)
				}
				e.h.PressKey(k)
			}
			if tt.fail != "" {
				e.h.FailKeyWait(tt.fail)
			}
			if got := e.cursor(); got != tt.want {
				t.Errorf("cursor = %+v, want %+v", got, tt.want)
			}
			e.ended(t)
			for _, l := range e.h.LogText("flash") {
				if strings.HasPrefix(l, "warn") || strings.HasPrefix(l, "error") {
					t.Errorf("log entry %q", l)
				}
			}
		})
	}
}

func TestFlashStatusShowsThePattern(t *testing.T) {
	t.Parallel()
	e := newFlash(t, "foo bar", nil)
	e.jump(t)
	if got := e.status(); got != "Flash[]" {
		t.Fatalf("status = %q", got)
	}
	e.press("b")
	e.press("a")
	if got := e.status(); got != "Flash[ba]" {
		t.Fatalf("status = %q", got)
	}
	e.press("backspace")
	e.press("backspace")
	e.press("backspace") // nothing left to remove
	if got := e.status(); got != "Flash[]" {
		t.Fatalf("status = %q", got)
	}
}

func TestFlashIgnoresASecondStart(t *testing.T) {
	t.Parallel()
	e := newFlash(t, "foo bar", nil)
	e.jump(t)
	if err := e.h.Run("flash.jump", nil); err != nil {
		t.Fatal(err)
	}
	if n := e.h.Host.CallCount("WaitKey"); n != 1 {
		t.Fatalf("WaitKey calls = %d, want the running session only", n)
	}
	e.press("escape")
	e.ended(t)
	e.jump(t) // a finished session can be started again
	e.press("escape")
	e.ended(t)
}

func TestFlashUnloadMidSessionCleansUp(t *testing.T) {
	t.Parallel()
	e := newFlash(t, "foo bar", nil)
	e.jump(t)
	if len(e.h.Host.Decor().Entries) == 0 {
		t.Fatal("no labels were drawn")
	}
	e.h.Unload("flash")
	if n := len(e.h.Host.Decor().Entries); n != 0 {
		t.Errorf("%d decorations left after the unload", n)
	}
	if e.h.Host.Keys().Pending != nil {
		t.Error("the key wait survived the unload")
	}
	if res := e.leaked(); len(res) != 0 {
		t.Errorf("resources left: %v", res)
	}
}

// TestFlashUsesTheRowsOnScreen: only text in the painted rows is matched, a soft-wrapped
// line is matched row by row (in byte columns), and a fold leaves its lines out.
func TestFlashUsesTheRowsOnScreen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		rows    []pluginapi.VisibleRow
		keys    []string
		matches []string
	}{
		{
			name:    "lines off screen are not matched",
			text:    "ab\nab\nab\nab",
			rows:    []pluginapi.VisibleRow{{Line: 1, ColTo: 2}, {Line: 2, ColTo: 2}},
			keys:    []string{"a"},
			matches: []string{"1:0-1", "2:0-1"},
		},
		{
			name:    "a wrapped line is matched per row",
			text:    "abab abab",
			rows:    []pluginapi.VisibleRow{{Line: 0, ColFrom: 0, ColTo: 5}, {Line: 0, ColFrom: 5, ColTo: 9, Wrapped: true}},
			keys:    []string{"b", "a"},
			matches: []string{"0:1-3", "0:6-8"},
		},
		{
			name:    "a match that crosses the wrap is not found",
			text:    "xcdy",
			rows:    []pluginapi.VisibleRow{{Line: 0, ColFrom: 0, ColTo: 2}, {Line: 0, ColFrom: 2, ColTo: 4, Wrapped: true}},
			keys:    []string{"c", "d"},
			matches: nil,
		},
		{
			name:    "a fold leaves lines out",
			text:    "ab\nab\nab\nab",
			rows:    []pluginapi.VisibleRow{{Line: 0, ColTo: 2}, {Line: 3, ColTo: 2}},
			keys:    []string{"b"},
			matches: []string{"0:1-2", "3:1-2"},
		},
		{
			name:    "a continuation row does not start a word in the middle of one",
			text:    "abcdef gh",
			rows:    []pluginapi.VisibleRow{{Line: 0, ColFrom: 0, ColTo: 3}, {Line: 0, ColFrom: 3, ColTo: 9, Wrapped: true}},
			matches: []string{"0:0-1", "0:7-8"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newFlash(t, tt.text, nil)
			e.h.Host.SetVisible(1, tt.rows)
			e.jump(t)
			for _, k := range tt.keys {
				e.press(k)
			}
			if got := e.matches(e.buf); !slices.Equal(got, tt.matches) {
				t.Errorf("matches = %v, want %v", got, tt.matches)
			}
		})
	}
}

// TestFlashAcrossWindows: matches in every window are labelled, the focused window's first;
// a label in another window focuses it and moves the caret there; a buffer shown twice is
// labelled once.
func TestFlashAcrossWindows(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*flashEnv, pluginapi.BufferID, pluginapi.WinID) {
		e := newFlash(t, "one two", nil)
		other := e.h.Host.AddBuffer("/w/b.txt", "red green")
		e.h.Host.SetActive(e.buf)
		win, err := e.h.Host.SplitWindow(1, pluginapi.SplitOptions{Dir: "right", Buffer: other})
		if err != nil {
			t.Fatal(err)
		}
		return e, other, win
	}
	t.Run("labels in both windows, the focused one first", func(t *testing.T) {
		t.Parallel()
		e, other, _ := setup(t)
		e.jump(t)
		// Skip letters: o, t (own) and r, g (other).
		if got, want := e.labels(e.buf), []string{"0:1=a", "0:5=s"}; !slices.Equal(got, want) {
			t.Errorf("labels of the focused window = %v, want %v", got, want)
		}
		if got, want := e.labels(other), []string{"0:1=d", "0:5=f"}; !slices.Equal(got, want) {
			t.Errorf("labels of the other window = %v, want %v", got, want)
		}
	})
	t.Run("a label in the other window focuses it and jumps", func(t *testing.T) {
		t.Parallel()
		e, other, win := setup(t)
		e.jump(t)
		e.press("f") // "green"
		cs, _ := e.h.Host.Cursors(other)
		if cs[0].Pos != (pluginapi.Pos{Line: 0, Col: 4}) {
			t.Errorf("cursor in the other buffer = %+v", cs[0].Pos)
		}
		if got := e.h.Host.CurrentWindow(); got != win {
			t.Errorf("focused window = %d, want %d", got, win)
		}
		e.ended(t)
	})
	t.Run("a label in the focused window does not move the focus", func(t *testing.T) {
		t.Parallel()
		e, _, _ := setup(t)
		e.jump(t)
		e.press("s") // "two"
		if got := e.cursor(); got != (pluginapi.Pos{Col: 4}) {
			t.Errorf("cursor = %+v", got)
		}
		if n := e.h.Host.CallCount("FocusWindow"); n != 0 {
			t.Errorf("FocusWindow calls = %d, want 0", n)
		}
	})
	t.Run("a buffer in two windows is labelled once", func(t *testing.T) {
		t.Parallel()
		e := newFlash(t, "one two", nil)
		if _, err := e.h.Host.SplitWindow(1, pluginapi.SplitOptions{Dir: "below"}); err != nil {
			t.Fatal(err)
		}
		e.jump(t)
		if got, want := e.labels(e.buf), []string{"0:1=a", "0:5=s"}; !slices.Equal(got, want) {
			t.Errorf("labels = %v, want %v", got, want)
		}
	})
}

func TestFlashCapsHighlights(t *testing.T) {
	t.Parallel()
	e := newFlash(t, strings.Repeat("a", 6000), nil)
	e.h.Host.SetVisible(1, []pluginapi.VisibleRow{{Line: 0, ColTo: 6000}})
	e.jump(t)
	e.press("a")
	if n := len(e.h.Host.Decor().Items("flash", "match", e.buf)); n != 2000 {
		t.Errorf("highlights = %d, want the cap of 2000", n)
	}
	// The pool has 26 letters; "a" is the character after every match, so it is not a label.
	if n := len(e.h.Host.Decor().Items("flash", "label", e.buf)); n != 25 {
		t.Errorf("labels = %d, want the 25 usable letters of the pool", n)
	}
}

// newFlashRealClock is flash with the real clock and a 100 ms slice budget, so that a slow
// host call is killed the way it would be in the editor.
func newFlashRealClock(t *testing.T, text string) *flashEnv {
	t.Helper()
	src, err := fs.ReadFile(bundled.FS, "plugins/flash/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	host := plugintest.NewFakeHost()
	id := host.AddBuffer("/w/a.txt", text)
	h := plugintest.New(t, plugintest.Files{"flash/init.lua": string(src)},
		plugintest.Options{Host: host, Timeout: 100 * time.Millisecond, Now: time.Now})
	h.Start()
	return &flashEnv{h: h, buf: id}
}

// killedSession starts a session and kills it with a slow host call on the next key. The
// slice runs out before the plugin can clean up after itself.
func killedSession(t *testing.T) *flashEnv {
	t.Helper()
	e := newFlashRealClock(t, "foo bar baz")
	e.jump(t)
	e.h.Host.LinesDelay = 300 * time.Millisecond
	e.press("b")
	e.h.Host.LinesDelay = 0
	if !slices.ContainsFunc(e.h.LogText("flash"), func(l string) bool { return strings.Contains(l, "timed out") || strings.Contains(l, "slow host call") }) {
		t.Fatalf("the session was not killed; log: %v", e.h.LogText("flash"))
	}
	return e
}

// TestFlashRecoversFromAKilledSession: a session killed by the slice budget cannot clear
// its decorations or its running flag; the next Flash: Jump takes over.
func TestFlashRecoversFromAKilledSession(t *testing.T) {
	t.Parallel()
	e := killedSession(t)
	e.jump(t) // not refused as a second start
	e.press("escape")
	e.ended(t)
	e.jump(t)
	e.press("d") // labels: foo a, bar s, baz d
	if got := e.cursor(); got != (pluginapi.Pos{Line: 0, Col: 8}) {
		t.Errorf("cursor = %+v, want 0:8", got)
	}
	e.ended(t)
}

// TestFlashWatchdogCleansAKilledSession: without another Flash: Jump the leftovers of a
// killed session (labels on screen, the Flash[...] status) go away on their own.
func TestFlashWatchdogCleansAKilledSession(t *testing.T) {
	t.Parallel()
	e := killedSession(t)
	if len(e.h.Host.Decor().Entries) == 0 {
		t.Fatal("expected the killed session to leave decorations behind")
	}
	e.h.Advance(2 * time.Second)
	e.ended(t)
}
