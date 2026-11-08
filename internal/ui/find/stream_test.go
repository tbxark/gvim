package find

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
)

// streamFake is a fakeEditor whose document counts as large: the bar must go to a
// match through Next, which here finds it on the memory buffer, and must never ask for
// the list of matches.
type streamFake struct {
	*fakeEditor
	finds int // calls of Find, which the bar must not make
	nexts int
	limit bool // Next reports that its budget ran out
}

func (s *streamFake) Streaming() bool { return true }

func (s *streamFake) Find(q search.Query) ([]search.Match, error) {
	s.finds++
	return s.fakeEditor.Find(q)
}

func (s *streamFake) Next(q search.Query, from buffer.Pos, forward bool) (search.Match, bool, error) {
	s.nexts++
	if s.limit {
		return search.Match{}, false, ErrSearchLimit
	}
	matches, err := search.Find(s.buf, q)
	if err != nil || len(matches) == 0 {
		return search.Match{}, false, err
	}
	var idx int
	if forward {
		idx, _ = search.Next(matches, from, true)
	} else {
		idx, _ = search.Prev(matches, from, true)
	}
	return matches[idx], true, nil
}

func newStreamBar(text string) (*Bar, *streamFake) {
	s := &streamFake{fakeEditor: newFake(text)}
	return Open(s, testStyles(), false), s
}

func TestStreamingBarDoesNotListMatches(t *testing.T) {
	b, s := newStreamBar("a b a b a")
	typeText(b, "a")
	if b.status != searchingText {
		t.Errorf("status after typing = %q, want %q until the scan reports", b.status, searchingText)
	}
	press(b, "alt+c")
	press(b, "enter")
	press(b, "f3")
	if s.finds != 0 {
		t.Errorf("the bar called Find %d times for a document that is searched in a stream", s.finds)
	}
	if s.nexts == 0 {
		t.Errorf("the bar never asked for the next match")
	}
}

func TestStreamStatusShowsProgressThenTheCount(t *testing.T) {
	tests := []struct {
		name string
		info StreamInfo
		want string
	}{
		{"running", StreamInfo{Progress: 0.37, Count: 1204}, "Searching... 37% (1,204 matches so far)"},
		{"running for a while", StreamInfo{Progress: 0.5, Count: 3, Elapsed: 4 * time.Second, Regex: true},
			"Searching... 50% (3 matches so far, 4s) regex search is about 50-300 MB/s"},
		{"done", StreamInfo{Done: true, Count: 3, Matches: []search.Match{
			{Start: buffer.Pos{Line: 0, Col: 0}, End: buffer.Pos{Line: 0, Col: 1}},
			{Start: buffer.Pos{Line: 0, Col: 4}, End: buffer.Pos{Line: 0, Col: 5}},
			{Start: buffer.Pos{Line: 0, Col: 8}, End: buffer.Pos{Line: 0, Col: 9}},
		}}, "Match 1 of 3"},
		{"none", StreamInfo{Done: true}, "No matches"},
		{"capped", StreamInfo{Done: true, Count: 100000, Capped: true}, "100,000+ matches"},
		{"partial", StreamInfo{Done: true, Count: 0, Partial: true}, "No matches (line index not complete: only the part indexed was searched)"},
		{"error", StreamInfo{Err: errors.New("bad pattern")}, "bad pattern"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := newStreamBar("a b a b a")
			typeText(b, "a")
			b.SetStream(tc.info)
			if b.status != tc.want {
				t.Errorf("status = %q, want %q", b.status, tc.want)
			}
		})
	}
}

func TestStreamStatusNumbersTheSelectedMatch(t *testing.T) {
	b, s := newStreamBar("a b a b a")
	typeText(b, "a")
	matches, err := search.Find(s.buf, b.query())
	if err != nil {
		t.Fatal(err)
	}
	press(b, "f3") // the first match after the caret at 0,0 is the one at column 0
	press(b, "f3")
	b.SetStream(StreamInfo{Done: true, Count: len(matches), Matches: matches})
	if !strings.HasPrefix(b.status, "Match 2 of 3") {
		t.Errorf("status = %q, want the second match", b.status)
	}
}

func TestStreamingBarStepsEnterAndWraps(t *testing.T) {
	b, s := newStreamBar("a b a b a")
	typeText(b, "a")
	press(b, "f3")
	if got := s.sel; got == nil || got.Start.Col != 0 {
		t.Fatalf("first F3 selected %+v, want the match at column 0", got)
	}
	press(b, "f3")
	press(b, "f3")
	if got := s.sel; got == nil || got.Start.Col != 8 {
		t.Fatalf("third F3 selected %+v, want the match at column 8", got)
	}
	press(b, "f3")
	if got := s.sel; got == nil || got.Start.Col != 0 {
		t.Fatalf("F3 past the last match selected %+v, want the wrap to column 0", got)
	}
	press(b, "shift+f3")
	if got := s.sel; got == nil || got.Start.Col != 8 {
		t.Fatalf("Shift+F3 from the first match selected %+v, want the wrap to column 8", got)
	}
	press(b, "enter")
	if b.Active() || !b.Committed() {
		t.Errorf("Enter should close the bar and keep the search (active %v, committed %v)", b.Active(), b.Committed())
	}
}

func TestStreamingBarEnterSelectsTheFirstMatchFromTheAnchor(t *testing.T) {
	b, s := newStreamBar("a b a b a")
	s.cursor = buffer.Pos{Col: 3}
	b.anchor = s.cursor
	typeText(b, "a")
	press(b, "enter")
	if got := s.sel; got == nil || got.Start.Col != 4 {
		t.Fatalf("Enter selected %+v, want the match at column 4", got)
	}
}

func TestStreamingBarShowsTheLimitAndNoMatch(t *testing.T) {
	b, s := newStreamBar("abc")
	typeText(b, "x")
	press(b, "f3")
	if b.status != "No matches" {
		t.Errorf("status = %q, want No matches", b.status)
	}
	s.limit = true
	press(b, "f3")
	if b.status != SearchLimitText {
		t.Errorf("status = %q, want %q", b.status, SearchLimitText)
	}
	if s.sel != nil {
		t.Errorf("a search that ran out of budget selected a match")
	}
}

func TestStreamingQueryReplaceGoesRoundOnce(t *testing.T) {
	b, s := newStreamBar("a a a")
	s.cursor = buffer.Pos{Col: 2}
	b.anchor = s.cursor
	typeText(b, "a")
	b.ShowReplace()
	b.StartQueryReplace()
	if got := s.sel; got == nil || got.Start.Col != 2 || b.status != queryPrompt {
		t.Fatalf("first question: selection %+v, status %q", got, b.status)
	}
	b.Update(key("n"))
	if got := s.sel; got == nil || got.Start.Col != 4 {
		t.Fatalf("after n the selection is %+v, want column 4", got)
	}
	b.Update(key("n"))
	if got := s.sel; got == nil || got.Start.Col != 0 || b.status != queryWrapped {
		t.Fatalf("after the wrap: selection %+v, status %q", got, b.status)
	}
	b.Update(key("n"))
	if b.Replacing() || !strings.HasPrefix(b.status, "Replaced 0") {
		t.Errorf("the run should end where it began: replacing %v, status %q", b.Replacing(), b.status)
	}
	if s.finds != 0 {
		t.Errorf("Find was called %d times", s.finds)
	}
}

func TestGroupWritesThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 1204: "1,204", 100000: "100,000", 1234567: "1,234,567"} {
		if got := group(n); got != want {
			t.Errorf("group(%d) = %q, want %q", n, got, want)
		}
	}
}
