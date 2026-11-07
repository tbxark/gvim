package bundled_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestViVisualOperators(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"viwd", "foo bar", [2]int{0, 1}, "viwd", " bar", [2]int{0, 0}, ""},
		{"vlld", "abcdef", [2]int{0, 1}, "vlld", "aef", [2]int{0, 1}, ""},
		{"vd on one character", "abc", [2]int{0, 1}, "vd", "ac", [2]int{0, 1}, ""},
		{"x in visual", "abc", [2]int{0, 0}, "vlx", "c", [2]int{0, 0}, ""},
		{"backwards selection", "abcdef", [2]int{0, 4}, "vhhd", "abf", [2]int{0, 2}, ""},
		{"vjd over two lines", "abc\ndef", [2]int{0, 1}, "vjd", "af", [2]int{0, 1}, ""},
		{"v$d takes the line break", "abc\ndef", [2]int{0, 1}, "v$d", "adef", [2]int{0, 1}, ""},
		{"vey puts the caret at the start", "ab cd", [2]int{0, 0}, "vey$p", "ab cdab", [2]int{0, 6}, ""},
		{"viwy keeps the text", "foo bar", [2]int{0, 5}, "viwy", "foo bar", [2]int{0, 4}, ""},
		{"Vd", "a\nb\nc", [2]int{1, 0}, "Vd", "a\nc", [2]int{1, 0}, ""},
		{"Vjd", "a\nb\nc\nd", [2]int{1, 0}, "Vjd", "a\nd", [2]int{1, 0}, ""},
		{"Vkd", "a\nb\nc\nd", [2]int{2, 0}, "Vkd", "a\nd", [2]int{1, 0}, ""},
		{"V> shifts", "a\nb", [2]int{0, 0}, "V>", "    a\nb", [2]int{0, 4}, ""},
		{"Vj> shifts both lines", "a\nb\nc", [2]int{0, 0}, "Vj>", "    a\n    b\nc", [2]int{0, 4}, ""},
		{"V3> shifts three times", "a", [2]int{0, 0}, "V3>", "            a", [2]int{0, 12}, ""},
		{"Vj< shifts back", "    a\n    b", [2]int{0, 4}, "Vj<", "a\nb", [2]int{0, 0}, ""},
		{"vlU", "abc", [2]int{0, 0}, "vlU", "ABc", [2]int{0, 0}, ""},
		{"vlu", "ABC", [2]int{0, 0}, "vlu", "abC", [2]int{0, 0}, ""},
		{"vl~", "aBc", [2]int{0, 0}, "vl~", "Abc", [2]int{0, 0}, ""},
		{"vlr", "abc", [2]int{0, 0}, "vlrx", "xxc", [2]int{0, 0}, ""},
		{"Vr", "ab\ncd", [2]int{0, 0}, "Vjrx", "xx\nxx", [2]int{0, 0}, ""},
		{"vc", "abc", [2]int{0, 0}, "vlcX<Esc>", "Xc", [2]int{0, 0}, ""},
		{"vs", "abc", [2]int{0, 0}, "vlsX<Esc>", "Xc", [2]int{0, 0}, ""},
		{"Vc keeps the indent", "  a\n  b", [2]int{0, 2}, "VjcX<Esc>", "  X", [2]int{0, 2}, ""},
		{"VJ joins", "a\nb\nc", [2]int{0, 0}, "VjJ", "a b\nc", [2]int{0, 1}, ""},
		{"VjjJ joins three lines", "a\nb\nc\nd", [2]int{0, 0}, "VjjJ", "a b c\nd", [2]int{0, 3}, ""},
		{"vp replaces the selection", "foo bar", [2]int{0, 0}, "yiwwviwp", "foo foo", [2]int{0, 6}, ""},
		{"vp puts the replaced text in the register", "foo bar", [2]int{0, 0}, "yiwwviwp$p", "foo foobar", [2]int{0, 9}, ""},
		{"Vp replaces lines", "a\nb", [2]int{0, 0}, "yyjVp", "a\na", [2]int{1, 0}, ""},
		{"vp with a line register splits the line", "ab", [2]int{0, 0}, "yyvp", "\nab\nb", [2]int{1, 0}, ""},
		{"vip", "a\nb\n\nc", [2]int{0, 0}, "vipd", "\nc", [2]int{0, 0}, ""},
		{"vi( selects inside", "f(a, b)", [2]int{0, 3}, "vi(d", "f()", [2]int{0, 2}, ""},
		{"va\"", `x "a" y`, [2]int{0, 3}, "va\"d", "x y", [2]int{0, 2}, ""},
		{"v with a count motion", "abcdef", [2]int{0, 0}, "v3ld", "ef", [2]int{0, 0}, ""},
		{"v then Esc leaves at the head", "abc", [2]int{0, 0}, "vl<Esc>", "abc", [2]int{0, 1}, ""},
		{"o swaps the ends", "abcdef", [2]int{0, 1}, "vllohd", "ef", [2]int{0, 0}, ""},
		{"visual search", "ab foo cd", [2]int{0, 0}, "v/«foo»d", "oo cd", [2]int{0, 0}, ""},
		{"ctrl+a in visual", "1\n1", [2]int{0, 0}, "Vj<C-a>", "2\n2", [2]int{0, 0}, ""},
		{"V then v switches the mode", "abc\ndef", [2]int{0, 1}, "Vvd", "ac\ndef", [2]int{0, 1}, "vi_normal"},
	})
}

