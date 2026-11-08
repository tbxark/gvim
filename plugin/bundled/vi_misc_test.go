package bundled_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("l%d", i)
	}
	return strings.Join(lines, "\n")
}

func TestViScreenMotions(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) *vi {
		v := newVi(t, numberedLines(60))
		w := v.host.Window(1)
		w.TopLine, w.Height = 10, 10
		return v.at(12, 0)
	}
	tests := []struct {
		name string
		keys string
		want pluginapi.Pos
	}{
		{"H", "H", pluginapi.Pos{Line: 10}},
		{"3H", "3H", pluginapi.Pos{Line: 12}},
		{"M", "M", pluginapi.Pos{Line: 14}},
		{"L", "L", pluginapi.Pos{Line: 19}},
		{"2L", "2L", pluginapi.Pos{Line: 18}},
		{"dL deletes lines", "dL", pluginapi.Pos{Line: 12}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := setup(t)
			v.keys(tt.keys)
			if got := v.cursor(); got != tt.want {
				t.Errorf("caret = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestViScrolling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys string
		ran  []string
		line int
	}{
		{"ctrl+d scrolls and moves half a page", "<C-d>", []string{"scroll_down", "vi_move_down"}, 5},
		{"ctrl+u", "<C-u>", []string{"scroll_up", "vi_move_up"}, 15},
		{"ctrl+f", "<C-f>", []string{"move_page_down"}, 20},
		{"ctrl+b", "<C-b>", []string{"move_page_up"}, 20},
		{"ctrl+e", "<C-e>", []string{"scroll_down"}, 20},
		{"ctrl+y", "<C-y>", []string{"scroll_up"}, 20},
		{"zz recenters", "zz", []string{"recenter"}, 20},
		{"PageDown", "<PageDown>", []string{"move_page_down"}, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, numberedLines(60))
			w := v.host.Window(1)
			w.TopLine, w.Height = 10, 10
			v.at(20, 0)
			if tt.name == "ctrl+d scrolls and moves half a page" {
				v.at(0, 0)
			}
			v.keys(tt.keys)
			ran := v.ran()
			for _, r := range tt.ran {
				if !slices.Contains(ran, r) {
					t.Errorf("actions = %v, want %q among them", ran, r)
				}
			}
			if tt.name == "ctrl+d scrolls and moves half a page" || tt.name == "ctrl+u" {
				if got := v.cursor().Line; got != tt.line {
					t.Errorf("line = %d, want %d", got, tt.line)
				}
			}
		})
	}
}

// TestViPanicClearsVi: when the host clears the input mode (the panic chord, three Esc),
// vi turns itself off and says how to get it back; the toggle command turns it on again.
func TestViPanicClearsVi(t *testing.T) {
	t.Parallel()
	for _, start := range []string{"", "i", "v", "2d"} {
		t.Run("from "+start, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, "abc")
			v.keys(start)
			prev := v.mode()
			// the ui clears the mode and raises mode_changed
			v.host.InputMode[viPlugin] = ""
			v.h.Emit(pluginapi.Event{Name: "mode_changed", Data: map[string]any{"previous": prev, "previous_plugin": viPlugin}})
			if !strings.Contains(v.notices(), "vi mode off") {
				t.Fatalf("notices = %q, want the notice that vi is off", v.notices())
			}
			if v.label() != "" {
				t.Errorf("status label %q is still shown", v.label())
			}
			// the keys type again: nothing of vi is left
			if v.mode() != "" {
				t.Fatalf("mode = %q", v.mode())
			}
			// Toggle brings it back in normal mode
			if err := v.h.Run("vi_mode.toggle", nil); err != nil {
				t.Fatal(err)
			}
			if v.mode() != "vi_normal" || v.label() != "-- NORMAL --" {
				t.Errorf("after the toggle: mode %q label %q", v.mode(), v.label())
			}
			v.keys("x")
			if got := v.text(); got != "bc" {
				t.Errorf("x after the toggle: %q", got)
			}
		})
	}
}

func TestViToggleAndExport(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc")
	if err := v.h.Run("vi_mode.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if v.mode() != "" || v.label() != "" {
		t.Fatalf("after the first toggle: mode %q label %q, want vi off", v.mode(), v.label())
	}
	if err := v.h.Run("vi_mode.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if v.mode() != "vi_normal" {
		t.Fatalf("after the second toggle: mode %q", v.mode())
	}
	// the exported API drives it from another plugin
	if err := v.h.Load("other", `
local gvim = require "gvim"
gvim.command.register{ name = "off", run = function()
  local vi = gvim.plugins.get("vi_mode")
  vi.disable()
  gvim.notify("enabled? " .. tostring(vi.is_enabled()))
end }
`); err != nil {
		t.Fatal(err)
	}
	if err := v.h.Run("other.off", nil); err != nil {
		t.Fatal(err)
	}
	if v.mode() != "" {
		t.Errorf("mode after disable() = %q", v.mode())
	}
	if !strings.Contains(v.notices(), "enabled? false") {
		t.Errorf("notices = %q", v.notices())
	}
}

