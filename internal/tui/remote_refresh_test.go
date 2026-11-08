package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/theme"
)

// restartIndex is a Files port that can re-enumerate in place.
type restartIndex struct {
	files    []string
	restarts int
}

func (r *restartIndex) Snapshot() ([]string, bool) { return r.files, false }
func (r *restartIndex) Done() <-chan struct{}      { return nil }
func (r *restartIndex) Close()                     {}
func (r *restartIndex) Restart()                   { r.restarts++ }

// TestExplorerRefreshRestartsTheFileIndex checks the File Explorer: Refresh action also
// re-enumerates the file list (the on-demand finder refresh), off the update goroutine.
func TestExplorerRefreshRestartsTheFileIndex(t *testing.T) {
	idx := &restartIndex{files: []string{"a.go"}}
	m := New(Deps{Editor: newFakeEditor("x"), Explorer: newFakeTree(1), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Files: idx})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if m.fileIndexRefreshCmd() == nil {
		t.Fatal("the model's files cannot restart, but the fake implements Restart")
	}
	m2, cmd := m.Update(explorer.ReloadMsg{})
	if m2 == nil || cmd == nil {
		t.Fatal("the explorer refresh scheduled no command")
	}
	collect(cmd)
	if idx.restarts != 1 {
		t.Fatalf("restarts = %d, want 1", idx.restarts)
	}
}

// TestReconnectRefreshesGitMarks checks a successful reconnect schedules a git marks read
// (the remote read goes through the authority port), and a failed one does not.
func TestReconnectRefreshesGitMarks(t *testing.T) {
	g := &fakeGit{marks: map[string]rune{"/ws/a.go": 'M'}}
	m := newGitModel(t, g)
	g.asked = nil
	m.SetAuthorityHooks(AuthorityHooks{AfterReconnect: func() (int, error) { return 0, nil }})
	_, cmd := m.Update(authorityReconnectedMsg{})
	if cmd == nil {
		t.Fatal("a successful reconnect scheduled no commands")
	}
	collect(cmd)
	if len(g.asked) != 1 || g.asked[0] != "/ws" {
		t.Fatalf("git reads after reconnect = %v, want the tree root", g.asked)
	}
	g.asked = nil
	_, cmd = m.Update(authorityReconnectedMsg{err: errReconnect})
	if cmd != nil {
		collect(cmd)
	}
	if len(g.asked) != 0 {
		t.Fatalf("a failed reconnect read git: %v", g.asked)
	}
}

var errReconnect = &fakeReconnectError{}

type fakeReconnectError struct{}

func (*fakeReconnectError) Error() string { return "dial failed" }
