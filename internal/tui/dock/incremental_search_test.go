package dock

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// restoredSearchPanel shows a read-only restored transcript of rows at a width
// wide enough that they are not re-wrapped.
func restoredSearchPanel(t *testing.T, rows ...string) *Panel {
	t.Helper()
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	p.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Width: 80, Rows: rows,
	})
	if !p.Scrolling() {
		t.Fatal("a restored transcript did not open in the scrollback view")
	}
	return &p
}

// applyKeys runs every message a scroll/search key command produces, and every
// command the Update answers with (the scrollback snapshot a search jump asks for),
// with a depth guard so a command that blocks waiting for shell output can never
// hang the test.
func applyKeys(t *testing.T, p *Panel, cmds ...tea.Cmd) {
	t.Helper()
	var apply func(cmd tea.Cmd, depth int)
	apply = func(cmd tea.Cmd, depth int) {
		if cmd == nil || depth > 6 {
			return
		}
		for _, msg := range messages(cmd) {
			if next, ok := p.Update(msg); ok {
				apply(next, depth+1)
			}
		}
	}
	for _, cmd := range cmds {
		apply(cmd, 0)
	}
}

// typeSearch types s into the open search input and applies each incremental
// answer (the real program runs these commands as they arrive).
func typeSearch(t *testing.T, p *Panel, s string) {
	t.Helper()
	for _, r := range s {
		cmd, ok := p.ScrollKey(tea.Key{Code: r, Text: string(r)})
		if !ok {
			t.Fatalf("typing %q was not taken by the search input", string(r))
		}
		applyKeys(t, p, cmd)
	}
}

// TestIncrementalSearchPreviewsWhileTyping checks the missing Fresh interaction:
// the scrollback search previews matches while the query is typed (the view jumps
// to the first match), Enter commits the query/matches, and a restored transcript
// participates exactly like a live one.
func TestIncrementalSearchPreviewsWhileTyping(t *testing.T) {
	t.Parallel()
	p := restoredSearchPanel(t, "alpha", "beta", "alphabet")
	if _, ok := p.ScrollKey(tea.Key{Code: '/', Text: "/"}); !ok || !p.scroll.typing {
		t.Fatal("'/' did not open the search input")
	}
	typeSearch(t, p, "a")
	if len(p.scroll.preview) != 3 {
		t.Fatalf("preview after 'a' = %v, want all three restored rows", p.scroll.preview)
	}
	if p.ScrollSelected() != 0 {
		t.Fatalf("preview did not jump to the first match: selected %d", p.ScrollSelected())
	}
	typeSearch(t, p, "l")
	if len(p.scroll.preview) != 2 {
		t.Fatalf("preview after 'al' = %v, want the two rows containing it", p.scroll.preview)
	}
	// The preview must not commit: the query/committed matches stay empty until Enter.
	if p.SearchQuery() != "" || len(p.scroll.matches) != 0 {
		t.Fatalf("typing committed a search: %q %v", p.SearchQuery(), p.scroll.matches)
	}
	if title := p.title(); !strings.Contains(title, "/al") {
		t.Fatalf("typing title = %q", title)
	}
	// Enter commits the previewed query and its matches.
	cmd := p.searchInputKey(tea.Key{Code: tea.KeyEnter})
	applyKeys(t, p, cmd)
	if p.SearchQuery() != "al" || p.SearchMatches() != 2 || len(p.scroll.preview) != 0 {
		t.Fatalf("committed search = %q matches %d preview %v", p.SearchQuery(), p.SearchMatches(), p.scroll.preview)
	}
	if title := p.title(); !strings.Contains(title, "/al 1/2") {
		t.Fatalf("committed title = %q", title)
	}
}

// TestIncrementalSearchOrdersAsyncAnswers checks the request ordering: an answer
// for an older keystroke (or for a preview that was superseded by Enter) arriving
// late is dropped, so it cannot move the view or replace the committed search.
func TestIncrementalSearchOrdersAsyncAnswers(t *testing.T) {
	t.Parallel()
	p := restoredSearchPanel(t, "alpha", "beta")
	p.startSearchInput()
	// Type "a" then "l" without applying the first answer, like a fast typist.
	cmdA, _ := p.ScrollKey(tea.Key{Code: 'a', Text: "a"})
	cmdL, _ := p.ScrollKey(tea.Key{Code: 'l', Text: "l"})
	if cmdA == nil || cmdL == nil {
		t.Fatal("an incremental search produced no command")
	}
	applyKeys(t, p, cmdL)
	if got := p.scroll.preview; len(got) != 1 {
		t.Fatalf("preview for the latest query = %v, want one row", got)
	}
	applyKeys(t, p, cmdA) // late answer for "a"
	if got := p.scroll.preview; len(got) != 1 {
		t.Fatalf("a late answer for an old keystroke replaced the preview: %v", got)
	}
	// A superseded preview must not land after Enter commits.
	cmdCommit := p.searchInputKey(tea.Key{Code: tea.KeyEnter})
	applyKeys(t, p, cmdCommit)
	committed := p.SearchMatches()
	if committed != 1 {
		t.Fatalf("committed matches = %d, want the one 'al' row", committed)
	}
	applyKeys(t, p, cmdA) // the oldest preview answers after the commit
	if p.SearchQuery() != "al" || p.SearchMatches() != committed || len(p.scroll.preview) != 0 {
		t.Fatalf("a late preview changed the committed search: %q %d preview %v", p.SearchQuery(), p.SearchMatches(), p.scroll.preview)
	}
}

