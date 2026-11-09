package dock

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// commitSearch opens the input (applying its initial preview), types query over
// any pre-fill, and commits it with Enter.
func commitSearch(t *testing.T, p *Panel, query string) {
	t.Helper()
	cmd, ok := p.ScrollKey(tea.Key{Code: '/', Text: "/"})
	if !ok || !p.scroll.typing {
		t.Fatal("'/' did not open the search input")
	}
	applyKeys(t, p, cmd)
	typeSearch(t, p, query)
	applyKeys(t, p, p.searchInputKey(tea.Key{Code: tea.KeyEnter}))
	if p.SearchQuery() != query {
		t.Fatalf("committed search = %q, want %q", p.SearchQuery(), query)
	}
}

// TestSearchHistoryPrefillRecallAndCommit checks the Fresh prompt-history
// interaction: the last query is pre-filled and selected on open (typing replaces
// it), Up recalls older queries with an async preview, Up at the oldest and Down
// past the newest behave, and a committed query is recorded once (newest-duplicate
// rule).
func TestSearchHistoryPrefillRecallAndCommit(t *testing.T) {
	t.Parallel()
	p := restoredSearchPanel(t, "one", "two", "three", "beta", "five")
	commitSearch(t, p, "beta")
	commitSearch(t, p, "two")
	if len(p.searchHistory) != 2 || p.searchHistory[0] != "beta" || p.searchHistory[1] != "two" {
		t.Fatalf("search history = %v, want [beta two]", p.searchHistory)
	}
	// Re-opening pre-fills the newest query, selected: a typed rune replaces it.
	openCmd, ok := p.ScrollKey(tea.Key{Code: '/', Text: "/"})
	if !ok {
		t.Fatal("'/' did not reopen the search input")
	}
	if !p.scroll.typing || p.scroll.searchInput != "two" || !p.scroll.searchPrefill {
		t.Fatalf("reopened input = %q prefill %v typing %v", p.scroll.searchInput, p.scroll.searchPrefill, p.scroll.typing)
	}
	applyKeys(t, p, openCmd) // initial preview of the pre-filled query
	if p.scroll.previewQuery != "two" || len(p.scroll.preview) != 1 {
		t.Fatalf("pre-filled preview = %q %v, want the one 'two' row", p.scroll.previewQuery, p.scroll.preview)
	}
	// Typing over the selection starts a new query, not an append.
	typed, _ := p.ScrollKey(tea.Key{Code: 'x', Text: "x"})
	applyKeys(t, p, typed)
	if p.scroll.searchInput != "x" {
		t.Fatalf("typing over the pre-fill gave %q, want x", p.scroll.searchInput)
	}
	// Recall older: Up lands on the previous query and previews it asynchronously.
	up, ok := p.ScrollKey(tea.Key{Code: tea.KeyUp})
	if !ok || up == nil {
		t.Fatalf("Up did not recall history: %v, %v", up, ok)
	}
	applyKeys(t, p, up)
	if p.scroll.searchInput != "two" || p.scroll.searchHistPos != 1 {
		t.Fatalf("first Up = %q pos %d", p.scroll.searchInput, p.scroll.searchHistPos)
	}
	up, _ = p.ScrollKey(tea.Key{Code: tea.KeyUp})
	applyKeys(t, p, up)
	if p.scroll.searchInput != "beta" || p.scroll.searchHistPos != 0 {
		t.Fatalf("second Up = %q pos %d", p.scroll.searchInput, p.scroll.searchHistPos)
	}
	// Up at the oldest keeps the input.
	up, _ = p.ScrollKey(tea.Key{Code: tea.KeyUp})
	applyKeys(t, p, up)
	if p.scroll.searchInput != "beta" {
		t.Fatalf("Up at the oldest changed the input: %q", p.scroll.searchInput)
	}
	// Down returns to the newer query; a late answer for the older recall must be
	// dropped by the request ordering.
	betaCmd, _ := p.ScrollKey(tea.Key{Code: tea.KeyUp}) // no-op at oldest
	_ = betaCmd
	down, _ := p.ScrollKey(tea.Key{Code: tea.KeyDown})
	applyKeys(t, p, down)
	if p.scroll.searchInput != "two" || p.scroll.searchHistPos != 1 {
		t.Fatalf("Down = %q pos %d", p.scroll.searchInput, p.scroll.searchHistPos)
	}
	if p.scroll.previewQuery != "two" || len(p.scroll.preview) != 1 {
		t.Fatalf("Down preview = %q %v", p.scroll.previewQuery, p.scroll.preview)
	}
	// Down past the newest restores the input the navigation started from (the
	// typed 'x' overwrote the pre-fill) and resets navigation.
	down, _ = p.ScrollKey(tea.Key{Code: tea.KeyDown})
	applyKeys(t, p, down)
	if p.scroll.searchHistPos != -1 || p.scroll.searchInput != "x" {
		t.Fatalf("Down past the newest = %q pos %d", p.scroll.searchInput, p.scroll.searchHistPos)
	}
	// Committing the restored input records 'x' (new query), then committing 'two'
	// again is skipped as the newest duplicate only when 'two' is already newest.
	applyKeys(t, p, p.searchInputKey(tea.Key{Code: tea.KeyEnter}))
	if p.SearchQuery() != "x" || len(p.searchHistory) != 3 {
		t.Fatalf("committed restored input = %q history %v", p.SearchQuery(), p.searchHistory)
	}
	commitSearch(t, p, "two")
	if len(p.searchHistory) != 4 {
		t.Fatalf("history after committing two = %v", p.searchHistory)
	}
	// A second immediate commit of the same query is a newest-duplicate: reopen and
	// press Enter without editing the selected pre-fill.
	openAgain, _ := p.ScrollKey(tea.Key{Code: '/', Text: "/"})
	applyKeys(t, p, openAgain)
	applyKeys(t, p, p.searchInputKey(tea.Key{Code: tea.KeyEnter}))
	if len(p.searchHistory) != 4 {
		t.Fatalf("history after a duplicate commit = %v", p.searchHistory)
	}
	if p.SearchQuery() != "two" || p.SearchMatches() != 1 {
		t.Fatalf("committed recalled query = %q/%d", p.SearchQuery(), p.SearchMatches())
	}
}

