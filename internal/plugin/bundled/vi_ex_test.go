package bundled_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestViSearch(t *testing.T) {
	t.Parallel()
	const text = "foo bar foo baz foo"
	runViCases(t, []viCase{
		{"/ finds the next match", text, [2]int{0, 0}, "/«foo»", text, [2]int{0, 8}, ""},
		{"n repeats", text, [2]int{0, 0}, "/«foo»n", text, [2]int{0, 16}, ""},
		{"n wraps around", text, [2]int{0, 0}, "/«foo»nn", text, [2]int{0, 0}, ""},
		{"N goes back", text, [2]int{0, 0}, "/«foo»nN", text, [2]int{0, 8}, ""},
		{"? searches backwards", text, [2]int{0, 16}, "?«foo»", text, [2]int{0, 8}, ""},
		{"? then n goes on backwards", text, [2]int{0, 16}, "?«foo»n", text, [2]int{0, 0}, ""},
		{"* searches the word under the caret", text, [2]int{0, 1}, "*", text, [2]int{0, 8}, ""},
		{"# searches backwards", text, [2]int{0, 8}, "#", text, [2]int{0, 0}, ""},
		{"* matches whole words only", "foo foobar foo", [2]int{0, 0}, "*", "foo foobar foo", [2]int{0, 11}, ""},
		{"2n", text, [2]int{0, 0}, "/«foo»2n", text, [2]int{0, 0}, ""},
		{"an empty pattern reuses the last one", text, [2]int{0, 0}, "/«foo»/«»", text, [2]int{0, 16}, ""},
		{"d/ deletes up to the match", text, [2]int{0, 0}, "d/«baz»", "baz foo", [2]int{0, 0}, ""},
		{"dn", text, [2]int{0, 0}, "/«foo»0dn", "foo baz foo", [2]int{0, 0}, ""},
		{"smart case: lower case ignores case", "x Foo foo", [2]int{0, 0}, "/«foo»", "x Foo foo", [2]int{0, 2}, ""},
		{"smart case: an upper case letter matches case", "x Foo foo Foo", [2]int{0, 0}, "/«Foo»", "x Foo foo Foo", [2]int{0, 2}, ""},
		{"vi word boundaries", "foobar foo", [2]int{0, 0}, "/«\\<foo\\>»", "foobar foo", [2]int{0, 7}, ""},
		{"vi alternation", "a b c", [2]int{0, 0}, "/«b\\|c»", "a b c", [2]int{0, 2}, ""},
		{"a plain parenthesis is literal", "f(x) g", [2]int{0, 0}, "/«(x)»", "f(x) g", [2]int{0, 1}, ""},
		{"not found leaves the caret", text, [2]int{0, 3}, "/«zzz»", text, [2]int{0, 3}, ""},
		{"a cancelled prompt does nothing", text, [2]int{0, 3}, "/<in!>", text, [2]int{0, 3}, ""},
		{"search across lines", "a\nb\nfoo", [2]int{0, 0}, "/«foo»", "a\nb\nfoo", [2]int{2, 0}, ""},
		{"search backwards across lines", "foo\nb\na", [2]int{2, 0}, "?«foo»", "foo\nb\na", [2]int{0, 0}, ""},
	})
}

func TestViMarks(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"'a jumps to the line", "a\n b\nc", [2]int{1, 3}, "majj'a", "a\n b\nc", [2]int{1, 1}, ""},
		{"`a jumps to the exact place", "ab\ncd", [2]int{0, 1}, "majj`a", "ab\ncd", [2]int{0, 1}, ""},
		{"d'a deletes the lines", "a\nb\nc\nd", [2]int{1, 0}, "majjd'a", "a", [2]int{0, 0}, ""},
		{"d`a deletes up to the mark", "abcdef", [2]int{0, 1}, "ma3ld`a", "aef", [2]int{0, 1}, ""},
		{"'' returns after a jump", "a\nb\nc", [2]int{0, 0}, "G''", "a\nb\nc", [2]int{0, 0}, ""},
		{"an unset mark fails", "abc", [2]int{0, 1}, "'z", "abc", [2]int{0, 1}, ""},
	})
}

