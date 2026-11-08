package bundled_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// A command of Vim that vi_mode does not have says so, and the keys that belong to it do not run
// as commands of their own: "cgnX<Esc>" must not delete a character with X, "qa" must not start an
// insert with a.
func TestViUnsupportedCommandsSayWhatTheyAre(t *testing.T) {
	t.Parallel()
	const text = "foo bar foo baz\nsecond line\n"
	tests := []struct {
		name   string
		keys   string
		at     [2]int
		notice string // a part of the message
		cursor [2]int
	}{
		{"cgn takes its text with it", "/«foo»cgnX<Esc>", [2]int{0, 0}, "not supported: gn (selecting the next match)", [2]int{0, 8}},
		{"R takes its text with it", "Rxy<Esc>", [2]int{0, 4}, "not supported: R (replace mode)", [2]int{0, 4}},
		{"gq takes its motion", "gqip", [2]int{0, 4}, "not supported: gq (formatting)", [2]int{0, 4}},
		{"gqq", "gqq", [2]int{0, 4}, "not supported: gq (formatting)", [2]int{0, 4}},
		{"= takes its motion", "=j", [2]int{0, 4}, "not supported: = (re-indenting)", [2]int{0, 4}},
		{"== is the doubled form", "==", [2]int{0, 4}, "not supported: = (re-indenting)", [2]int{0, 4}},
		{"! takes its motion", "!j", [2]int{0, 4}, "not supported: ! (filtering through a program)", [2]int{0, 4}},
		{"g; is one command", "g;", [2]int{0, 4}, "not supported: g; (the change list)", [2]int{0, 4}},
		{"@: takes its register", "@:", [2]int{0, 4}, "not supported: @ (running a macro): use the editor's", [2]int{0, 4}},
		{"qa takes its register", "qa", [2]int{0, 4}, "not supported: q (recording a macro)", [2]int{0, 4}},
		{"Tab is the jump list", "<Tab>", [2]int{0, 4}, "not supported: Tab (the jump list)", [2]int{0, 4}},
		{"zf is a fold", "zfj", [2]int{0, 4}, "not supported: zf (creating a fold): vi_mode has no folds", [2]int{0, 4}},
		{"U", "U", [2]int{0, 4}, "not supported: U (undoing the changes of a line)", [2]int{0, 4}},
		{"a key that is no command", "\\", [2]int{0, 4}, "not a vi command: \\", [2]int{0, 4}},
		{"g and an unknown key", "gZ", [2]int{0, 4}, "not a vi command: gZ", [2]int{0, 4}},
		{"an unknown object", "diZ", [2]int{0, 4}, "not a text object: iZ", [2]int{0, 4}},
		{"an unknown motion", "dZ", [2]int{0, 4}, "not a motion: Z", [2]int{0, 4}},
		{"a mark vi does not keep", "'.", [2]int{0, 4}, "not supported: the mark .", [2]int{0, 4}},
		{"a mark that is not set", "'z", [2]int{0, 4}, "E20: Mark not set", [2]int{0, 4}},
		{"a bad mark name", "m1", [2]int{0, 4}, "E191: Argument must be a letter", [2]int{0, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, text).at(tt.at[0], tt.at[1])
			v.keys(tt.keys)
			if got := v.text(); got != text {
				t.Errorf("text = %q: a key of the unsupported command ran as a command", got)
			}
			if got := v.notices(); !strings.Contains(got, tt.notice) {
				t.Errorf("notices = %q, want one with %q", got, tt.notice)
			}
			if got := v.mode(); got != "vi_normal" {
				t.Errorf("mode = %q, want vi_normal", got)
			}
			if o := v.host.UI().Last("input"); o != nil && o.Open {
				t.Error("a prompt is open: a key of the unsupported command opened it")
			}
			if got, want := v.cursor(), (struct{ Line, Col int }{tt.cursor[0], tt.cursor[1]}); got.Line != want.Line || got.Col != want.Col {
				t.Errorf("caret = %+v, want %+v", got, want)
			}
			// vi is in working order afterwards
			v.keys("0x")
			if !strings.HasPrefix(v.text(), "oo bar") {
				t.Errorf("x after it: text = %q", v.text())
			}
		})
	}
}

// R takes everything up to Esc with it, and the status row shows that vi is waiting for it.
func TestViReplaceModeKeysAreIgnoredUntilEsc(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc\n")
	v.keys("R")
	for _, k := range []string{"x", "d", "d", "j", "<CR>", "x"} {
		v.keys(k)
		if got := v.text(); got != "abc\n" {
			t.Fatalf("after %q the text is %q", k, got)
		}
		if got := v.label(); !strings.Contains(got, "R") {
			t.Fatalf("after %q the label is %q, want the pending R", k, got)
		}
	}
	v.keys("<Esc>x")
	if got := v.text(); got != "bc\n" {
		t.Fatalf("x after Esc: text = %q", got)
	}
}

