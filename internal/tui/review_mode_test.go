package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/reviewdiff"
	"github.com/tbxark/gvim/theme"
)

type fakeReviewGit struct {
	changesByBase map[string]ReviewChangesResult
	diffs         map[string]*reviewdiff.FileDiff // keyed by base + "\x00" + path
	refs          []string
	changesCalls  int
}

func (f *fakeReviewGit) Changes(_ context.Context, _ string, baseRef string) (ReviewChangesResult, error) {
	f.changesCalls++
	if res, ok := f.changesByBase[baseRef]; ok {
		return res, nil
	}
	return ReviewChangesResult{RepoRoot: "/repo", Branch: "main", BaseRef: baseRef}, nil
}

func (f *fakeReviewGit) FileDiff(_ context.Context, _ string, baseRef string, entry reviewdiff.FileEntry) (*reviewdiff.FileDiff, error) {
	if fd, ok := f.diffs[baseRef+"\x00"+entry.Path]; ok {
		cp := *fd
		return &cp, nil
	}
	return &reviewdiff.FileDiff{Path: entry.Path, Status: entry.Status, BaseRef: baseRef}, nil
}

func (f *fakeReviewGit) Refs(_ context.Context, _ string) ([]string, error) {
	if len(f.refs) > 0 {
		return f.refs, nil
	}
	return []string{"HEAD", "main"}, nil
}

func newSampleReviewGit() *fakeReviewGit {
	return &fakeReviewGit{
		refs: []string{"HEAD", "main", "feat"},
		changesByBase: map[string]ReviewChangesResult{
			"HEAD": {
				RepoRoot: "/repo",
				Branch:   "feat",
				BaseRef:  "HEAD",
				Files: []reviewdiff.FileEntry{
					{Path: "a.go", Status: reviewdiff.StatusModified, Added: 1, Deleted: 1},
					{Path: "b.go", Status: reviewdiff.StatusAdded, Added: 2},
				},
			},
			"main": {
				RepoRoot: "/repo",
				Branch:   "feat",
				BaseRef:  "main",
				Files: []reviewdiff.FileEntry{
					{Path: "a.go", Status: reviewdiff.StatusModified, Added: 1, Deleted: 1},
					{Path: "b.go", Status: reviewdiff.StatusAdded, Added: 2},
					{Path: "c.go", Status: reviewdiff.StatusModified, Added: 3, Deleted: 0},
				},
			},
		},
		diffs: map[string]*reviewdiff.FileDiff{
			"HEAD\x00a.go": {
				Path:     "a.go",
				Status:   reviewdiff.StatusModified,
				BaseRef:  "HEAD",
				OldLines: []string{"package a", "var oldVal = 1", "var keep = 2"},
				NewLines: []string{"package a", "var newVal = 2", "var keep = 2"},
			},
			"HEAD\x00b.go": {
				Path:     "b.go",
				Status:   reviewdiff.StatusAdded,
				BaseRef:  "HEAD",
				NewLines: []string{"package b", "func B() {}"},
			},
			"main\x00a.go": {
				Path:     "a.go",
				Status:   reviewdiff.StatusModified,
				BaseRef:  "main",
				OldLines: []string{"package a", "var oldVal = 1"},
				NewLines: []string{"package a", "var newVal = 2"},
			},
		},
	}
}

func drainReviewCmds(m *Model, cmd tea.Cmd) {
	for range 8 {
		if cmd == nil {
			return
		}
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			var next []tea.Cmd
			for _, sub := range batch {
				if sub == nil {
					continue
				}
				sm := sub()
				if sm == nil {
					continue
				}
				// Ignore tick commands that would block or reschedule.
				if _, isTick := sm.(clockTickMsg); isTick {
					continue
				}
				_, c := m.Update(sm)
				if c != nil {
					next = append(next, c)
				}
			}
			if len(next) == 0 {
				return
			}
			cmd = tea.Batch(next...)
			continue
		}
		_, cmd = m.Update(msg)
	}
}

