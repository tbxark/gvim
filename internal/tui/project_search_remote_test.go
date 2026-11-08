package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/searchpanel"
)

// newRemoteProjectModel returns a model over ed with the given project-search ports and
// the remote workspace marker set (the panel root must be the remote workspace).
func newRemoteProjectModel(t *testing.T, s searchpanel.Searcher, r searchpanel.Replacer, g searchpanel.GitGrepper) *Model {
	t.Helper()
	m := newProjectPanelModel(t, newProjEd(nil, nil), s, r, g)
	m.fileRoot = "/remote/w"
	return m
}

// TestRemoteProjectSearchPanelOpens checks a remote workspace now opens the Search and
// Replace panel with the authority-backed ports (search works remotely), while the replace
// confirmation path stays refused with a remote-specific notice.
func TestRemoteProjectSearchPanelOpens(t *testing.T) {
	t.Parallel()
	s := &fakeProjectSearcher{}
	r := &fakeProjectReplacer{}
	g := &fakeGitGrepper{}
	m := newRemoteProjectModel(t, s, r, g)
	before := len(m.overlays)
	must(t, m.Run("start_search_replace"))
	if len(m.overlays) != before+1 {
		t.Fatal("a remote Search and Replace did not open the panel")
	}
	// The replacement confirmation is still refused remotely: a safe remote replace is not
	// wired, so no prompt and no Replacer call.
	if cmd := m.askProjectReplace(searchpanel.ReplaceRequest{}); cmd != nil {
		t.Fatal("askProjectReplace returned a command in a remote workspace")
	}
	if len(r.reqs) != 0 {
		t.Fatalf("the local replacer was called remotely: %v", r.reqs)
	}
	if got := m.statusView().Message; !strings.Contains(got, "remote workspace") {
		t.Fatalf("replace notice = %q, want the remote refusal", got)
	}
}

// TestRemoteGitGrepPanelOpens checks Git Grep opens with the authority-backed grepper in a
// remote workspace.
func TestRemoteGitGrepPanelOpens(t *testing.T) {
	t.Parallel()
	m := newRemoteProjectModel(t, &fakeProjectSearcher{}, &fakeProjectReplacer{}, &fakeGitGrepper{})
	before := len(m.overlays)
	must(t, m.Run("start_git_grep"))
	if len(m.overlays) != before+1 {
		t.Fatal("a remote Git Grep did not open the panel")
	}
}

// TestRemoteSearchResultActionsStillWork checks the result-list and next/previous-match
// actions are not refused remotely any more: with remote search results they operate on
// the panel state, and with none they show the ordinary no-results notice.
func TestRemoteSearchResultActionsStillWork(t *testing.T) {
	t.Parallel()
	m := newRemoteProjectModel(t, &fakeProjectSearcher{}, &fakeProjectReplacer{}, &fakeGitGrepper{})
	for _, action := range []string{"search_replace_next_match", "search_replace_prev_match", "project_search_results"} {
		must(t, m.Run(action))
		got := m.statusView().Message
		if strings.Contains(got, "not available in a remote workspace") {
			t.Fatalf("%s was still refused remotely: %q", action, got)
		}
		if !strings.Contains(got, "No project search results") {
			t.Fatalf("%s notice = %q, want the ordinary no-results notice", action, got)
		}
	}
}

// TestLocalProjectSearchStillOpens checks the remote changes do not regress a local
// workspace: the same action opens the panel.
func TestLocalProjectSearchStillOpens(t *testing.T) {
	t.Parallel()
	m := newProjectPanelModel(t, newProjEd(nil, nil), &fakeProjectSearcher{}, &fakeProjectReplacer{}, &fakeGitGrepper{})
	before := len(m.overlays)
	must(t, m.Run("start_search_replace"))
	if len(m.overlays) == before {
		t.Fatal("a local Search and Replace did not open the panel")
	}
}

// TestRemoteProjectSearchUsesRemoteRoot checks a search typed in a remote workspace's panel
// runs through the remote ports with the remote workspace as its root (the local root is
// never used).
func TestRemoteProjectSearchUsesRemoteRoot(t *testing.T) {
	t.Parallel()
	s := &fakeProjectSearcher{}
	m := newRemoteProjectModel(t, s, &fakeProjectReplacer{}, &fakeGitGrepper{})
	must(t, m.Run("start_search_replace"))
	projectSearch(t, m, "todo")
	if len(s.reqs) == 0 {
		t.Fatal("the remote-search port was not asked")
	}
	if got := s.reqs[len(s.reqs)-1].Root; got != "/remote/w" {
		t.Fatalf("search root = %q, want the remote workspace", got)
	}
}

// TestRemoteGitGrepUsesRemoteRoot checks a Git Grep query in a remote workspace runs through
// the remote grepper with the remote workspace root.
func TestRemoteGitGrepUsesRemoteRoot(t *testing.T) {
	t.Parallel()
	g := &fakeGitGrepper{}
	m := newRemoteProjectModel(t, &fakeProjectSearcher{}, &fakeProjectReplacer{}, g)
	must(t, m.Run("start_git_grep"))
	projectSearch(t, m, "todo")
	if len(g.reqs) == 0 {
		t.Fatal("the remote git-grep port was not asked")
	}
	if got := g.reqs[len(g.reqs)-1].Root; got != "/remote/w" {
		t.Fatalf("git grep root = %q, want the remote workspace", got)
	}
}