// An operator that is not a change does not take what is typed next with it: dgn is one command.
func TestViOperatorWithAnUnsupportedMotionDoesNotSwallow(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc\n")
	v.keys("dgn")
	if !strings.Contains(v.notices(), "not supported: gn (selecting the next match)") {
		t.Fatalf("notices = %q", v.notices())
	}
	v.keys("x")
	if got := v.text(); got != "bc\n" {
		t.Fatalf("x after dgn: text = %q, want it to run", got)
	}
}

// In the visual mode an unsupported command says so and leaves the selection as it is.
func TestViUnsupportedCommandsInVisualMode(t *testing.T) {
	t.Parallel()
	const text = "foo bar\n"
	for _, keys := range []string{"gq", "=", "gn", "K", "<Tab>"} {
		t.Run(keys, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, text)
			v.keys("vl" + keys)
			if got := v.mode(); got != "vi_visual" {
				t.Errorf("mode = %q, want the visual mode to stay", got)
			}
			if got := v.notices(); !strings.Contains(got, "not supported:") {
				t.Errorf("notices = %q", got)
			}
			if got := v.text(); got != text {
				t.Errorf("text = %q", got)
			}
			if _, ok := v.selection(); !ok {
				t.Error("the selection is gone")
			}
		})
	}
}

// Commands of Vim that vi_mode does not have must say so, and the keys that belong to them
// must not run as commands of their own.
func TestViUnsupportedCommandsSayWhy(t *testing.T) {
	t.Parallel()
	const text = "foo bar foo baz\nsecond line\n"
	tests := []struct {
		name   string
		keys   string
		notice string
		cursor pluginapi.Pos
	}{
		{"cgn is not supported and X is not a command", "/«foo»cgnX<Esc>", "not supported: gn", pluginapi.Pos{Line: 0, Col: 8}},
		{"dgn", "dgn", "not supported: gn", pluginapi.Pos{}},
		{"gn", "gn", "not supported: gn", pluginapi.Pos{}},
		{"R swallows the typed text until Esc", "Rxy<Esc>", "not supported: R", pluginapi.Pos{}},
		{"gq swallows its motion", "gqip", "not supported: gq", pluginapi.Pos{}},
		{"gqq", "gqq", "not supported: gq", pluginapi.Pos{}},
		{"= swallows its motion", "=ip", "not supported: =", pluginapi.Pos{}},
		{"g;", "g;", "not supported: g;", pluginapi.Pos{}},
		{"@: swallows the register", "@:", "not supported: @", pluginapi.Pos{}},
		{"q swallows the register", "qa", "not supported: q", pluginapi.Pos{}},
		{"gw", "gwip", "not supported: gw", pluginapi.Pos{}},
		{"mark '.", "'.", "not supported: the mark .", pluginapi.Pos{}},
		{"mark `[", "`[", "not supported: the mark [", pluginapi.Pos{}},
		{"set mark ]", "m]", "E191", pluginapi.Pos{}}, // Vim 9.1 gives E191 for a character that is no mark
		{"unset mark a", "'a", "E20: Mark not set", pluginapi.Pos{}},
		{"zo", "zo", "not supported: zo", pluginapi.Pos{}},
		{"visual gq", "vgq", "not supported: gq", pluginapi.Pos{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, text)
			v.keys(tt.keys)
			if got := v.notices(); !strings.Contains(got, tt.notice) {
				t.Errorf("notices = %q, want %q", got, tt.notice)
			}
			if got := v.text(); got != text {
				t.Errorf("text = %q, want it unchanged", got)
			}
			if !v.noPrompt() {
				t.Errorf("a prompt is open")
			}
			if got := v.cursor(); got != tt.cursor && tt.name != "visual gq" {
				t.Errorf("caret = %+v, want %+v: the keys of the command ran", got, tt.cursor)
			}
		})
	}
}

// After an unsupported command the next command works as usual.
func TestViUnsupportedCommandIsOver(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc\ndef\n")
	v.keys("gqipx")
	if got := v.text(); got != "bc\ndef\n" {
		t.Errorf("text = %q, want x to delete after the swallowed gqip", got)
	}
	v = newVi(t, "abc\ndef\n")
	v.keys("Rxy<Esc>x")
	if got := v.text(); got != "bc\ndef\n" {
		t.Errorf("text = %q, want x to work after Esc ends the swallowed R", got)
	}
}

func (v *vi) noPrompt() bool {
	o := v.host.UI().Last("input")
	return o == nil || !o.Open
}

// ctrl+c is an Esc in every vi mode.
func TestViCtrlCActsAsEscape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys string
		want string
		mode string
	}{
		{"insert", "ix<C-c>", "xabc def", "vi_normal"},
		{"visual", "vl<C-c>", "abc def", "vi_normal"},
		{"pending operator", "d<C-c>x", "bc def", "vi_normal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, "abc def")
			v.keys(tt.keys)
			if got := v.text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
			if got := v.mode(); got != tt.mode {
				t.Errorf("mode = %q, want %q", got, tt.mode)
			}
			if _, ok := v.selection(); ok {
				t.Errorf("a selection is left")
			}
		})
	}
}
