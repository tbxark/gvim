package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/config"
)

// configResolver gives an editor the per-language settings that the default config
// has, as the app does, so a test sees the real built-in language entries.
func configResolver() SettingsResolver {
	cfg := config.Defaults()
	return func(language, path string) LanguageSettings {
		s := cfg.EditorFor(language, path)
		return LanguageSettings{
			Language: s.Language, TabSize: s.TabSize, UseTabs: s.UseTabs, AutoIndent: s.AutoIndent,
			AutoClose: s.AutoClose, AutoSurround: s.AutoSurround, CommentPrefix: s.CommentPrefix,
		}
	}
}

// smartOpts are the editor options with every smart typing rule on.
var smartOpts = Options{AutoIndent: true, AutoClose: true, AutoSurround: true, TabSize: 4}

// typedAt types s at at in text with opts and returns the editor.
func typedAt(t *testing.T, opts Options, text string, at buffer.Pos, s string) *Editor {
	t.Helper()
	e := docWith(opts, text)
	e.SetCursor(at, false)
	typeText(t, e, s)
	return e
}

func TestAutoClose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		text    string
		col     int
		typed   string
		want    string
		wantCol int
	}{
		{"paren", smartOpts, "", 0, "(", "()", 1},
		{"bracket", smartOpts, "", 0, "[", "[]", 1},
		{"brace", smartOpts, "", 0, "{", "{}", 1},
		{"double quote", smartOpts, "", 0, `"`, `""`, 1},
		{"single quote", smartOpts, "", 0, "'", "''", 1},
		{"backtick", smartOpts, "", 0, "`", "``", 1},
		{"before a space", smartOpts, " x", 0, "(", "() x", 1},
		{"before a letter", smartOpts, "x", 0, "(", "(x", 1},
		{"before a digit", smartOpts, "1", 0, "[", "[1", 1},
		{"before an underscore", smartOpts, "_", 0, `"`, `"_`, 1},
		{"before a non-ASCII letter", smartOpts, "é", 0, "(", "()é", 1},
		{"before a closer", smartOpts, ")", 0, "(", "())", 1},
		{"off", Options{TabSize: 4}, "", 0, "(", "(", 1},
		{"a closer that is not there is typed", smartOpts, "", 0, ")", ")", 1},
		{"skips over a closer", smartOpts, "()", 1, ")", "()", 2},
		{"skips over a closing quote", smartOpts, `""`, 1, `"`, `""`, 2},
		{"skip-over needs the option", Options{TabSize: 4}, "()", 1, ")", "())", 2},
		{"typing a pair then the closer", smartOpts, "", 0, "(a)", "(a)", 3},
		{"nested pairs", smartOpts, "", 0, "([{", "([{}])", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := typedAt(t, tt.opts, tt.text, buffer.Pos{Col: tt.col}, tt.typed)
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != (buffer.Pos{Col: tt.wantCol}) {
				t.Fatalf("cursor = %+v, want column %d", got, tt.wantCol)
			}
		})
	}
}

func TestAutoCloseLanguageExceptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		language string
		typed    string
		want     string
	}{
		{"text", `"`, `"`},
		{"text", "'", "'"},
		{"text", "`", "`"},
		{"text", "(", "()"},
		{"markdown", "'", "'"},
		{"markdown", `"`, `""`},
		{"mdx", "'", "'"},
		{"go", "'", "''"},
	}
	for _, tt := range tests {
		t.Run(tt.language+" "+tt.typed, func(t *testing.T) {
			t.Parallel()
			e := docWith(smartOpts, "")
			e.SetLanguageSettings(func(string, string) LanguageSettings {
				return LanguageSettings{Language: tt.language, TabSize: 4, AutoClose: true}
			})
			typeText(t, e, tt.typed)
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAutoCloseFollowsTheLanguageSettings(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "")
	e.SetLanguageSettings(configResolver())
	e.Active().Path = "README.md"
	typeText(t, e, "(")
	if got := text(e); got != "(" {
		t.Fatalf("text = %q, want markdown to leave the bracket alone", got)
	}
}

func TestQuoteInsideAStringIsNotPaired(t *testing.T) {
	t.Parallel()
	for _, inside := range []bool{false, true} {
		e := docWith(smartOpts, "")
		var asked []buffer.Pos
		e.SetStringContext(func(_ *Document, p buffer.Pos) bool {
			asked = append(asked, p)
			return inside
		})
		typeText(t, e, `"`)
		want := `""`
		if inside {
			want = `"`
		}
		if got := text(e); got != want {
			t.Fatalf("inside=%v: text = %q, want %q", inside, got, want)
		}
		if len(asked) != 1 || asked[0] != (buffer.Pos{}) {
			t.Fatalf("inside=%v: asked %v, want one query at the insert position", inside, asked)
		}
		typeText(t, e, "(")
		if len(asked) != 1 {
			t.Fatalf("a bracket must not ask the string context, asked %v", asked)
		}
	}
}

func TestAutoSurround(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		opts       Options
		anchor     buffer.Pos
		head       buffer.Pos
		typed      string
		want       string
		wantCursor buffer.Pos
	}{
		{"paren", smartOpts, buffer.Pos{Col: 0}, buffer.Pos{Col: 5}, "(", "(hello) world", buffer.Pos{Col: 7}},
		{"reversed selection", smartOpts, buffer.Pos{Col: 5}, buffer.Pos{Col: 0}, "(", "(hello) world", buffer.Pos{Col: 7}},
		{"quote", smartOpts, buffer.Pos{Col: 6}, buffer.Pos{Col: 11}, `"`, `hello "world"`, buffer.Pos{Col: 13}},
		{"inner selection", smartOpts, buffer.Pos{Col: 2}, buffer.Pos{Col: 4}, "{", "he{ll}o world", buffer.Pos{Col: 6}},
		{"a closer replaces the selection", smartOpts, buffer.Pos{Col: 0}, buffer.Pos{Col: 5}, ")", ") world", buffer.Pos{Col: 1}},
		{"a letter replaces the selection", smartOpts, buffer.Pos{Col: 0}, buffer.Pos{Col: 5}, "x", "x world", buffer.Pos{Col: 1}},
		{"off", Options{TabSize: 4, AutoClose: true}, buffer.Pos{Col: 0}, buffer.Pos{Col: 5}, "(", "() world", buffer.Pos{Col: 1}},
		{"independent of auto_close", Options{TabSize: 4, AutoSurround: true}, buffer.Pos{Col: 0}, buffer.Pos{Col: 5}, "[", "[hello] world", buffer.Pos{Col: 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(tt.opts, "hello world")
			e.SelectRange(tt.anchor, tt.head)
			typeText(t, e, tt.typed)
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			d := e.Active()
			if d.Cursor != tt.wantCursor || d.HasSelection() {
				t.Fatalf("cursor = %+v selection = %v, want %+v and none", d.Cursor, d.Sel, tt.wantCursor)
			}
		})
	}
}

func TestAutoSurroundIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "hello")
	e.SelectRange(buffer.Pos{Col: 0}, buffer.Pos{Col: 5})
	typeText(t, e, "(")
	run(t, e, "undo")
	if got := text(e); got != "hello" {
		t.Fatalf("text after undo = %q, want hello", got)
	}
}

func TestAutoSurroundHonoursLanguageExceptions(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "hello")
	e.SetLanguageSettings(func(string, string) LanguageSettings {
		return LanguageSettings{Language: "text", TabSize: 4, AutoSurround: true}
	})
	e.SelectRange(buffer.Pos{Col: 0}, buffer.Pos{Col: 5})
	typeText(t, e, `"`)
	if got := text(e); got != `"` {
		t.Fatalf("text = %q, want the quote to replace the selection in plain text", got)
	}
}

func TestAutoCloseAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "a b\nc d")
	e.SetCursor(buffer.Pos{Col: 1}, false)
	run(t, e, "add_cursor_below")
	typeText(t, e, "(")
	if got := text(e); got != "a() b\nc() d" {
		t.Fatalf("text = %q, want a pair at both carets", got)
	}
	want := carets{primary: Caret{Cursor: caretPos(1, 2)}, extras: []Caret{{Cursor: caretPos(0, 2)}}}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
	typeText(t, e, ")")
	if got := text(e); got != "a() b\nc() d" {
		t.Fatalf("text after the closers = %q, want both stepped over", got)
	}
	want = carets{primary: Caret{Cursor: caretPos(1, 3)}, extras: []Caret{{Cursor: caretPos(0, 3)}}}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
}

func TestAutoSurroundAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "ab\ncd")
	e.Active().setCarets([]caret{
		{anchor: caretPos(0, 0), head: caretPos(0, 2), primary: true},
		{anchor: caretPos(1, 0), head: caretPos(1, 2)},
	})
	typeText(t, e, "[")
	if got := text(e); got != "[ab]\n[cd]" {
		t.Fatalf("text = %q, want both selections wrapped", got)
	}
}

func TestClosingBracketReindents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		text    string
		at      buffer.Pos
		typed   string
		want    string
		wantPos buffer.Pos
	}{
		{"dedents to the opener", smartOpts, "if x {\n    a\n    ", buffer.Pos{Line: 2, Col: 4}, "}", "if x {\n    a\n}", buffer.Pos{Line: 2, Col: 1}},
		{"keeps an opener's indent", smartOpts, "    if x {\n        a\n        ", buffer.Pos{Line: 2, Col: 8}, "}", "    if x {\n        a\n    }", buffer.Pos{Line: 2, Col: 5}},
		{"skips a matched block", smartOpts, "f {\n    g {\n    }\n    ", buffer.Pos{Line: 3, Col: 4}, "}", "f {\n    g {\n    }\n}", buffer.Pos{Line: 3, Col: 1}},
		{"paren", smartOpts, "f(\n    a,\n    ", buffer.Pos{Line: 2, Col: 4}, ")", "f(\n    a,\n)", buffer.Pos{Line: 2, Col: 1}},
		{"needs auto_indent", Options{TabSize: 4, AutoClose: true}, "{\n    ", buffer.Pos{Line: 1, Col: 4}, "}", "{\n    }", buffer.Pos{Line: 1, Col: 5}},
		{"only blanks before the caret", smartOpts, "{\n    x ", buffer.Pos{Line: 1, Col: 6}, "}", "{\n    x }", buffer.Pos{Line: 1, Col: 7}},
		{"at column 0 it is just typed", smartOpts, "{\n", buffer.Pos{Line: 1, Col: 0}, "}", "{\n}", buffer.Pos{Line: 1, Col: 1}},
		{"over an existing closer fixes the indent", smartOpts, "f {\n        }", buffer.Pos{Line: 1, Col: 8}, "}", "f {\n}", buffer.Pos{Line: 1, Col: 1}},
		{"over a closer with the right indent just steps", smartOpts, "f {\n}", buffer.Pos{Line: 1, Col: 0}, "}", "f {\n}", buffer.Pos{Line: 1, Col: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := typedAt(t, tt.opts, tt.text, tt.at, tt.typed)
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != tt.wantPos {
				t.Fatalf("cursor = %+v, want %+v", got, tt.wantPos)
			}
		})
	}
}

func TestClosingBracketReindentUsesTabsWhenSet(t *testing.T) {
	t.Parallel()
	opts := smartOpts
	opts.UseTabs = true
	e := typedAt(t, opts, "if x {\n\tif y {\n\t\ta\n\t\t", buffer.Pos{Line: 3, Col: 2}, "}")
	if got := text(e); got != "if x {\n\tif y {\n\t\ta\n\t}" {
		t.Fatalf("text = %q", got)
	}
}

