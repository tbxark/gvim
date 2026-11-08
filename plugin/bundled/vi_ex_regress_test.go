package bundled_test

import (
	"strings"
	"testing"
)

func TestViGlobalMatchesEmptyPatterns(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		// The caret of :g/pat/d is on the line that took the place of the last deleted one, as in
		// Vim (these two rows pinned the first deleted line before).
		{":g/^$/d", "a\n\nb\n\n\nc\n", [2]int{0, 0}, ":«g/^$/d»", "a\nb\nc\n", [2]int{2, 0}, ""},
		{":g/^\\s*$/d", "a\n\nb\n \n\nc\n", [2]int{0, 0}, ":«g/^\\s*$/d»", "a\nb\nc\n", [2]int{2, 0}, ""},
		{":g/\\v^$/d", "a\n\nb\n", [2]int{0, 0}, ":«g/\\v^$/d»", "a\nb\n", [2]int{1, 0}, ""},
		{":g/^/d empties the buffer", "a\nb\n", [2]int{0, 0}, ":«g/^/d»", "", [2]int{0, 0}, ""},
		{":g/$/s", "a\nb\n", [2]int{0, 0}, ":«g/$/s/$/;/»", "a;\nb;\n", [2]int{1, 0}, ""},
		{":g/^/s/^/#/", "a\nb\n", [2]int{0, 0}, ":«g/^/s/^/#/»", "#a\n#b\n", [2]int{1, 0}, ""},
		{":v/./d", "a\n\nb\n\n", [2]int{0, 0}, ":«v/./d»", "a\nb\n", [2]int{1, 0}, ""},
		{":g/^/m0 reverses the buffer", "1\n2\n3\n", [2]int{0, 0}, ":«g/^/m0»", "3\n2\n1\n", [2]int{0, 0}, ""},
		{":g/pat/m$ moves the lines to the end", "a1\nb\na2\nc\n", [2]int{0, 0}, ":«g/a/m$»", "b\nc\na1\na2\n", [2]int{3, 0}, ""},
		{":g/pat/t. copies the lines after themselves", "a\nb\n", [2]int{0, 0}, ":«g/b/t.»", "a\nb\nb\n", [2]int{2, 0}, ""},
		{":g/pat/t$", "a\nb\n", [2]int{0, 0}, ":«g/a/t$»", "a\nb\na\n", [2]int{2, 0}, ""},
	})
}

func TestViIntervalPatterns(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"\\{n}", "aabb a\n", [2]int{0, 0}, ":«%s/a\\{2}/X/»", "Xbb a\n", [2]int{0, 0}, ""},
		{"\\{n\\}", "aabb a\n", [2]int{0, 0}, ":«%s/a\\{2\\}/X/»", "Xbb a\n", [2]int{0, 0}, ""},
		{"\\{n,m}", "aaab a\n", [2]int{0, 0}, ":«%s/a\\{1,2}/X/g»", "XXb X\n", [2]int{0, 0}, ""},
		{"\\{n,}", "aaab a\n", [2]int{0, 0}, ":«%s/a\\{2,}/X/g»", "Xb a\n", [2]int{0, 0}, ""},
		{"\\{,m}", "aaab\n", [2]int{0, 0}, ":«%s/a\\{,2}/X/»", "Xab\n", [2]int{0, 0}, ""},
		{"\\{-n,m} is not greedy", "aaa\n", [2]int{0, 0}, ":«%s/a\\{-1,}/X/»", "Xaa\n", [2]int{0, 0}, ""},
		{"on any character", "aabb a\n", [2]int{0, 0}, ":«%s/.\\{3}/X/g»", "XX\n", [2]int{0, 0}, ""},
		{"in a search", "xx aab\n", [2]int{0, 0}, "/«a\\{2}»", "xx aab\n", [2]int{0, 3}, ""},
		{"in :g", "aa\nab\n", [2]int{0, 0}, ":«g/a\\{2}/d»", "ab\n", [2]int{0, 0}, ""},
		{"very magic", "aabb a\n", [2]int{0, 0}, ":«%s/\\va{2}/X/»", "Xbb a\n", [2]int{0, 0}, ""},
		{"a plain brace stays literal", "a{2}\n", [2]int{0, 0}, ":«%s/a{2}/X/»", "X\n", [2]int{0, 0}, ""},
	})
}

