package find

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
)

func pos(line, col int) buffer.Pos { return buffer.Pos{Line: line, Col: col} }

func TestTypingDoesNotMoveTheCaret(t *testing.T) {
	t.Parallel()
	f := newFake("foo bar foo\nfoo")
	f.cursor = pos(0, 4)
	b := Open(f, testStyles(), false)
	typeText(b, "foo")
	press(b, "alt+c")

	if f.sel != nil || f.cursor != pos(0, 4) {
		t.Fatalf("typing moved the caret to %v with selection %v", f.cursor, f.sel)
	}
	if got := b.Query().Text; got != "foo" {
		t.Fatalf("query = %q", got)
	}
}

func TestEscRestoresCaretAndSelection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		presel  *search.Match
		keys    []string
		wantSel *search.Match
	}{
		{name: "after typing", keys: []string{"o"}},
		{name: "after F3 steps", keys: []string{"o", "f3", "f3"}},
		{
			name:    "keeps the selection the user had",
			presel:  &search.Match{Start: pos(0, 4), End: pos(0, 7)},
			keys:    []string{"o", "f3", "f3"},
			wantSel: &search.Match{Start: pos(0, 4), End: pos(0, 7)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake("foo bar foo\nfoo")
			f.cursor = pos(0, 4)
			if tt.presel != nil {
				f.Select(*tt.presel)
			}
			wantCursor := f.cursor
			b := Open(f, testStyles(), false)
			typeText(b, "fo")
			for _, k := range tt.keys {
				press(b, k)
			}
			press(b, "esc")

			if b.Active() || b.Committed() {
				t.Fatalf("Esc left the bar active=%v committed=%v", b.Active(), b.Committed())
			}
			if f.cursor != wantCursor {
				t.Fatalf("caret = %v, want %v", f.cursor, wantCursor)
			}
			switch {
			case tt.wantSel == nil && f.sel != nil:
				t.Fatalf("selection = %v, want none", f.sel)
			case tt.wantSel != nil && (f.sel == nil || *f.sel != *tt.wantSel):
				t.Fatalf("selection = %v, want %v", f.sel, tt.wantSel)
			}
		})
	}
}

func TestEnterJumpsToTheMatchAndClosesTheBar(t *testing.T) {
	t.Parallel()
	f := newFake("foo bar foo\nfoo")
	f.cursor = pos(0, 4)
	b := Open(f, testStyles(), false)
	typeText(b, "foo")
	press(b, "enter")

	want := search.Match{Start: pos(0, 8), End: pos(0, 11)}
	if f.sel == nil || *f.sel != want {
		t.Fatalf("selection = %v, want %v", f.sel, want)
	}
	if b.Active() {
		t.Fatal("Enter left the bar open")
	}
	if !b.Committed() {
		t.Fatal("the search is not kept for highlights and F3")
	}
}

func TestEnterAfterF3KeepsTheStep(t *testing.T) {
	t.Parallel()
	f := newFake("foo foo foo")
	b := Open(f, testStyles(), false)
	typeText(b, "foo")
	press(b, "f3")
	press(b, "f3")
	press(b, "enter")
	if f.sel == nil || f.sel.Start != pos(0, 4) {
		t.Fatalf("selection = %v, want the second match", f.sel)
	}
}

func TestCtrlFActsAsEnter(t *testing.T) {
	t.Parallel()
	f := newFake("foo")
	b := Open(f, testStyles(), false)
	typeText(b, "foo")
	press(b, "ctrl+f")
	if b.Active() || f.sel == nil {
		t.Fatalf("Ctrl+F: active=%v selection=%v", b.Active(), f.sel)
	}
}

func TestEnterWithoutMatchKeepsTheBarOpen(t *testing.T) {
	t.Parallel()
	f := newFake("abc")
	b := Open(f, testStyles(), false)
	typeText(b, "zzz")
	press(b, "enter")
	if !b.Active() || b.Committed() {
		t.Fatalf("active=%v committed=%v, want open and not committed", b.Active(), b.Committed())
	}
	if b.statusText() != search.StatusText(0, 0) {
		t.Fatalf("status = %q", b.statusText())
	}
}

func TestEnterOnEmptyQueryClosesWithoutCommitting(t *testing.T) {
	t.Parallel()
	b := Open(newFake("abc"), testStyles(), false)
	press(b, "enter")
	if b.Active() || b.Committed() {
		t.Fatalf("active=%v committed=%v", b.Active(), b.Committed())
	}
}

