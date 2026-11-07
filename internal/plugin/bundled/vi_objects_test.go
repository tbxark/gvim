package bundled_test

import (
	"testing"
)

func TestViTextObjects(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"diw", "foo bar", [2]int{0, 1}, "diw", " bar", [2]int{0, 0}, ""},
		{"diw on blanks", "foo   bar", [2]int{0, 4}, "diw", "foobar", [2]int{0, 3}, ""},
		{"daw", "foo bar baz", [2]int{0, 4}, "daw", "foo baz", [2]int{0, 4}, ""},
		{"daw on the first word", "foo bar", [2]int{0, 0}, "daw", "bar", [2]int{0, 0}, ""},
		{"daw on the last word takes the blank before", "foo bar", [2]int{0, 4}, "daw", "foo", [2]int{0, 2}, ""},
		{"ciw", "foo bar", [2]int{0, 5}, "ciwX<Esc>", "foo X", [2]int{0, 4}, ""},
		{"diW", "a.b c", [2]int{0, 0}, "diW", " c", [2]int{0, 0}, ""},
		{"daW", "a.b c", [2]int{0, 1}, "daW", "c", [2]int{0, 0}, ""},
		{"d2aw", "a b c d", [2]int{0, 0}, "d2aw", "c d", [2]int{0, 0}, ""},
		{"d3iw", "a b c d", [2]int{0, 0}, "d3iw", " c d", [2]int{0, 0}, ""},
		{"ci\"", `x = "hello" y`, [2]int{0, 6}, "ci\"Z<Esc>", `x = "Z" y`, [2]int{0, 5}, ""},
		{"ci\" from before the quote", `x = "hello" y`, [2]int{0, 0}, "ci\"Z<Esc>", `x = "Z" y`, [2]int{0, 5}, ""},
		{"da\" takes a blank", `x = "hello" y`, [2]int{0, 6}, "da\"", `x = y`, [2]int{0, 4}, ""},
		{"di\" on the quote itself", `a "b" c`, [2]int{0, 2}, "di\"", `a "" c`, [2]int{0, 3}, ""},
		{"escaped quotes do not end the string", `"a\"b" c`, [2]int{0, 1}, "di\"", `"" c`, [2]int{0, 1}, ""},
		{"di'", "x 'abc' y", [2]int{0, 4}, "di'", "x '' y", [2]int{0, 3}, ""},
		{"di`", "x `abc` y", [2]int{0, 4}, "di`", "x `` y", [2]int{0, 3}, ""},
		{"di(", "f(a, b)", [2]int{0, 3}, "di(", "f()", [2]int{0, 2}, ""},
		{"di) too", "f(a, b)", [2]int{0, 3}, "di)", "f()", [2]int{0, 2}, ""},
		{"dib", "f(a, b)", [2]int{0, 3}, "dib", "f()", [2]int{0, 2}, ""},
		{"da(", "f(a, b)", [2]int{0, 3}, "da(", "f", [2]int{0, 0}, ""},
		{"di( on the opener", "f(a, b)", [2]int{0, 1}, "di(", "f()", [2]int{0, 2}, ""},
		{"di( on the closer", "f(a, b)", [2]int{0, 6}, "di(", "f()", [2]int{0, 2}, ""},
		{"nested: d2i(", "(a (b) c)", [2]int{0, 4}, "d2i(", "()", [2]int{0, 1}, ""},
		{"di[", "x[1, 2]", [2]int{0, 3}, "di[", "x[]", [2]int{0, 2}, ""},
		{"di<", "x<tag>", [2]int{0, 3}, "di<", "x<>", [2]int{0, 2}, ""},
		{"di{ over lines", "if x {\n  a\n  b\n}", [2]int{1, 2}, "di{", "if x {\n}", [2]int{1, 0}, ""},
		{"diB over lines", "if x {\n  a\n  b\n}", [2]int{1, 2}, "diB", "if x {\n}", [2]int{1, 0}, ""},
		{"ci{ over lines opens a line", "if x {\n  a\n}", [2]int{1, 2}, "ci{X<Esc>", "if x {\n  X\n}", [2]int{1, 2}, ""},
		{"da{ over lines", "f {\n  a\n}\nx", [2]int{1, 2}, "da{", "f \nx", [2]int{0, 1}, ""},
		{"di( on one line inside a longer one", "g(a) + f(b)", [2]int{0, 9}, "di(", "g(a) + f()", [2]int{0, 9}, ""},
		{"no object: nothing happens", "abc", [2]int{0, 1}, "di(", "abc", [2]int{0, 1}, ""},
		{"dip", "a\nb\n\nc", [2]int{0, 0}, "dip", "\nc", [2]int{0, 0}, ""},
		{"dap takes the blank lines after", "a\nb\n\nc", [2]int{0, 0}, "dap", "c", [2]int{0, 0}, ""},
		{"dap on the last paragraph takes the blank lines before", "a\n\nb\nc", [2]int{2, 0}, "dap", "a", [2]int{0, 0}, ""},
		{"yip then P", "a\nb\n\nc", [2]int{1, 0}, "yipP", "a\nb\na\nb\n\nc", [2]int{0, 0}, ""},
		{"dit", "<b>hi</b>", [2]int{0, 4}, "dit", "<b></b>", [2]int{0, 3}, ""},
		{"dat", "x <b>hi</b> y", [2]int{0, 6}, "dat", "x  y", [2]int{0, 2}, ""},
		{"dit with nested tags", "<a><b>x</b>y</a>", [2]int{0, 5}, "dit", "<a><b></b>y</a>", [2]int{0, 6}, ""},
	})
}

