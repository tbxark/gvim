package editor

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/document"
)

func TestDabbrevExpandCyclesCandidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		Text    string
		Line    int
		Col     int
		presses int
		Want    []string // text of the caret line after each press
		wantErr bool     // the first press fails and changes nothing
	}{
		{
			Name:    "nearest earlier word first, then the next, then the prefix",
			Text:    "foobar fox fo",
			Col:     13,
			presses: 4,
			Want:    []string{"foobar fox fox", "foobar fox foobar", "foobar fox fo", "foobar fox fox"},
		},
		{
			Name:    "an uppercase prefix matches case-sensitively",
			Text:    "Foobar foo Fo",
			Col:     13,
			presses: 3,
			Want:    []string{"Foobar foo Foobar", "Foobar foo Fo", "Foobar foo Foobar"},
		},
		{
			Name:    "a lowercase prefix matches any case",
			Text:    "FOOBAR foo fo",
			Col:     13,
			presses: 3,
			Want:    []string{"FOOBAR foo foo", "FOOBAR foo FOOBAR", "FOOBAR foo fo"},
		},
		{
			Name:    "a word that is not longer than the prefix is not a candidate",
			Text:    "fo fo",
			Col:     5,
			wantErr: true,
		},
		{
			Name:    "no earlier word matches",
			Text:    "xyz fo",
			Col:     6,
			wantErr: true,
		},
		{
			Name:    "words after the caret are not candidates",
			Text:    "fo\nfoxy",
			Line:    0,
			Col:     2,
			wantErr: true,
		},
		{
			Name:    "no prefix before the caret",
			Text:    "foo ",
			Col:     4,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			marksCaret(e, tt.Line, tt.Col)
			if tt.wantErr {
				if r := e.Do("dabbrev_expand", nil); r.Err == nil {
					t.Fatal("dabbrev_expand returned nil, want an error")
				}
				if got := e.Active().Buffer.Text(); got != tt.Text {
					t.Fatalf("text changed to %q on failure", got)
				}
				return
			}
			for i := range tt.presses {
				if r := e.Do("dabbrev_expand", nil); r.Err != nil {
					t.Fatalf("press %d: %v", i+1, r.Err)
				}
				if got := e.Active().Buffer.Line(tt.Line); got != tt.Want[i] {
					t.Fatalf("after press %d line = %q, want %q", i+1, got, tt.Want[i])
				}
			}
		})
	}
}

// withDabbrevDocs returns an editor with one tab per text and the given tab active.
func withDabbrevDocs(active int, texts ...string) *Editor {
	e := New(Options{Rows: 5, Cols: 80})
	e.docs = nil
	for _, text := range texts {
		e.docs = append(e.docs, newDocument("", text))
	}
	e.switchTo(active)
	return e
}

// TestDabbrevMultiBufferCyclesOtherBuffers: after the active buffer's candidates,
// repeated presses cycle the other open buffers' words in tab order, then restore
// the typed prefix.
func TestDabbrevMultiBufferCyclesOtherBuffers(t *testing.T) {
	t.Parallel()
	e := withDabbrevDocs(0, "fox fo", "foobar\n", "foobaz\n")
	marksCaret(e, 0, 6)
	want := []string{"fox fox", "fox foobar", "fox foobaz", "fox fo"}
	for i, w := range want {
		if r := e.Do("dabbrev_expand", nil); r.Err != nil {
			t.Fatalf("press %d: %v", i+1, r.Err)
		}
		if got := e.Active().Buffer.Line(0); got != w {
			t.Fatalf("after press %d line = %q, want %q", i+1, got, w)
		}
	}
}

// TestDabbrevMultiBufferWithoutActiveMatches: other buffers are searched when the
// active buffer has no candidate; the first press already inserts one.
func TestDabbrevMultiBufferWithoutActiveMatches(t *testing.T) {
	t.Parallel()
	e := withDabbrevDocs(0, "xy fo", "foobar\n")
	marksCaret(e, 0, 5)
	if r := e.Do("dabbrev_expand", nil); r.Err != nil {
		t.Fatalf("first press: %v", r.Err)
	}
	if got := e.Active().Buffer.Line(0); got != "xy foobar" {
		t.Fatalf("line = %q, want the other buffer's word", got)
	}
	if r := e.Do("dabbrev_expand", nil); r.Err != nil {
		t.Fatalf("second press: %v", r.Err)
	}
	if got := e.Active().Buffer.Line(0); got != "xy fo" {
		t.Fatalf("after the cycle line = %q, want the prefix", got)
	}
}

