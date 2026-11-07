package search

import (
	"errors"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestFindRangeMatchesFindOnTheWholeBuffer(t *testing.T) {
	t.Parallel()
	pieces := []string{"foo", "Foo", "FOO", " ", "\n", "é", "日本", "foobar", "bar foo", "ſ", "K", "k", "ß", "ẞ", "_", "9", "x.y"}
	queries := []Query{
		{Text: "foo"},
		{Text: "foo", CaseSensitive: true},
		{Text: "foo", WholeWord: true},
		{Text: "foo", WholeWord: true, CaseSensitive: true},
		{Text: "k"},
		{Text: "ß"},
		{Text: "ſ"},
		{Text: "é日"},
		{Text: "x.y", CaseSensitive: true},
		{Text: "o+", Regex: true},
		{Text: "^foo", Regex: true},
		{Text: `\bfoo\b`, Regex: true},
		{Text: "foo", Regex: true, WholeWord: true},
		{Text: `o\nb`, Regex: true, CaseSensitive: true},
		{Text: "x*", Regex: true},
	}
	for seed := uint64(1); seed <= 30; seed++ {
		rng := rand.New(rand.NewPCG(seed, 7))
		var sb strings.Builder
		for range 80 {
			sb.WriteString(pieces[rng.IntN(len(pieces))])
		}
		b := buffer.New(sb.String())
		for _, q := range queries {
			want, err := Find(b, q)
			if err != nil {
				t.Fatal(err)
			}
			got, truncated, err := FindRange(b, q, buffer.Pos{}, b.End(), MaxFindMax)
			if err != nil || truncated {
				t.Fatalf("seed %d %+v: err %v truncated %v", seed, q, err, truncated)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("seed %d %+v:\n got %v\nwant %v\ntext %q", seed, q, got, want, b.Text())
			}
		}
	}
}

func TestFindRangeInsideTheRange(t *testing.T) {
	t.Parallel()
	b := buffer.New("héllo foo\nfoo 日本 foo\nlast foo\nend")
	tests := []struct {
		name     string
		q        Query
		from, to buffer.Pos
		max      int
		want     []Match
		trunc    bool
	}{
		{"whole", Query{Text: "foo"}, buffer.Pos{}, b.End(), 0,
			[]Match{m(0, 6, 0, 9), m(1, 0, 1, 3), m(1, 7, 1, 10), m(2, 5, 2, 8)}, false},
		{"from mid line", Query{Text: "foo"}, buffer.Pos{Line: 0, Col: 7}, b.End(), 0,
			[]Match{m(1, 0, 1, 3), m(1, 7, 1, 10), m(2, 5, 2, 8)}, false},
		{"to cuts a match", Query{Text: "foo"}, buffer.Pos{}, buffer.Pos{Line: 1, Col: 2}, 0,
			[]Match{m(0, 6, 0, 9)}, false},
		{"to and from swapped", Query{Text: "foo"}, buffer.Pos{Line: 2, Col: 0}, buffer.Pos{Line: 1, Col: 0}, 0,
			[]Match{m(1, 0, 1, 3), m(1, 7, 1, 10)}, false},
		{"one line", Query{Text: "foo"}, buffer.Pos{Line: 1, Col: 4}, buffer.Pos{Line: 1, Col: 10}, 0,
			[]Match{m(1, 7, 1, 10)}, false},
		{"max stops", Query{Text: "foo"}, buffer.Pos{}, b.End(), 2,
			[]Match{m(0, 6, 0, 9), m(1, 0, 1, 3)}, true},
		{"max equal to the count is not truncated", Query{Text: "foo"}, buffer.Pos{}, b.End(), 4,
			[]Match{m(0, 6, 0, 9), m(1, 0, 1, 3), m(1, 7, 1, 10), m(2, 5, 2, 8)}, false},
		{"positions are clamped", Query{Text: "end"}, buffer.Pos{Line: -3, Col: -1}, buffer.Pos{Line: 99, Col: 99}, 0,
			[]Match{m(3, 0, 3, 3)}, false},
		{"regex anchors see the range start", Query{Text: "^foo", Regex: true}, buffer.Pos{Line: 1, Col: 7}, b.End(), 0,
			[]Match{m(1, 7, 1, 10)}, false},
		{"whole word finds no o", Query{Text: "o", WholeWord: true}, buffer.Pos{}, b.End(), 1, nil, false},
		{"empty query", Query{}, buffer.Pos{}, b.End(), 0, nil, false},
	}
	for _, tt := range tests {
		got, trunc, err := FindRange(b, tt.q, tt.from, tt.to, tt.max)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !slices.Equal(got, tt.want) || trunc != tt.trunc {
			t.Errorf("%s:\n got %v trunc %v\nwant %v trunc %v", tt.name, got, trunc, tt.want, tt.trunc)
		}
	}
	if _, _, err := FindRange(b, Query{Text: "("}, buffer.Pos{}, b.End(), 0); err != nil {
		t.Errorf("a literal ( is not a pattern: %v", err)
	}
	if _, _, err := FindRange(b, Query{Text: "(", Regex: true}, buffer.Pos{}, b.End(), 0); err == nil {
		t.Error("a bad regex was accepted")
	}
}

func TestFindRangeWholeWordFilterKeepsFillingMax(t *testing.T) {
	t.Parallel()
	// Many "ab" inside words come first; the words "ab" are at the end.
	b := buffer.New(strings.Repeat("xabx ", 50) + strings.Repeat("ab ", 5))
	for _, regex := range []bool{false, true} {
		got, trunc, err := FindRange(b, Query{Text: "ab", WholeWord: true, Regex: regex}, buffer.Pos{}, b.End(), 3)
		if err != nil || len(got) != 3 || !trunc {
			t.Errorf("regex=%v: %v truncated %v err %v, want 3 whole-word matches and truncated", regex, got, trunc, err)
			continue
		}
		if got[0].Start.Col != 250 {
			t.Errorf("regex=%v: first match at %v, want col 250", regex, got[0].Start)
		}
	}
}

func TestFindRangeStopsAtMax(t *testing.T) {
	line := strings.Repeat("abcdefghi", 12) // 108 bytes
	n := (10 << 20) / (len(line) + 1)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = line
	}
	b := buffer.New(strings.Join(lines, "\n"))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got, trunc, err := FindRange(b, Query{Text: ".", Regex: true}, buffer.Pos{}, b.End(), 1000)
	runtime.ReadMemStats(&after)
	if err != nil || len(got) != 1000 || !trunc {
		t.Fatalf("%d matches, truncated %v, err %v", len(got), trunc, err)
	}
	if cap(got) > 2000 {
		t.Errorf("result slice has capacity %d", cap(got))
	}
	// Reading the 10 MiB range is the one big allocation; the matches of the
	// whole text would take more than 10 million entries of 16 bytes on top.
	// This test is not parallel, so nothing else allocates meanwhile.
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 120<<20 {
		t.Errorf("FindRange allocated %d MiB", grown>>20)
	}
}