func TestViSubstituteFlags(t *testing.T) {
	t.Parallel()
	const text = "one one\nx one\n"
	runViCases(t, []viCase{
		{"n only counts", text, [2]int{0, 0}, ":«%s/one/1/gn»", text, [2]int{0, 0}, ""},
		{"c: n declines, y accepts", text, [2]int{0, 0}, ":«%s/one/1/gc»<pick:1><pick:0><pick:0>", "one 1\nx 1\n", [2]int{1, 0}, ""},
		{"c: a replaces the rest", text, [2]int{0, 0}, ":«%s/one/1/gc»<pick:1><pick:2>", "one 1\nx 1\n", [2]int{1, 0}, ""},
		{"c: q stops", text, [2]int{0, 0}, ":«%s/one/1/gc»<pick:0><pick:3>", "1 one\nx one\n", [2]int{0, 0}, ""},
		{"c: l replaces this one and stops", text, [2]int{0, 0}, ":«%s/one/1/gc»<pick:4>", "1 one\nx one\n", [2]int{0, 0}, ""},
		{"c: all declined changes nothing", "a a\n", [2]int{0, 0}, ":«s/a/b/gc»<pick:1><pick:1>", "a a\n", [2]int{0, 0}, ""},
		{"& keeps the flags of the last :s", "a a\nb a a\n", [2]int{0, 0}, ":«s/a/X/g»:«2&&»", "X X\nb X X\n", [2]int{1, 0}, ""},
		{":&& repeats with the replacement", "one two one\nfour one\n", [2]int{0, 0}, ":«s/one/1/»:«&&»", "1 two 1\nfour one\n", [2]int{0, 0}, ""},
		{":& repeats without flags", "a a\na a\n", [2]int{0, 0}, ":«s/a/X/g»j:«&»", "X X\nX a\n", [2]int{1, 0}, ""},
	})
	v := newVi(t, text)
	v.keys(":«%s/one/1/gn»")
	if got := v.notices(); !strings.Contains(got, "3 matches on 2 lines") {
		t.Errorf("notices = %q, want the count of the matches", got)
	}
}

func TestViSubstituteCaseEscapes(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"\\u", "foo bar\n", [2]int{0, 0}, ":«%s/\\w\\+/\\u&/g»", "Foo Bar\n", [2]int{0, 0}, ""},
		{"\\u on the start of a word", "foo bar\n", [2]int{0, 0}, ":«%s/\\<./\\u&/g»", "Foo Bar\n", [2]int{0, 0}, ""},
		{"\\U", "foo bar\n", [2]int{0, 0}, ":«%s/\\w\\+/\\U&/g»", "FOO BAR\n", [2]int{0, 0}, ""},
		{"\\U to \\E", "one two\n", [2]int{0, 0}, ":«%s/\\(\\w\\+\\) \\(\\w\\+\\)/\\U\\1\\E \\2/»", "ONE two\n", [2]int{0, 0}, ""},
		{"\\L", "FOO BAR\n", [2]int{0, 0}, ":«%s/\\w\\+/\\L&/g»", "foo bar\n", [2]int{0, 0}, ""},
		{"\\l", "FOO\n", [2]int{0, 0}, ":«%s/\\w\\+/\\l&/»", "fOO\n", [2]int{0, 0}, ""},
		{"\\u\\L is a capital and lower case", "fOO bAR\n", [2]int{0, 0}, ":«%s/\\w\\+/\\u\\L&/g»", "Foo Bar\n", [2]int{0, 0}, ""},
		{"\\u on literal text", "x\n", [2]int{0, 0}, ":«s/x/\\uabc/»", "Abc\n", [2]int{0, 0}, ""},
		{"\\u on a multibyte letter", "élan\n", [2]int{0, 0}, ":«s/./\\u&/»", "Élan\n", [2]int{0, 0}, ""},
		{"a plain replacement is unchanged", "foo\n", [2]int{0, 0}, ":«s/o/0/g»", "f00\n", [2]int{0, 0}, ""},
		{"an expression is refused", "foo\n", [2]int{0, 0}, ":«s/o/\\=1/»", "foo\n", [2]int{0, 0}, ""},
	})
}

