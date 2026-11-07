package bundled_test

import (
	"testing"
)

func TestViOperators(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"x", "hello", [2]int{0, 1}, "x", "hllo", [2]int{0, 1}, ""},
		{"3x", "hello", [2]int{0, 1}, "3x", "ho", [2]int{0, 1}, ""},
		{"x on the last character moves left", "hello", [2]int{0, 4}, "x", "hell", [2]int{0, 3}, ""},
		{"x on an empty line does nothing", "\nabc", [2]int{0, 0}, "x", "\nabc", [2]int{0, 0}, ""},
		{"X", "hello", [2]int{0, 2}, "X", "hllo", [2]int{0, 1}, ""},
		{"x on a multi-byte character", "héllo", [2]int{0, 1}, "x", "hllo", [2]int{0, 1}, ""},
		{"dw", "foo bar baz", [2]int{0, 0}, "dw", "bar baz", [2]int{0, 0}, ""},
		{"d2w", "foo bar baz", [2]int{0, 0}, "d2w", "baz", [2]int{0, 0}, ""},
		{"2dw", "foo bar baz", [2]int{0, 0}, "2dw", "baz", [2]int{0, 0}, ""},
		{"2d2w multiplies", "a b c d e", [2]int{0, 0}, "2d2w", "e", [2]int{0, 0}, ""},
		{"dw on the last word keeps the line break", "foo\nbar", [2]int{0, 0}, "dw", "\nbar", [2]int{0, 0}, ""},
		{"dw from the middle of a word", "foobar baz", [2]int{0, 3}, "dw", "foobaz", [2]int{0, 3}, ""},
		{"db", "foo bar", [2]int{0, 4}, "db", "bar", [2]int{0, 0}, ""},
		{"de", "foo bar", [2]int{0, 0}, "de", " bar", [2]int{0, 0}, ""},
		{"d$", "foo bar", [2]int{0, 3}, "d$", "foo", [2]int{0, 2}, ""},
		{"D", "foo bar", [2]int{0, 3}, "D", "foo", [2]int{0, 2}, ""},
		{"d0", "foo bar", [2]int{0, 4}, "d0", "bar", [2]int{0, 0}, ""},
		{"d^", "  foo bar", [2]int{0, 6}, "d^", "  bar", [2]int{0, 2}, ""},
		{"dd", "a\nb\nc", [2]int{1, 0}, "dd", "a\nc", [2]int{1, 0}, ""},
		{"dd on the last line", "a\nb\nc", [2]int{2, 0}, "dd", "a\nb", [2]int{1, 0}, ""},
		{"dd on the only line", "a", [2]int{0, 0}, "dd", "", [2]int{0, 0}, ""},
		{"2dd", "a\nb\nc\nd", [2]int{1, 0}, "2dd", "a\nd", [2]int{1, 0}, ""},
		{"dd goes to the first non-blank", "a\n  b\n  c", [2]int{0, 0}, "dd", "  b\n  c", [2]int{0, 2}, ""},
		{"dj", "a\nb\nc\nd", [2]int{1, 0}, "dj", "a\nd", [2]int{1, 0}, ""},
		{"dk", "a\nb\nc\nd", [2]int{2, 0}, "dk", "a\nd", [2]int{1, 0}, ""},
		{"dG", "a\nb\nc\nd", [2]int{1, 0}, "dG", "a", [2]int{0, 0}, ""},
		{"dgg", "a\nb\nc\nd", [2]int{2, 0}, "dgg", "d", [2]int{0, 0}, ""},
		{"d} deletes up to the empty line", "a\nb\n\nc", [2]int{0, 0}, "d}", "\nc", [2]int{0, 0}, ""},
		{"dfx is inclusive", "abcxdef", [2]int{0, 0}, "dfx", "def", [2]int{0, 0}, ""},
		{"dtx is up to", "abcxdef", [2]int{0, 0}, "dtx", "xdef", [2]int{0, 0}, ""},
		{"dFx", "abxcdef", [2]int{0, 5}, "dFx", "abef", [2]int{0, 2}, ""},
		{"d% over brackets", "a(b)c", [2]int{0, 1}, "d%", "ac", [2]int{0, 1}, ""},
		{"dh", "abc", [2]int{0, 2}, "dh", "ac", [2]int{0, 1}, ""},
		{"dl", "abc", [2]int{0, 1}, "dl", "ac", [2]int{0, 1}, ""},
		{"cw", "foo bar", [2]int{0, 0}, "cwX<Esc>", "X bar", [2]int{0, 0}, ""},
		{"cw on the end of a word changes only one character", "foo bar", [2]int{0, 2}, "cwX<Esc>", "foX bar", [2]int{0, 2}, ""},
		{"cw on blanks", "foo   bar", [2]int{0, 3}, "cwX<Esc>", "fooXbar", [2]int{0, 3}, ""},
		{"c2w", "a b c d", [2]int{0, 0}, "c2wX<Esc>", "X c d", [2]int{0, 0}, ""},
		{"cc keeps the indent", "a\n  b\nc", [2]int{1, 3}, "ccX<Esc>", "a\n  X\nc", [2]int{1, 2}, ""},
		{"S", "  abc", [2]int{0, 3}, "SX<Esc>", "  X", [2]int{0, 2}, ""},
		{"C", "foo bar", [2]int{0, 3}, "CX<Esc>", "fooX", [2]int{0, 3}, ""},
		{"s", "abc", [2]int{0, 1}, "sX<Esc>", "aXc", [2]int{0, 1}, ""},
		{"3s", "abcdef", [2]int{0, 1}, "3sX<Esc>", "aXef", [2]int{0, 1}, ""},
		{"s on an empty line", "", [2]int{0, 0}, "sX<Esc>", "X", [2]int{0, 0}, ""},
		{"cc on two lines", "a\nb\nc", [2]int{0, 0}, "2ccX<Esc>", "X\nc", [2]int{0, 0}, ""},
		{"r", "abc", [2]int{0, 1}, "rX", "aXc", [2]int{0, 1}, ""},
		{"3r", "abcdef", [2]int{0, 1}, "3rX", "aXXXef", [2]int{0, 3}, ""},
		{"r with a count past the line fails", "abc", [2]int{0, 1}, "5rX", "abc", [2]int{0, 1}, ""},
		{"r enter splits the line", "ab cd", [2]int{0, 2}, "r<CR>", "ab\ncd", [2]int{1, 0}, ""},
		{"J", "a\nb\nc", [2]int{0, 0}, "J", "a b\nc", [2]int{0, 1}, ""},
		{"3J", "a\nb\nc\nd", [2]int{0, 0}, "3J", "a b c\nd", [2]int{0, 3}, ""},
		{"J strips the indent", "a\n   b", [2]int{0, 0}, "J", "a b", [2]int{0, 1}, ""},
		{"J before a closing paren adds no space", "f(a\n)", [2]int{0, 0}, "J", "f(a)", [2]int{0, 3}, ""},
		{"gJ keeps the blanks", "a\n b", [2]int{0, 0}, "gJ", "a b", [2]int{0, 1}, ""},
		{"J on the last line fails", "a", [2]int{0, 0}, "J", "a", [2]int{0, 0}, ""},
		{"~", "abc", [2]int{0, 0}, "~", "Abc", [2]int{0, 1}, ""},
		{"3~", "abcd", [2]int{0, 0}, "3~", "ABCd", [2]int{0, 3}, ""},
		{">>", "a\nb", [2]int{0, 0}, ">>", "    a\nb", [2]int{0, 4}, ""},
		{"<<", "      a", [2]int{0, 6}, "<<", "  a", [2]int{0, 2}, ""},
		{">j", "a\nb\nc", [2]int{0, 0}, ">j", "    a\n    b\nc", [2]int{0, 4}, ""},
		{"2>>", "a\nb\nc", [2]int{0, 0}, "2>>", "    a\n    b\nc", [2]int{0, 4}, ""},
		{"> skips blank lines", "a\n\nb", [2]int{0, 0}, ">2j", "    a\n\n    b", [2]int{0, 4}, ""},
		{"gUw", "foo bar", [2]int{0, 0}, "gUw", "FOO bar", [2]int{0, 0}, ""},
		{"guu", "FOO Bar", [2]int{0, 2}, "guu", "foo bar", [2]int{0, 2}, ""},
		{"gUU", "foo bar", [2]int{0, 2}, "gUU", "FOO BAR", [2]int{0, 2}, ""},
		{"g~~", "Foo bAr", [2]int{0, 0}, "g~~", "fOO BaR", [2]int{0, 0}, ""},
		{"g?? rot13", "Hello", [2]int{0, 0}, "g??", "Uryyb", [2]int{0, 0}, ""},
		{"gUiw on non-ASCII letters", "héllo wörld", [2]int{0, 0}, "gUiw", "HÉLLO wörld", [2]int{0, 0}, ""},
		{"guiw on non-ASCII letters", "HÉLLO", [2]int{0, 0}, "guiw", "héllo", [2]int{0, 0}, ""},
		{"g~iw on non-ASCII letters", "Héllo", [2]int{0, 0}, "g~iw", "hÉLLO", [2]int{0, 0}, ""},
		{"ctrl+a", "x 7 y", [2]int{0, 0}, "<C-a>", "x 8 y", [2]int{0, 2}, ""},
		{"5 ctrl+a", "x 7 y", [2]int{0, 0}, "5<C-a>", "x 12 y", [2]int{0, 3}, ""},
		{"ctrl+x goes negative", "x 0 y", [2]int{0, 0}, "<C-x>", "x -1 y", [2]int{0, 3}, ""},
		{"ctrl+a on a negative number", "a -3", [2]int{0, 0}, "5<C-a>", "a 2", [2]int{0, 2}, ""},
		{"ctrl+a on hex", "0xff", [2]int{0, 0}, "<C-a>", "0x100", [2]int{0, 4}, ""},
		{"ctrl+a keeps leading zeros", "007", [2]int{0, 0}, "<C-a>", "008", [2]int{0, 2}, ""},
	})
}