// TestSearchHistoryIsBoundedAndSkipsDuplicates checks the InputHistory rules
// directly: empty queries and an exact repeat of the newest entry are skipped and
// the oldest entry is dropped past 100.
func TestSearchHistoryIsBoundedAndSkipsDuplicates(t *testing.T) {
	t.Parallel()
	var p Panel
	p.rememberSearch("")
	p.rememberSearch("a")
	p.rememberSearch("a")
	if len(p.searchHistory) != 1 || p.searchHistory[0] != "a" {
		t.Fatalf("history after duplicates = %v", p.searchHistory)
	}
	for i := 0; i < searchHistoryMax+20; i++ {
		p.rememberSearch(fmt.Sprintf("q%d", i))
	}
	if len(p.searchHistory) != searchHistoryMax {
		t.Fatalf("history length = %d, want %d", len(p.searchHistory), searchHistoryMax)
	}
	if first := p.searchHistory[0]; first != "q20" {
		t.Fatalf("oldest history entry = %q, want q20", first)
	}
	if last := p.searchHistory[len(p.searchHistory)-1]; last != fmt.Sprintf("q%d", searchHistoryMax+19) {
		t.Fatalf("newest history entry = %q", last)
	}
}

// TestSearchCancelOnFocusLoss checks Esc-equivalent behaviour when the dock loses
// the keyboard focus with the input open: the input/preview are dropped, the
// saved view and committed search are restored, pending async answers are ignored,
// and nothing keeps consuming keys after the dock is unfocused.
func TestSearchCancelOnFocusLoss(t *testing.T) {
	t.Parallel()
	p := restoredSearchPanel(t, "alpha", "beta", "gamma")
	commitSearch(t, p, "beta")
	top, selected := p.scroll.top, p.scroll.selected
	// Open a new query and leave its incremental answer un-applied.
	if _, ok := p.ScrollKey(tea.Key{Code: '/', Text: "/"}); !ok {
		t.Fatal("'/' did not open the input")
	}
	p.scroll.searchInput = ""
	p.scroll.searchPrefill = false
	typeSearch(t, p, "alpha")
	pending, _ := p.ScrollKey(tea.Key{Code: 'p', Text: "p"}) // an in-flight preview
	if pending == nil {
		t.Fatal("typing produced no preview command")
	}
	p.SetFocus(false)
	if p.scroll.typing || p.scroll.searchInput != "" || len(p.scroll.preview) != 0 {
		t.Fatalf("focus loss left the input: typing %v input %q preview %v", p.scroll.typing, p.scroll.searchInput, p.scroll.preview)
	}
	if p.scroll.top != top || p.scroll.selected != selected {
		t.Fatalf("focus loss view = %d/%d, want %d/%d", p.scroll.top, p.scroll.selected, top, selected)
	}
	if p.SearchQuery() != "beta" || p.SearchMatches() != 1 {
		t.Fatalf("focus loss changed the committed search: %q/%d", p.SearchQuery(), p.SearchMatches())
	}
	// The late preview answer for the old input must be ignored.
	applyKeys(t, p, pending)
	if p.scroll.typing || len(p.scroll.preview) != 0 || p.scroll.top != top || p.scroll.selected != selected {
		t.Fatalf("a late preview acted after focus loss: typing %v preview %v view %d/%d", p.scroll.typing, p.scroll.preview, p.scroll.top, p.scroll.selected)
	}
	// Re-focused, a plain key is not swallowed by a hidden search input.
	p.SetFocus(true)
	if _, ok := p.ScrollKey(tea.Key{Code: 'x', Text: "x"}); !ok {
		t.Fatal("the pager did not take a plain key after the focus round trip")
	}
	if p.scroll.typing {
		t.Fatal("the search input reopened after the focus round trip")
	}
}