func TestViRegisters(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"ywP", "foo bar", [2]int{0, 0}, "ywP", "foo foo bar", [2]int{0, 3}, ""},
		{"ywp", "foo bar", [2]int{0, 0}, "ywp", "ffoo oo bar", [2]int{0, 4}, ""},
		{"yyp", "a\nb", [2]int{0, 0}, "yyp", "a\na\nb", [2]int{1, 0}, ""},
		{"yyP", "a\nb", [2]int{1, 0}, "yyP", "a\nb\nb", [2]int{1, 0}, ""},
		{"ddp swaps lines", "a\nb", [2]int{0, 0}, "ddp", "b\na", [2]int{1, 0}, ""},
		{"xp swaps characters", "ab", [2]int{0, 0}, "xp", "ba", [2]int{0, 1}, ""},
		{"3p of a character", "a", [2]int{0, 0}, "yl3p", "aaaa", [2]int{0, 3}, ""},
		{"2p of a line", "a\nb", [2]int{0, 0}, "yy2p", "a\na\na\nb", [2]int{1, 0}, ""},
		{"p on the last line", "a", [2]int{0, 0}, "yyp", "a\na", [2]int{1, 0}, ""},
		{"gp leaves the caret after the text", "a\nb\nc", [2]int{0, 0}, "yygp", "a\na\nb\nc", [2]int{2, 0}, ""},
		{"named registers", "foo bar", [2]int{0, 0}, "\"ayw$\"ap", "foo barfoo ", [2]int{0, 10}, ""},
		{"an uppercase register appends", "a b", [2]int{0, 0}, "\"ayw\"Aywx\"ap", " a a b", [2]int{0, 4}, ""},
		{"the black hole keeps the register", "a\nb", [2]int{0, 0}, "yyj\"_ddp", "a\na", [2]int{1, 0}, ""},
		{"register 0 keeps the yank", "a\nb", [2]int{0, 0}, "yyjdd\"0p", "a\na", [2]int{1, 0}, ""},
		{"numbered registers shift", "a\nb\nc", [2]int{0, 0}, "dddd\"2p", "c\na", [2]int{1, 0}, ""},
		{"a small delete goes to the minus register", "abc def", [2]int{0, 0}, "xw\"-p", "bc daef", [2]int{0, 4}, ""},
		{"p of a multi-line character text", "ab\ncd", [2]int{0, 1}, "vjy$p", "abb\ncd\ncd", [2]int{0, 2}, ""},
		{"nothing in the register", "abc", [2]int{0, 0}, "\"zp", "abc", [2]int{0, 0}, ""},
	})
}