// TestViInclusiveMotionOnEmptyLine: an operator with an inclusive end-of-line motion on an
// empty line has nothing to act on; it must not take the line break (S22-1).
func TestViInclusiveMotionOnEmptyLine(t *testing.T) {
	t.Parallel()
	const text = "abc\n\ndef\nghi"
	runViCases(t, []viCase{
		{"D", text, [2]int{1, 0}, "D", text, [2]int{1, 0}, ""},
		{"d$", text, [2]int{1, 0}, "d$", text, [2]int{1, 0}, ""},
		{"C", text, [2]int{1, 0}, "CxX<Esc>", "abc\nxX\ndef\nghi", [2]int{1, 1}, ""},
		{"c$", text, [2]int{1, 0}, "c$x<Esc>", "abc\nx\ndef\nghi", [2]int{1, 0}, ""},
		{"y$ then P puts nothing", text, [2]int{1, 0}, "y$jP", text, [2]int{2, 0}, ""},
		{"d$ on an empty last line", "abc\n", [2]int{1, 0}, "d$", "abc\n", [2]int{1, 0}, ""},
		{"2D reaches the next line", text, [2]int{1, 0}, "2D", "abc\n\nghi", [2]int{1, 0}, ""},
		{"d$ still deletes to the end of a line", text, [2]int{0, 1}, "d$", "a\n\ndef\nghi", [2]int{0, 0}, ""},
	})
}