// TestDabbrevMultiBufferDeduplicates: a word already offered by the active buffer
// is not offered again from another buffer, and a word shared by two other buffers
// appears once.
func TestDabbrevMultiBufferDeduplicates(t *testing.T) {
	t.Parallel()
	e := withDabbrevDocs(0, "foobar fo", "foobar\n", "foobaz foobar\n")
	marksCaret(e, 0, 10)
	want := []string{"foobar foobar", "foobar foobaz", "foobar fo"}
	for i, w := range want {
		if r := e.Do("dabbrev_expand", nil); r.Err != nil {
			t.Fatalf("press %d: %v", i+1, r.Err)
		}
		if got := e.Active().Buffer.Line(0); got != w {
			t.Fatalf("after press %d line = %q, want %q", i+1, got, w)
		}
	}
}

// TestDabbrevMultiBufferBoundedBuffers: only DabbrevOtherBufferLimit other buffers
// are scanned, so a session with many tabs stays cheap.
func TestDabbrevMultiBufferBoundedBuffers(t *testing.T) {
	t.Parallel()
	texts := []string{"fo"}
	for i := 0; i < document.DabbrevOtherBufferLimit+4; i++ {
		texts = append(texts, fmt.Sprintf("fooword%d\n", i))
	}
	e := withDabbrevDocs(0, texts...)
	marksCaret(e, 0, 2)
	// Candidate i (0-based) comes from other buffer i; after the last included
	// buffer the next press restores the prefix.
	for i := 0; i < document.DabbrevOtherBufferLimit; i++ {
		if r := e.Do("dabbrev_expand", nil); r.Err != nil {
			t.Fatalf("press %d: %v", i+1, r.Err)
		}
		want := fmt.Sprintf("fooword%d", i)
		if got := e.Active().Buffer.Line(0); got != want {
			t.Fatalf("after press %d line = %q, want %q", i+1, got, want)
		}
	}
	if r := e.Do("dabbrev_expand", nil); r.Err != nil {
		t.Fatalf("restore press: %v", r.Err)
	}
	if got := e.Active().Buffer.Line(0); got != "fo" {
		t.Fatalf("after the bounded cycle line = %q, want the prefix", got)
	}
}

// TestDabbrevMultiBufferWindowIsBounded: only the middle window of a large other
// buffer is scanned, so a word at the start is outside the bounded window while a
// word in the middle is found.
func TestDabbrevMultiBufferWindowIsBounded(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("foofar\n")
	const filler = 3000
	for i := 0; i < filler; i++ {
		if i == filler/2 {
			sb.WriteString("foonear\n")
			continue
		}
		sb.WriteString(strings.Repeat("x", 100))
		sb.WriteString("\n")
	}
	e := withDabbrevDocs(0, "zz fo", sb.String())
	marksCaret(e, 0, 5)
	if r := e.Do("dabbrev_expand", nil); r.Err != nil {
		t.Fatalf("first press: %v", r.Err)
	}
	if got := e.Active().Buffer.Line(0); got != "zz foonear" {
		t.Fatalf("line = %q, want the middle window's word, not the far one", got)
	}
}

// TestDabbrevMultiBufferSmartCaseAndUndo: an uppercase prefix matches other-buffer
// words case-sensitively, and one press is one undo step.
func TestDabbrevMultiBufferSmartCaseAndUndo(t *testing.T) {
	t.Parallel()
	e := withDabbrevDocs(0, "FO", "FOOBAR\n", "foobar\n")
	marksCaret(e, 0, 2)
	if r := e.Do("dabbrev_expand", nil); r.Err != nil {
		t.Fatalf("uppercase prefix press: %v", r.Err)
	}
	if got := e.Active().Buffer.Line(0); got != "FOOBAR" {
		t.Fatalf("line = %q, want the case-sensitive match only", got)
	}
	if _, ok := e.Active().Buffer.Undo(); !ok {
		t.Fatal("Undo reported no group")
	}
	if got := e.Active().Buffer.Line(0); got != "FO" {
		t.Fatalf("after undo line = %q, want the prefix", got)
	}
	// A lowercase prefix matches either case; FOOBAR is before foobar in tab order.
	e = withDabbrevDocs(0, "fo", "FOOBAR\n", "foobar\n")
	marksCaret(e, 0, 2)
	if r := e.Do("dabbrev_expand", nil); r.Err != nil {
		t.Fatalf("lowercase prefix press: %v", r.Err)
	}
	if got := e.Active().Buffer.Line(0); got != "FOOBAR" {
		t.Fatalf("line = %q, want the first case-insensitive match", got)
	}
}

