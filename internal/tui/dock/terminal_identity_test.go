package dock

import (
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

// identityTabs builds a dock with one running fake terminal per term. The starter
// answers each NewTab with the next term, so the tabs have real live shells.
func identityTabs(t *testing.T, terms ...*fakeTerm) *Tabs {
	t.Helper()
	starter := &fakeStarter{}
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: starter})
	for _, term := range terms {
		starter.term = term
		applyTabStart(t, &tabs, tabs.NewTab())
	}
	return &tabs
}

// TestTabIDsStableAcrossMoveCloseAndDuplicateLabels checks the runtime identity is
// independent of the position and the display name: moving or renaming a tab keeps
// its id, closing it drops the id for good, and a later tab never reuses it.
func TestTabIDsStableAcrossMoveCloseAndDuplicateLabels(t *testing.T) {
	t.Parallel()
	tabs := identityTabs(t, newFakeTerm(), newFakeTerm())
	first, second := tabs.TabID(0), tabs.TabID(1)
	if first == 0 || second == 0 || first == second {
		t.Fatalf("tab ids = %d, %d", first, second)
	}
	// Duplicate labels must not change the identity mapping.
	tabs.SetActive(0)
	tabs.RenameActive("same")
	tabs.SetActive(1)
	tabs.RenameActive("same")
	if i, ok := tabs.IndexForID(second); !ok || i != 1 {
		t.Fatalf("second id -> %d, %v", i, ok)
	}
	// Move tab 0 right: the ids follow the tabs, not the positions.
	tabs.SetActive(0)
	if !tabs.MoveActive(1) {
		t.Fatal("the tab did not move")
	}
	if got := tabs.TabID(0); got != second {
		t.Fatalf("tab at 0 = %d, want the moved second id %d", got, second)
	}
	if i, ok := tabs.IndexForID(first); !ok || i != 1 {
		t.Fatalf("first id after the move -> %d, %v", i, ok)
	}
	// Close the tab: its id is gone and cannot address the remaining/new tabs.
	tabs.removeAt(0)
	if _, ok := tabs.IndexForID(second); ok {
		t.Fatal("a closed tab's id still resolves")
	}
	for i := 0; i < tabs.Count(); i++ {
		if tabs.TabID(i) == second {
			t.Fatalf("closed id %d is still in use", second)
		}
	}
	// A later tab gets a fresh id, not the closed one.
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	applyTabStart(t, tabs, tabs.NewTab())
	if got := tabs.TabID(tabs.Count() - 1); got == 0 || got == second {
		t.Fatalf("new tab id = %d, want a fresh id (closed %d)", got, second)
	}
}

// TestTranscriptsCarryIdentityAndCombinedOffset checks Transcripts uses the stable
// ids and reports the combined scrollback row of the window's first row, so a hit
// line can be revealed exactly; the newest history window is kept.
func TestTranscriptsCarryIdentityAndCombinedOffset(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.history = make([]string, 600)
	for i := range term.history {
		term.history[i] = "row"
	}
	tabs := identityTabs(t, term)
	got := tabs.Transcripts()
	if len(got) != 1 {
		t.Fatalf("transcripts = %d", len(got))
	}
	if got[0].ID != tabs.TabID(0) || got[0].ID == 0 {
		t.Fatalf("transcript id = %d, want %d", got[0].ID, tabs.TabID(0))
	}
	// 600 history rows: the newest 500 are kept, so First is 100 (no prior transcript,
	// no screen rows in the fake). The rows are the history's tail.
	if got[0].First != 100 {
		t.Fatalf("transcript First = %d, want 100", got[0].First)
	}
	if len(got[0].Lines) != 500 || got[0].Lines[0] != "row" {
		t.Fatalf("transcript lines = %d", len(got[0].Lines))
	}
}