// TestIncrementalSearchCancelRestoresTheView checks Esc cancels the preview and
// puts the view and committed search back exactly as they were (restored records
// keep their search experience across an incremental attempt).
func TestIncrementalSearchCancelRestoresTheView(t *testing.T) {
	t.Parallel()
	p := restoredSearchPanel(t, "one", "two", "three", "beta", "five")
	// Commit a search for "beta" (row 3) and note the view.
	p.startSearchInput()
	typeSearch(t, p, "beta")
	cmd := p.searchInputKey(tea.Key{Code: tea.KeyEnter})
	applyKeys(t, p, cmd)
	if p.SearchQuery() != "beta" || p.SearchMatches() != 1 {
		t.Fatalf("committed search = %q/%d", p.SearchQuery(), p.SearchMatches())
	}
	top, selected := p.scroll.top, p.scroll.selected
	if selected != 3 {
		t.Fatalf("committed search selected %d, want row 3", selected)
	}
	// Start another incremental search; its preview jumps the view elsewhere.
	p.startSearchInput()
	typeSearch(t, p, "one")
	if p.ScrollSelected() != 0 {
		t.Fatalf("preview selected %d, want row 0", p.ScrollSelected())
	}
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeyEscape}); !ok {
		t.Fatal("Esc was not taken while typing")
	}
	if p.scroll.typing || len(p.scroll.preview) != 0 {
		t.Fatalf("Esc left the input/preview: typing %v preview %v", p.scroll.typing, p.scroll.preview)
	}
	if p.scroll.top != top || p.scroll.selected != selected {
		t.Fatalf("Esc view = top %d selected %d, want %d/%d", p.scroll.top, p.scroll.selected, top, selected)
	}
	if p.SearchQuery() != "beta" || p.SearchMatches() != 1 {
		t.Fatalf("Esc changed the committed search: %q/%d", p.SearchQuery(), p.SearchMatches())
	}
}

// TestIncrementalSearchOnALiveTranscript checks the preview also runs over a live
// shell's history/screen rows (not only restored text rows).
func TestIncrementalSearchOnALiveTranscript(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20) // line-1..line-3 history and the live rows
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyKeys(t, p, cmd)
	p.startSearchInput()
	typeSearch(t, p, "line-3")
	if len(p.scroll.preview) != 1 {
		t.Fatalf("live preview = %v, want the one line-3 row", p.scroll.preview)
	}
	if got, ok := p.combinedText(p.ScrollSelected()); !ok || !strings.Contains(got, "line-3") {
		t.Fatalf("preview selected row = %q, %v", got, ok)
	}
}

// TestIncrementalSearchPreviewNextPrevious checks the Fresh search-prompt
// interaction: F3/Shift+F3 step the preview through the matches while the query
// stays editable, wrapping at the ends, and Enter commits the query with the same
// matches. (Fresh binds find_next/find_previous to F3/Shift+F3 when searchPrompt is
// open, keeping the bar open.)
func TestIncrementalSearchPreviewNextPrevious(t *testing.T) {
	t.Parallel()
	p := restoredSearchPanel(t, "alpha", "beta", "alphabet", "alpine", "gamma")
	p.startSearchInput()
	typeSearch(t, p, "al")
	if len(p.scroll.preview) != 3 || p.ScrollSelected() != 0 {
		t.Fatalf("preview = %v selected %d, want three matches starting at row 0", p.scroll.preview, p.ScrollSelected())
	}
	step := func(shift bool) {
		k := tea.Key{Code: tea.KeyF3}
		if shift {
			k.Mod = tea.ModShift
		}
		cmd, ok := p.ScrollKey(k)
		if !ok {
			t.Fatal("F3 was not taken by the open search input")
		}
		applyKeys(t, p, cmd)
	}
	step(false)
	if p.ScrollSelected() != 2 {
		t.Fatalf("F3 selected %d, want the second preview match row 2", p.ScrollSelected())
	}
	step(false)
	if p.ScrollSelected() != 3 {
		t.Fatalf("F3 selected %d, want the third preview match row 3", p.ScrollSelected())
	}
	step(false) // wraps
	if p.ScrollSelected() != 0 {
		t.Fatalf("F3 wrap selected %d, want row 0", p.ScrollSelected())
	}
	step(true) // previous wraps backwards
	if p.ScrollSelected() != 3 {
		t.Fatalf("Shift+F3 selected %d, want the last preview match row 3", p.ScrollSelected())
	}
	if !p.scroll.typing || p.SearchQuery() != "" {
		t.Fatalf("stepping closed or committed the search: typing %v query %q", p.scroll.typing, p.SearchQuery())
	}
	if title := p.title(); !strings.Contains(title, "/al") || strings.Contains(title, "1/3") {
		t.Fatalf("stepping title = %q, want the editable query without committed k/n", title)
	}
	// Enter commits the same query; n/N then step the committed matches.
	applyKeys(t, p, p.searchInputKey(tea.Key{Code: tea.KeyEnter}))
	if p.SearchQuery() != "al" || p.SearchMatches() != 3 {
		t.Fatalf("committed search = %q/%d", p.SearchQuery(), p.SearchMatches())
	}
	applyKeys(t, p, p.nextMatch(1))
	if p.ScrollSelected() != 2 {
		t.Fatalf("n after commit selected %d, want row 2", p.ScrollSelected())
	}
}

