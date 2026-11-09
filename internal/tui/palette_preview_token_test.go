package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
)

type leakFiles struct{ files []string }

func (f leakFiles) Snapshot() ([]string, bool) { return f.files, false }

type leakRanker struct{}

func (leakRanker) Rank(string, []string, int) []palette.Ranked {
	return []palette.Ranked{{Path: "a.go"}}
}

type leakPreviewer struct{}

func (leakPreviewer) ReadHead(string, int) ([]byte, bool, error) {
	return []byte("LATE PREVIEW\n"), false, nil
}

// runPreviewCommand walks a command tree and returns the preview message it produced.
func runPreviewCommand(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	var found tea.Msg
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				walk(sub)
			}
			return
		}
		if msg != nil {
			found = msg
		}
	}
	walk(cmd)
	if found == nil {
		t.Fatal("the command tree produced no preview message")
	}
	return found
}

// newLeakPalette builds a file palette whose selected a.go preview reads LATE PREVIEW.
func newLeakPalette(t *testing.T) *palette.Palette {
	t.Helper()
	return palette.New(palette.Options{
		Query: "a.go", Root: "/w",
		Files: leakFiles{files: []string{"a.go"}}, Ranker: leakRanker{}, Preview: leakPreviewer{},
	})
}

// TestLatePreviewFromClosedPaletteDoesNotReachReopenedOne reproduces the root-level
// collision: A starts a preview read, closes, B opens with the same owner/seq/rel, and
// A's late result is delivered. The root routes the non-input message to B, whose
// instance token rejects it, so B neither paints nor caches the stale preview.
func TestLatePreviewFromClosedPaletteDoesNotReachReopenedOne(t *testing.T) {
	a := newLeakPalette(t)
	rank := a.Init()
	if rank == nil {
		t.Fatal("A produced no ranking command")
	}
	if _, cmd := a.Update(rank()); cmd == nil {
		t.Fatal("A produced no preview command")
	} else {
		late := runPreviewCommand(t, cmd)
		a.Dismiss()

		b := newLeakPalette(t)
		rankB := b.Init()
		if rankB == nil {
			t.Fatal("B produced no ranking command")
		}
		b.Update(rankB()) // selects the same a.go and starts no executed read

		m := newTestModel(t, newFakeEditor("x"), nil, 120, 30)
		m.pushOverlay(b)
		m.Update(late)
		if !b.Active() {
			t.Fatal("B closed while receiving A's late preview")
		}
		frame := strings.Join(frameText(m), "\n")
		if strings.Contains(frame, "LATE PREVIEW") {
			t.Fatalf("B painted A's late preview:\n%s", frame)
		}
	}
}

type leakPathRanker struct{ path string }

func (r leakPathRanker) Rank(string, []string, int) []palette.Ranked {
	return []palette.Ranked{{Path: r.path}}
}

// newRankPalette builds a file palette whose ranking always returns the given row path.
func newRankPalette(t *testing.T, row string) *palette.Palette {
	t.Helper()
	return palette.New(palette.Options{
		Query: "a.go", Root: "/w",
		Files: leakFiles{files: []string{"a.go"}}, Ranker: leakPathRanker{path: row},
	})
}

// TestLateFileRankFromClosedPaletteDoesNotReachReopenedOne: the same root-level
// collision as the preview case, for FileRankedMsg. A's late ranking must not replace B's
// fresh rows even though both palettes use owner 0, seq 1 and the same query text.
func TestLateFileRankFromClosedPaletteDoesNotReachReopenedOne(t *testing.T) {
	a := newRankPalette(t, "stale.go")
	rankA := a.Init()
	if rankA == nil {
		t.Fatal("A produced no ranking command")
	}
	late := rankA()
	a.Dismiss()

	b := newRankPalette(t, "fresh.go")
	rankB := b.Init()
	if rankB == nil {
		t.Fatal("B produced no ranking command")
	}
	b.Update(rankB())

	m := newTestModel(t, newFakeEditor("x"), nil, 120, 30)
	m.pushOverlay(b)
	m.Update(late)
	frame := strings.Join(frameText(m), "\n")
	if !strings.Contains(frame, "fresh.go") {
		t.Fatalf("B lost its fresh ranking:\n%s", frame)
	}
	if strings.Contains(frame, "stale.go") {
		t.Fatalf("B accepted A's late ranking:\n%s", frame)
	}
}
