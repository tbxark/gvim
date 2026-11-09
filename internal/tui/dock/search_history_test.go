package dock

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// commitSearch opens the input (applying its initial preview), types query over
// any pre-fill, and commits it with Enter.
func commitSearch(t *testing.T, p *Panel, query string) {
	t.Helper()
	if !p.Scrolling() {
		// A live tab enters the pager first (Ctrl+Space), like the user does.
		enter, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
		applyKeys(t, p, enter)
		if !p.Scrolling() {
			t.Fatal("Ctrl+Space did not enter the scrollback view")
		}
	}
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
	if len(p.searchHistoryEntries()) != 2 || p.searchHistoryEntries()[0] != "beta" || p.searchHistoryEntries()[1] != "two" {
		t.Fatalf("search history = %v, want [beta two]", p.searchHistoryEntries())
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
	if p.SearchQuery() != "x" || len(p.searchHistoryEntries()) != 3 {
		t.Fatalf("committed restored input = %q history %v", p.SearchQuery(), p.searchHistoryEntries())
	}
	commitSearch(t, p, "two")
	if len(p.searchHistoryEntries()) != 4 {
		t.Fatalf("history after committing two = %v", p.searchHistoryEntries())
	}
	// A second immediate commit of the same query is a newest-duplicate: reopen and
	// press Enter without editing the selected pre-fill.
	openAgain, _ := p.ScrollKey(tea.Key{Code: '/', Text: "/"})
	applyKeys(t, p, openAgain)
	applyKeys(t, p, p.searchInputKey(tea.Key{Code: tea.KeyEnter}))
	if len(p.searchHistoryEntries()) != 4 {
		t.Fatalf("history after a duplicate commit = %v", p.searchHistoryEntries())
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
	if len(p.searchHistoryEntries()) != 1 || p.searchHistoryEntries()[0] != "a" {
		t.Fatalf("history after duplicates = %v", p.searchHistoryEntries())
	}
	for i := 0; i < terminal.MaxSearchHistoryEntries+20; i++ {
		p.rememberSearch(fmt.Sprintf("q%d", i))
	}
	if len(p.searchHistoryEntries()) != terminal.MaxSearchHistoryEntries {
		t.Fatalf("history length = %d, want %d", len(p.searchHistoryEntries()), terminal.MaxSearchHistoryEntries)
	}
	if first := p.searchHistoryEntries()[0]; first != "q20" {
		t.Fatalf("oldest history entry = %q, want q20", first)
	}
	if last := p.searchHistoryEntries()[len(p.searchHistoryEntries())-1]; last != fmt.Sprintf("q%d", terminal.MaxSearchHistoryEntries+19) {
		t.Fatalf("newest history entry = %q", last)
	}
	// The save side is bounded too: a long/control query is cleaned and cut to the
	// record's 512-byte limit, and the snapshot never exceeds 100 entries.
	p.rememberSearch(strings.Repeat("x", terminal.MaxSearchQueryBytes+30))
	p.rememberSearch("bad\x01\nquery")
	snapshot := p.historyStore().snapshot()
	if len(snapshot) > terminal.MaxSearchHistoryEntries {
		t.Fatalf("snapshot length = %d", len(snapshot))
	}
	last := snapshot[len(snapshot)-1]
	if last != "badquery" {
		t.Fatalf("cleaned snapshot query = %q", last)
	}
	long := snapshot[len(snapshot)-2]
	if len(long) != terminal.MaxSearchQueryBytes {
		t.Fatalf("snapshot long query length = %d, want %d", len(long), terminal.MaxSearchQueryBytes)
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

// applyTabStart runs one tab-start command and the commands Update answers with
// (bounded depth), so history/start state is settled like in the running dock.
func applyTabStart(t *testing.T, tabs *Tabs, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("start did not return a command")
	}
	var run func(cmd tea.Cmd, depth int)
	run = func(cmd tea.Cmd, depth int) {
		if cmd == nil || depth > 3 {
			return
		}
		for _, msg := range messages(cmd) {
			if next, ok := tabs.Update(msg); ok {
				run(next, depth+1)
			}
		}
	}
	run(cmd, 0)
}

// TestDockSearchHistoryIsSharedAndPersisted checks the slice: one dock-wide history
// shared by every tab (Fresh's window-level search prompt history), saved into the
// terminal state, restored without starting anything, and absent (empty) in an old
// record that has no field.
func TestDockSearchHistoryIsSharedAndPersisted(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: starter})
	applyTabStart(t, &tabs, tabs.NewTab())
	first := tabs.tabs[0]
	commitSearch(t, first, "alpha")
	applyTabStart(t, &tabs, tabs.NewTab())
	second := tabs.tabs[len(tabs.tabs)-1]
	if got := second.searchHistoryEntries(); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("second tab history = %v, want the first tab's query", got)
	}
	// Opening the search input in the other tab pre-fills the shared newest query.
	if !second.Scrolling() {
		enter, _ := second.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
		applyKeys(t, second, enter)
	}
	open, _ := second.ScrollKey(tea.Key{Code: '/', Text: "/"})
	applyKeys(t, second, open)
	if second.scroll.searchInput != "alpha" || !second.scroll.searchPrefill {
		t.Fatalf("second tab prefill = %q/%v", second.scroll.searchInput, second.scroll.searchPrefill)
	}
	applyKeys(t, second, second.searchInputKey(tea.Key{Code: tea.KeyEscape}))
	commitSearch(t, second, "beta")
	if got := first.searchHistoryEntries(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("shared history = %v, want [alpha beta]", got)
	}
	// The state carries the dock-wide history; a restore installs it without
	// starting a process (a transcript-only restore has no starter calls).
	saved := tabs.SavedState()
	if len(saved.SearchHistory) != 2 || saved.SearchHistory[1] != "beta" {
		t.Fatalf("saved search history = %v", saved.SearchHistory)
	}
	restoredStarter := &fakeStarter{term: newFakeTerm()}
	var restored Tabs
	restored.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	restored.SetPorts(Ports{Starter: restoredStarter})
	restored.Restore(saved)
	if len(restoredStarter.dirs) != 0 || len(restoredStarter.launches) != 0 {
		t.Fatalf("restore started a process: dirs %v launches %v", restoredStarter.dirs, restoredStarter.launches)
	}
	if got := restored.history.entries(); len(got) != 2 || got[0] != "alpha" {
		t.Fatalf("restored history = %v", got)
	}
	openRestored, _ := restored.tabs[0].ScrollKey(tea.Key{Code: '/', Text: "/"})
	applyKeys(t, restored.tabs[0], openRestored)
	if restored.tabs[0].scroll.searchInput != "beta" {
		t.Fatalf("restored prefill = %q, want beta", restored.tabs[0].scroll.searchInput)
	}
	// Old record compatibility: no field means an empty history, still no process.
	var legacy Tabs
	legacy.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	legacyStarter := &fakeStarter{term: newFakeTerm()}
	legacy.SetPorts(Ports{Starter: legacyStarter})
	legacy.Restore(terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"out"}})
	if len(legacy.history.entries()) != 0 {
		t.Fatalf("legacy record history = %v, want empty", legacy.history.entries())
	}
	if len(legacyStarter.dirs) != 0 || len(legacyStarter.launches) != 0 {
		t.Fatalf("legacy restore started a process: %v/%v", legacyStarter.dirs, legacyStarter.launches)
	}
}

