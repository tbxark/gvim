package editor

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// rulesEditor returns an editor whose document has the given language id, the smart
// typing options on and four-space indents, and the caret at the end of text.
func rulesEditor(lang string, ind *IndentRules, text string) *Editor {
	e := docWith(smartOpts, text)
	e.SetLanguageSettings(func(string, string) LanguageSettings {
		return LanguageSettings{
			Language: lang, TabSize: 4, AutoIndent: true, AutoClose: true, AutoSurround: true,
			Indent: ind,
		}
	})
	d := e.Active()
	last := d.Buffer.Lines() - 1
	e.SetCursor(buffer.Pos{Line: last, Col: d.Buffer.LineLen(last)}, false)
	return e
}

func TestEnterFollowsTheLanguageIndentRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		lang string
		text string
		want string // the indent of the new line
	}{
		{"python opens a block at a colon", "python", "if x:", "    "},
		{"python return dedents the next line", "python", "if x:\n    return 1", ""},
		{"python keeps the column of a blank line", "python", "if a:\n    b\n        ", "        "},
		{"rust opens at a brace", "rust", "fn f() {", "    "},
		{"rust does not open at a colon", "rust", "case x:", ""},
		{"rust braceless if indents the body", "rust", "if (x)", "    "},
		{"rust bare else indents the body", "rust", "    else", "        "},
		{"c keeps the indent of a plain line", "c", "    x = 1;", "    "},
		{"ruby def opens a block", "ruby", "def f", "    "},
		{"ruby one-liner self closes", "ruby", "def f; end", ""},
		{"ruby do block", "ruby", "xs.each do |i|", "    "},
		{"lua function opens a block", "lua", "function f()", "    "},
		{"lua then opens a block", "lua", "if x then", "    "},
		{"bash then opens a block", "bash", "if x; then", "    "},
		{"bash subshell paren does not open", "bash", "echo (", ""},
		{"bash function brace opens", "bash", "f() {", "    "},
		{"fish function", "fish", "function f", "    "},
		{"go keeps the heuristic, which opens at a colon", "go", "x:", "    "},
		{"no language keeps the heuristic", "", "x:", "    "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := rulesEditor(tt.lang, nil, tt.text)
			run(t, e, "insert_newline")
			lines := strings.Split(text(e), "\n")
			if got := lines[len(lines)-1]; got != tt.want {
				t.Fatalf("new line = %q, want %q (text %q)", got, tt.want, text(e))
			}
		})
	}
}

func TestEnterBeforeACloserDedentsByTheRules(t *testing.T) {
	t.Parallel()
	e := rulesEditor("rust", nil, "if x {\n    y\n}")
	e.SetCursor(buffer.Pos{Line: 2}, false)
	run(t, e, "insert_newline")
	if got := text(e); got != "if x {\n    y\n\n}" {
		t.Fatalf("text = %q, want the new line at the closer's indent", got)
	}
}

func TestEnterIgnoresAnOpenerInAComment(t *testing.T) {
	t.Parallel()
	const src = "fn f() { // x"
	for _, masked := range []bool{false, true} {
		e := rulesEditor("rust", nil, src)
		if masked {
			e.SetNonCodeContext(func(_ *Document, line int) []bool {
				if line != 0 {
					return nil
				}
				m := make([]bool, len(src))
				for i := strings.Index(src, "//"); i < len(m); i++ {
					m[i] = true
				}
				return m
			})
		}
		run(t, e, "insert_newline")
		want := "\n"
		if masked {
			want = "\n    " // the comment is blanked, so the line ends in a brace
		}
		if got := strings.TrimPrefix(text(e), src); got != want {
			t.Fatalf("masked=%v: new line = %q, want %q", masked, got, want)
		}
	}
}

