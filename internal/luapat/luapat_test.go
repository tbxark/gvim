package luapat

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// convRow is one conformance row. want is the canonical rendering of what
// Lua 5.1 returns: values separated by spaces, strings quoted with \xHH for
// bytes outside 0x20..0x7e, "nil" for no match and "error" for a raised error.
// gmatch renders every iteration as "[values]"; gsub renders `"result" count`.
type convRow struct {
	op      string // find, match, gmatch, gsub
	s, p    string
	init    int // 0: absent
	plain   bool
	repl    string
	fn      string // named replacement function or table for gsub
	max     int    // gsub limit, 0: absent (negative: replace nothing)
	want    string
	wantErr string // optional substring of the error text
}

func quoteLua(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c >= 32 && c < 127:
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func capText(c Capture) string {
	if c.IsPos {
		return strconv.Itoa(c.Pos)
	}
	return c.Str
}

func renderCaps(caps []Capture) []string {
	out := make([]string, len(caps))
	for i, c := range caps {
		if c.IsPos {
			out[i] = strconv.Itoa(c.Pos)
		} else {
			out[i] = quoteLua(c.Str)
		}
	}
	return out
}

// replacement functions and the table of the gsub rows, mirrored in the Lua
// oracle that produced the wants.
var gsubTable = map[string]string{"a": "[A]", "hello": "HI", "12": "X"}

func namedFunc(name string) func(Result) (string, bool, error) {
	switch name {
	case "upper":
		return func(r Result) (string, bool, error) { return strings.ToUpper(capText(r.Caps[0])), true, nil }
	case "all":
		return func(r Result) (string, bool, error) {
			parts := make([]string, len(r.Caps))
			for i, c := range r.Caps {
				parts[i] = capText(c)
			}
			return "<" + strings.Join(parts, ",") + ">", true, nil
		}
	case "nil", "false":
		return func(Result) (string, bool, error) { return "", false, nil }
	case "num":
		return func(Result) (string, bool, error) { return "42", true, nil }
	case "tbl":
		return func(r Result) (string, bool, error) {
			if r.Caps[0].IsPos {
				return "", false, nil
			}
			v, ok := gsubTable[r.Caps[0].Str]
			return v, ok, nil
		}
	}
	panic("unknown function " + name)
}

// runRow executes a row against the package and renders the outcome.
func runRow(r convRow) (string, error) {
	o := Options{}
	switch r.op {
	case "find":
		init := r.init
		if init == 0 {
			init = 1
		}
		res, ok, err := Find(r.s, r.p, init, r.plain, o)
		if err != nil {
			return "error", err
		}
		if !ok {
			return "nil", nil
		}
		return strings.Join(append([]string{strconv.Itoa(res.Start + 1), strconv.Itoa(res.End)}, renderCaps(res.Caps)...), " "), nil
	case "match":
		init := r.init
		if init == 0 {
			init = 1
		}
		res, ok, err := Match(r.s, r.p, init, o)
		if err != nil {
			return "error", err
		}
		if !ok {
			return "nil", nil
		}
		return strings.Join(renderCaps(res.Caps), " "), nil
	case "gmatch":
		it, err := GMatch(r.s, r.p, o)
		if err != nil {
			return "error", err
		}
		var b strings.Builder
		for n := 0; ; n++ {
			res, ok, err := it.Next()
			if err != nil {
				return "error", err
			}
			if !ok {
				return b.String(), nil
			}
			if n > 1000 {
				return "error", errors.New("loop")
			}
			b.WriteString("[" + strings.Join(renderCaps(res.Caps), " ") + "]")
		}
	case "gsub":
		rep := Replacement{Template: r.repl}
		if r.fn != "" {
			rep.Func = namedFunc(r.fn)
		}
		max := r.max
		if max == 0 {
			max = Unlimited
		}
		out, n, err := Gsub(r.s, r.p, rep, max, o)
		if err != nil {
			return "error", err
		}
		return quoteLua(out) + " " + strconv.Itoa(n), nil
	}
	panic("unknown op " + r.op)
}

// TestConformance runs the table of conformance_rows_test.go. The wants were
// produced by running every row through LuaJIT 2.1 (whose string library is
// the Lua 5.1 lstrlib) and checking that luapat agrees; the places where
// LuaJIT deviates from PUC Lua 5.1 are pinned by TestConformancePUC instead.
func TestConformance(t *testing.T) {
	if len(conformanceRows) < 150 {
		t.Fatalf("conformance table has %d rows, want >= 150", len(conformanceRows))
	}
	for i, r := range conformanceRows {
		got, err := runRow(r)
		if got != r.want {
			t.Errorf("row %d %s(%q, %q) init=%d plain=%v repl=%q fn=%q max=%d: got %s (err %v), want %s",
				i, r.op, r.s, r.p, r.init, r.plain, r.repl, r.fn, r.max, got, err, r.want)
			continue
		}
		if r.want == "error" && err == nil {
			t.Errorf("row %d: error rendering without an error value", i)
		}
	}
}

// TestConformancePUC pins behaviour where PUC Lua 5.1 differs from LuaJIT or
// from the stock gopher-lua engine, and the one deliberate deviation.
func TestConformancePUC(t *testing.T) {
	rows := []convRow{
		// 5.1 gmatch does not treat '^' as an anchor: it is an ordinary byte
		{op: "gmatch", s: "aaa", p: "^a", want: ""},
		{op: "gmatch", s: "a^a^a", p: "^a", want: `["^a"]["^a"]`},
		{op: "gmatch", s: "a^a", p: "(^)", want: `["^"]`},
		// 5.1 clamps init to the subject: an empty match at the end
		{op: "find", s: "abc", p: "", init: 10, want: "4 3"},
		{op: "find", s: "abc", p: "c", init: 10, want: "nil"},
		{op: "match", s: "abc", p: "", init: 10, want: `""`},
		{op: "find", s: "abc", p: "", init: -10, want: "1 0"},
		{op: "find", s: "abc", p: "", init: 3, want: "3 2"},
		{op: "find", s: "abc", p: "", init: 4, want: "4 3"},
		// gmatch advances one byte after an empty match and tries the end
		{op: "gmatch", s: "ab", p: "%a*", want: `["ab"][""]`},
		{op: "gmatch", s: "ab", p: "b*", want: `[""]["b"][""]`},
		// gsub: a non-empty match is skipped; an empty one copies a byte, and the
		// empty match at the end of the subject counts too
		{op: "gsub", s: "abc", p: "b*", repl: "-", want: `"-a--c-" 4`},
		{op: "gsub", s: "abc", p: "%w*", repl: "-", want: `"--" 2`},
		// a replacement escape followed by a non-digit stands for itself
		{op: "gsub", s: "abc", p: "b", repl: "%x", want: `"axc" 1`},
		{op: "gsub", s: "abc", p: "b", repl: "%%", want: `"a%c" 1`},
		// deliberate deviation: PUC 5.1 appends a NUL byte for a trailing '%'
		// in the replacement, 5.2+ and LuaJIT raise; so does luapat
		{op: "gsub", s: "abc", p: "b", repl: "%", want: "error", wantErr: "invalid use of '%' in replacement string"},
		// %z is the NUL byte class of 5.1
		{op: "find", s: "a\x00b", p: "%z", want: "2 2"},
		{op: "find", s: "a\x00b", p: "%Z+", want: "1 1"},
		// the frontier sees the byte before init, not the start of the slice
		{op: "find", s: "ab", p: "%f[%a]b", init: 2, want: "nil"},
		{op: "find", s: "a-b", p: "%f[%a]b", init: 3, want: "3 3"},
		// back reference to a position capture never matches
		{op: "find", s: "aa", p: "()%1", want: "nil"},
	}
	for i, r := range rows {
		got, err := runRow(r)
		if got != r.want {
			t.Errorf("row %d %s(%q, %q) init=%d repl=%q: got %s (%v), want %s", i, r.op, r.s, r.p, r.init, r.repl, got, err, r.want)
		}
		if r.wantErr != "" && (err == nil || !strings.Contains(err.Error(), r.wantErr)) {
			t.Errorf("row %d: err = %v, want %q", i, err, r.wantErr)
		}
	}
}

func TestErrorMessages(t *testing.T) {
	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"trailing_escape", func() error { _, _, e := Find("a", "%", 1, false, Options{}); return e }, "malformed pattern (ends with '%')"},
		{"missing_bracket", func() error { _, _, e := Find("a", "[a", 1, false, Options{}); return e }, "malformed pattern (missing ']')"},
		{"missing_bracket_escape", func() error { _, _, e := Find("a", "[a%]", 1, false, Options{}); return e }, "malformed pattern (missing ']')"},
		{"frontier_without_set", func() error { _, _, e := Find("a", "%fa", 1, false, Options{}); return e }, "missing '[' after '%f' in pattern"},
		{"balance_without_args", func() error { _, _, e := Find("a", "%bx", 1, false, Options{}); return e }, "missing arguments to '%b'"},
		{"unmatched_close", func() error { _, _, e := Match("a", "a)", 1, Options{}); return e }, "invalid pattern capture"},
		{"unfinished_capture", func() error { _, _, e := Find("a", "(a", 1, false, Options{}); return e }, "unfinished capture"},
		{"bad_back_reference", func() error { _, _, e := Find("a", "(a)%2", 1, false, Options{}); return e }, "invalid capture index"},
		{"back_reference_to_open", func() error { _, _, e := Find("a", "(a%1)", 1, false, Options{}); return e }, "invalid capture index"},
		{"too_many_captures", func() error {
			_, _, e := Find("a", strings.Repeat("(", 33)+"a"+strings.Repeat(")", 33), 1, false, Options{})
			return e
		}, "too many captures"},
		{"bad_capture_in_replacement", func() error {
			_, _, e := Gsub("a", "a", Replacement{Template: "%2"}, Unlimited, Options{})
			return e
		}, "invalid capture index"},
		{"gmatch_error_on_next", func() error {
			it, e := GMatch("a", "%", Options{})
			if e != nil {
				return e
			}
			_, _, e = it.Next()
			return e
		}, "malformed pattern (ends with '%')"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			var pe *PatternError
			if !errors.As(err, &pe) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want a PatternError containing %q", err, c.want)
			}
		})
	}
	// errors are lazy like in C Lua: a faulty tail that is never reached is fine
	if _, ok, err := Find("b", "a%", 1, false, Options{}); err != nil || ok {
		t.Errorf("unreached malformed tail: %v %v", ok, err)
	}
}

