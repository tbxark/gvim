package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/searchpanel"
)

// newRemoteProjectModel returns a model over ed with the given project-search ports and
// the remote workspace marker set, so the local-filesystem search entry points must be
// refused.
func newRemoteProjectModel(t *testing.T, ed EditorState, s searchpanel.Searcher, r searchpanel.Replacer, g searchpanel.GitGrepper) *Model {
	t.Helper()
	m := newProjectPanelModel(t, ed, s, r, g)
	m.fileRoot = "/remote/w"
	return m
}

// TestRemoteProjectSearchEntryPointsRefused checks every search/replace entry point in a
// remote workspace is refused with a clear notice and never calls the local ports: open
// Search and Replace, open Git Grep, next/previous match, results list and the replacement
// confirmation path.
func TestRemoteProjectSearchEntryPointsRefused(t *testing.T) {
	t.Parallel()
	s := &fakeProjectSearcher{}
	r := &fakeProjectReplacer{}
	g := &fakeGitGrepper{}
	m := newRemoteProjectModel(t, newProjEd(nil, nil), s, r, g)
	overlays := len(m.overlays)
	for _, action := range []string{
		"start_search_replace", "start_git_grep", "search_replace_next_match",
		"search_replace_prev_match", "project_search_results",
	} {
		must(t, m.Run(action))
		if len(m.overlays) != overlays {
			t.Fatalf("%s opened an overlay in a remote workspace", action)
		}
		if got := m.statusView().Message; !strings.Contains(got, "remote workspace") {
			t.Fatalf("%s notice = %q, want the remote refusal", action, got)
		}
	}
	// The replacement confirmation path must refuse before opening a prompt or calling
	// the replacer.
	if cmd := m.askProjectReplace(searchpanel.ReplaceRequest{}); cmd != nil {
		t.Fatal("askProjectReplace returned a command in a remote workspace")
	}
	if len(m.overlays) != overlays {
		t.Fatal("askProjectReplace opened a prompt in a remote workspace")
	}
	if len(s.reqs) != 0 || len(r.reqs) != 0 || len(g.reqs) != 0 {
		t.Fatalf("local ports were called: search %v replace %v git %v", s.reqs, r.reqs, g.reqs)
	}
}

// TestLocalProjectSearchStillOpens checks the guard does not regress a local workspace:
// the same actions still open the project panel.
func TestLocalProjectSearchStillOpens(t *testing.T) {
	t.Parallel()
	s := &fakeProjectSearcher{}
	r := &fakeProjectReplacer{}
	g := &fakeGitGrepper{}
	m := newProjectPanelModel(t, newProjEd(nil, nil), s, r, g)
	before := len(m.overlays)
	must(t, m.Run("start_search_replace"))
	if len(m.overlays) == before {
		t.Fatal("a local Search and Replace did not open the panel")
	}
}