func TestEnterInSearchRowWithReplaceRowMovesToReplacement(t *testing.T) {
	t.Parallel()
	f := newFake("a a")
	b := Open(f, testStyles(), true)
	b.inReplace = false
	b.focusRow()
	typeText(b, "a")
	press(b, "enter")
	if !b.Active() || !b.inReplace {
		t.Fatalf("active=%v inReplace=%v, want the replacement row focused", b.Active(), b.inReplace)
	}
	if f.sel != nil {
		t.Fatalf("selection %v moved", f.sel)
	}
}

func TestOpenWithReplaceKeepsTheKeysOnTheSearchRow(t *testing.T) {
	t.Parallel()
	// Fresh's Ctrl+R flow: type the search, Enter, type the replacement, Enter.
	f := newFake("a a a")
	b := Open(f, testStyles(), true)
	if !b.replaceOpen || b.inReplace {
		t.Fatalf("replaceOpen=%v inReplace=%v, want the row shown with the search row focused", b.replaceOpen, b.inReplace)
	}
	typeText(b, "a")
	press(b, "enter")
	typeText(b, "bb")
	press(b, "enter")
	if got := f.text(); got != "bb bb bb" {
		t.Fatalf("text = %q", got)
	}
	if got := b.Query().Text; got != "a" {
		t.Fatalf("search = %q, want the typed search kept out of the replacement", got)
	}
}

func TestEnterInReplaceRowReplacesAll(t *testing.T) {
	t.Parallel()
	f := newFake("a a a")
	b := Open(f, testStyles(), false)
	typeText(b, "a")
	press(b, "tab")
	typeText(b, "bb")
	press(b, "enter")

	if got := f.text(); got != "bb bb bb" {
		t.Fatalf("text = %q", got)
	}
	if got := b.statusText(); got != "Replaced 3" {
		t.Fatalf("status = %q", got)
	}
}

func TestConfirmEachTurnsReplaceIntoQueryReplace(t *testing.T) {
	t.Parallel()
	f := newFake("a a")
	b := Open(f, testStyles(), false)
	press(b, "alt+i")
	if !b.opts[optConfirm] || b.statusText() != "Confirm each enabled" {
		t.Fatalf("confirm=%v status=%q", b.opts[optConfirm], b.statusText())
	}
	typeText(b, "a")
	press(b, "tab")
	typeText(b, "b")
	press(b, "enter")
	if b.qr == nil {
		t.Fatal("Enter did not start query replace")
	}
	if f.text() != "a a" {
		t.Fatalf("text = %q, changed before answering", f.text())
	}
	typeText(b, "y")
	typeText(b, "n")
	if f.text() != "b a" {
		t.Fatalf("text = %q", f.text())
	}
	if b.query().Text != "a" {
		t.Fatalf("confirm each leaked into the query: %+v", b.query())
	}
}

func TestSetOptionsAndConfirmLabel(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	b.SetOptions(true, true, true, true)
	if q := b.Query(); !q.CaseSensitive || !q.WholeWord || !q.Regex {
		t.Fatalf("query = %+v", q)
	}
	rows := frame(b, 100, 6)
	if want := "[x] Confirm Each (Alt+I)"; !strings.Contains(rows[4], want) {
		t.Fatalf("options row %q lacks %q", rows[4], want)
	}
}

func TestQueryReplaceWrapsAround(t *testing.T) {
	t.Parallel()
	f := newFake("x x x x")
	f.cursor = pos(0, 4)
	b := Open(f, testStyles(), false)
	typeText(b, "x")
	press(b, "tab")
	typeText(b, "y")
	b.StartQueryReplace()

	typeText(b, "y") // 0:4
	if b.statusText() != "Replace? (y)es (n)o (a)ll (c)ancel" {
		t.Fatalf("status = %q before the wrap", b.statusText())
	}
	typeText(b, "y") // 0:6
	if f.text() != "x x y y" {
		t.Fatalf("text = %q", f.text())
	}
	if got := b.statusText(); got != "[Wrapped] Replace? (y)es (n)o (a)ll (c)ancel" {
		t.Fatalf("status = %q, want the wrapped question", got)
	}
	if f.sel == nil || f.sel.Start != pos(0, 0) {
		t.Fatalf("selection = %v, want the match at 0:0", f.sel)
	}
	typeText(b, "n")
	typeText(b, "y") // 0:2
	if f.text() != "x y y y" {
		t.Fatalf("text = %q", f.text())
	}
	if b.qr != nil {
		t.Fatal("run did not stop at the original start")
	}
	if got := b.statusText(); got != "Replaced 3 occurrence(s)" {
		t.Fatalf("status = %q", got)
	}
}