func TestUserIndentRulesOverrideTheFamily(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		lang string
		ind  *IndentRules
		text string
		want string
	}{
		{"a language with no family starts blank", "foo", &IndentRules{IncreaseIndentPattern: `BEGIN$`}, "BEGIN", "    "},
		{"no family, no match", "foo", &IndentRules{IncreaseIndentPattern: `BEGIN$`}, "x:", ""},
		{"a set pattern replaces the family's", "rust", &IndentRules{IncreaseIndentPattern: `\bthen$`}, "a then", "    "},
		{"the family's other patterns stay", "rust", &IndentRules{IncreaseIndentPattern: `\bthen$`}, "if (x)", "    "},
		{"the replaced pattern is gone", "rust", &IndentRules{IncreaseIndentPattern: `\bthen$`}, "fn f() {", ""},
		{"self close cancels an increase", "foo", &IndentRules{IncreaseIndentPattern: `BEGIN`, SelfClosePattern: `END`}, "BEGIN x END", ""},
		{"indent next line", "foo", &IndentRules{IndentNextLinePattern: `^IF`}, "IF x", "    "},
		{"dedent next line", "foo", &IndentRules{DedentNextLinePattern: `^\s*STOP`}, "    STOP", ""},
		{"a user rule applies to a tree-sitter language", "go", &IndentRules{IncreaseIndentPattern: `BEGIN$`}, "x:", ""},
		{"a bad pattern is dropped", "foo", &IndentRules{IncreaseIndentPattern: `(`}, "x(", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := rulesEditor(tt.lang, tt.ind, tt.text)
			run(t, e, "insert_newline")
			lines := strings.Split(text(e), "\n")
			if got := lines[len(lines)-1]; got != tt.want {
				t.Fatalf("new line = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeywordTypedLineDedents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lang  string
		ind   *IndentRules
		text  string
		typed string
		want  string
	}{
		{"python else colon", "python", nil, "if x:\n    y\n    else", ":", "if x:\n    y\nelse:"},
		{"python elif colon", "python", nil, "if x:\n    y\n    elif z", ":", "if x:\n    y\nelif z:"},
		{"python elsewhere never", "python", nil, "if x:\n    y\n    elsewhere", ":", "if x:\n    y\n    elsewhere:"},
		{"python case under match keeps the indent", "python", nil, "match x:\n    case y", ":", "match x:\n    case y:"},
		{"python already shallow", "python", nil, "if x:\n    y\nelse", ":", "if x:\n    y\nelse:"},
		{"ruby end", "ruby", nil, "def f\n    x\n    en", "d", "def f\n    x\nend"},
		{"ruby an earlier letter does not fire", "ruby", nil, "def f\n    x\n    e", "n", "def f\n    x\n    en"},
		{"ruby text after the trigger never fires", "ruby", nil, "def f\n    x\n    end", "x", "def f\n    x\n    endx"},
		{"lua until", "lua", nil, "repeat\n    x\n    unti", "l", "repeat\n    x\nuntil"},
		{"bash fi", "bash", nil, "if x; then\n    y\n    f", "i", "if x; then\n    y\nfi"},
		{"user pattern", "foo", &IndentRules{DecreaseIndentPattern: `^\s*CLOSE`}, "OPEN\n    x\n    CLOS", "E", "OPEN\n    x\nCLOSE"},
		{"a language without a decrease rule", "rust", nil, "if x {\n    y\n    els", "e", "if x {\n    y\n    else"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := rulesEditor(tt.lang, tt.ind, tt.text)
			typeText(t, e, tt.typed)
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeywordDedentIsOneUndoStepAndKeepsTheCaretAtTheEnd(t *testing.T) {
	t.Parallel()
	e := rulesEditor("python", nil, "if x:\n    y\n    else")
	typeText(t, e, ":")
	d := e.Active()
	if want := (buffer.Pos{Line: 2, Col: 5}); d.Cursor != want {
		t.Fatalf("caret = %+v, want %+v", d.Cursor, want)
	}
	run(t, e, "undo")
	if got := text(e); got != "if x:\n    y\n    else" {
		t.Fatalf("text after undo = %q", got)
	}
}

func TestKeywordDedentConditions(t *testing.T) {
	t.Parallel()
	const src = "if x:\n    y\n    else"
	t.Run("caret not at the end of the line", func(t *testing.T) {
		t.Parallel()
		e := rulesEditor("python", nil, src+" z")
		e.SetCursor(buffer.Pos{Line: 2, Col: 8}, false)
		typeText(t, e, ":")
		if got := text(e); got != src+": z" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("a selection", func(t *testing.T) {
		t.Parallel()
		e := rulesEditor("python", nil, src+"x")
		e.SelectRange(buffer.Pos{Line: 2, Col: 8}, buffer.Pos{Line: 2, Col: 9})
		typeText(t, e, ":")
		if got := text(e); got != src+":" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("auto indent off", func(t *testing.T) {
		t.Parallel()
		e := rulesEditor("python", nil, src)
		e.SetLanguageSettings(func(string, string) LanguageSettings {
			return LanguageSettings{Language: "python", TabSize: 4}
		})
		typeText(t, e, ":")
		if got := text(e); got != src+":" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("a trigger inside a comment", func(t *testing.T) {
		t.Parallel()
		e := rulesEditor("python", nil, src)
		e.SetNonCodeContext(func(_ *Document, line int) []bool {
			if line != 2 {
				return nil
			}
			return []bool{false, false, false, false, true, true, true, true}
		})
		typeText(t, e, ":")
		if got := text(e); got != src+":" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("every caret", func(t *testing.T) {
		t.Parallel()
		e := rulesEditor("python", nil, "if x:\n    y\n    else\nif z:\n    w\n    else")
		e.Active().setCarets([]caret{
			{anchor: caretPos(2, 8), head: caretPos(2, 8), primary: true},
			at(5, 8),
		})
		typeText(t, e, ":")
		if got := text(e); got != "if x:\n    y\nelse:\nif z:\n    w\nelse:" {
			t.Fatalf("text = %q", got)
		}
	})
}

func TestClosingBracketUsesTheRulesDedent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		lang string
		text string
		want string
	}{
		{"rust closes at the opener's level", "rust", "fn f() {\n    x\n        ", "fn f() {\n    x\n}"},
		{"a nested block", "rust", "a {\n    b {\n        x\n        ", "a {\n    b {\n        x\n    }"},
		{"a language with no decrease rule for it uses the scan", "ruby", "f(\n    x\n        ", "f(\n    x\n)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := rulesEditor(tt.lang, nil, tt.text)
			closer := "}"
			if tt.lang == "ruby" {
				closer = ")"
			}
			typeText(t, e, closer)
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}