func TestViGlobalKeepsItsCommandWritten(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"an escaped delimiter in the replacement", "a b\n", [2]int{0, 0}, ":«g/b/s/$/ \\/\\/ z/»", "a b // z\n", [2]int{0, 0}, ""},
		{"an escaped delimiter in the pattern of :s", "a b\n", [2]int{0, 0}, ":«g/b/s/b/x\\/y/»", "a x/y\n", [2]int{0, 0}, ""},
		{"an escaped delimiter in the pattern of :g", "a/b\nc\n", [2]int{0, 0}, ":«g/a\\/b/d»", "c\n", [2]int{0, 0}, ""},
		{":%s still takes it", "a b\n", [2]int{0, 0}, ":«%s/$/ \\/\\/ z/»", "a b // z\n", [2]int{0, 0}, ""},
	})
}

func TestViBarChainsCommands(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{":s|s", "one two\n", [2]int{0, 0}, ":«s/one/1/|s/two/2/»", "1 2\n", [2]int{0, 0}, ""},
		{":g with a chain of :s", "xac\nyac\n", [2]int{0, 0}, ":«g/x/s/a/b/|s/c/d/»", "xbd\nyac\n", [2]int{0, 0}, ""},
		{":d|d", "a\nb\nc\nd\n", [2]int{0, 0}, ":«1d|1d»", "c\nd\n", [2]int{0, 0}, ""},
		{"\\| in a pattern is an alternation", "a b c\n", [2]int{0, 0}, ":«s/a\\|b/X/g»", "X X c\n", [2]int{0, 0}, ""},
		{"a bar inside the pattern of :s is text", "a|b\n", [2]int{0, 0}, ":«s/a|b/X/»", "X\n", [2]int{0, 0}, ""},
	})
}

func TestViCountedOpen(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{"3o with nothing typed", "a\nb\n", [2]int{0, 0}, "3o<Esc>", "a\n\n\n\nb\n", [2]int{3, 0}, ""},
		{"3O with nothing typed", "a\nb\n", [2]int{1, 0}, "3O<Esc>", "a\n\n\n\nb\n", [2]int{1, 0}, ""},
		{"2o with text", "a\n", [2]int{0, 0}, "2ox<Esc>", "a\nx\nx\n", [2]int{2, 0}, ""},
		{". repeats 3o", "a\nb\n", [2]int{0, 0}, "3o<Esc>G.", "a\n\n\n\nb\n\n\n\n", [2]int{7, 0}, ""},
	})
}

func TestViStarIgnoresSmartCase(t *testing.T) {
	t.Parallel()
	const text = "Alpha beta alpha\n"
	runViCases(t, []viCase{
		{"* finds the other case", text, [2]int{0, 0}, "*", text, [2]int{0, 11}, ""},
		{"# finds the other case", text, [2]int{0, 11}, "#", text, [2]int{0, 0}, ""},
		{"n after * keeps ignoring smart case", "Alpha x alpha y ALPHA\n", [2]int{0, 0}, "*n", "Alpha x alpha y ALPHA\n", [2]int{0, 16}, ""},
		{"a typed search still honours it", text, [2]int{0, 0}, "/«Alpha»", text, [2]int{0, 0}, ""},
	})
}

func TestViWriteRange(t *testing.T) {
	t.Parallel()
	v := newVi(t, "1\n2\n3\n4\n")
	v.keys(":«2,3w part.txt»")
	if got, ok := v.h.OS.File("/work/part.txt"); !ok || got != "2\n3\n" {
		t.Fatalf("part.txt = %q (%v), want the two lines", got, ok)
	}
	if n := v.host.CallCount("Save"); n != 0 {
		t.Errorf("Save calls = %d, a partial write must not save the buffer", n)
	}
	v.h.OS.SetFile("/work/a.txt", "x\n")
	v.keys(":«w >> a.txt»")
	if got, _ := v.h.OS.File("/work/a.txt"); got != "x\n1\n2\n3\n4\n" {
		t.Errorf("a.txt = %q, want the buffer appended", got)
	}
	v.keys(":«$w >> a.txt»")
	if got, _ := v.h.OS.File("/work/a.txt"); got != "x\n1\n2\n3\n4\n4\n" {
		t.Errorf("a.txt = %q, want the last line appended", got)
	}
	if _, ok := v.h.OS.File("/work/>> a.txt"); ok {
		t.Error(">> became a file name")
	}
	v.keys(":«2,3w»")
	if !strings.Contains(v.notices(), "E140") {
		t.Errorf("notices = %q, want E140", v.notices())
	}
}