func TestViVisualModes(t *testing.T) {
	t.Parallel()
	v := newVi(t, "abc\ndef\nghi").at(0, 1)
	v.keys("v")
	if v.mode() != "vi_visual" || v.label() != "-- VISUAL --" {
		t.Fatalf("after v: mode %q label %q", v.mode(), v.label())
	}
	if sel, ok := v.selection(); !ok || sel.From != (pluginapi.Pos{Line: 0, Col: 1}) || sel.To != (pluginapi.Pos{Line: 0, Col: 2}) {
		t.Fatalf("selection after v = %+v %v, want the character under the caret", sel, ok)
	}
	v.keys("j")
	if sel, ok := v.selection(); !ok || sel.From != (pluginapi.Pos{Line: 0, Col: 1}) || sel.To != (pluginapi.Pos{Line: 1, Col: 2}) {
		t.Fatalf("selection after vj = %+v %v", sel, ok)
	}
	v.keys("V")
	if v.label() != "-- VISUAL LINE --" {
		t.Fatalf("label = %q", v.label())
	}
	if sel, ok := v.selection(); !ok || sel.From != (pluginapi.Pos{Line: 0, Col: 0}) || sel.To != (pluginapi.Pos{Line: 1, Col: 3}) {
		t.Fatalf("selection after V = %+v %v, want the two lines", sel, ok)
	}
	v.keys("<Esc>")
	if v.mode() != "vi_normal" || v.label() != "-- NORMAL --" {
		t.Fatalf("after Esc: mode %q label %q", v.mode(), v.label())
	}
	if _, ok := v.selection(); ok {
		t.Fatal("a selection is left after Esc")
	}
	// gv selects the last area again
	v.keys("gv")
	if v.mode() != "vi_visual" {
		t.Fatalf("mode after gv = %q", v.mode())
	}
	if sel, ok := v.selection(); !ok || sel.From.Line != 0 || sel.To.Line != 1 {
		t.Fatalf("selection after gv = %+v %v", sel, ok)
	}
}

func TestViVisualBlock(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"block delete", "abcd\nefgh\nijkl", [2]int{0, 1}, "<C-v>jld", "ad\neh\nijkl", [2]int{0, 1}, ""},
		{"block yank and put", "ab\ncd", [2]int{0, 0}, "<C-v>jy$p", "aba\ncdc", [2]int{0, 2}, ""},
		{"block I", "abcd\nefgh\nijkl", [2]int{0, 1}, "<C-v>jI-<Esc>", "a-bcd\ne-fgh\nijkl", [2]int{0, 1}, ""},
		{"block A", "abcd\nefgh\nijkl", [2]int{0, 1}, "<C-v>jA!<Esc>", "ab!cd\nef!gh\nijkl", [2]int{0, 1}, ""},
		{"block $A", "ab\nefgh", [2]int{0, 0}, "<C-v>j$A;<Esc>", "ab;\nefgh;", [2]int{0, 0}, ""},
		{"block c", "abcd\nefgh", [2]int{0, 1}, "<C-v>jlcX<Esc>", "aXd\neXh", [2]int{0, 1}, ""},
		{"block r", "abcd\nefgh", [2]int{0, 1}, "<C-v>jlrx", "axxd\nexxh", [2]int{0, 1}, ""},
		{"block U", "abcd\nefgh", [2]int{0, 1}, "<C-v>jlU", "aBCd\neFGh", [2]int{0, 1}, ""},
		{"block with short lines", "abcd\nx\nijkl", [2]int{0, 2}, "<C-v>jjd", "abd\nx\nijl", [2]int{0, 2}, ""},
	})
}

// TestViVisualKeepsTheWantedColumn: j and k in visual mode aim at the column the head was
// moved to by a horizontal move, not at the clamped column of the short line it crossed
// (S22-3).
func TestViVisualKeepsTheWantedColumn(t *testing.T) {
	t.Parallel()
	const text = "abcdef ghi\nxy\n\n0123456789 abc\nlast line here"
	runViCases(t, []viCase{
		{"vjjjd", text, [2]int{0, 3}, "vjjjd", "abc456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"vjkd", text, [2]int{0, 3}, "vjkd", "abcef ghi\nxy\n\n0123456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"vkkkd from below", text, [2]int{3, 3}, "vkkkd", "abc456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"a count keeps it too", text, [2]int{0, 3}, "v3jd", "abc456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"a horizontal move sets it", "abcdef\nxy\n0123456789", [2]int{0, 5}, "vjhjd", "abcde123456789", [2]int{0, 5}, ""},
		{"Vjjj ignores it", text, [2]int{0, 3}, "Vjjjd", "last line here", [2]int{0, 0}, ""},
		{"block I", text, [2]int{0, 3}, "<C-v>jjjIZ<Esc>", "abcZdef ghi\nxy\n\n012Z3456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"block c", text, [2]int{0, 3}, "<C-v>jjjcZ<Esc>", "abcZef ghi\nxy\n\n012Z456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"block r", text, [2]int{0, 3}, "<C-v>jjjrX", "abcXef ghi\nxy\n\n012X456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"block d going up", text, [2]int{3, 3}, "<C-v>kkkd", "abcef ghi\nxy\n\n012456789 abc\nlast line here", [2]int{0, 3}, ""},
		{"block $ stays to the end of the line", "ab\ncdef\ng", [2]int{0, 0}, "<C-v>$jjA!<Esc>", "ab!\ncdef!\ng!", [2]int{0, 0}, ""},
		{"v$ then j keeps the end", "abc\nd\nefg", [2]int{0, 1}, "v$jd", "aefg", [2]int{0, 1}, ""},
	})
}