func TestViColonCommands(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{":N goes to a line", "a\n  b\nc", [2]int{0, 0}, ":«2»", "a\n  b\nc", [2]int{1, 2}, ""},
		{":$ goes to the last line", "a\nb\nc", [2]int{0, 0}, ":«$»", "a\nb\nc", [2]int{2, 0}, ""},
		{":N past the end goes to the last line", "a\nb", [2]int{0, 0}, ":«99»", "a\nb", [2]int{1, 0}, ""},
		{":s on the current line", "foo foo\nfoo", [2]int{0, 0}, ":«s/foo/X/»", "X foo\nfoo", [2]int{0, 0}, ""},
		{":s with g", "foo foo\nfoo", [2]int{0, 0}, ":«s/foo/X/g»", "X X\nfoo", [2]int{0, 0}, ""},
		{":%s", "foo foo\nfoo", [2]int{0, 0}, ":«%s/foo/X/g»", "X X\nX", [2]int{1, 0}, ""},
		{":%s with a different delimiter", "a/b", [2]int{0, 0}, ":«%s#/#-#»", "a-b", [2]int{0, 0}, ""},
		{":s with groups", "ab", [2]int{0, 0}, ":«s/\\(a\\)\\(b\\)/\\2\\1/»", "ba", [2]int{0, 0}, ""},
		{":s with &", "ab", [2]int{0, 0}, ":«s/a/&&/»", "aab", [2]int{0, 0}, ""},
		{":s with word boundaries", "foo foobar", [2]int{0, 0}, ":«s/\\<foo\\>/X/g»", "X foobar", [2]int{0, 0}, ""},
		{":s ignoring case", "Foo", [2]int{0, 0}, ":«s/foo/X/i»", "X", [2]int{0, 0}, ""},
		{":s with a line break", "a b", [2]int{0, 0}, ":«s/ /\\r/»", "a\nb", [2]int{1, 0}, ""},
		{":s that finds nothing", "abc", [2]int{0, 0}, ":«s/z/y/»", "abc", [2]int{0, 0}, ""},
		{":s without a pattern repeats", "a a\na a", [2]int{0, 0}, ":«s/a/b/»j:«s»", "b a\nb a", [2]int{1, 0}, ""},
		{"& repeats the substitute", "a a\na a", [2]int{0, 0}, ":«s/a/b/»j&", "b a\nb a", [2]int{1, 0}, ""},
		{":1,2d", "a\nb\nc\nd", [2]int{3, 0}, ":«1,2d»", "c\nd", [2]int{0, 0}, ""},
		{":.,+1d", "a\nb\nc\nd", [2]int{1, 0}, ":«.,+1d»", "a\nd", [2]int{1, 0}, ""},
		{":%d", "a\nb", [2]int{0, 0}, ":«%d»", "", [2]int{0, 0}, ""},
		{":d then p", "a\nb", [2]int{0, 0}, ":«d»p", "b\na", [2]int{1, 0}, ""},
		{":2,3y then P", "a\nb\nc", [2]int{0, 0}, ":«2,3y»P", "b\nc\na\nb\nc", [2]int{0, 0}, ""},
		// Vim leaves the caret on the line that took the place of the last deleted one
		{":g/x/d", "ax\nb\ncx\nd", [2]int{0, 0}, ":«g/x/d»", "b\nd", [2]int{1, 0}, ""},
		{":g! deletes the lines that do not match", "ax\nb\ncx", [2]int{0, 0}, ":«g!/x/d»", "ax\ncx", [2]int{1, 0}, ""},
		{":v/x/d", "ax\nb\ncx", [2]int{0, 0}, ":«v/x/d»", "ax\ncx", [2]int{1, 0}, ""},
		{":g/x/s/a/b/", "ax\nb\nax", [2]int{0, 0}, ":«g/x/s/a/b/»", "bx\nb\nbx", [2]int{2, 0}, ""},
		{":g with consecutive lines", "x\nx\nb\nx", [2]int{0, 0}, ":«g/x/d»", "b", [2]int{0, 0}, ""},
		{":j", "a\nb\nc", [2]int{0, 0}, ":«j»", "a b\nc", [2]int{0, 0}, ""},            // Vim 9.1: the caret is on the first non-blank of the joined line
		{":1,3j", "a\nb\nc\nd", [2]int{0, 0}, ":«1,3j»", "a b c\nd", [2]int{0, 0}, ""}, // Vim 9.1: first non-blank of the joined line
		{":>", "a\nb", [2]int{0, 0}, ":«>»", "    a\nb", [2]int{0, 4}, ""},
		{":%>>", "a\nb", [2]int{0, 0}, ":«%>>»", "        a\n        b", [2]int{1, 8}, ""}, // Vim 9.1: the caret is on the last line of the range
		{":<", "    a", [2]int{0, 4}, ":«<»", "a", [2]int{0, 0}, ""},
		{":m0 moves a line to the top", "a\nb\nc", [2]int{2, 0}, ":«m0»", "c\na\nb", [2]int{0, 0}, ""},
		{":m$ moves a line to the end", "a\nb\nc", [2]int{0, 0}, ":«m$»", "b\nc\na", [2]int{2, 0}, ""},
		{":m+1", "a\nb\nc", [2]int{0, 0}, ":«m+1»", "b\na\nc", [2]int{1, 0}, ""},
		{":t. copies a line below", "a\nb", [2]int{0, 0}, ":«t.»", "a\na\nb", [2]int{1, 0}, ""},
		{":t0 copies to the top", "a\nb", [2]int{1, 0}, ":«t0»", "b\na\nb", [2]int{0, 0}, ""},
		{":sort", "c\na\nb", [2]int{0, 0}, ":«sort»", "a\nb\nc", [2]int{0, 0}, ""},
		{":sort!", "a\nc\nb", [2]int{0, 0}, ":«sort!»", "c\nb\na", [2]int{0, 0}, ""},
		{":sort n", "10\n9\n100", [2]int{0, 0}, ":«sort n»", "9\n10\n100", [2]int{0, 0}, ""},
		{":sort u", "b\na\nb", [2]int{0, 0}, ":«sort u»", "a\nb", [2]int{0, 0}, ""},
		{":sort with a range", "z\nc\na\nb", [2]int{0, 0}, ":«2,4sort»", "z\na\nb\nc", [2]int{1, 0}, ""},
		{"a visual range", "a\nb\nc\nd", [2]int{1, 0}, "Vj:«'<,'>d»", "a\nd", [2]int{1, 0}, ""},
		{"the visual prompt starts with the range", "a\nb", [2]int{0, 0}, "Vj:", "a\nb", [2]int{1, 0}, ""},
		{":normal is not a command", "abc", [2]int{0, 0}, ":«frobnicate»", "abc", [2]int{0, 0}, ""},
		{"a cancelled colon prompt", "abc", [2]int{0, 0}, ":<in!>", "abc", [2]int{0, 0}, ""},
		{"an empty command line", "abc", [2]int{0, 0}, ":«»", "abc", [2]int{0, 0}, ""},
		{"ZZ saves and quits", "abc", [2]int{0, 0}, "ZQ", "abc", [2]int{0, 0}, ""},
	})
}