func newReviewTestModel(t *testing.T, port ReviewGitPort, startReview bool) *Model {
	t.Helper()
	ed := newFakeEditor("hello", "world")
	ed.quitAction = "quit"
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	m := New(Deps{
		Editor:      ed,
		Keys:        testKeys(),
		Theme:       th,
		Clock:       testClock,
		StartReview: startReview,
		Root:        RootPorts{ReviewGit: port},
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m
}

func TestReviewModeTogglePreservesEditState(t *testing.T) {
	t.Parallel()
	port := newSampleReviewGit()
	m := newReviewTestModel(t, port, false)
	m.explorerOpen = false

	// Enter Review Diff Mode.
	cmd := m.dispatch(Command{Action: "toggle_review_diff_mode"})
	if !m.review.active {
		t.Fatal("expected review mode to be active")
	}
	if !m.Holds("review_diff_mode") {
		t.Fatal("Holds(review_diff_mode) = false, want true")
	}
	drainReviewCmds(m, cmd)

	if len(m.review.model.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(m.review.model.Files))
	}
	if m.review.model.Diff == nil || m.review.model.Diff.Path != "a.go" {
		t.Fatalf("loaded diff = %+v, want a.go", m.review.model.Diff)
	}

	// Render frame to ensure layout, sidebar, header, diff, and status bar paint cleanly.
	var g grid.Grid
	g.Resize(100, 24)
	m.paintInto(&g)
	if len(m.review.headerLayout.Buttons) == 0 {
		t.Fatal("expected Review header bar buttons to be placed")
	}
	// Status bar carries clickable [REVIEW] pill.
	l := m.layout()
	foundPill := false
	for _, r := range l.Status {
		if r.Kind == frame.HitStatusSegment && r.Index == frame.StatusSegmentReviewMode && strings.Contains(r.Text, "[REVIEW]") {
			foundPill = true
			break
		}
	}
	if !foundPill {
		t.Fatal("status bar missing clickable [REVIEW] pill")
	}

	// Press 'q' returns to Edit Mode (does NOT quit) and restores explorerOpen=false.
	qCmd := press(m, typed("q"))
	if m.review.active {
		t.Fatal("expected q from edit-entered review mode to return to Edit Mode")
	}
	if qCmd != nil {
		if _, isQuit := qCmd().(tea.QuitMsg); isQuit {
			t.Fatal("q should not quit gvim when entered from Edit Mode")
		}
	}
	if m.explorerOpen {
		t.Fatal("expected explorerOpen=false to be restored on exit")
	}
}

func TestReviewModeDirectStartQuitBehavior(t *testing.T) {
	t.Parallel()
	port := newSampleReviewGit()
	m := newReviewTestModel(t, port, true)
	if !m.review.active || !m.review.startedDirect {
		t.Fatal("expected StartReview=true to launch directly in review mode")
	}
	drainReviewCmds(m, m.reviewInitCmd())

	// Pressing 'q' when launched directly via --review quits gvim.
	cmd := press(m, typed("q"))
	if cmd == nil {
		t.Fatal("expected quit cmd from q in direct --review launch")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}

func TestReviewModeNavigationFilterSearchAndCrossFileHunk(t *testing.T) {
	t.Parallel()
	port := newSampleReviewGit()
	m := newReviewTestModel(t, port, false)
	drainReviewCmds(m, m.dispatch(Command{Action: "toggle_review_diff_mode"}))

	// Typing 'q' inside sidebar filter must NOT exit Review Diff Mode.
	press(m, typed("/"))
	if !m.review.model.FilterActive {
		t.Fatal("expected / in FocusFiles to activate filter input")
	}
	press(m, typed("q"))
	if !m.review.active {
		t.Fatal("typing q inside filter must not exit Review Diff Mode")
	}
	if m.review.model.FilterQuery != "q" {
		t.Fatalf("FilterQuery = %q, want q", m.review.model.FilterQuery)
	}
	// Esc clears filter.
	drainReviewCmds(m, press(m, tea.KeyPressMsg{Code: tea.KeyEscape}))
	if m.review.model.FilterActive || m.review.model.FilterQuery != "" {
		t.Fatalf("Esc should clear filter, got active=%v query=%q", m.review.model.FilterActive, m.review.model.FilterQuery)
	}

	// Tab focuses diff pane; 'v' toggles Split, 'f' toggles Full, 'w' toggles Wrap.
	press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.review.model.Focus != reviewdiff.FocusDiff {
		t.Fatal("Tab should move focus to diff pane")
	}
	press(m, typed("v"))
	if m.review.model.ViewMode != reviewdiff.ViewSplit {
		t.Fatal("v should toggle to ViewSplit")
	}
	press(m, typed("f"))
	if !m.review.model.FullFile {
		t.Fatal("f should toggle FullFile on")
	}
	press(m, typed("w"))
	if m.review.model.Wrap {
		t.Fatal("w should toggle Wrap off")
	}

	// In-diff search with / finds matches and Esc clears.
	press(m, typed("/"))
	if !m.review.model.SearchActive {
		t.Fatal("expected / in FocusDiff to activate search")
	}
	for _, ch := range "newVal" {
		press(m, typed(string(ch)))
	}
	if len(m.review.model.SearchMatches) == 0 {
		t.Fatal("expected in-diff search to find newVal")
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.review.model.SearchQuery != "" {
		t.Fatal("Esc should clear search")
	}

	// ']' at the last hunk of a.go crosses to b.go.
	drainReviewCmds(m, press(m, typed("]")))
	if m.review.model.Diff == nil || m.review.model.Diff.Path != "b.go" {
		t.Fatalf("after ] crossing file, diff = %+v, want b.go", m.review.model.Diff)
	}

	// '[' at the first hunk of b.go crosses back to a.go.
	drainReviewCmds(m, press(m, typed("[")))
	if m.review.model.Diff == nil || m.review.model.Diff.Path != "a.go" {
		t.Fatalf("after [ crossing file, diff = %+v, want a.go", m.review.model.Diff)
	}
}

func TestReviewModeStaleAsyncDiscarded(t *testing.T) {
	t.Parallel()
	port := newSampleReviewGit()
	m := newReviewTestModel(t, port, false)
	drainReviewCmds(m, m.dispatch(Command{Action: "toggle_review_diff_mode"}))

	// Deliver a stale reviewDiffMsg with an old generation: must not overwrite a.go.
	stale := reviewDiffMsg{
		gen:     m.review.diffGen - 1,
		path:    "a.go",
		baseRef: "HEAD",
		diff:    &reviewdiff.FileDiff{Path: "a.go", BaseRef: "HEAD", OldLines: []string{"stale"}, NewLines: []string{"stale2"}},
	}
	m.Update(stale)
	if m.review.model.Diff.OldLines[0] == "stale" {
		t.Fatal("stale reviewDiffMsg overwrote newer diff")
	}
}

func TestReviewModeBaseRefSwitchAndStatusClick(t *testing.T) {
	t.Parallel()
	port := newSampleReviewGit()
	m := newReviewTestModel(t, port, false)
	drainReviewCmds(m, m.dispatch(Command{Action: "toggle_review_diff_mode"}))

	// Press 'b' to open base ref picker.
	drainReviewCmds(m, press(m, typed("b")))
	if len(m.overlays) == 0 {
		t.Fatal("expected base ref picker overlay to open")
	}
	// Move down from HEAD to main and press Enter.
	press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	drainReviewCmds(m, press(m, tea.KeyPressMsg{Code: tea.KeyEnter}))
	if m.review.model.BaseRef != "main" {
		t.Fatalf("BaseRef = %q, want main", m.review.model.BaseRef)
	}
	if len(m.review.model.Files) != 3 {
		t.Fatalf("files vs main = %d, want 3", len(m.review.model.Files))
	}

	// Clicking [REVIEW] pill in status bar exits back to Edit Mode.
	l := m.layout()
	pillX := -1
	for _, r := range l.Status {
		if r.Kind == frame.HitStatusSegment && r.Index == frame.StatusSegmentReviewMode {
			pillX = r.X
			break
		}
	}
	if pillX < 0 {
		t.Fatal("missing [REVIEW] status run")
	}
	m.Update(tea.MouseClickMsg{X: pillX, Y: l.StatusY, Button: tea.MouseLeft})
	if m.review.active {
		t.Fatal("clicking [REVIEW] pill should exit Review Diff Mode")
	}
}

type fakeOpenEditor struct {
	*fakeEditor
	openedPath string
	gotoLine   int
	gotoCol    int
}

func (f *fakeOpenEditor) OpenFile(p string) error {
	f.openedPath = p
	return nil
}

func (f *fakeOpenEditor) OpenPreview(p string) error {
	f.openedPath = p
	return nil
}

func (f *fakeOpenEditor) GotoLine(line, col int) error {
	f.gotoLine = line
	f.gotoCol = col
	return nil
}

func TestReviewModeJumpToEditHeaderButtonsAndUnsavedIndicator(t *testing.T) {
	t.Parallel()
	port := newSampleReviewGit()
	baseEd := newFakeEditor("hello", "world")
	baseEd.tabs = []TabInfo{{Name: "a.go", Path: "/repo/a.go", Modified: true, Active: true}}
	ed := &fakeOpenEditor{fakeEditor: baseEd}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	m := New(Deps{
		Editor: ed,
		Keys:   testKeys(),
		Theme:  th,
		Clock:  testClock,
		Root:   RootPorts{ReviewGit: port},
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	drainReviewCmds(m, m.dispatch(Command{Action: "toggle_review_diff_mode"}))

	if !m.review.model.UnsavedInEditor {
		t.Fatal("expected UnsavedInEditor=true when /repo/a.go has a modified open tab")
	}

	// Paint once so headerLayout is populated, then click the Unified->Split button.
	var g grid.Grid
	g.Resize(100, 24)
	m.paintInto(&g)
	var viewBtn reviewdiff.HeaderButton
	for _, b := range m.review.headerLayout.Buttons {
		if b.Action == reviewdiff.HeaderViewMode {
			viewBtn = b
			break
		}
	}
	if viewBtn.W == 0 {
		t.Fatal("expected HeaderViewMode button in header bar")
	}
	m.Update(tea.MouseClickMsg{X: viewBtn.X, Y: m.review.headerLayout.Y, Button: tea.MouseLeft})
	if m.review.model.ViewMode != reviewdiff.ViewSplit {
		t.Fatal("clicking ViewMode header pill should toggle to ViewSplit")
	}

	// Focus diff pane and press Enter on the changed line (line 2) -> jumps to Edit Mode.
	press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	drainReviewCmds(m, press(m, tea.KeyPressMsg{Code: tea.KeyEnter}))
	if m.review.active {
		t.Fatal("Enter on diff row should exit Review Diff Mode to Edit Mode")
	}
	if !strings.HasSuffix(ed.openedPath, "a.go") || ed.gotoLine != 2 {
		t.Fatalf("opened (%q, line %d), want a.go line 2", ed.openedPath, ed.gotoLine)
	}
}

func TestReviewModeDynamicPaletteCommands(t *testing.T) {
	m := newReviewTestModel(t, newSampleReviewGit(), false)
	hasAction := func(action string) bool {
		for _, c := range m.allCommands() {
			if c.Action == action {
				return true
			}
		}
		return false
	}
	if hasAction("review_diff_mode_refresh") || hasAction("review_diff_mode_change_base") {
		t.Fatal("review sub-commands should be hidden while Edit Mode is active")
	}
	drainReviewCmds(m, m.dispatch(Command{Action: "toggle_review_diff_mode"}))
	if !hasAction("review_diff_mode_refresh") || !hasAction("review_diff_mode_change_base") {
		t.Fatal("review sub-commands should be listed while Review Diff Mode is active")
	}
}