// TestDockSearchHistoryMergeBoundsAndCleans checks a migrated workspace's queries are
// cleaned, deduped against the dock's own history and bounded like a saved record,
// purely in memory (no tab/process is touched).
func TestDockSearchHistoryMergeBoundsAndCleans(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	starter := &fakeStarter{term: term}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: starter})
	applyTabStart(t, &tabs, tabs.NewTab())
	tabs.MergeSearchHistory([]string{"alpha", "beta", "alpha", "bad\x01query"})
	if got := tabs.SearchHistory(); len(got) != 3 || got[0] != "alpha" || got[1] != "beta" || got[2] != "badquery" {
		t.Fatalf("merged history = %v", got)
	}
	if len(starter.dirs) != 1 || len(starter.launches) != 0 {
		t.Fatalf("merging history started a process: dirs %v launches %v", starter.dirs, starter.launches)
	}
	if got := tabs.TabNames(); len(got) != 1 {
		t.Fatalf("merging history changed the tabs: %v", got)
	}
	// The merge honors the record cap too.
	queries := make([]string, 0, terminal.MaxSearchHistoryEntries+10)
	for i := 0; i < terminal.MaxSearchHistoryEntries+10; i++ {
		queries = append(queries, fmt.Sprintf("m%d", i))
	}
	tabs.MergeSearchHistory(queries)
	if got := tabs.SearchHistory(); len(got) != terminal.MaxSearchHistoryEntries {
		t.Fatalf("merged history length = %d, want %d", len(got), terminal.MaxSearchHistoryEntries)
	}
	if got := tabs.SearchHistory(); got[len(got)-1] != fmt.Sprintf("m%d", terminal.MaxSearchHistoryEntries+9) {
		t.Fatalf("merged newest = %q", got[len(got)-1])
	}
}