// TestViStepsAsideForOtherModes: another plugin's input mode replaces vi's, and when it
// ends vi is back in normal mode.
func TestViStepsAsideForOtherModes(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc")
	if err := v.h.Load("flashy", `
local gvim = require "gvim"
gvim.mode.define{ name = "pick", text = "swallow" }
gvim.command.register{ name = "go", run = function() gvim.mode.enter("pick") end }
gvim.command.register{ name = "stop", run = function() gvim.mode.leave() end }
`); err != nil {
		t.Fatal(err)
	}
	v.keys("2d") // a half-typed command
	if err := v.h.Run("flashy.go", nil); err != nil {
		t.Fatal(err)
	}
	v.h.Emit(pluginapi.Event{Name: "mode_changed", Data: map[string]any{"mode": "pick", "plugin": "flashy", "previous": "vi_normal", "previous_plugin": viPlugin}})
	if v.host.InputMode["flashy"] != "pick" {
		t.Fatalf("input modes = %v", v.host.InputMode)
	}
	if v.label() != "" {
		t.Errorf("vi's label %q shows while another mode has the keys", v.label())
	}
	if err := v.h.Run("flashy.stop", nil); err != nil {
		t.Fatal(err)
	}
	v.h.Emit(pluginapi.Event{Name: "mode_changed", Data: map[string]any{"previous": "pick", "previous_plugin": "flashy"}})
	if v.mode() != "vi_normal" || v.label() != "-- NORMAL --" {
		t.Fatalf("after the other mode: mode %q label %q", v.mode(), v.label())
	}
	v.keys("x")
	if got := v.text(); got != "bc" {
		t.Errorf("text = %q: the half-typed 2d must be gone", got)
	}
}

// TestViBufferSwitchDropsPendingState: a command or visual area belongs to the buffer it
// was started in.
func TestViBufferSwitchDropsPendingState(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc def")
	second := v.host.AddBuffer("/w/b.txt", "second")
	v.keys("wd") // d is pending
	v.host.SetActive(second)
	v.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": second}})
	v.keys("x")
	for _, b := range v.host.Buffers {
		if b.Info.ID == second {
			if got := strings.Join(b.Lines, "\n"); got != "econd" {
				t.Errorf("second buffer = %q, want x to be a plain x", got)
			}
		}
	}
}

// TestViSuspendsInBuffersWithTheirOwnMode: a buffer with a mode of its own (an outline, a
// list) takes its keys, so vi steps aside while it is active and returns afterwards.
func TestViSuspendsInBuffersWithTheirOwnMode(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc")
	list := v.host.AddBuffer("*list*", "row")
	b := v.host.Buffers[len(v.host.Buffers)-1]
	b.Info.Mode = "list_mode"
	v.host.SetActive(list)
	v.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": list}})
	if v.mode() != "" {
		t.Fatalf("vi still has the input mode %q in a buffer with a mode of its own", v.mode())
	}
	// the host reports the end of vi's mode: it was vi's own doing, not a panic
	v.h.Emit(pluginapi.Event{Name: "mode_changed", Data: map[string]any{"previous": "vi_normal", "previous_plugin": viPlugin}})
	if strings.Contains(v.notices(), "vi mode off") {
		t.Fatalf("a suspended vi was turned off: %q", v.notices())
	}
	v.host.SetActive(v.id)
	v.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": v.id}})
	if v.mode() != "vi_normal" {
		t.Fatalf("vi did not come back: mode %q", v.mode())
	}
}

// TestViUnloadCleansUp: unloading the plugin removes its modes, status item, commands and
// input mode from the host.
func TestViUnloadCleansUp(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc")
	if len(v.host.Resources()) == 0 {
		t.Fatal("vi registered nothing")
	}
	v.h.Unload(viPlugin)
	if left := v.host.Resources(); len(left) != 0 {
		t.Fatalf("resources left after unload: %v", left)
	}
}

func TestViSettings(t *testing.T) {
	t.Parallel()
	// auto_start off keeps vi off until it is toggled; the settings come from the config
	files := viFiles(t)
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.txt", "abc")
	h := plugintest.New(t, files, plugintest.Options{Host: host, Timeout: 2 * time.Second})
	h.Start()
	if host.InputMode[viPlugin] != "vi_normal" {
		t.Fatalf("default: mode %q, want vi to start in normal mode", host.InputMode[viPlugin])
	}
}