func (v *vi) ran() []string { return v.host.Cmd().Ran() }

func (v *vi) notices() string { return strings.Join(v.h.NoticeTexts(), " | ") }

func TestViEditorCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		keys   string
		modify bool
		second bool // a second tab is open
		ran    []string
		notice string
	}{
		{"q on a clean last tab quits", ":«q»", false, false, []string{"quit"}, ""},
		{"q on a modified tab refuses", ":«q»", true, false, nil, "E37"},
		{"q! closes the tab and quits on the last one", ":«q!»", true, false, []string{"close_tab", "quit"}, ""},
		{"q! on one of two tabs only closes it", ":«q!»", true, true, []string{"close_tab"}, ""},
		{"qa! with unsaved changes asks to discard", ":«qa!»", true, false, []string{"quit_discard"}, ""},
		{"qa! with nothing unsaved just quits", ":«qa!»", false, false, []string{"quit"}, ""},
		{"qa with unsaved changes refuses", ":«qa»", true, false, nil, "E37"},
		{"qa on clean tabs quits", ":«qa»", false, true, []string{"quit"}, ""},
		{"ZQ discards", "ZQ", true, false, []string{"close_tab", "quit"}, ""},
		{"bn", ":«bn»", false, true, []string{"next_buffer"}, ""},
		{"bp", ":«bp»", false, true, []string{"prev_buffer"}, ""},
		{"sp", ":«sp»", false, false, []string{"split_horizontal"}, ""},
		{"vs", ":«vs»", false, false, []string{"split_vertical"}, ""},
		{"enew", ":«enew»", false, false, []string{"new"}, ""},
		{"noh", ":«noh»", false, false, []string{"clear_search"}, ""},
		{"an unknown command", ":«nosuch»", false, false, nil, "E492"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, "abc")
			if tt.second {
				v.host.AddBuffer("/w/b.txt", "other")
			}
			if tt.modify {
				v.keys("x")
				v.host.Cmd().Calls = nil
			}
			v.keys(tt.keys)
			got := v.ran()
			switch {
			case tt.ran == nil && len(got) != 0:
				t.Errorf("actions = %v, want none", got)
			case tt.ran != nil && !slices.Equal(got, tt.ran):
				t.Errorf("actions = %v, want %v", got, tt.ran)
			}
			if tt.notice != "" && !strings.Contains(v.notices(), tt.notice) {
				t.Errorf("notices = %q, want one with %q", v.notices(), tt.notice)
			}
			if tt.name == "q! closes the tab and quits on the last one" || tt.name == "ZQ discards" || tt.name == "q! on one of two tabs only closes it" {
				calls := v.host.Cmd().Calls
				if len(calls) == 0 || calls[0].Args["force"] != true {
					t.Errorf("close_tab args = %+v, want force", calls)
				}
			}
		})
	}
}