// TestRevealTranscriptLineFocusesAndSelectsRow checks a stable-id reveal switches to
// the tab, enters the read-only pager and selects the requested combined row, while
// starting no shell; a restored no-session tab and a stale id report false.
func TestRevealTranscriptLineFocusesAndSelectsRow(t *testing.T) {
	t.Parallel()
	tabs := identityTabs(t, newFakeTerm(), newFakeTerm())
	// Give the second tab a history so entering the pager has rows to fetch, while
	// still starting no shell: the history is the in-memory fake, not a process.
	tabs.tabs[1].term.(*fakeTerm).history = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n"}
	second := tabs.TabID(1)
	dirs := len(tabs.ports.Starter.(*fakeStarter).dirs)
	cmd, ok := tabs.RevealTranscriptLine(second, 12)
	if !ok || cmd == nil {
		t.Fatalf("reveal = %v, %v", cmd, ok)
	}
	if tabs.ActiveIndex() != 1 || !tabs.Scrolling() {
		t.Fatalf("active %d scrolling %v", tabs.ActiveIndex(), tabs.Scrolling())
	}
	if got := tabs.ScrollSelected(); got != 12 {
		t.Fatalf("selected row = %d, want 12", got)
	}
	if got := len(tabs.ports.Starter.(*fakeStarter).dirs); got != dirs {
		t.Fatalf("reveal started %d shells", got-dirs)
	}
	// A closed tab's id is refused without changing the active tab.
	tabs.removeAt(1)
	before := tabs.ActiveIndex()
	if _, ok := tabs.RevealTranscriptLine(second, 12); ok {
		t.Fatal("a stale id revealed a tab")
	}
	if tabs.ActiveIndex() != before {
		t.Fatal("a stale id changed the active tab")
	}
}

// TestRevealTranscriptLineSupportsExitedAndRefusesRestored checks an exited live tab
// can still be positioned (its scrollback transcript remains), while a restored tab
// with no session has no pager and reports false.
func TestRevealTranscriptLineSupportsExitedAndRefusesRestored(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	tabs := identityTabs(t, term)
	id := tabs.TabID(0)
	p := tabs.Active()
	if cmd, _ := p.Update(exitMsg{gen: p.gen, code: 0, codeOK: true}); cmd != nil {
		_ = cmd
	}
	if _, ok := tabs.RevealTranscriptLine(id, 1); !ok {
		t.Fatal("an exited live tab was not positioned")
	}

	restored := identityTabs(t, newFakeTerm())
	restoredID := restored.TabID(0)
	restored.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{{Open: true, Rows: []string{"saved line"}}},
	})
	if _, ok := restored.RevealTranscriptLine(restoredID, 0); ok {
		t.Fatal("a restored no-session tab reported a pager")
	}
}

// TestStartDiscoveredKeepsIDsAligned checks every tab append carries a stable id:
// StartDiscovered (the StartProgram/agent/discovery path) used to append tabs and
// names without ids, so Transcripts/TabID panicked or returned the wrong identity.
// Later NewTab appends must stay aligned too.
func TestStartDiscoveredKeepsIDsAligned(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: starter})
	cmd, ok := tabs.StartDiscovered([]string{"/usr/bin/claude", "--x"}, "discovered")
	if !ok || cmd == nil {
		t.Fatalf("StartDiscovered = %v, %v", cmd, ok)
	}
	applyTabStart(t, &tabs, cmd)
	if tabs.Count() != 1 || len(tabs.ids) != 1 {
		t.Fatalf("tabs/ids = %d/%d after StartDiscovered", tabs.Count(), len(tabs.ids))
	}
	if id := tabs.TabID(0); id == 0 || id != tabs.ids[0] {
		t.Fatalf("discovered tab id = %d, ids %v", id, tabs.ids)
	}
	tr := tabs.Transcripts()
	if len(tr) != 1 || tr[0].ID == 0 || tr[0].ID != tabs.TabID(0) {
		t.Fatalf("transcripts after StartDiscovered = %+v", tr)
	}
	// A later NewTab keeps the parallel arrays aligned and gives a fresh id.
	starter.term = newFakeTerm()
	applyTabStart(t, &tabs, tabs.NewTab())
	if tabs.Count() != 2 || len(tabs.ids) != 2 {
		t.Fatalf("tabs/ids = %d/%d after NewTab", tabs.Count(), len(tabs.ids))
	}
	if a, b := tabs.TabID(0), tabs.TabID(1); a == 0 || b == 0 || a == b {
		t.Fatalf("ids after NewTab = %d, %d", a, b)
	}
	if tr := tabs.Transcripts(); len(tr) != 2 || tr[1].ID != tabs.TabID(1) {
		t.Fatalf("transcripts after NewTab = %+v", tr)
	}
}