func TestFindRangeWorkCap(t *testing.T) {
	t.Parallel()
	mib := strings.Repeat("the quick brown fox jumps over the lazy dog\n", (1<<20)/44)
	big := buffer.New(mib)
	tests := []struct {
		name   string
		q      Query
		costly bool
	}{
		{"nested repeat on 1 MiB", Query{Text: `(?:[a-z]+\s*){1,50}!`, Regex: true}, true},
		{"dot star repeat on 1 MiB", Query{Text: `(?s)(.*a){20}z`, Regex: true}, true},
		{"word boundary on 1 MiB", Query{Text: `\bfox\b`, Regex: true}, false},
		{"plain string as regex", Query{Text: "lazy", Regex: true, CaseSensitive: true}, false},
		{"plain string", Query{Text: "lazy"}, false},
	}
	for _, tt := range tests {
		got, _, err := FindRange(big, tt.q, buffer.Pos{}, big.End(), 5)
		if tt.costly {
			if !errors.Is(err, ErrTooCostly) || got != nil {
				t.Errorf("%s: %v, %v, want ErrTooCostly", tt.name, got, err)
			}
			// The refusal is the up-front rule: the text length times the program size is
			// over the cap, which is known before any text is read. Asserting the work,
			// and not how long the refusal took, keeps the test independent of the load.
			re, cerr := compile(tt.q)
			if cerr != nil {
				t.Fatalf("%s: %v", tt.name, cerr)
			}
			if work := int64(rangeBytes(big, buffer.Pos{}, big.End())) * int64(programSize(re)); work <= MaxRegexWork {
				t.Errorf("%s: refused, but its work %d is not over the cap %d", tt.name, work, MaxRegexWork)
			}
			continue
		}
		if err != nil || len(got) != 5 {
			t.Errorf("%s: %d matches, err %v", tt.name, len(got), err)
		}
	}
}

