package search

import (
	"errors"
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestReplace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		q       Query
		repl    string
		m       Match
		want    string // the text after the edit
		wantRet string // the expanded replacement
	}{
		{
			name:    "literal replacement is not expanded",
			text:    "x y",
			q:       Query{Text: "x", CaseSensitive: true},
			repl:    "$1",
			m:       m(0, 0, 0, 1),
			want:    "$1 y",
			wantRet: "$1",
		},
		{
			name:    "regex expands capture groups",
			text:    "alice@host bob@box",
			q:       Query{Text: `(\w+)@(\w+)`, CaseSensitive: true, Regex: true},
			repl:    "$2/$1",
			m:       m(0, 11, 0, 18),
			want:    "alice@host box/bob",
			wantRet: "box/bob",
		},
		{
			name:    "escapes in a regex replacement",
			text:    "a b",
			q:       Query{Text: "a", CaseSensitive: true, Regex: true},
			repl:    `x\ny\tz\\w\rq`,
			m:       m(0, 0, 0, 1),
			want:    "x\ny\tz\\w\rq b",
			wantRet: "x\ny\tz\\w\rq",
		},
		{
			name:    "unknown escape and trailing backslash pass through",
			text:    "a",
			q:       Query{Text: "a", CaseSensitive: true, Regex: true},
			repl:    `\q\`,
			m:       m(0, 0, 0, 1),
			want:    `\q\`,
			wantRet: `\q\`,
		},
		{
			name:    "literal replacement keeps backslashes",
			text:    "a",
			q:       Query{Text: "a", CaseSensitive: true},
			repl:    `\n`,
			m:       m(0, 0, 0, 1),
			want:    `\n`,
			wantRet: `\n`,
		},
		{
			name:    "group number followed by letters",
			text:    "10",
			q:       Query{Text: `(\d+)`, CaseSensitive: true, Regex: true},
			repl:    "$1px oo$1oo",
			m:       m(0, 0, 0, 2),
			want:    "10px oo10oo",
			wantRet: "10px oo10oo",
		},
		{
			name:    "escaped dollar stays literal",
			text:    "10",
			q:       Query{Text: `(\d+)`, CaseSensitive: true, Regex: true},
			repl:    "$$1 ${1}x",
			m:       m(0, 0, 0, 2),
			want:    "$1 10x",
			wantRet: "$1 10x",
		},
		{
			name:    "regex unmatched group expands to empty",
			text:    "ab",
			q:       Query{Text: `a(x)?b`, CaseSensitive: true, Regex: true},
			repl:    "[$1]",
			m:       m(0, 0, 0, 2),
			want:    "[]",
			wantRet: "[]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := buffer.New(tt.text)
			got, err := Replace(b, tt.q, tt.repl, tt.m)
			if err != nil {
				t.Fatalf("Replace() error = %v", err)
			}
			if got != tt.wantRet {
				t.Fatalf("Replace() returned %q, want %q", got, tt.wantRet)
			}
			if b.Text() != tt.want {
				t.Fatalf("buffer = %q, want %q", b.Text(), tt.want)
			}
		})
	}
}

func TestReplaceNotAMatch(t *testing.T) {
	t.Parallel()
	b := buffer.New("abc")
	_, err := Replace(b, Query{Text: "x"}, "y", m(0, 0, 0, 1))
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("Replace() error = %v, want ErrNoMatch", err)
	}
	if b.Text() != "abc" {
		t.Fatalf("buffer changed to %q on a failed Replace", b.Text())
	}
}

func TestReplaceUndo(t *testing.T) {
	t.Parallel()
	b := buffer.New("foo bar")
	if _, err := Replace(b, Query{Text: "foo", CaseSensitive: true}, "baz", m(0, 0, 0, 3)); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if b.Text() != "baz bar" {
		t.Fatalf("after Replace, buffer = %q, want %q", b.Text(), "baz bar")
	}
	undoAll(b)
	if b.Text() != "foo bar" {
		t.Fatalf("after undo, buffer = %q, want %q", b.Text(), "foo bar")
	}
}

func TestReplaceAll(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		text      string
		q         Query
		repl      string
		wantCount int
		want      string
	}{
		{
			name:      "literal",
			text:      "cat concat cat.",
			q:         Query{Text: "cat", CaseSensitive: true, WholeWord: true},
			repl:      "dog",
			wantCount: 2,
			want:      "dog concat dog.",
		},
		{
			name:      "regex groups in every match",
			text:      "alice@host bob@box",
			q:         Query{Text: `(\w+)@(\w+)`, CaseSensitive: true, Regex: true},
			repl:      "$2/$1",
			wantCount: 2,
			want:      "host/alice box/bob",
		},
		{
			name:      "empty replacement deletes",
			text:      "a-b-c",
			q:         Query{Text: "-", CaseSensitive: true},
			repl:      "",
			wantCount: 2,
			want:      "abc",
		},
		{
			name:      "multi-line replacement shifts later matches",
			text:      "a x\nx x",
			q:         Query{Text: "x", CaseSensitive: true},
			repl:      "LONGER\nZ",
			wantCount: 3,
			want:      "a LONGER\nZ\nLONGER\nZ LONGER\nZ",
		},
		{
			name:      "caret comments every line",
			text:      "a\nb\n\nc",
			q:         Query{Text: "^", Regex: true},
			repl:      "// ",
			wantCount: 4,
			want:      "// a\n// b\n// \n// c",
		},
		{
			name:      "dollar appends to every line",
			text:      "a\nb",
			q:         Query{Text: "$", Regex: true},
			repl:      ";",
			wantCount: 2,
			want:      "a;\nb;",
		},
		{
			name:      "star pattern terminates",
			text:      "abc",
			q:         Query{Text: "x*", Regex: true},
			repl:      "-",
			wantCount: 4,
			want:      "-a-b-c-",
		},
		{
			name:      "word boundary",
			text:      "ab cd",
			q:         Query{Text: `\b`, Regex: true},
			repl:      "|",
			wantCount: 4,
			want:      "|ab| |cd|",
		},
		{
			name:      "zero matches leave the buffer alone",
			text:      "abc",
			q:         Query{Text: "xyz"},
			repl:      "q",
			wantCount: 0,
			want:      "abc",
		},
		{
			name:      "empty query replaces nothing",
			text:      "abc",
			q:         Query{},
			repl:      "q",
			wantCount: 0,
			want:      "abc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := buffer.New(tt.text)
			n, err := ReplaceAll(b, tt.q, tt.repl)
			if err != nil {
				t.Fatalf("ReplaceAll() error = %v", err)
			}
			if n != tt.wantCount {
				t.Fatalf("ReplaceAll() count = %d, want %d", n, tt.wantCount)
			}
			if b.Text() != tt.want {
				t.Fatalf("buffer = %q, want %q", b.Text(), tt.want)
			}
		})
	}
}

func TestReplaceAllUndo(t *testing.T) {
	t.Parallel()
	const original = "foo bar foo\nfoo"
	b := buffer.New(original)
	n, err := ReplaceAll(b, Query{Text: "foo", CaseSensitive: true}, "longer")
	if err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}
	if n != 3 {
		t.Fatalf("ReplaceAll() count = %d, want 3", n)
	}
	if want := "longer bar longer\nlonger"; b.Text() != want {
		t.Fatalf("after ReplaceAll, buffer = %q, want %q", b.Text(), want)
	}
	undoAll(b)
	if b.Text() != original {
		t.Fatalf("after undo, buffer = %q, want %q", b.Text(), original)
	}
	if b.Modified() {
		t.Fatalf("buffer still modified after undoing to the start")
	}
}

func TestReplaceAllInvalidRegex(t *testing.T) {
	t.Parallel()
	b := buffer.New("abc")
	n, err := ReplaceAll(b, Query{Text: "[", Regex: true}, "x")
	if err == nil {
		t.Fatalf("ReplaceAll() count = %d, want an error", n)
	}
	if b.Text() != "abc" {
		t.Fatalf("buffer changed to %q on a failed ReplaceAll", b.Text())
	}
}

func TestQueryReplace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		text      string
		q         Query
		repl      string
		decisions []Decision
		wantCount int
		want      string
	}{
		{
			name:      "yes, no, yes, then quit",
			text:      "a a a a",
			q:         Query{Text: "a", CaseSensitive: true},
			repl:      "X",
			decisions: []Decision{DecisionReplace, DecisionSkip, DecisionReplace, DecisionQuit},
			wantCount: 2,
			want:      "X a X a",
		},
		{
			name:      "replace all from a match on",
			text:      "a a a a",
			q:         Query{Text: "a", CaseSensitive: true},
			repl:      "X",
			decisions: []Decision{DecisionSkip, DecisionReplaceAll},
			wantCount: 3,
			want:      "a X X X",
		},
		{
			name:      "multi-line replacement with later matches",
			text:      "x x x",
			q:         Query{Text: "x", CaseSensitive: true},
			repl:      "\n",
			decisions: []Decision{DecisionReplace, DecisionSkip, DecisionReplace},
			wantCount: 2,
			want:      "\n x \n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := buffer.New(tt.text)
			calls := 0
			n, err := QueryReplace(b, tt.q, tt.repl, func(Match, int) Decision {
				d := tt.decisions[calls]
				calls++
				return d
			})
			if err != nil {
				t.Fatalf("QueryReplace() error = %v", err)
			}
			if n != tt.wantCount {
				t.Fatalf("QueryReplace() count = %d, want %d", n, tt.wantCount)
			}
			if b.Text() != tt.want {
				t.Fatalf("buffer = %q, want %q", b.Text(), tt.want)
			}
		})
	}
}

func TestQueryReplaceReportsCurrentPositions(t *testing.T) {
	t.Parallel()
	// Replacing the first "x" with a newline moves the second one to line 1.
	b := buffer.New("x x")
	var seen []Match
	_, err := QueryReplace(b, Query{Text: "x", CaseSensitive: true}, "\n", func(m Match, _ int) Decision {
		seen = append(seen, m)
		return DecisionReplace
	})
	if err != nil {
		t.Fatalf("QueryReplace() error = %v", err)
	}
	want := []Match{m(0, 0, 0, 1), m(1, 1, 1, 2)}
	if !slices.Equal(seen, want) {
		t.Fatalf("decide saw %v, want %v", seen, want)
	}
}

func TestDecisionFromKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  rune
		want Decision
		ok   bool
	}{
		{'y', DecisionReplace, true},
		{'n', DecisionSkip, true},
		{'!', DecisionReplaceAll, true},
		{'q', DecisionQuit, true},
		// Fresh's prompt reads "(a)ll" and "(c)ancel", so a and c are accepted too.
		{'a', DecisionReplaceAll, true},
		{'c', DecisionQuit, true},
		{'Y', DecisionReplace, true},
		{'N', DecisionSkip, true},
		{'A', DecisionReplaceAll, true},
		{'C', DecisionQuit, true},
		{'Q', DecisionQuit, true},
		{'x', DecisionSkip, false},
	}
	for _, tt := range tests {
		got, ok := DecisionFromKey(tt.key)
		if got != tt.want || ok != tt.ok {
			t.Errorf("DecisionFromKey(%q) = (%v, %v), want (%v, %v)", tt.key, got, ok, tt.want, tt.ok)
		}
	}
}

// undoAll reverts every undo step, then reports how many there were.
func undoAll(b *buffer.Buffer) int {
	n := 0
	for {
		if _, ok := b.Undo(); !ok {
			return n
		}
		n++
	}
}