func TestEnterBetweenBrackets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		text    string
		at      buffer.Pos
		want    string
		wantPos buffer.Pos
	}{
		{"braces", smartOpts, "{}", buffer.Pos{Col: 1}, "{\n    \n}", buffer.Pos{Line: 1, Col: 4}},
		{"parens", smartOpts, "f()", buffer.Pos{Col: 2}, "f(\n    \n)", buffer.Pos{Line: 1, Col: 4}},
		{"brackets", smartOpts, "[]", buffer.Pos{Col: 1}, "[\n    \n]", buffer.Pos{Line: 1, Col: 4}},
		{"keeps the opener's indent", smartOpts, "    if x {}", buffer.Pos{Col: 10}, "    if x {\n        \n    }", buffer.Pos{Line: 1, Col: 8}},
		{"text after the closer rides along", smartOpts, "{};", buffer.Pos{Col: 1}, "{\n    \n};", buffer.Pos{Line: 1, Col: 4}},
		{"quotes do not expand", smartOpts, `""`, buffer.Pos{Col: 1}, "\"\n\"", buffer.Pos{Line: 1, Col: 0}},
		{"mismatched pair does not expand", smartOpts, "(]", buffer.Pos{Col: 1}, "(\n    ]", buffer.Pos{Line: 1, Col: 4}},
		{"needs auto_close", Options{AutoIndent: true, TabSize: 4}, "{}", buffer.Pos{Col: 1}, "{\n    }", buffer.Pos{Line: 1, Col: 4}},
		{"needs auto_indent", Options{AutoClose: true, TabSize: 4}, "{}", buffer.Pos{Col: 1}, "{\n}", buffer.Pos{Line: 1, Col: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(tt.opts, tt.text)
			e.SetCursor(tt.at, false)
			run(t, e, "insert_newline")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != tt.wantPos {
				t.Fatalf("cursor = %+v, want %+v", got, tt.wantPos)
			}
		})
	}
}

func TestEnterBetweenBracketsWithTabs(t *testing.T) {
	t.Parallel()
	opts := smartOpts
	opts.UseTabs = true
	e := docWith(opts, "\tf() {}")
	e.SetCursor(buffer.Pos{Col: 6}, false)
	run(t, e, "insert_newline")
	if got := text(e); got != "\tf() {\n\t\t\n\t}" {
		t.Fatalf("text = %q", got)
	}
	if got := e.Active().Cursor; got != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("cursor = %+v, want line 1 column 2", got)
	}
}

func TestEnterBetweenBracketsAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "{}\n[]")
	e.SetCursor(buffer.Pos{Col: 1}, false)
	run(t, e, "add_cursor_below")
	run(t, e, "insert_newline")
	if got := text(e); got != "{\n    \n}\n[\n    \n]" {
		t.Fatalf("text = %q", got)
	}
	want := carets{primary: Caret{Cursor: caretPos(4, 4)}, extras: []Caret{{Cursor: caretPos(1, 4)}}}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
	run(t, e, "undo")
	if got := text(e); got != "{}\n[]" {
		t.Fatalf("text after undo = %q, want one step", got)
	}
}

func TestEnterIndentsAfterAnOpener(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		text    string
		at      buffer.Pos
		want    string
		wantCol int
	}{
		{"brace", smartOpts, "if x {", buffer.Pos{Col: 6}, "if x {\n    ", 4},
		{"paren", smartOpts, "f(", buffer.Pos{Col: 2}, "f(\n    ", 4},
		{"bracket", smartOpts, "x = [", buffer.Pos{Col: 5}, "x = [\n    ", 4},
		{"colon", smartOpts, "if x:", buffer.Pos{Col: 5}, "if x:\n    ", 4},
		{"trailing blanks are ignored", smartOpts, "if x {  ", buffer.Pos{Col: 8}, "if x {  \n    ", 4},
		{"adds to the line's indent", smartOpts, "    if x {", buffer.Pos{Col: 10}, "    if x {\n        ", 8},
		{"other text keeps the indent", smartOpts, "    x = 1", buffer.Pos{Col: 9}, "    x = 1\n    ", 4},
		{"tab size", Options{AutoIndent: true, TabSize: 2}, "{", buffer.Pos{Col: 1}, "{\n  ", 2},
		{"use tabs", Options{AutoIndent: true, TabSize: 4, UseTabs: true}, "{", buffer.Pos{Col: 1}, "{\n\t", 1},
		{"use tabs keeps the level", Options{AutoIndent: true, TabSize: 4, UseTabs: true}, "\t{", buffer.Pos{Col: 2}, "\t{\n\t\t", 2},
		{"off", Options{TabSize: 4}, "{", buffer.Pos{Col: 1}, "{\n", 0},
		{"text before the caret decides", smartOpts, "a {b", buffer.Pos{Col: 3}, "a {\n    b", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(tt.opts, tt.text)
			e.SetCursor(tt.at, false)
			run(t, e, "insert_newline")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != (buffer.Pos{Line: 1, Col: tt.wantCol}) {
				t.Fatalf("cursor = %+v, want line 1 column %d", got, tt.wantCol)
			}
		})
	}
}

func TestEnterOnABlankLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		text      string
		line      int
		plainText bool
		want      string
	}{
		{"takes the indent of the line above", "    a\n\n", 2, false, "    a\n\n\n    "},
		{"opens a level after an opener", "f {\n\n", 2, false, "f {\n\n\n    "},
		{"skips blank lines above", "  a\n\n\n", 3, false, "  a\n\n\n\n  "},
		{"stops at a closer below", "f {\n    a\n\n}", 2, false, "f {\n    a\n\n    \n}"},
		{"drops to a sibling's smaller indent", "f {\n    a\n\ng", 2, false, "f {\n    a\n\n\ng"},
		{"plain text keeps the caret column", "    a\n\n", 2, true, "    a\n\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(smartOpts, tt.text)
			if tt.plainText {
				e.SetLanguageSettings(func(string, string) LanguageSettings {
					return LanguageSettings{Language: "text", TabSize: 4, AutoIndent: true}
				})
			}
			e.SetCursor(buffer.Pos{Line: tt.line}, false)
			run(t, e, "insert_newline")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSmartBackspace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		text    string
		col     int
		want    string
		wantCol int
	}{
		{"removes tab_size spaces", Options{TabSize: 4}, "        x", 8, "    x", 4},
		{"removes only the trailing indent unit", Options{TabSize: 4}, "      x", 6, "  x", 2},
		{"fewer spaces than a unit", Options{TabSize: 4}, "  x", 2, "x", 0},
		{"a tab goes whole", Options{TabSize: 4}, "\t\tx", 2, "\tx", 1},
		{"spaces after a tab", Options{TabSize: 4}, "\t  x", 3, "\tx", 1},
		{"tab size 2", Options{TabSize: 2}, "      x", 6, "    x", 4},
		{"text before the caret deletes one", Options{TabSize: 4}, "ab    ", 6, "ab   ", 5},
		{"caret inside the indent", Options{TabSize: 4}, "      x", 3, "   x", 0},
		{"works without auto_close", Options{TabSize: 4}, "    ", 4, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(tt.opts, tt.text)
			e.SetCursor(buffer.Pos{Col: tt.col}, false)
			run(t, e, "delete_backward")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != (buffer.Pos{Col: tt.wantCol}) {
				t.Fatalf("cursor = %+v, want column %d", got, tt.wantCol)
			}
		})
	}
}

func TestBackspaceAtLineStartJoinsLines(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "ab\n    cd")
	e.SetCursor(buffer.Pos{Line: 1}, false)
	run(t, e, "delete_backward")
	if got := text(e); got != "ab    cd" {
		t.Fatalf("text = %q, want the lines joined", got)
	}
}