func TestResultShape(t *testing.T) {
	r, ok, err := Find("key = value", "(%w+)%s*=%s*()(%w+)", 1, false, Options{})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	want := []Capture{{Str: "key"}, {Pos: 7, IsPos: true}, {Str: "value"}}
	if r.Start != 0 || r.End != 11 || len(r.Caps) != 3 || r.Caps[0] != want[0] || r.Caps[1] != want[1] || r.Caps[2] != want[2] {
		t.Errorf("Find = %+v", r)
	}
	if r, _, _ = Find("abc", "b", 1, false, Options{}); len(r.Caps) != 0 || r.Start != 1 || r.End != 2 {
		t.Errorf("Find without captures = %+v", r)
	}
	if r, _, _ = Match("abc", "b", 1, Options{}); len(r.Caps) != 1 || r.Caps[0].Str != "b" {
		t.Errorf("Match without captures = %+v", r)
	}
	if _, ok, _ = Find("abc", "x", 1, false, Options{}); ok {
		t.Error("miss reported a match")
	}
}

func TestGsubCallbacks(t *testing.T) {
	boom := errors.New("boom")
	var seen []Result
	out, n, err := Gsub("a1b22", "(%a)(%d+)", Replacement{Func: func(r Result) (string, bool, error) {
		seen = append(seen, r)
		return "", r.Caps[0].Str == "a", nil // keep the "b22" match
	}}, Unlimited, Options{})
	if err != nil || out != "b22" || n != 2 || len(seen) != 2 || seen[1].Start != 2 || seen[1].Caps[1].Str != "22" {
		t.Errorf("out=%q n=%d err=%v seen=%+v", out, n, err, seen)
	}
	_, _, err = Gsub("aaa", "a", Replacement{Func: func(Result) (string, bool, error) { return "", false, boom }}, Unlimited, Options{})
	if err != boom {
		t.Errorf("callback error not returned unchanged: %v", err)
	}
	if out, n, _ := Gsub("aaa", "a", Replacement{Template: "b"}, -1, Options{}); out != "aaa" || n != 0 {
		t.Errorf("negative limit: %q %d", out, n)
	}
}

func TestIterStops(t *testing.T) {
	it, err := GMatch("ab", "%a", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, ok, _ := it.Next(); !ok {
			t.Fatalf("match %d missing", i)
		}
	}
	for i := 0; i < 3; i++ {
		if _, ok, err := it.Next(); ok || err != nil {
			t.Fatalf("exhausted iterator returned %v %v", ok, err)
		}
	}
}
