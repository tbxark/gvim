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