// TestDabbrevCandidateLimit: one session returns at most DabbrevMaxCandidates
// candidates (Fresh's 30), nearest first; the next press restores the prefix.
func TestDabbrevCandidateLimit(t *testing.T) {
	t.Parallel()
	words := make([]string, document.DabbrevMaxCandidates+5)
	for i := range words {
		words[i] = fmt.Sprintf("fooword%02d", i)
	}
	line := strings.Join(words, " ") + " fo"
	e := withDabbrevDocs(0, line)
	marksCaret(e, 0, len([]rune(line)))
	for i := 0; i < document.DabbrevMaxCandidates; i++ {
		if r := e.Do("dabbrev_expand", nil); r.Err != nil {
			t.Fatalf("press %d: %v", i+1, r.Err)
		}
		want := fmt.Sprintf("fooword%02d", len(words)-1-i)
		if got := e.Active().Buffer.Line(0); !strings.HasSuffix(got, want) {
			t.Fatalf("after press %d line = %q, want suffix %q", i+1, got, want)
		}
	}
	if r := e.Do("dabbrev_expand", nil); r.Err != nil {
		t.Fatalf("restore press: %v", r.Err)
	}
	if got := e.Active().Buffer.Line(0); got != line {
		t.Fatalf("after the capped cycle line = %q, want the prefix restored", got)
	}
}

// TestRecentTabIndicesMRUOrder: the active tab is first, then the tabs in activation
// order; closing and reordering tabs keeps the surviving documents' order (identity is
// the document, not the tab index).
func TestRecentTabIndicesMRUOrder(t *testing.T) {
	t.Parallel()
	e := withDabbrevDocs(0, "a", "b", "c")
	e.switchTo(1)
	e.switchTo(2)
	e.switchTo(0)
	if got, want := e.RecentTabIndices(), []int{0, 2, 1}; !slices.Equal(got, want) {
		t.Fatalf("RecentTabIndices = %v, want %v", got, want)
	}
	// Move the last-active other tab (C, index 2) before A; identity survives the move.
	if err := e.MoveTab(e.DocIDs()[2], -2); err != nil {
		t.Fatalf("MoveTab: %v", err)
	}
	if got, want := e.RecentTabIndices(), []int{1, 0, 2}; !slices.Equal(got, want) {
		t.Fatalf("RecentTabIndices after MoveTab = %v, want %v (A=1, C=0, B=2)", got, want)
	}
	// Close B (index 2 now): the remaining MRU order is A then C.
	if err := e.CloseTab(2, true); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	// A is still active at index 1 after C moved before it; C follows at index 0.
	if got, want := e.RecentTabIndices(), []int{1, 0}; !slices.Equal(got, want) {
		t.Fatalf("RecentTabIndices after close = %v, want %v", got, want)
	}
	// Restoration: tabs are adopted one by one and then the saved active tab is
	// activated. Identity is the document, so the active tab is first and the rest keep
	// their add/activation order.
	r := New(Options{Empty: true})
	r.addDoc(newDocument("", "x"))
	r.addDoc(newDocument("", "y"))
	r.addDoc(newDocument("", "z"))
	r.ActivateTab(1)
	if got, want := r.RecentTabIndices(), []int{1, 2, 0}; !slices.Equal(got, want) {
		t.Fatalf("RecentTabIndices after restore = %v, want %v", got, want)
	}
}

// TestDabbrevMultiBufferMRUOrder: other tabs are searched most-recently-used first,
// not in tab order.
func TestDabbrevMultiBufferMRUOrder(t *testing.T) {
	t.Parallel()
	// Tab order is active, foob, fooc; activation order makes fooc the most recent
	// other tab, so the cycle must offer fooc before foob.
	e := withDabbrevDocs(0, "fo", "foob\n", "fooc\n")
	e.switchTo(1)
	e.switchTo(2)
	e.switchTo(0)
	marksCaret(e, 0, 2)
	want := []string{"fooc", "foob", "fo"}
	for i, w := range want {
		if r := e.Do("dabbrev_expand", nil); r.Err != nil {
			t.Fatalf("press %d: %v", i+1, r.Err)
		}
		if got := e.Active().Buffer.Line(0); got != w {
			t.Fatalf("after press %d line = %q, want %q", i+1, got, w)
		}
	}
}
