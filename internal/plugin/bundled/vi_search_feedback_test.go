package bundled_test

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// searchMarks are the ranges of the search highlight on the vi buffer.
func (v *vi) searchMarks() []pluginapi.Range {
	var out []pluginapi.Range
	for _, d := range v.host.Decor().Items(viPlugin, "search", v.id) {
		out = append(out, d.Range)
	}
	return out
}

func TestViCommandLineHistory(t *testing.T) {
	t.Parallel()
	v := newVi(t, "one one\ntwo\n")
	v.keys(":«s/one/X/»")
	v.keys(":«s/two/Y/»")
	v.keys(":«s/two/Y/»") // a repeated line is remembered once, as the latest
	v.keys(":")
	if got := v.host.UI().Last("input").Input.History; !slices.Equal(got, []string{"s/one/X/", "s/two/Y/"}) {
		t.Errorf(": history = %q", got)
	}
	v.keys("<in!>")
	v.keys("/«two»")
	v.keys("/")
	if got := v.host.UI().Last("input").Input.History; !slices.Equal(got, []string{"two"}) {
		t.Errorf("/ history = %q, want only the searches", got)
	}
	v.keys("<in!>")
}

func TestViSearchHighlightRows(t *testing.T) {
	t.Parallel()
	v := newVi(t, "one two one\nthree one\n")
	v.keys("/«one»")
	want := []pluginapi.Range{
		{From: pluginapi.Pos{Line: 0, Col: 0}, To: pluginapi.Pos{Line: 0, Col: 3}},
		{From: pluginapi.Pos{Line: 0, Col: 8}, To: pluginapi.Pos{Line: 0, Col: 11}},
		{From: pluginapi.Pos{Line: 1, Col: 6}, To: pluginapi.Pos{Line: 1, Col: 9}},
	}
	if got := v.searchMarks(); !slices.Equal(got, want) {
		t.Errorf("marks after / = %v, want %v", got, want)
	}
	v.keys(":«noh»")
	if got := v.searchMarks(); len(got) != 0 {
		t.Errorf("marks after :noh = %v", got)
	}
	v.keys("n")
	if got := v.searchMarks(); len(got) != 3 {
		t.Errorf("n shows the marks again, got %v", got)
	}
	v.keys(":«set nohls»")
	if got := v.searchMarks(); len(got) != 0 {
		t.Errorf("marks after :set nohls = %v", got)
	}
	v.keys("n")
	if got := v.searchMarks(); len(got) != 0 {
		t.Errorf("nohls: n must not mark, got %v", got)
	}
	v.keys(":«set hls»")
	if got := v.searchMarks(); len(got) != 3 {
		t.Errorf("marks after :set hls = %v", got)
	}
}

// The marks go when vi turns off or steps aside, and follow the active buffer.
func TestViSearchHighlightFollowsViState(t *testing.T) {
	t.Parallel()
	t.Run("toggling vi off clears them", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "one two one\n")
		v.keys("/«one»")
		if len(v.searchMarks()) == 0 {
			t.Fatal("no marks after /")
		}
		if err := v.h.Run("vi_mode.toggle", nil); err != nil {
			t.Fatal(err)
		}
		if got := v.searchMarks(); len(got) != 0 {
			t.Errorf("marks after vi was turned off = %v", got)
		}
	})
	t.Run("a panic that turns vi off clears them", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "one two one\n")
		v.keys("/«one»")
		v.host.InputMode[viPlugin] = ""
		v.h.Emit(pluginapi.Event{Name: "mode_changed", Data: map[string]any{"previous": "vi_normal", "previous_plugin": viPlugin}})
		if got := v.searchMarks(); len(got) != 0 {
			t.Errorf("marks after the panic = %v", got)
		}
	})
	t.Run("a buffer with its own mode clears them, and coming back marks again", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "one two one\n")
		v.keys("/«one»")
		list := v.host.AddBuffer("*list*", "row")
		v.host.Buffers[len(v.host.Buffers)-1].Info.Mode = "list_mode"
		v.host.SetActive(list)
		v.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": list}})
		if got := v.searchMarks(); len(got) != 0 {
			t.Errorf("marks while suspended = %v", got)
		}
		v.host.SetActive(v.id)
		v.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": v.id}})
		if got := v.searchMarks(); len(got) != 2 {
			t.Errorf("marks after coming back = %v", got)
		}
	})
	t.Run("the marks move to the active buffer", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "one two one\n")
		v.keys("/«one»")
		second := v.host.AddBuffer("/w/b.txt", "x one")
		v.host.SetActive(second)
		v.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": second}})
		if got := v.searchMarks(); len(got) != 0 {
			t.Errorf("marks stayed on the buffer that was left: %v", got)
		}
		if got := v.host.Decor().Items(viPlugin, "search", second); len(got) != 1 {
			t.Errorf("marks on the second buffer = %v", got)
		}
	})
}

func TestViSearchWrapMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys string
		at   [2]int
		want string
	}{
		{"forward past the last match", "n", [2]int{1, 6}, "search hit BOTTOM, continuing at TOP"},
		{"backward past the first match", "N", [2]int{0, 0}, "search hit TOP, continuing at BOTTOM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, "one two\nthree one\n")
			v.keys("/«one»")
			v.at(tt.at[0], tt.at[1])
			v.keys(tt.keys)
			if got := v.notices(); !strings.Contains(got, tt.want) {
				t.Errorf("notices = %q, want %q", got, tt.want)
			}
		})
	}
	v := newVi(t, "one two\nthree one\n")
	v.keys("/«one»")
	if got := v.notices(); strings.Contains(got, "continuing") {
		t.Errorf("a search that did not wrap said %q", got)
	}
}

func TestViSetOptionsAreReal(t *testing.T) {
	t.Parallel()
	t.Run("noet makes > indent with tabs", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«set noet»")
		v.keys(">>")
		if got := v.text(); !strings.HasPrefix(got, "\ta") {
			t.Errorf("text = %q, want a tab", got)
		}
	})
	t.Run("et makes > indent with spaces", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«set noet»")
		v.keys(":«set et sw=2»")
		v.keys(">>")
		if got := v.text(); !strings.HasPrefix(got, "  a") {
			t.Errorf("text = %q, want two spaces", got)
		}
	})
	t.Run("ts sets the indent width and the tab width", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\nb\n")
		v.keys(":«set ts=8»")
		v.keys(">>")
		if got := v.text(); !strings.HasPrefix(got, "        a") {
			t.Errorf("text = %q, want 8 spaces (sw=0 follows ts)", got)
		}
		v.keys(":«set noet»")
		v.keys("j>>")
		if got := v.text(); !strings.Contains(got, "\n\tb") {
			t.Errorf("text = %q, want one tab, which is 8 wide at ts=8", got)
		}
	})
	t.Run("an option that does nothing is rejected", func(t *testing.T) {
		t.Parallel()
		for _, opt := range []string{"ai", "incsearch", "autoindent", "is"} {
			v := newVi(t, "a\n")
			v.keys(":«set " + opt + "»")
			if got := v.notices(); !strings.Contains(got, "not supported") {
				t.Errorf(":set %s notices = %q, want not supported", opt, got)
			}
		}
	})
}

func TestViPagerCommands(t *testing.T) {
	t.Parallel()
	t.Run("reg lists every register on its own line", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "one two\nthree\n")
		v.keys("yyw\"ayw:«reg»")
		o := v.host.UI().Last("popup")
		if o == nil || !o.Open {
			t.Fatalf("no popup; notices %q", v.notices())
		}
		text := strings.Join(o.Popup.Lines, "\n")
		for _, want := range []string{`"a`, `"0`, "one two^J", "two"} {
			if !strings.Contains(text, want) {
				t.Errorf("popup %q lacks %q", text, want)
			}
		}
		if len(o.Popup.Lines) < 3 {
			t.Errorf("lines = %q, want a line per register", o.Popup.Lines)
		}
	})
	t.Run("help lists the commands on several lines", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«help»")
		o := v.host.UI().Last("popup")
		if o == nil || !o.Open || len(o.Popup.Lines) < 5 {
			t.Fatalf("popup = %+v", o)
		}
		if !strings.Contains(strings.Join(o.Popup.Lines, "\n"), ":s/pat/rep/") {
			t.Errorf("help lines = %q", o.Popup.Lines)
		}
	})
	t.Run("long rows are wrapped, not cut", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«help»")
		o := v.host.UI().Last("popup")
		if o == nil || !o.Open {
			t.Fatalf("popup = %+v", o)
		}
		for _, l := range o.Popup.Lines {
			if n := utf8.RuneCountInString(l); n > 50 {
				t.Errorf("row of %d cells: %q", n, l)
			}
		}
		text := strings.Join(o.Popup.Lines, "\n")
		for _, want := range []string{":saveas", ":vnew", "[no]scs", "gn g;", "not supported"} {
			if !strings.Contains(text, want) {
				t.Errorf("wrapping lost %q: %q", want, o.Popup.Lines)
			}
		}
	})
}