func TestBackspaceBetweenAPairDeletesBoth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts Options
		text string
		col  int
		want string
	}{
		{"parens", smartOpts, "f()", 2, "f"},
		{"brackets", smartOpts, "[]", 1, ""},
		{"braces", smartOpts, "{}", 1, ""},
		{"double quotes", smartOpts, `""`, 1, ""},
		{"single quotes", smartOpts, "''", 1, ""},
		{"backticks", smartOpts, "``", 1, ""},
		{"with content it deletes one", smartOpts, "(a)", 2, "()"},
		{"a mismatched pair deletes one", smartOpts, "(]", 1, "]"},
		{"two different quotes delete one", smartOpts, `"'`, 1, "'"},
		{"needs auto_close", Options{AutoIndent: true, TabSize: 4}, "()", 1, ")"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(tt.opts, tt.text)
			e.SetCursor(buffer.Pos{Col: tt.col}, false)
			run(t, e, "delete_backward")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBackspaceAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "()\n    x\n[]")
	e.Active().setCarets([]caret{
		{anchor: caretPos(0, 1), head: caretPos(0, 1), primary: true},
		{anchor: caretPos(1, 4), head: caretPos(1, 4)},
		{anchor: caretPos(2, 1), head: caretPos(2, 1)},
	})
	run(t, e, "delete_backward")
	if got := text(e); got != "\nx\n" {
		t.Fatalf("text = %q, want the pairs and the indent removed", got)
	}
	run(t, e, "undo")
	if got := text(e); got != "()\n    x\n[]" {
		t.Fatalf("text after undo = %q, want one step", got)
	}
}

func TestBackspaceWithCaretsCloserThanATabSizeDoesNotOverlap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		cols  []int
		want  string
		count int
	}{
		{"adjacent carets in the indent", "      x", []int{2, 3}, "    x", 1},
		{"carets two apart in the indent", "      x", []int{3, 5}, "    x", 2},
		{"carets a tab size apart still take a unit each", "        x", []int{4, 8}, "x", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(smartOpts, tt.text)
			var cs []caret
			for i, col := range tt.cols {
				cs = append(cs, caret{anchor: caretPos(0, col), head: caretPos(0, col), primary: i == 0})
			}
			e.Active().setCarets(cs)
			run(t, e, "delete_backward")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := len(e.Active().allCarets()); got != tt.count {
				t.Fatalf("carets = %d, want %d", got, tt.count)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.text {
				t.Fatalf("text after undo = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestTypingStillMergesIntoOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "")
	typeText(t, e, "foo(bar)")
	run(t, e, "undo")
	if got := text(e); got != "" {
		t.Fatalf("text after one undo = %q, want all of the typing undone", got)
	}
}

func TestTabAndTabSizeFollowTheLanguage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{"go uses tabs", "main.go", "\t"},
		{"makefile uses tabs", "Makefile", "\t"},
		{"python uses spaces", "main.py", "    "},
		{"rust uses spaces", "main.rs", "    "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{TabSize: 4}, "")
			e.SetLanguageSettings(configResolver())
			e.Active().Path = tt.path
			run(t, e, "insert_tab")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnterIndentFollowsTheLanguage(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "func f() {")
	e.SetLanguageSettings(configResolver())
	e.Active().Path = "main.go"
	e.SetCursor(buffer.Pos{Col: 10}, false)
	run(t, e, "insert_newline")
	if got := text(e); got != "func f() {\n\t" {
		t.Fatalf("text = %q, want a tab", got)
	}
}

func TestViewSettingsOverrideTheLanguage(t *testing.T) {
	t.Parallel()
	e := docWith(Options{TabSize: 4}, "")
	e.SetLanguageSettings(configResolver())
	e.Active().Path = "main.go"
	if r := e.Do("set_tab_size", map[string]any{"size": 2}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if got := e.tabSizeOf(e.Active()); got != 2 {
		t.Fatalf("tab size = %d, want the buffer's 2", got)
	}
}

func TestMultiRuneTextIsInsertedAsIs(t *testing.T) {
	t.Parallel()
	e := docWith(smartOpts, "")
	if r := e.Do("insert_char", map[string]any{"text": "(a"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if got := text(e); got != "(a" {
		t.Fatalf("text = %q, want a paste to be literal", got)
	}
	e.SelectRange(buffer.Pos{}, buffer.Pos{Col: 2})
	if r := e.Do("insert_char", map[string]any{"text": "[]"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if got := text(e); got != "[]" {
		t.Fatalf("text = %q, want the selection replaced, not wrapped", got)
	}
}