func TestViInsert(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"i", "abc", [2]int{0, 1}, "iX<Esc>", "aXbc", [2]int{0, 1}, ""},
		{"a", "abc", [2]int{0, 1}, "aX<Esc>", "abXc", [2]int{0, 2}, ""},
		{"a at the end", "abc", [2]int{0, 2}, "aX<Esc>", "abcX", [2]int{0, 3}, ""},
		{"I", "  abc", [2]int{0, 4}, "IX<Esc>", "  Xabc", [2]int{0, 2}, ""},
		{"gI", "  abc", [2]int{0, 4}, "gIX<Esc>", "X  abc", [2]int{0, 0}, ""},
		{"A", "abc", [2]int{0, 0}, "AX<Esc>", "abcX", [2]int{0, 3}, ""},
		{"o", "a\nb", [2]int{0, 0}, "oX<Esc>", "a\nX\nb", [2]int{1, 0}, ""},
		{"o keeps the indent", "  a\nb", [2]int{0, 2}, "oX<Esc>", "  a\n  X\nb", [2]int{1, 2}, ""},
		{"O", "a\nb", [2]int{1, 0}, "OX<Esc>", "a\nX\nb", [2]int{1, 0}, ""},
		{"O keeps the indent", "  a", [2]int{0, 2}, "OX<Esc>", "  X\n  a", [2]int{0, 2}, ""},
		{"3i repeats the text", "abc", [2]int{0, 0}, "3ix<Esc>", "xxxabc", [2]int{0, 2}, ""},
		{"3o repeats the line", "a", [2]int{0, 0}, "3ox<Esc>", "a\nx\nx\nx", [2]int{3, 0}, ""},
		{"3A", "a", [2]int{0, 0}, "3Ab<Esc>", "abbb", [2]int{0, 3}, ""},
		{"insert enters the mode", "abc", [2]int{0, 0}, "i", "abc", [2]int{0, 0}, "vi_insert"},
		{"typing a newline", "ab", [2]int{0, 1}, "i<CR><Esc>", "a\nb", [2]int{1, 0}, ""},
		{"backspace in insert", "abc", [2]int{0, 2}, "a<BS><BS><Esc>", "a", [2]int{0, 0}, ""},
		{"Esc at the start of a line stays", "abc", [2]int{0, 0}, "i<Esc>", "abc", [2]int{0, 0}, ""},
		{"gi returns to the last insert", "abc\ndef", [2]int{0, 0}, "iX<Esc>jgiY<Esc>", "XYabc\ndef", [2]int{0, 1}, ""},
		{"multi-byte insert", "ab", [2]int{0, 1}, "iéx<Esc>", "aéxb", [2]int{0, 3}, ""},
	})
}

func TestViCounts(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"count then register", "a\nb\nc", [2]int{0, 0}, "2\"ayy\"ap", "a\na\nb\nb\nc", [2]int{1, 0}, ""},
		{"zero is a motion without a count", "abc", [2]int{0, 2}, "0", "abc", [2]int{0, 0}, ""},
		{"10 is a count", "abcdefghijkl", [2]int{0, 0}, "10l", "abcdefghijkl", [2]int{0, 10}, ""},
		{"Esc cancels a count", "abc", [2]int{0, 0}, "3<Esc>l", "abc", [2]int{0, 1}, ""},
		{"Esc cancels an operator", "abc", [2]int{0, 0}, "d<Esc>l", "abc", [2]int{0, 1}, ""},
	})
}