func TestQueryReplaceAllIncludesTheWrappedPart(t *testing.T) {
	t.Parallel()
	f := newFake("x x x x")
	f.cursor = pos(0, 4)
	b := Open(f, testStyles(), false)
	typeText(b, "x")
	press(b, "tab")
	typeText(b, "yy")
	b.StartQueryReplace()
	typeText(b, "!")
	if got := f.text(); got != "yy yy yy yy" {
		t.Fatalf("text = %q", got)
	}
	if b.qr != nil {
		t.Fatal("run still going")
	}
}

func TestQueryReplaceWrapKeepsTheStartAfterEarlierReplacements(t *testing.T) {
	t.Parallel()
	// The first answered match is after the start; replacements before the start
	// are longer and must not make the run revisit replaced text.
	f := newFake("x x x")
	f.cursor = pos(0, 2)
	b := Open(f, testStyles(), false)
	typeText(b, "x")
	press(b, "tab")
	typeText(b, "xxx")
	b.StartQueryReplace()
	typeText(b, "!")
	if got := f.text(); got != "xxx xxx xxx" {
		t.Fatalf("text = %q", got)
	}
}

func TestCtrlAltRInTheBarStartsQueryReplace(t *testing.T) {
	t.Parallel()
	f := newFake("a a")
	b := Open(f, testStyles(), false)
	typeText(b, "a")
	press(b, "ctrl+alt+r")
	if b.qr == nil || !b.replaceOpen {
		t.Fatalf("qr=%v replaceOpen=%v", b.qr, b.replaceOpen)
	}
}

func TestFreshQueryReplaceFlowAsksOnEnterInReplaceRow(t *testing.T) {
	t.Parallel()
	f := newFake("a a")
	b := Open(f, testStyles(), true)
	b.inReplace = false
	b.focusRow()
	b.StartQueryReplace() // Ctrl+Alt+R with nothing typed yet
	if b.qr != nil || b.statusText() != queryEmpty {
		t.Fatalf("qr=%v status=%q", b.qr, b.statusText())
	}
	typeText(b, "a")
	press(b, "enter")
	typeText(b, "b")
	press(b, "enter")
	if b.qr == nil {
		t.Fatal("Enter in the replacement row did not ask about each match")
	}
	typeText(b, "!")
	if got := f.text(); got != "b b" {
		t.Fatalf("text = %q", got)
	}
}