// TestIncrementalSearchStepOrdersAndRestores checks F3 answers follow the async
// ordering (a late answer for an older query is dropped), a first F3 on an
// un-answered query lands on its first match, Shift+F3 lands on its last, and Esc
// still restores the pre-search view.
func TestIncrementalSearchStepOrdersAndRestores(t *testing.T) {
	t.Parallel()
	p := restoredSearchPanel(t, "one", "two", "three", "beta", "five")
	// Commit "beta" (row 3) as the previous search.
	p.startSearchInput()
	typeSearch(t, p, "beta")
	applyKeys(t, p, p.searchInputKey(tea.Key{Code: tea.KeyEnter}))
	top, selected := p.scroll.top, p.scroll.selected
	// Start a new query but leave its typing answer un-applied; a step request must
	// supersede it (newer sequence), and a late typing answer must be dropped.
	p.startSearchInput()
	cmdTyping, _ := p.ScrollKey(tea.Key{Code: 'o', Text: "o"})
	cmdStep, _ := p.ScrollKey(tea.Key{Code: tea.KeyF3})
	if cmdTyping == nil || cmdStep == nil {
		t.Fatal("typing/F3 produced no search command")
	}
	applyKeys(t, p, cmdStep) // first F3 on the un-answered query lands on its first match
	if got, ok := p.combinedText(p.ScrollSelected()); !ok || !strings.Contains(got, "one") {
		t.Fatalf("first F3 selected %q, %v, want the first 'o' match", got, ok)
	}
	applyKeys(t, p, cmdTyping) // late answer for the pre-F3 typing
	if got, ok := p.combinedText(p.ScrollSelected()); !ok || !strings.Contains(got, "one") {
		t.Fatalf("a late typing answer moved the preview: %q, %v", got, ok)
	}
	// Shift+F3 on a fresh un-answered query lands on its last match.
	p2 := restoredSearchPanel(t, "alpha", "beta", "alphabet", "alpine")
	p2.startSearchInput()
	_, _ = p2.ScrollKey(tea.Key{Code: 'a', Text: "a"}) // un-applied answer for "a"
	cmdPrev, _ := p2.ScrollKey(tea.Key{Code: tea.KeyF3, Mod: tea.ModShift})
	applyKeys(t, p2, cmdPrev)
	last := len(p2.scroll.preview) - 1
	if p2.scroll.previewIdx != last || p2.ScrollSelected() != p2.scroll.preview[last] {
		t.Fatalf("first Shift+F3 idx %d selected %d preview %v, want the last match", p2.scroll.previewIdx, p2.ScrollSelected(), p2.scroll.preview)
	}
	// Esc drops the preview and restores the pre-search view/search.
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeyEscape}); !ok {
		t.Fatal("Esc was not taken")
	}
	if p.scroll.top != top || p.scroll.selected != selected || p.scroll.previewQuery != "" || p.scroll.previewIdx != 0 || p.scroll.previewStep != 0 {
		t.Fatalf("after Esc: top %d selected %d preview %q/%d/%d, want %d/%d and cleared", p.scroll.top, p.scroll.selected, p.scroll.previewQuery, p.scroll.previewIdx, p.scroll.previewStep, top, selected)
	}
	if p.SearchQuery() != "beta" || p.SearchMatches() != 1 {
		t.Fatalf("Esc changed the committed search: %q/%d", p.SearchQuery(), p.SearchMatches())
	}
}
