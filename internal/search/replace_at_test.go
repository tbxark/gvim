package search

import (
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/buffer/buffertest"
)

// ReplaceAt does what Replace does, on a memory buffer and on a large one, for
// every match of the query.
func TestReplaceAtEqualsReplace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		q    Query
		repl string
	}{
		{"literal is not expanded", "x y x\nxx\nz x", Query{Text: "x", CaseSensitive: true}, "$1"},
		{"regex expands capture groups", "alice@host bob@box\ncarol@home", Query{Text: `(\w+)@(\w+)`, CaseSensitive: true, Regex: true}, "$2/$1"},
		{"escapes in a regex replacement", "a b\na", Query{Text: "a", CaseSensitive: true, Regex: true}, `x\ny\tz\\w`},
		{"group number followed by letters", "10 20\n30", Query{Text: `(\d+)`, Regex: true}, "$1px"},
		{"unmatched group is empty", "ab\nab", Query{Text: `a(x)?b`, Regex: true}, "[$1]"},
		{"case insensitive", "Foo foo\nFOO", Query{Text: "foo"}, "bar"},
		{"whole word", "foo foox\nxfoo foo", Query{Text: "foo", WholeWord: true}, "bar"},
		{"line anchors", "bar\nbaz\n bar\nbar bar", Query{Text: `^bar$`, Regex: true}, "BAR"},
		{"multi-line match", "a\nfoo\nbar\nb\nfoo\nbar", Query{Text: `foo\nbar`, Regex: true}, "X"},
		{"multi-line literal", "a\nfoo\nbar\nb\nfoo\nbar", Query{Text: "foo\nbar"}, "X"},
		{"multibyte", "héllo wörld\n日本語 wörld", Query{Text: "wörld"}, "W"},
		{"replacement with a line break", "a b\nc b", Query{Text: "b"}, "1\n2"},
		{"delete", "a b\nc b", Query{Text: " b"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			all, err := Find(buffer.New(tt.text), tt.q)
			if err != nil || len(all) == 0 {
				t.Fatalf("setup: %d matches, %v", len(all), err)
			}
			for i, m := range all {
				ref := buffer.New(tt.text)
				wantRet, err := Replace(ref, tt.q, tt.repl, m)
				if err != nil {
					t.Fatal(err)
				}
				big, _ := largeBuffer(t, tt.text)
				for name, b := range map[string]*buffer.Buffer{"memory": buffer.New(tt.text), "large": big} {
					ret, err := ReplaceAt(b, tt.q, tt.repl, m)
					if err != nil || ret != wantRet {
						t.Fatalf("%s match %d: ReplaceAt = %q, %v; want %q", name, i, ret, err, wantRet)
					}
					if got, want := b.Text(), ref.Text(); got != want {
						t.Fatalf("%s match %d: text = %q, want %q", name, i, got, want)
					}
					// One undo step reverts the replacement, one redo repeats it.
					b.Undo()
					if b.Text() != tt.text {
						t.Fatalf("%s match %d: after Undo %q, want %q", name, i, b.Text(), tt.text)
					}
					b.Redo()
					if b.Text() != ref.Text() {
						t.Fatalf("%s match %d: after Redo %q, want %q", name, i, b.Text(), ref.Text())
					}
				}
			}
		})
	}
}

func TestReplaceAtNotAMatch(t *testing.T) {
	t.Parallel()
	text := "abc\nxyz\nabc"
	tests := []struct {
		name string
		q    Query
		m    Match
	}{
		{"text differs", Query{Text: "x"}, m(0, 0, 0, 1)},
		{"wrong end", Query{Text: "abc"}, m(0, 0, 0, 2)},
		{"empty query", Query{}, m(0, 0, 0, 0)},
		{"line past the end", Query{Text: "abc"}, m(5, 0, 5, 3)},
		{"negative line", Query{Text: "abc"}, m(-1, 0, 0, 3)},
		{"end before start", Query{Text: "abc"}, m(2, 3, 2, 0)},
		{"column past the line", Query{Text: "abc"}, m(0, 9, 0, 12)},
		{"whole word fails", Query{Text: "bc", WholeWord: true}, m(0, 1, 0, 3)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			big, _ := largeBuffer(t, text)
			for name, b := range map[string]*buffer.Buffer{"memory": buffer.New(text), "large": big} {
				ver := b.Version()
				if _, err := ReplaceAt(b, tt.q, "Q", tt.m); !errors.Is(err, ErrNoMatch) {
					t.Errorf("%s: error = %v, want ErrNoMatch", name, err)
				}
				if b.Text() != text || b.Version() != ver {
					t.Errorf("%s: buffer changed to %q (version %d -> %d)", name, b.Text(), ver, b.Version())
				}
			}
		})
	}
	// A stale match, after the text moved under it.
	b := buffer.New("foo")
	stale := m(0, 0, 0, 3)
	b.Insert(buffer.Pos{}, "x")
	if _, err := ReplaceAt(b, Query{Text: "foo"}, "bar", stale); !errors.Is(err, ErrNoMatch) {
		t.Errorf("stale match: %v, want ErrNoMatch", err)
	}
}