func TestViDotRepeat(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"dw.", "a b c d", [2]int{0, 0}, "dw.", "c d", [2]int{0, 0}, ""},
		{"x..", "abcde", [2]int{0, 0}, "x..", "de", [2]int{0, 0}, ""},
		{"3x then .", "abcdefgh", [2]int{0, 0}, "3x.", "gh", [2]int{0, 0}, ""},
		{"3x then 2.", "abcdefgh", [2]int{0, 0}, "3x2.", "fgh", [2]int{0, 0}, ""},
		{"dd.", "a\nb\nc", [2]int{0, 0}, "dd.", "c", [2]int{0, 0}, ""},
		{"ciw then w.", "aa bb", [2]int{0, 0}, "ciwX<Esc>w.", "X X", [2]int{0, 2}, ""},
		{"cw keeps the typed text", "aa bb cc", [2]int{0, 0}, "cwxy<Esc>w.w.", "xy xy xy", [2]int{0, 7}, ""},
		{"A;<Esc> then j.", "a\nb", [2]int{0, 0}, "A;<Esc>j.", "a;\nb;", [2]int{1, 1}, ""},
		{"ihi<Esc> then .", "x", [2]int{0, 0}, "ihi<Esc>.", "hhiix", [2]int{0, 2}, ""},
		{"o then .", "a", [2]int{0, 0}, "ob<Esc>.", "a\nb\nb", [2]int{2, 0}, ""},
		{"3ix<Esc> then .", "a", [2]int{0, 0}, "3ix<Esc>.", "xxxxxxa", [2]int{0, 4}, ""},
		{">>.", "a", [2]int{0, 0}, ">>.", "        a", [2]int{0, 8}, ""},
		{"J.", "a\nb\nc\nd", [2]int{0, 0}, "J.", "a b c\nd", [2]int{0, 3}, ""},
		{"p.", "a", [2]int{0, 0}, "yyp.", "a\na\na", [2]int{2, 0}, ""},
		{"r then .", "abcd", [2]int{0, 0}, "rXl.", "XXcd", [2]int{0, 1}, ""},
		{"~..", "abcd", [2]int{0, 0}, "~..", "ABCd", [2]int{0, 3}, ""},
		{"daw.", "a b c d", [2]int{0, 0}, "daw.", "c d", [2]int{0, 0}, ""},
		{"df, then .", "a,b,c", [2]int{0, 0}, "df,.", "c", [2]int{0, 0}, ""},
		{"ctrl+a then .", "5", [2]int{0, 0}, "<C-a>..", "8", [2]int{0, 0}, ""},
		{"Vjd then .", "a\nb\nc\nd\ne\nf", [2]int{0, 0}, "Vjd.", "e\nf", [2]int{0, 0}, ""},
		{"vld then .", "abcdef", [2]int{0, 0}, "vld.", "ef", [2]int{0, 0}, ""},
		{". with nothing to repeat", "abc", [2]int{0, 0}, ".", "abc", [2]int{0, 0}, ""},
		{"yank is not repeated", "abc", [2]int{0, 0}, "xyl.", "c", [2]int{0, 0}, ""},
		{"S then .", "a\nb", [2]int{0, 0}, "SX<Esc>j.", "X\nX", [2]int{1, 0}, ""},
		{"C then .", "ab\ncd", [2]int{0, 1}, "CX<Esc>j0l.", "aX\ncX", [2]int{1, 1}, ""},
		{"dot after an undone change", "abc", [2]int{0, 0}, "xu.", "bc", [2]int{0, 0}, ""},
	})
}

func TestViUndo(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"u", "abc", [2]int{0, 0}, "xu", "abc", [2]int{0, 0}, ""},
		{"ctrl+r", "abc", [2]int{0, 0}, "xu<C-r>", "bc", [2]int{0, 0}, ""},
		{"2u", "abc", [2]int{0, 0}, "xx<Esc>2u", "abc", [2]int{0, 0}, ""},
		{"u with nothing to undo", "abc", [2]int{0, 1}, "u", "abc", [2]int{0, 1}, ""},
		{"a change is one undo step", "a b c", [2]int{0, 0}, "d2wu", "a b c", [2]int{0, 0}, ""},
	})
}
