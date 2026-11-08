package bundled_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// matches are the highlighted ranges of the search highlight, as "line:col".
func (v *vi) matches() []string {
	v.t.Helper()
	var out []string
	for _, it := range v.host.Decor().Items(viPlugin, "search", v.id) {
		if it.Kind != pluginapi.DecorHighlight {
			continue
		}
		out = append(out, itoa(it.Range.From.Line)+":"+itoa(it.Range.From.Col))
	}
	return out
}

const searchText = "one two one\nthree one\nfour\n"

// A search paints every match in the window, the same ones the search finds; :noh hides them
// until the next search, and :set nohls turns the feature off.
func TestViSearchHighlight(t *testing.T) {
	t.Parallel()
	v := newVi(t, searchText)
	if got := v.matches(); len(got) != 0 {
		t.Fatalf("highlights before any search: %v", got)
	}
	v.keys("/«one»")
	if got, want := v.matches(), []string{"0:0", "0:8", "1:6"}; !slices.Equal(got, want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}
	v.keys(":«noh»")
	if got := v.matches(); len(got) != 0 {
		t.Fatalf("matches after :noh = %v", got)
	}
	v.keys("n")
	if got, want := v.matches(), []string{"0:0", "0:8", "1:6"}; !slices.Equal(got, want) {
		t.Fatalf("matches after n = %v, want them back", got)
	}
	v.keys(":«set nohls»")
	if got := v.matches(); len(got) != 0 {
		t.Fatalf("matches after :set nohls = %v", got)
	}
	v.keys("n")
	if got := v.matches(); len(got) != 0 {
		t.Fatalf("a search painted with hlsearch off: %v", got)
	}
	v.keys(":«set hls»")
	if got, want := v.matches(), []string{"0:0", "0:8", "1:6"}; !slices.Equal(got, want) {
		t.Fatalf("matches after :set hls = %v, want %v", got, want)
	}
	// * and :s search too
	v.keys("gg*")
	if got := v.matches(); len(got) != 0 && len(got) != 3 {
		t.Fatalf("matches after * = %v", got)
	}
	v.keys(":«s/two/2/»")
	if got, want := v.matches(), []string{"0:4"}; !slices.Equal(got, want) {
		t.Fatalf("matches after :s = %v, want %v", got, want)
	}
}

// The highlight follows the window: it is painted again when the window scrolls.
func TestViSearchHighlightFollowsTheWindow(t *testing.T) {
	t.Parallel()
	v := newVi(t, searchText)
	v.keys("/«one»")
	n := len(v.host.Decor().Entries)
	v.h.Emit(pluginapi.Event{Name: "viewport_changed", Data: map[string]any{"buf": v.id, "top_line": 0, "height": 10, "width": 80}})
	v.h.Advance(200 * 1e6) // past the debounce
	if got := len(v.host.Decor().Entries); got != n {
		t.Fatalf("entries = %d after a scroll, want the same %d painted again", got, n)
	}
}

// A search that wraps says so, as Vim does.
func TestViSearchWrapMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		at   [2]int
		keys string
		want string
	}{
		{"forward past the last match", [2]int{1, 6}, "/«one»", "search hit BOTTOM, continuing at TOP"},
		{"n past the last match", [2]int{1, 6}, "/«one»<Esc>n", "search hit BOTTOM, continuing at TOP"},
		{"backward past the first match", [2]int{0, 0}, "?«one»", "search hit TOP, continuing at BOTTOM"},
		{"N past the first match", [2]int{0, 0}, "/«one»NN", "search hit TOP, continuing at BOTTOM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, searchText).at(tt.at[0], tt.at[1])
			v.keys(tt.keys)
			if got := v.notices(); !strings.Contains(got, tt.want) {
				t.Errorf("notices = %q, want %q", got, tt.want)
			}
		})
	}
	// no wrap, no message
	v := newVi(t, searchText)
	v.keys("/«three»")
	if got := v.notices(); strings.Contains(got, "search hit") {
		t.Errorf("notices = %q, a search that did not wrap said it did", got)
	}
}

// TestViSetOptionsThatWork: :set tells what works: hls, et, ts and sw change what vi does, the options the editor decides
// are refused, and an unknown one is an error.
func TestViSetOptionsThatWork(t *testing.T) {
	t.Parallel()
	t.Run("sw and et change what > does", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«set sw=3 et»>>")
		if got := v.text(); got != "   a\n" {
			t.Fatalf("text = %q, want three spaces", got)
		}
	})
	t.Run("noet makes tabs", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«set noet sw=8 ts=8»>>")
		if got := v.text(); got != "\ta\n" {
			t.Fatalf("text = %q, want a tab", got)
		}
	})
	t.Run("ts is the buffer's tab size", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«set noet ts=2 sw=4»>>")
		if got := v.text(); got != "\t\ta\n" {
			t.Fatalf("text = %q, want two tabs of 2 columns", got)
		}
	})
	t.Run("options that the editor decides are refused", func(t *testing.T) {
		t.Parallel()
		for _, opt := range []string{"ai", "noai", "is", "incsearch"} {
			v := newVi(t, "a\n")
			v.keys(":«set " + opt + "»")
			if got := v.notices(); !strings.Contains(got, "is not supported") || !strings.Contains(got, opt) {
				t.Errorf(":set %s: notices = %q", opt, got)
			}
		}
	})
	t.Run("unknown", func(t *testing.T) {
		t.Parallel()
		v := newVi(t, "a\n")
		v.keys(":«set bogus»")
		if got := v.notices(); !strings.Contains(got, "E518: Unknown option: bogus") {
			t.Errorf("notices = %q", got)
		}
	})
}

// :help lists the commands in a popup, not in one cut-off status line.
func TestViHelpIsAList(t *testing.T) {
	t.Parallel()
	v := newVi(t, "a\n")
	v.keys(":«help»")
	pop := v.host.UI().Last("popup")
	if pop == nil || !pop.Open || len(pop.Popup.Lines) < 5 {
		t.Fatalf("popup = %+v", pop)
	}
	all := strings.Join(pop.Popup.Lines, "\n")
	for _, want := range []string{":w", ":s/pat/rep/", ":g/pat/cmd", "hls", ":noh"} {
		if !strings.Contains(all, want) {
			t.Errorf("help lacks %q:\n%s", want, all)
		}
	}
}