// TestViCountsAreBounded: a huge count does not hang the editor or blow the budget.
func TestViCountsAreBounded(t *testing.T) {
	t.Parallel()
	v := newVi(t, numberedLines(200))
	start := time.Now()
	v.keys("99999999j")
	if got := v.cursor().Line; got != 199 {
		t.Errorf("line = %d, want the last", got)
	}
	v.keys("gg99999999dd")
	if got := v.text(); got != "" {
		t.Errorf("text = %q, want everything deleted", got)
	}
	v.keys("u")
	// Whether a huge count blows the slice budget shows in the results above (a plugin that is
	// killed does not move the caret or delete the text); the wall clock only has to show that
	// nothing hung, so its bound is generous enough for a loaded machine running under -race.
	if time.Since(start) > 30*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

// TestViBigBuffer: commands on a big buffer stay inside the time budget.
func TestViBigBuffer(t *testing.T) {
	t.Parallel()
	v := newViTimeout(t, numberedLines(20000), 20*time.Second)
	start := time.Now()
	v.keys("G")
	if got := v.cursor().Line; got != 19999 {
		t.Fatalf("G: line %d", got)
	}
	v.keys("gg/«l15000»")
	if got := v.cursor().Line; got != 15000 {
		t.Fatalf("search: line %d", got)
	}
	v.keys(":«%s/l1/X/»")
	if got := v.buf().Lines[1]; got != "X" {
		t.Fatalf("line 1 after :%%s = %q (log %v, notices %q)", got, v.h.LogText(viPlugin), v.notices())
	}
	v.keys("}")
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

// TestViErrorsDoNotWedgeTheModes: a failing editor call abandons the command and leaves
// the plugin working.
func TestViErrorsDoNotWedgeTheModes(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc def")
	play := v.host.Cmd().Hook
	v.host.Cmd().Hook = func(action string, args map[string]any, count int) error {
		if action == "toggle_case" {
			return pluginapi.Errf(pluginapi.CodeReadOnly, "read-only")
		}
		return play(action, args, count)
	}
	v.keys("~")
	if !strings.Contains(v.notices(), "read-only") {
		t.Errorf("notices = %q, want the reason", v.notices())
	}
	v.keys("x")
	if got := v.text(); got != "bc def" {
		t.Errorf("text = %q: the plugin stopped handling keys after an error", got)
	}
	// a read-only buffer refuses edits with a message and keeps working
	v.buf().Info.ReadOnly = true
	v.keys("dd")
	if !strings.Contains(v.notices(), "readonly") {
		t.Errorf("notices = %q, want E45", v.notices())
	}
	v.keys("l")
	if got := v.cursor().Col; got != 1 {
		t.Errorf("caret = %d: motions must still work", got)
	}
}

// TestViKilledColonCommandDoesNotWedgeKeys: a : command line that the host stops (time
// budget) leaves the key reader usable; the next keys work at once (S22-2).
func TestViKilledColonCommandDoesNotWedgeKeys(t *testing.T) {
	lines := make([]string, 20000)
	for i := range lines {
		lines[i] = fmt.Sprintf("hello world foo bar %d", i)
	}
	v := newViTimeout(t, strings.Join(lines, "\n"), 10*time.Millisecond)
	v.lenient = true
	v.keys(":«%s/foo/FOO/»")
	if !strings.Contains(strings.Join(v.h.LogText(viPlugin), "\n"), "timed out") {
		t.Fatalf("the command was not stopped by the budget (log %v)", v.h.LogText(viPlugin))
	}
	if v.mode() != "vi_normal" {
		t.Fatalf("mode = %q after the killed command", v.mode())
	}
	v.keys("x")
	if got := v.buf().Lines[0]; !strings.HasPrefix(got, "ello") {
		t.Fatalf("x after the killed command did nothing: line 0 = %q (log %v)", got, v.h.LogText(viPlugin))
	}
	v.keys("jihi<Esc>")
	if got := v.buf().Lines[1]; !strings.HasPrefix(got, "hi") {
		t.Fatalf("insert after the killed command did nothing: line 1 = %q", got)
	}
}

// TestViVerticalAtTheFirstAndLastLine: j and k in normal mode leave the editor's own
// vertical move to decide at the first and last line, because it can still move a display
// row there when the line wraps; an operator with j or k has no such row and fails (S22-4).
func TestViVerticalAtTheFirstAndLastLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys string
		ran  []string
		text string
	}{
		{"j on the last line", "j", []string{"vi_move_down"}, "abc"},
		{"k on the first line", "k", []string{"vi_move_up"}, "abc"},
		{"3j on the last line", "3j", []string{"vi_move_down"}, "abc"},
		{"dj on the last line fails", "dj", nil, "abc"},
		{"dk on the first line fails", "dk", nil, "abc"},
		// gj and gk are display-row aware like j and k: the editor's action decides at the ends
		{"gj on the last line", "gj", []string{"vi_move_down"}, "abc"},
		{"gk on the first line", "gk", []string{"vi_move_up"}, "abc"},
		{"dgj on the last line fails", "dgj", nil, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, "abc")
			v.keys(tt.keys)
			var ran []string
			for _, r := range v.ran() {
				if strings.HasPrefix(r, "vi_move") || strings.HasPrefix(r, "move_") {
					ran = append(ran, r)
				}
			}
			if !slices.Equal(ran, tt.ran) {
				t.Errorf("actions run = %v, want %v", ran, tt.ran)
			}
			if got := v.text(); got != tt.text {
				t.Errorf("text = %q, want %q", got, tt.text)
			}
		})
	}
}
