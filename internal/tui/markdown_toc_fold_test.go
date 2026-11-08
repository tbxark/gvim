package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/fold"
)

// foldBufferModel builds a model whose Contents section has fold_buffer on and a
// Markdown buffer holding text.
func foldBufferModel(t *testing.T, name, text string) (*Model, *tocEditor) {
	t.Helper()
	cfg := DefaultMarkdownTOC()
	cfg.Settings.FoldBuffer = true
	ed := newTOCEditor(name, strings.Split(text, "\n")...)
	return newTOCModel(t, ed, &cfg), ed
}

// headingFoldText has three headings with bodies, so every heading has a section
// to fold: Title line 0 (through line 8), Setup line 3 (through line 5), Usage line
// 6 (through line 8).
const headingFoldText = "# Title\nintro\n\n## Setup\none\n\n## Usage\ntwo\nthree"

// TestContentsFoldBufferPublishesHeadingRanges checks Fresh's setFoldingRanges
// behaviour: with fold_buffer on the section publishes every heading's section as a
// toggleable range, tied to the text version it scanned, for the active document.
func TestContentsFoldBufferPublishesHeadingRanges(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	if !m.contents.Open() {
		t.Fatal("the Contents section did not open for a Markdown buffer")
	}
	if !ed.outlineSet || ed.outlinePath != "notes.md" {
		t.Fatalf("published %v for %q, want ranges for notes.md", ed.outlineSet, ed.outlinePath)
	}
	want := []fold.Range{{Start: 0, End: 8}, {Start: 3, End: 5}, {Start: 6, End: 8}}
	if !slices.Equal(ed.outlineRanges, want) {
		t.Fatalf("published ranges = %v, want %v", ed.outlineRanges, want)
	}
	if ed.outlineVersion != ed.version {
		t.Errorf("published version = %d, want the scanned version %d", ed.outlineVersion, ed.version)
	}
	if len(ed.toggles) != 0 {
		t.Errorf("publishing pre-collapsed folds: %v", ed.toggles)
	}
}

// TestContentsFoldBufferOffPublishesNothing checks the setting gate: without
// fold_buffer nothing is published, so the side effect is off by default.
func TestContentsFoldBufferOffPublishesNothing(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", strings.Split(headingFoldText, "\n")...)
	m := newTOCModel(t, ed, nil)
	if !m.contents.Open() {
		t.Fatal("the Contents section did not open for a Markdown buffer")
	}
	if ed.outlineSet {
		t.Fatalf("fold_buffer off published %v", ed.outlineRanges)
	}
	if len(ed.toggles) != 0 {
		t.Errorf("fold_buffer off folded lines %v", ed.toggles)
	}
}

// TestContentsFoldTogglesTheBufferFold checks the per-heading gesture: collapsing a
// Contents row folds that heading's section in the buffer, and expanding it opens
// the fold again.
func TestContentsFoldTogglesTheBufferFold(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	if !m.contents.ToggleFoldPos(0) {
		t.Fatal("row 0 is not a foldable heading")
	}
	if !ed.folded[0] {
		t.Fatalf("collapsing Title folded %v, want line 0", ed.folded)
	}
	if !m.contents.ToggleFoldPos(0) {
		t.Fatal("row 0 is not a foldable heading")
	}
	if ed.folded[0] {
		t.Fatalf("expanding Title left the buffer fold closed: %v", ed.folded)
	}
}

// TestContentsFoldSurvivesTheSameScan checks a rescan that does not move the folded
// heading: the fold is kept at its line and no new fold is toggled.
func TestContentsFoldSurvivesTheSameScan(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	m.contents.ToggleFoldPos(0)
	toggles := len(ed.toggles)
	m.rescanContents()
	if !ed.folded[0] {
		t.Fatal("a rescan of the same text opened the fold")
	}
	if len(ed.toggles) != toggles {
		t.Errorf("a rescan of the same text toggled folds %v", ed.toggles)
	}
}

// TestContentsFoldDoesNotFoldAShiftedHeading checks the byte-keyed fold state: a
// heading whose byte offset changed across an edit (the heading above moved) is not
// folded at its new line, and the old line is opened. The fold never lands on a line
// it was not computed for.
func TestContentsFoldDoesNotFoldAShiftedHeading(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	m.contents.ToggleFoldPos(0) // Title, heading byte 0
	// An edit above Title shifts every heading's byte offset; rescan without the
	// debounce, as the tick would.
	ed.doc.lines = append([]string{"new first line"}, ed.doc.lines...)
	ed.version++
	m.rescanContents()
	if ed.folded[0] {
		t.Fatalf("the fold stayed on old line 0: %v", ed.folded)
	}
	for line := range ed.folded {
		if ed.folded[line] {
			t.Fatalf("a fold landed on line %d, want none after a shifted heading", line)
		}
	}
}

// TestContentsFoldSurvivesAnEditBelowIt checks a heading whose byte offset survived
// the edit: a fold inside a section whose heading did not move is kept, and a
// rescan does not toggle it again.
func TestContentsFoldSurvivesAnEditBelowIt(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	m.contents.ToggleFoldPos(0) // Title, folded at line 0
	toggles := len(ed.toggles)
	// Editing the body of the last heading leaves Title's byte offset (0) alone.
	ed.doc.lines[len(ed.doc.lines)-1] = "three and more"
	ed.version++
	m.rescanContents()
	if !ed.folded[0] {
		t.Fatal("a fold whose heading did not move was opened by the rescan")
	}
	if len(ed.toggles) != toggles {
		t.Errorf("the rescan toggled %v, want no new toggle", ed.toggles)
	}
}

// TestContentsFoldClearedOnClose checks the clear path: closing the section clears
// the published ranges and opens the folds the section applied.
func TestContentsFoldClearedOnClose(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	m.contents.ToggleFoldPos(0)
	m.closeContents()
	if !ed.outlineSet || len(ed.outlineRanges) != 0 {
		t.Fatalf("closing published %v, want the ranges cleared", ed.outlineRanges)
	}
	if ed.folded[0] {
		t.Fatalf("closing left line 0 folded: %v", ed.folded)
	}
}

// TestContentsFoldBufferNeedsMarkdown checks the non-Markdown control: a text buffer
// publishes no heading ranges even with fold_buffer on.
func TestContentsFoldBufferNeedsMarkdown(t *testing.T) {
	t.Parallel()
	cfg := DefaultMarkdownTOC()
	cfg.Settings.FoldBuffer = true
	ed := newTOCEditor("a.txt", "# not markdown")
	m := newTOCModel(t, ed, &cfg)
	if m.contents.Open() {
		t.Fatal("the Contents section opened for a text buffer")
	}
	if ed.outlineSet {
		t.Fatalf("a text buffer published heading ranges %v", ed.outlineRanges)
	}
}
