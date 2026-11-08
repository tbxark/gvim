package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// TestContentsFoldStaysPublishedOnPanelClose checks Fresh's tracking rule: closing
// the section hides the panel but the Markdown buffer stays tracked, so the
// published heading ranges and the folds the section applied stay in the buffer.
func TestContentsFoldStaysPublishedOnPanelClose(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	m.contents.ToggleFoldPos(0)
	m.closeContents()
	if m.contents.Open() {
		t.Fatal("the Contents section stayed open")
	}
	if !ed.outlineSet || len(ed.outlineRanges) != 3 {
		t.Fatalf("closing published %v, want the ranges kept", ed.outlineRanges)
	}
	if !ed.folded[0] {
		t.Fatalf("closing opened line 0, want the buffer fold kept: %v", ed.folded)
	}
}

// TestContentsFoldPublishedWhileHidden checks the hidden-section scan: with the
// panel closed an edit of the tracked Markdown buffer still rescans and publishes
// the ranges for the new text version (the debounce is driven directly).
func TestContentsFoldPublishedWhileHidden(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	m.closeContents()
	// An edit of the last section: the heading offsets stay, the version moves.
	ed.doc.lines[len(ed.doc.lines)-1] = "three and more"
	ed.version++
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !m.toc.armed {
		t.Fatal("an edit while the section is hidden did not arm the rescan")
	}
	m.tocRescan(tocRescanMsg{seq: m.toc.seq})
	if !ed.outlineSet || ed.outlineVersion != ed.version {
		t.Fatalf("hidden rescan published version %d (set %v), want %d", ed.outlineVersion, ed.outlineSet, ed.version)
	}
	want := []fold.Range{{Start: 0, End: 8}, {Start: 3, End: 5}, {Start: 6, End: 8}}
	if !slices.Equal(ed.outlineRanges, want) {
		t.Fatalf("hidden rescan published %v, want %v", ed.outlineRanges, want)
	}
}

// switchTOCBuffer makes the fake editor's active buffer another file.
func switchTOCBuffer(ed *tocEditor, name string, lines ...string) {
	ed.name = name
	ed.doc = newFakeEditor(lines...).doc
	ed.tabs = []TabInfo{{Name: name, Active: true}}
}

// TestContentsFoldHiddenClearsOnBufferSwitch checks the buffer-switch clear path
// with the panel hidden: the old buffer's ranges go and the new Markdown buffer's
// ranges are published (tracking continues for the new buffer).
func TestContentsFoldHiddenClearsOnBufferSwitch(t *testing.T) {
	t.Parallel()
	m, ed := foldBufferModel(t, "notes.md", headingFoldText)
	m.closeContents()
	switchTOCBuffer(ed, "other.md", "# Other", "body")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if ed.outlinePath != "other.md" {
		t.Fatalf("published path %q, want other.md after the switch", ed.outlinePath)
	}
	if !ed.outlineSet || len(ed.outlineRanges) != 1 || ed.outlineRanges[0].Start != 0 {
		t.Fatalf("published ranges %v for the new buffer, want its heading", ed.outlineRanges)
	}
	// A non-Markdown buffer clears the ranges and stops tracking.
	switchTOCBuffer(ed, "plain.txt", "# not a heading")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !ed.outlineSet || len(ed.outlineRanges) != 0 {
		t.Fatalf("a text buffer kept published ranges %v", ed.outlineRanges)
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

// TestContentsFoldBufferLiveApply checks that contents.fold_buffer is a live
// setting: switching it on through the settings change publishes at once, and
// switching it off clears the published ranges and opens the folds the section
// applied (a hand fold would stay).
func TestContentsFoldBufferLiveApply(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", strings.Split(headingFoldText, "\n")...)
	m := newTOCModel(t, ed, nil) // fold_buffer off: nothing published
	if m.contents.FoldBuffer() || ed.outlineSet {
		t.Fatalf("fold_buffer started on or published: %v %v", m.contents.FoldBuffer(), ed.outlineRanges)
	}
	m.applySettingsChange(SettingsChange{
		Settings: Settings{FoldBuffer: true},
		Live:     []string{"contents.fold_buffer"},
	})
	if !m.contents.FoldBuffer() {
		t.Fatal("saving contents.fold_buffer = true did not reach the section")
	}
	if !ed.outlineSet || len(ed.outlineRanges) != 3 {
		t.Fatalf("switching fold_buffer on published %v, want the heading ranges", ed.outlineRanges)
	}
	// A section fold, then switching the setting off: the ranges go and the fold
	// the section applied is opened.
	if !m.contents.ToggleFoldPos(0) {
		t.Fatal("row 0 is not foldable")
	}
	if !ed.folded[0] {
		t.Fatalf("the section fold did not reach the buffer: %v", ed.folded)
	}
	m.applySettingsChange(SettingsChange{
		Settings: Settings{FoldBuffer: false},
		Live:     []string{"contents.fold_buffer"},
	})
	if m.contents.FoldBuffer() {
		t.Fatal("saving contents.fold_buffer = false did not reach the section")
	}
	if !ed.outlineSet || len(ed.outlineRanges) != 0 {
		t.Fatalf("switching fold_buffer off published %v, want the ranges cleared", ed.outlineRanges)
	}
	if ed.folded[0] {
		t.Fatalf("switching fold_buffer off left line 0 folded: %v", ed.folded)
	}
}