func TestFindRangeWorkCapExemptsPlainStrings(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 12 MiB buffer")
	}
	t.Parallel()
	b := buffer.New(strings.Repeat("0123456789abcdef\n", (12<<20)/17) + "foo")
	got, _, err := FindRange(b, Query{Text: "foo", Regex: true, CaseSensitive: true}, buffer.Pos{}, b.End(), 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("a plain-string regex over 12 MiB: %v, %v", got, err)
	}
}

func TestFindRangeTooLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 17 MiB buffer")
	}
	t.Parallel()
	b := buffer.New(strings.Repeat(strings.Repeat("a", 1023)+"\n", 17*1024))
	if _, _, err := FindRange(b, Query{Text: "zzz"}, buffer.Pos{}, b.End(), 0); !errors.Is(err, ErrRangeTooLarge) {
		t.Fatalf("err = %v, want ErrRangeTooLarge", err)
	}
	// A part of it is fine.
	if _, _, err := FindRange(b, Query{Text: "zzz"}, buffer.Pos{}, buffer.Pos{Line: 1000}, 0); err != nil {
		t.Fatalf("a 1 MiB part: %v", err)
	}
}

func TestFindRangeRefusesLargeBuffer(t *testing.T) {
	t.Parallel()
	b, src := largeBuffer(t, "foo\nbar\nfoo")
	read := src.BytesRead()
	for _, q := range []Query{{Text: "foo"}, {Text: "fo+", Regex: true}, {Text: ""}} {
		got, trunc, err := FindRange(b, q, buffer.Pos{}, buffer.Pos{Line: 2}, 0)
		if !errors.Is(err, ErrLargeBuffer) || got != nil || trunc {
			t.Errorf("%+v: %v %v %v, want ErrLargeBuffer", q, got, trunc, err)
		}
	}
	if src.BytesRead() != read {
		t.Errorf("the refusal read %d bytes of the source", src.BytesRead()-read)
	}
}

func TestFindRangeTruncatedFlag(t *testing.T) {
	t.Parallel()
	b := buffer.New(strings.Repeat("a\n", 50))
	for _, tt := range []struct {
		max   int
		n     int
		trunc bool
	}{{49, 49, true}, {50, 50, false}, {51, 50, false}, {1, 1, true}, {MaxFindMax * 2, 50, false}} {
		got, trunc, err := FindRange(b, Query{Text: "a"}, buffer.Pos{}, b.End(), tt.max)
		if err != nil || len(got) != tt.n || trunc != tt.trunc {
			t.Errorf("max %d: %d matches truncated %v err %v, want %d %v", tt.max, len(got), trunc, err, tt.n, tt.trunc)
		}
	}
	big := buffer.New(strings.Repeat("a", DefaultFindMax+5))
	if got, trunc, _ := FindRange(big, Query{Text: "a"}, buffer.Pos{}, big.End(), 0); len(got) != DefaultFindMax || !trunc {
		t.Errorf("default max on %d matches returned %d, truncated %v", DefaultFindMax+5, len(got), trunc)
	}
}

func TestFoldFinderMatchesRegexpFolding(t *testing.T) {
	t.Parallel()
	runes := []rune("aAkKsSſKßẞσςΣ日é É")
	rng := rand.New(rand.NewPCG(1, 2))
	for range 300 {
		var n, h strings.Builder
		for range 1 + rng.IntN(3) {
			n.WriteRune(runes[rng.IntN(len(runes))])
		}
		for range 12 {
			h.WriteRune(runes[rng.IntN(len(runes))])
		}
		q := Query{Text: n.String()}
		re, err := compile(q)
		if err != nil {
			t.Fatal(err)
		}
		want := re.FindAllStringIndex(h.String(), -1)
		got := literalOffsets(q, h.String(), 100)
		if len(got) != len(want) {
			t.Fatalf("needle %q in %q: %v, regexp says %v", n.String(), h.String(), got, want)
		}
		for i := range got {
			if got[i][0] != want[i][0] || got[i][1] != want[i][1] {
				t.Fatalf("needle %q in %q: %v, regexp says %v", n.String(), h.String(), got, want)
			}
		}
	}
}

