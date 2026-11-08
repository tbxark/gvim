package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// stashPort is a fake InterruptedSaves that returns a fixed list or an error.
type stashPort struct {
	items []Interrupted
	err   error
}

func (p stashPort) Interrupted() ([]Interrupted, error) { return p.items, p.err }

// newStashModel returns a model over a file-opening fake editor with the stash port p.
func newStashModel(t *testing.T, p InterruptedSaves) (*Model, *fileFake) {
	t.Helper()
	ed := &fileFake{fakeEditor: newFakeEditor("x")}
	m := New(Deps{
		Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Stashes: p},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, ed
}

// settleStash runs the command of a message and feeds its results back to the model.
func settleStash(m *Model, cmd tea.Cmd) {
	for _, msg := range collect(cmd) {
		_, next := m.Update(msg)
		settleStash(m, next)
	}
}

// TestReviewInterruptedSavesListsTheStashedFiles checks that the review lists the files
// that have a stash, untitled buffers included, and that the list is drawn over the frame.
func TestReviewInterruptedSavesListsTheStashedFiles(t *testing.T) {
	t.Parallel()
	m, _ := newStashModel(t, stashPort{items: []Interrupted{
		{Path: "/ws/a.txt", Stash: "/cache/stash-1.txt"},
		{Path: "", Stash: "/cache/stash-2.txt"},
	}})
	settleStash(m, rootPaletteRun(m, "review_interrupted_saves"))
	rows, ok := overlayRows(m)
	if !ok {
		t.Fatal("review did not open a list overlay")
	}
	sameLines(t, "stashed files", rows, []string{"a.txt=stash-1.txt", "untitled buffer=stash-2.txt"})
}

// TestReviewInterruptedSavesOpensTheChosenFileAndNamesTheStash checks that choosing a
// file opens it and that the notice says where its unsaved text is kept.
func TestReviewInterruptedSavesOpensTheChosenFileAndNamesTheStash(t *testing.T) {
	t.Parallel()
	m, ed := newStashModel(t, stashPort{items: []Interrupted{{Path: "/ws/a.txt", Stash: "/cache/stash-1.txt"}}})
	settleStash(m, rootPaletteRun(m, "review_interrupted_saves"))
	press(m, special(tea.KeyEnter))
	if !slices.Contains(ed.opened, "/ws/a.txt") {
		t.Fatalf("opened %q, want /ws/a.txt", ed.opened)
	}
	if !strings.Contains(m.notice, "/cache/stash-1.txt") {
		t.Fatalf("notice = %q, want the stash path", m.notice)
	}
}

// TestReviewInterruptedSavesNoticesWhenThereIsNothing checks the three ways the review
// has nothing to show: no port, no stash, and a port that fails.
func TestReviewInterruptedSavesNoticesWhenThereIsNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		port InterruptedSaves
		want string
	}{
		{"no session directory", nil, "No session directory, so no interrupted saves to review"},
		{"nothing stashed", stashPort{}, "No interrupted saves to review"},
		{"port fails", stashPort{err: errors.New("session: decode")}, "session: decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := newStashModel(t, tt.port)
			settleStash(m, rootPaletteRun(m, "review_interrupted_saves"))
			if m.notice != tt.want {
				t.Fatalf("notice = %q, want %q", m.notice, tt.want)
			}
			if _, ok := overlayRows(m); ok {
				t.Fatal("review opened a list with nothing to show")
			}
		})
	}
}
