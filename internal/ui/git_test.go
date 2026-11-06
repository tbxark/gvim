package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
)

// fakeGit records the directories it was asked about and answers with marks.
type fakeGit struct {
	marks map[string]rune
	err   error
	asked []string
}

func (g *fakeGit) Marks(dir string) (map[string]rune, error) {
	g.asked = append(g.asked, dir)
	return g.marks, g.err
}

// collect runs cmd and returns the messages it produced, with the messages of a
// batch flattened. It must not be given a command that waits, such as a tick.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, collect(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// newGitModel returns a model over a tree rooted at /ws with the git port g.
func newGitModel(t *testing.T, g GitStatus) *Model {
	t.Helper()
	ed := newFakeEditor("x")
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(3),
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Git: g},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

func TestInitReadsGitStatusOfTheTreeRoot(t *testing.T) {
	t.Parallel()
	g := &fakeGit{marks: map[string]rune{"/ws/f1": 'M'}}
	m := newGitModel(t, g)
	msgs := collect(m.Init())
	if len(msgs) != 1 {
		t.Fatalf("Init sent %d messages, want the git status only", len(msgs))
	}
	if len(g.asked) != 1 || g.asked[0] != "/ws" {
		t.Fatalf("git status asked about %v, want the tree root /ws", g.asked)
	}
	if _, ok := msgs[0].(gitStatusMsg); !ok {
		t.Fatalf("Init sent %T, want gitStatusMsg", msgs[0])
	}
}

func TestGitStatusMarksReachTheExplorer(t *testing.T) {
	t.Parallel()
	g := &fakeGit{marks: map[string]rune{"/ws/f1": 'M'}}
	m := newGitModel(t, g)
	m.Update(gitStatusMsg{root: "/ws", marks: g.marks})
	if got := m.explorerGeometry(m.layout()).Marks["/ws/f1"]; got != 'M' {
		t.Fatalf("explorer mark of f1 = %q, want M", got)
	}
}

func TestGitStatusForAnotherRootIsIgnored(t *testing.T) {
	t.Parallel()
	m := newGitModel(t, &fakeGit{})
	m.Update(gitStatusMsg{root: "/other", marks: map[string]rune{"/other/x": 'A'}})
	if len(m.root.gitMarks) != 0 {
		t.Fatalf("marks of a root that was left were applied: %v", m.root.gitMarks)
	}
}

func TestGitStatusErrorIsANoticeAndClearsMarks(t *testing.T) {
	t.Parallel()
	m := newGitModel(t, &fakeGit{})
	m.Update(gitStatusMsg{root: "/ws", marks: map[string]rune{"/ws/f1": 'M'}})
	m.Update(gitStatusMsg{root: "/ws", err: errors.New("corrupt index")})
	if len(m.root.gitMarks) != 0 {
		t.Errorf("marks kept after a failed status: %v", m.root.gitMarks)
	}
	if got := m.statusView().message; got != "git status: corrupt index" {
		t.Errorf("status message = %q, want the git error", got)
	}
}

func TestFileOperationRefreshesGitStatus(t *testing.T) {
	t.Parallel()
	g := &fakeGit{marks: map[string]rune{"/ws/new.txt": 'U'}}
	m := newGitModel(t, g)
	_, cmd := m.Update(fileResultMsg{job: fileJob{kind: jobCreate, dir: "/ws", name: "new.txt"}, out: "/ws/new.txt"})
	var got bool
	for _, msg := range collect(cmd) {
		if _, ok := msg.(gitStatusMsg); ok {
			got = true
		}
	}
	if !got {
		t.Fatal("a file operation did not start a git status read")
	}
}

func TestNoGitPortSendsNoStatus(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), newFakeTree(2), 80, 20)
	if msgs := collect(m.Init()); len(msgs) != 0 {
		t.Fatalf("Init without a git port sent %d messages", len(msgs))
	}
}