// TestViWriteSaves: :w saves through the buffer, waits for the save, and reports it.
func TestViWriteSaves(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc")
	v.keys("x:«w»")
	v.h.CompleteBuffers()
	if got := v.host.CallCount("Save"); got != 1 {
		t.Fatalf("Save calls = %d, want 1 (calls %v)", got, v.host.Calls())
	}
	if !strings.Contains(v.notices(), "written") {
		t.Errorf("notices = %q, want a written message", v.notices())
	}
}

func TestViSetOptions(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc")
	v.keys(":«set nonumber»")
	got, err := v.host.GetOption(v.id, "line_numbers")
	if err != nil || got != false {
		t.Fatalf("line_numbers = %v %v, want false", got, err)
	}
	v.keys(":«set nu»")
	if got, _ := v.host.GetOption(v.id, "line_numbers"); got != true {
		t.Fatalf("line_numbers = %v, want true", got)
	}
	v.keys(":«set bogus»")
	if !strings.Contains(v.notices(), "E518") {
		t.Errorf("notices = %q, want E518", v.notices())
	}
}

func TestViStatusLabel(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc def")
	if v.label() != "-- NORMAL --" {
		t.Fatalf("label = %q", v.label())
	}
	v.keys("2d")
	if v.label() != "-- NORMAL -- 2d" {
		t.Errorf("label with a pending operator = %q", v.label())
	}
	v.keys("<Esc>i")
	if v.label() != "-- INSERT --" {
		t.Errorf("label in insert = %q", v.label())
	}
	v.keys("<Esc>")
	if v.label() != "-- NORMAL --" {
		t.Errorf("label after Esc = %q", v.label())
	}
	v.keys("V")
	if v.label() != "-- VISUAL LINE --" {
		t.Errorf("label in visual line = %q", v.label())
	}
}

// TestViEveryKeyIsHandled: nothing a user types in normal or visual mode reaches the
// buffer, whatever it is.
func TestViEveryKeyIsHandled(t *testing.T) {
	t.Parallel()
	const text = "abc\ndef"
	var chars []string
	for r := ' '; r < 0x7f; r++ {
		chars = append(chars, string(r))
	}
	chars = append(chars, "é", "世")
	for _, ch := range chars {
		if strings.ContainsAny(ch, "iaIAoOcCsSRdxXDyYpPJrJ~ugG.@q<>=!\"'`:/?&ZmfFtTvVzv0123456789") || strings.Contains("ctrl", ch) {
			continue // commands that edit or wait for more keys
		}
		v := newVi(t, text)
		v.keys(ch)
		if got := v.text(); got != text {
			t.Errorf("key %q changed the text to %q", ch, got)
		}
		if v.mode() != "vi_normal" {
			t.Errorf("key %q left the mode %q", ch, v.mode())
		}
	}
	// pasted text goes to the mode's text function in normal mode (it is put as P puts it, see
	// TestViPaste; it used to be swallowed) and is typed in insert mode
	v := newVi(t, text)
	info, _ := v.modeInfo()
	if info.Paste != pluginapi.TextHandler {
		t.Errorf("normal mode paste policy = %v, want the text handler", info.Paste)
	}
	v.keys("i")
	info, _ = v.modeInfo()
	if info.Paste != pluginapi.TextPass || info.Text != pluginapi.TextPass {
		t.Errorf("insert mode policies = %v %v, want pass", info.Paste, info.Text)
	}
}