func TestFindRangeFoldedLiteralIsLinear(t *testing.T) {
	// A needle that almost matches at every offset: scanning each offset from
	// scratch costs text*needle (3.4 s for 4 MB and 500 bytes); one pass does not.
	// The bound is in runes read, which is exact, and not in seconds, which depend on
	// the machine: the scan reads every rune of the text once and no rune twice.
	text := strings.Repeat(strings.Repeat("a", 999)+"\n", 4000)
	b := buffer.New(text)
	needle := strings.Repeat("a", 500) + "b"
	for _, q := range []Query{{Text: needle}, {Text: needle, WholeWord: true}} {
		l := newLiteralSearch(q)
		if got := l.offsets(text, 10); len(got) != 0 {
			t.Fatalf("whole word %v: offsets = %v, want nothing", q.WholeWord, got)
		}
		if want := utf8.RuneCountInString(text); l.sc.seen != want {
			t.Errorf("whole word %v: the scan read %d runes of a text of %d, want one read per rune", q.WholeWord, l.sc.seen, want)
		}
		got, trunc, err := FindRange(b, q, buffer.Pos{}, b.End(), 10)
		if err != nil || len(got) != 0 || trunc {
			t.Fatalf("whole word %v: %v %v %v, want nothing", q.WholeWord, got, trunc, err)
		}
	}
	lines, _, err := FindLines(b, LinesQuery{Query: Query{Text: needle}, Budget: MaxLinesBudget})
	if err != nil || len(lines) != 0 {
		t.Errorf("FindLines: %v %v", lines, err)
	}
}

func TestFoldedLiteralOffsets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, needle, text string
		whole              bool
		want               [][2]int
	}{
		{"overlap restarts after the match", "aa", "aaaaa", false, [][2]int{{0, 2}, {2, 4}}},
		{"kmp fallback", "abab", "abaabab", false, [][2]int{{3, 7}}},
		{"overlapping candidates when words reject", "aa", "aa aaa", true, [][2]int{{0, 2}}},
		{"whole word after an overlapping reject", "aba", "ababa aba", true, [][2]int{{6, 9}}},
		{"sign folds to k", "k", "\u212a K", false, [][2]int{{0, 3}, {4, 5}}},
		{"long s", "stress", "STRE\u017f\u017f", false, [][2]int{{0, 8}}},
		{"multibyte offsets", "É", "xéÉ", false, [][2]int{{1, 3}, {3, 5}}},
		{"invalid bytes are replacement runes", "\uFFFDa", "\xffA", false, [][2]int{{0, 2}}},
		{"needle longer than text", "abcd", "abc", false, nil},
	}
	for _, tt := range tests {
		got := literalOffsets(Query{Text: tt.needle, WholeWord: tt.whole}, tt.text, 100)
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFindRangeWholeWordRegexIsBounded(t *testing.T) {
	// Every match of "." fails the whole-word test inside a long word. Asking
	// the regexp for ever more matches used to allocate 2 GiB for this.
	b := buffer.New(strings.Repeat(strings.Repeat("a", 1000)+"\n", 3000))
	q := Query{Text: ".", Regex: true, WholeWord: true, CaseSensitive: true}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got, _, err := FindRange(b, q, buffer.Pos{}, b.End(), 1000)
	runtime.ReadMemStats(&after)
	if len(got) != 0 {
		t.Errorf("%d matches, want none", len(got))
	}
	if !errors.Is(err, ErrTooCostly) {
		t.Errorf("err = %v, want ErrTooCostly for a filter that rejects this many candidates", err)
	}
	// The old loop allocated 2 GiB; this one 36 MiB, or 225 MiB under -race.
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 512 {
		t.Errorf("allocated %d MiB", mb)
	}
	// A filter that accepts enough still works, with many rejections first.
	b = buffer.New(strings.Repeat("ax", 20000) + " ab ab ab")
	got, trunc, err := FindRange(b, Query{Text: "a.", Regex: true, WholeWord: true, CaseSensitive: true}, buffer.Pos{}, b.End(), 2)
	if err != nil || len(got) != 2 || !trunc {
		t.Errorf("got %v truncated %v err %v, want 2 matches and truncated", got, trunc, err)
	}
}