func TestReplaceAtInvalidRegex(t *testing.T) {
	t.Parallel()
	b := buffer.New("abc")
	if _, err := ReplaceAt(b, Query{Text: "(", Regex: true}, "x", m(0, 0, 0, 1)); err == nil || errors.Is(err, ErrNoMatch) {
		t.Errorf("error = %v, want a compile error", err)
	}
}

// ReplaceAt reads the lines of the match and nothing else of a large file.
func TestReplaceAtReadsOnlyTheMatchLines(t *testing.T) {
	t.Parallel()
	const n = 4000
	text := numbered(n, 2500)
	b, src := largeBuffer(t, text)
	size := src.Size()
	before := b.Stats()
	ret, err := ReplaceAt(b, Query{Text: "needle"}, "pin", m(2500, 0, 2500, 6))
	if err != nil || ret != "pin" {
		t.Fatalf("ReplaceAt = %q, %v", ret, err)
	}
	st := b.Stats()
	if read := st.ReadBytes - before.ReadBytes; read > size/20 {
		t.Errorf("replacing one match read %d of %d bytes", read, size)
	}
	if st.OverCap != 0 || st.MaterializedBytes-before.MaterializedBytes > 64 {
		t.Errorf("OverCap %d, materialized %d bytes", st.OverCap, st.MaterializedBytes-before.MaterializedBytes)
	}
	if got := b.Line(2500); got != "pin" || b.Line(2499) != "line 2499" || !b.Modified() {
		t.Errorf("line 2500 = %q, modified %v", got, b.Modified())
	}
}

func TestReplaceAtRefusals(t *testing.T) {
	t.Parallel()
	t.Run("line too long to edit", func(t *testing.T) {
		t.Parallel()
		long := "foo " + strings.Repeat("y", 200) // over the test line cap of 64
		b, _ := largeBuffer(t, "a\n"+long+"\nfoo")
		ver := b.Version()
		if _, err := ReplaceAt(b, Query{Text: "foo"}, "bar", m(1, 0, 1, 3)); !errors.Is(err, buffer.ErrLineTooLong) {
			t.Fatalf("error = %v, want ErrLineTooLong", err)
		}
		if b.Version() != ver || b.Modified() {
			t.Errorf("a refused replace changed the buffer (version %d -> %d)", ver, b.Version())
		}
		// A short line after it still replaces.
		if ret, err := ReplaceAt(b, Query{Text: "foo"}, "bar", m(2, 0, 2, 3)); err != nil || ret != "bar" {
			t.Errorf("ReplaceAt on a short line = %q, %v", ret, err)
		}
	})
	t.Run("line that is not valid UTF-8", func(t *testing.T) {
		t.Parallel()
		data := []byte("a\nfoo \xff bar\nfoo")
		b, err := buffer.OpenLarge(buffertest.NewMemSource(data), buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 64})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = b.Close() }()
		if !b.Index(0).Complete {
			t.Fatal("setup: index incomplete")
		}
		ver := b.Version()
		if _, err := ReplaceAt(b, Query{Text: "foo"}, "x", m(1, 0, 1, 3)); !errors.Is(err, buffer.ErrInvalidUTF8) {
			t.Fatalf("error = %v, want ErrInvalidUTF8", err)
		}
		if b.Version() != ver || b.Refusal() != nil {
			t.Errorf("version %d -> %d, or the refusal was left set", ver, b.Version())
		}
	})
	t.Run("match over the read limit", func(t *testing.T) {
		t.Parallel()
		line := strings.Repeat("z", maxReplaceBytes+1)
		b := buffer.New(line)
		if _, err := ReplaceAt(b, Query{Text: "z"}, "y", m(0, 0, 0, 1)); !errors.Is(err, ErrMatchTooLarge) {
			t.Fatalf("one long line: error = %v, want ErrMatchTooLarge", err)
		}
		// Many short lines: the refusal comes from the line count, before any line is read.
		many := buffer.New(strings.Repeat("\n", maxReplaceBytes+10))
		span := Match{Start: buffer.Pos{}, End: buffer.Pos{Line: maxReplaceBytes + 5}}
		if _, err := ReplaceAt(many, Query{Text: `(?s).*`, Regex: true}, "y", span); !errors.Is(err, ErrMatchTooLarge) {
			t.Fatalf("many lines: error = %v, want ErrMatchTooLarge", err)
		}
		if b.Text() != line || many.Modified() {
			t.Error("a refused replace changed the buffer")
		}
	})
}