func TestZeroWidthMatchesNavigateAndTerminate(t *testing.T) {
	t.Parallel()
	t.Run("F3 walks the line starts", func(t *testing.T) {
		t.Parallel()
		f := newFake("a\nb\nc")
		b := Open(f, testStyles(), false)
		press(b, "alt+r")
		typeText(b, "^")
		var got []buffer.Pos
		for range 4 {
			press(b, "f3")
			got = append(got, f.cursor)
		}
		want := []buffer.Pos{pos(0, 0), pos(1, 0), pos(2, 0), pos(0, 0)}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("steps = %v, want %v", got, want)
			}
		}
	})
	t.Run("query replace of ^ with text", func(t *testing.T) {
		t.Parallel()
		f := newFake("a\nb")
		b := Open(f, testStyles(), false)
		press(b, "alt+r")
		typeText(b, "^")
		press(b, "tab")
		typeText(b, "// ")
		b.StartQueryReplace()
		typeText(b, "!")
		if got := f.text(); got != "// a\n// b" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("query replace of an empty match with nothing", func(t *testing.T) {
		t.Parallel()
		f := newFake("ab")
		b := Open(f, testStyles(), false)
		press(b, "alt+r")
		typeText(b, "x*")
		press(b, "tab")
		b.StartQueryReplace()
		typeText(b, "!")
		if got := f.text(); got != "ab" || b.qr != nil {
			t.Fatalf("text = %q, running = %v", got, b.qr != nil)
		}
	})
	t.Run("replace all of x*", func(t *testing.T) {
		t.Parallel()
		f := newFake("ab")
		b := Open(f, testStyles(), false)
		press(b, "alt+r")
		typeText(b, "x*")
		press(b, "tab")
		typeText(b, "-")
		press(b, "enter")
		if got := f.text(); got != "-a-b-" {
			t.Fatalf("text = %q", got)
		}
	})
}

func TestHistoryWalksWithUpAndDown(t *testing.T) {
	t.Parallel()
	f := newFake("a b c")
	b := Open(f, testStyles(), false)
	for _, q := range []string{"a", "b", "c"} {
		b.search.SetValue(q)
		press(b, "enter")
		b.Reopen()
	}
	b.search.SetValue("dra")
	steps := []struct {
		key  string
		want string
	}{
		{"up", "c"}, {"up", "b"}, {"up", "a"}, {"up", "a"},
		{"down", "b"}, {"down", "c"}, {"down", "dra"},
	}
	for _, s := range steps {
		press(b, s.key)
		if got := b.search.Value(); got != s.want {
			t.Fatalf("after %s: field = %q, want %q", s.key, got, s.want)
		}
	}
}

func TestHistoryDedupesAndRingsAreSeparate(t *testing.T) {
	t.Parallel()
	f := newFake("a b")
	b := Open(f, testStyles(), false)
	for _, q := range []string{"a", "b", "a"} {
		b.search.SetValue(q)
		b.Reopen()
		press(b, "enter")
		b.Reopen()
	}
	press(b, "up")
	press(b, "up")
	if got := b.search.Value(); got != "b" {
		t.Fatalf("second Up = %q, want b (a pushed twice is one entry)", got)
	}

	b.search.SetValue("a")
	press(b, "tab")
	typeText(b, "R1")
	press(b, "enter")
	b.repl.SetValue("")
	press(b, "up")
	if got := b.repl.Value(); got != "R1" {
		t.Fatalf("replacement history = %q", got)
	}
	if got := b.search.Value(); got != "a" {
		t.Fatalf("search field = %q changed by the replacement history", got)
	}
}

func TestPrefillFromSelectionThenHistory(t *testing.T) {
	t.Parallel()
	t.Run("single-line selection", func(t *testing.T) {
		t.Parallel()
		f := newFake("hello world")
		f.Select(search.Match{Start: pos(0, 6), End: pos(0, 11)})
		b := Open(f, testStyles(), false)
		b.Prefill()
		if got := b.search.Value(); got != "world" {
			t.Fatalf("field = %q", got)
		}
		typeText(b, "x") // the pre-filled text is selected, so typing replaces it
		if got := b.search.Value(); got != "x" {
			t.Fatalf("typing over the prefill gave %q", got)
		}
	})
	t.Run("multi-line selection falls back to history", func(t *testing.T) {
		t.Parallel()
		f := newFake("ab\ncd")
		first := Open(f, testStyles(), false)
		first.search.SetValue("cd")
		press(first, "enter")
		f.Select(search.Match{Start: pos(0, 0), End: pos(1, 2)})
		b := Open(f, testStyles(), false)
		b.Inherit(first)
		b.Prefill()
		if got := b.search.Value(); got != "cd" {
			t.Fatalf("field = %q, want the last search", got)
		}
	})
	t.Run("backspace deletes the whole prefill", func(t *testing.T) {
		t.Parallel()
		f := newFake("abc")
		f.Select(search.Match{Start: pos(0, 0), End: pos(0, 3)})
		b := Open(f, testStyles(), false)
		b.Prefill()
		press(b, "backspace")
		if got := b.search.Value(); got != "" {
			t.Fatalf("field = %q", got)
		}
	})
	t.Run("selection that is the current match keeps the regex that found it", func(t *testing.T) {
		t.Parallel()
		f := newFake("foo1 foo2")
		first := Open(f, testStyles(), false)
		first.SetOptions(false, false, true, false)
		first.search.SetValue(`foo\d`)
		press(first, "enter")
		f.Select(search.Match{Start: pos(0, 5), End: pos(0, 9)})
		b := Open(f, testStyles(), false)
		b.Inherit(first)
		b.Prefill()
		if got := b.search.Value(); got != `foo\d` {
			t.Fatalf("field = %q, want the regex", got)
		}
	})
}

func TestInheritKeepsTogglesAndHistory(t *testing.T) {
	t.Parallel()
	f := newFake("a")
	first := Open(f, testStyles(), false)
	press(first, "alt+c")
	press(first, "alt+r")
	press(first, "alt+i")
	first.search.SetValue("a")
	press(first, "enter")

	b := Open(f, testStyles(), false)
	b.Inherit(first)
	if q := b.Query(); !q.CaseSensitive || !q.Regex || q.WholeWord || !b.opts[optConfirm] {
		t.Fatalf("toggles not inherited: %+v confirm=%v", q, b.opts[optConfirm])
	}
	press(b, "up")
	if got := b.search.Value(); got != "a" {
		t.Fatalf("history not inherited, Up gave %q", got)
	}
}

func TestSetSearchCommitsTheQuery(t *testing.T) {
	t.Parallel()
	b := Open(newFake("a b a"), testStyles(), false)
	b.SetSearch("a")
	if b.Query().Text != "a" || !b.Committed() {
		t.Fatalf("query=%q committed=%v", b.Query().Text, b.Committed())
	}
	b.Clear()
	if b.Committed() {
		t.Fatal("Clear left the search committed")
	}
}
