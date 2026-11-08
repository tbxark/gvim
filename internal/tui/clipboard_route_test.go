package tui

import (
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
)

// newTreeRootModel is newExplorerModel over a tree whose first row is the tree
// root, with the root selected. The second row is alpha.txt.
func newTreeRootModel(t *testing.T, ops *fakeOps) (*Model, *fakeEditor, *fakeTree) {
	t.Helper()
	m, ed, tr := newExplorerModel(t, ops, "alpha.txt")
	tr.rows = append([]explorer.Row{{Name: "ws", Path: "/ws", IsDir: true, Expanded: true}}, tr.rows...)
	tr.selected = 0
	return m, ed, tr
}

// pickRows picks the first n rows of the tree with Space, moving down between
// picks. The cursor ends on the last picked row.
func pickRows(m *Model, n int) {
	for i := range n {
		press(m, typed(" "))
		if i < n-1 {
			press(m, special(tea.KeyDown))
		}
	}
}

// TestTreeRootRefusesDeleteRenameAndCutBeforeAnyPrompt checks that the tree root
// cannot be deleted, renamed or cut. The refusal is a notice and no prompt opens, so
// a later Yes has nothing to trash.
func TestTreeRootRefusesDeleteRenameAndCutBeforeAnyPrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "delete", key: special(tea.KeyDelete)},
		{name: "rename", key: special(tea.KeyF2)},
		{name: "cut", key: ctrl('x')},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ops := &fakeOps{}
			m, _, _ := newTreeRootModel(t, ops)
			settle(t, m, press(m, tt.key))
			if len(m.overlays) != 0 {
				t.Errorf("%s on the tree root opened %d prompts, want none", tt.name, len(m.overlays))
			}
			if m.notice == "" {
				t.Errorf("%s on the tree root gave no notice", tt.name)
			}
			settle(t, m, press(m, typed("y")))
			if len(ops.calls) != 0 {
				t.Errorf("%s on the tree root ran %v after y, want no file operation", tt.name, ops.calls)
			}
			if m.clip != nil || m.explorerPanel.CutPending() {
				t.Errorf("%s on the tree root left a cut or clipboard: clip %+v, cut pending %v", tt.name, m.clip, m.explorerPanel.CutPending())
			}
		})
	}
}

// TestCopyOfTreeRootIsAllowed checks that the tree root can be copied, and that the
// copy pastes like any other entry.
func TestCopyOfTreeRootIsAllowed(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newTreeRootModel(t, ops)
	settle(t, m, press(m, ctrl('c')))
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	if want := []string{"copy /ws /other/ws"}; !equalStrings(ops.calls, want) {
		t.Errorf("paste after copying the root ran %v, want %v", ops.calls, want)
	}
}

// TestMultiCopyPasteRunsOneCopyPerPathInOrder checks that a copy of two picked rows
// keeps both, so the paste copies both in display order.
func TestMultiCopyPasteRunsOneCopyPerPathInOrder(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt", "gamma.txt")
	pickRows(m, 2)
	settle(t, m, press(m, ctrl('c')))
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	want := []string{"copy /ws/alpha.txt /other/alpha.txt", "copy /ws/beta.txt /other/beta.txt"}
	if !equalStrings(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
}

// TestMultiCutPasteMovesEveryPickedItemInOrder checks that a cut of two picked rows
// dims both rows, and that the paste moves both, in order, and empties the
// clipboard.
func TestMultiCutPasteMovesEveryPickedItemInOrder(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt", "gamma.txt")
	pickRows(m, 2)
	settle(t, m, press(m, ctrl('x')))
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if st, ok := rowName(m, name); !ok || !st.Faint {
			t.Errorf("%s is not dimmed under the cut (drawn %v)", name, ok)
		}
	}
	if st, ok := rowName(m, "gamma.txt"); !ok || st.Faint {
		t.Errorf("gamma.txt is dimmed but was not cut (drawn %v)", ok)
	}
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	want := []string{"move /ws/alpha.txt /other/alpha.txt", "move /ws/beta.txt /other/beta.txt"}
	if !equalStrings(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
	if m.clip != nil {
		t.Errorf("clipboard kept after both items moved: %+v", m.clip)
	}
}

// TestMultiPasteAsksAtEachConflictAndKeepsGoing checks that a multi-item paste asks
// about each existing destination, and that the answer to one does not end the rest.
func TestMultiPasteAsksAtEachConflictAndKeepsGoing(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{conflict: true}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	pickRows(m, 2)
	settle(t, m, press(m, ctrl('c')))
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	if len(m.overlays) != 1 {
		t.Fatalf("paste did not ask about alpha.txt: %d overlays", len(m.overlays))
	}
	_, cmd = m.Update(typed("y"))
	settle(t, m, cmd)
	if len(m.overlays) != 1 {
		t.Fatalf("paste did not ask about beta.txt after alpha.txt was overwritten: %d overlays", len(m.overlays))
	}
	_, cmd = m.Update(typed("n"))
	settle(t, m, cmd)
	if len(m.overlays) != 0 {
		t.Errorf("prompt still open after the last answer")
	}
	want := []expanel.Conflict{expanel.ConflictCancel, expanel.ConflictOverwrite, expanel.ConflictCancel, expanel.ConflictSkip}
	if !slices.Equal(ops.policies, want) {
		t.Errorf("policies = %v, want %v", ops.policies, want)
	}
}

// TestFailedItemStopsAMultiPaste checks that an item that fails shows its error and
// stops the paste, so the items after it are not started.
func TestFailedItemStopsAMultiPaste(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{err: errors.New("disk full"), failSrc: "/ws/alpha.txt"}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	pickRows(m, 2)
	settle(t, m, press(m, ctrl('x')))
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	if want := []string{"move /ws/alpha.txt /other/alpha.txt"}; !equalStrings(ops.calls, want) {
		t.Errorf("calls = %v, want %v", ops.calls, want)
	}
	if m.notice != "disk full" {
		t.Errorf("notice = %q, want the disk error", m.notice)
	}
}

// TestTrashOfAClipboardEntryClearsIt checks that trashing a cut or copied entry
// empties its clipboard entry, so a later paste does not act on a path that a new
// file may now hold.
func TestTrashOfAClipboardEntryClearsIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "cut", key: ctrl('x')},
		{name: "copy", key: ctrl('c')},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ops := &fakeOps{}
			m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
			settle(t, m, press(m, tt.key))
			settle(t, m, press(m, special(tea.KeyDelete)))
			settle(t, m, press(m, typed("y")))
			if want := []string{"trash /ws/alpha.txt"}; !equalStrings(ops.calls, want) {
				t.Fatalf("calls after the trash = %v, want %v", ops.calls, want)
			}
			if m.clip != nil || m.explorerPanel.CutPending() {
				t.Errorf("clipboard kept after the trash: clip %+v, cut pending %v", m.clip, m.explorerPanel.CutPending())
			}
			_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
			settle(t, m, cmd)
			if want := []string{"trash /ws/alpha.txt"}; !equalStrings(ops.calls, want) {
				t.Errorf("paste after the trash ran %v, want no file operation", ops.calls)
			}
		})
	}
}

// TestRenameOfACutItemDropsItFromTheClipboard checks that a renamed entry is no
// longer on the cut, so a paste does not move the old path.
func TestRenameOfACutItemDropsItFromTheClipboard(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt")
	settle(t, m, press(m, ctrl('x')))
	settle(t, m, press(m, special(tea.KeyF2)))
	press(m, special(tea.KeyBackspace))
	typeText(m, "2")
	settle(t, m, press(m, special(tea.KeyEnter)))
	if m.clip != nil || m.explorerPanel.CutPending() {
		t.Errorf("cut kept after the rename: clip %+v, cut pending %v", m.clip, m.explorerPanel.CutPending())
	}
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	if want := []string{"rename /ws/alpha.txt /ws/alpha.tx2"}; !equalStrings(ops.calls, want) {
		t.Errorf("calls = %v, want %v", ops.calls, want)
	}
}

// TestCtrlVKeepsTheCutDimmingUntilTheCutIsDone checks that Ctrl+V does not clear the
// cut dimming before the paste runs. A paste that stops partway leaves the entries
// still on the clipboard dimmed, so the rows match the clipboard.
func TestCtrlVKeepsTheCutDimmingUntilTheCutIsDone(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{err: errors.New("disk full"), failSrc: "/ws/beta.txt"}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	pickRows(m, 2)
	settle(t, m, press(m, ctrl('x')))
	cmd := press(m, ctrl('v'))
	if st, ok := rowName(m, "beta.txt"); !ok || !st.Faint {
		t.Errorf("beta.txt is not dimmed between Ctrl+V and the paste (drawn %v)", ok)
	}
	settle(t, m, cmd)
	if m.notice != "disk full" {
		t.Fatalf("notice = %q, want the disk error", m.notice)
	}
	if m.clip == nil || !slices.Equal(m.clip.paths, []string{"/ws/beta.txt"}) {
		t.Fatalf("clipboard = %+v, want only /ws/beta.txt", m.clip)
	}
	if st, ok := rowName(m, "beta.txt"); !ok || !st.Faint {
		t.Errorf("beta.txt is on the clipboard but not dimmed after the failed paste (drawn %v)", ok)
	}
}

// TestCutOfFolderAndItsChildMovesTheFolderOnce checks that a cut of a folder and one
// of its children moves the folder, which takes the child along. The child's own move
// must not be queued after the folder has moved, where it would find nothing.
func TestCutOfFolderAndItsChildMovesTheFolderOnce(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, tr := newExplorerModel(t, ops, "d", "x")
	tr.rows[0].IsDir = true
	tr.rows[1] = explorer.Row{Name: "x", Path: "/ws/d/x", Depth: 1}
	pickRows(m, 2)
	settle(t, m, press(m, ctrl('x')))
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	if want := []string{"move /ws/d /other/d"}; !equalStrings(ops.calls, want) {
		t.Errorf("calls = %v, want %v", ops.calls, want)
	}
	if m.notice != "" {
		t.Errorf("notice = %q, want none", m.notice)
	}
}

// TestRenameToTheSameNameKeepsTheClipboard checks that a rename to the name the entry
// already has moves nothing, and so leaves a pending copy or cut on the clipboard.
func TestRenameToTheSameNameKeepsTheClipboard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
		cut  bool
	}{
		{name: "copy", key: ctrl('c'), cut: false},
		{name: "cut", key: ctrl('x'), cut: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ops := &fakeOps{}
			m, _, _ := newExplorerModel(t, ops, "alpha.txt")
			settle(t, m, press(m, tt.key))
			settle(t, m, press(m, special(tea.KeyF2)))
			settle(t, m, press(m, special(tea.KeyEnter)))
			if want := []string{"rename /ws/alpha.txt /ws/alpha.txt"}; !equalStrings(ops.calls, want) {
				t.Fatalf("calls = %v, want %v", ops.calls, want)
			}
			if m.clip == nil || m.clip.cut != tt.cut || !slices.Equal(m.clip.paths, []string{"/ws/alpha.txt"}) {
				t.Errorf("clipboard after the same-name rename = %+v, want the %s kept", m.clip, tt.name)
			}
			if m.explorerPanel.CutPending() != tt.cut {
				t.Errorf("cut pending = %v after the same-name rename, want %v", m.explorerPanel.CutPending(), tt.cut)
			}
		})
	}
}

// TestTrashClearsThePickOfTheTrashedEntry checks that a picked entry that is trashed
// loses its pick. A new file that takes the same path and row is not picked.
func TestTrashClearsThePickOfTheTrashedEntry(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, tr := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	press(m, typed(" "))
	settle(t, m, press(m, special(tea.KeyDelete)))
	settle(t, m, press(m, typed("y")))
	if want := []string{"trash /ws/alpha.txt"}; !equalStrings(ops.calls, want) {
		t.Fatalf("calls after the trash = %v, want %v", ops.calls, want)
	}
	// A new file with the same name appears at the same row.
	tr.rows[0] = explorer.Row{Name: "alpha.txt", Path: "/ws/alpha.txt"}
	press(m, special(tea.KeyDown))
	settle(t, m, press(m, ctrl('c')))
	if m.clip == nil || !slices.Equal(m.clip.paths, []string{"/ws/beta.txt"}) {
		t.Errorf("copy after the trash = %+v, want only the cursor row /ws/beta.txt", m.clip)
	}
}

// TestCutPasteIntoItsOwnDirectoryCancelsTheCut checks that a cut pasted into the
// directory the entry is already in moves nothing and cancels the cut, as in Fresh.
func TestCutPasteIntoItsOwnDirectoryCancelsTheCut(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt")
	settle(t, m, press(m, ctrl('x')))
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/ws"})
	settle(t, m, cmd)
	if m.clip != nil || m.explorerPanel.CutPending() {
		t.Errorf("cut kept after a paste into its own directory: clip %+v, cut pending %v", m.clip, m.explorerPanel.CutPending())
	}
}

// TestCutSurvivesHidingTheExplorer checks that a pending cut is still pending after
// the explorer is hidden and shown again. The rows stay dimmed, and Esc cancels the
// cut and empties the clipboard, so a paste after it moves nothing.
func TestCutSurvivesHidingTheExplorer(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	settle(t, m, press(m, ctrl('x')))
	press(m, ctrl('b')) // hides the panel
	press(m, ctrl('b')) // shows it again and gives it the focus
	if !m.explorerPanel.CutPending() {
		t.Fatal("cut not pending after the explorer was hidden and shown")
	}
	if st, ok := rowName(m, "alpha.txt"); !ok || !st.Faint {
		t.Errorf("alpha.txt is not dimmed after the explorer was shown again (drawn %v)", ok)
	}
	press(m, special(tea.KeyEscape))
	if m.clip != nil || m.explorerPanel.CutPending() {
		t.Errorf("Esc after the show left the cut: clip %+v, cut pending %v", m.clip, m.explorerPanel.CutPending())
	}
	settle(t, m, press(m, ctrl('v')))
	if len(ops.calls) != 0 {
		t.Errorf("paste after Esc ran %v, want no file operation", ops.calls)
	}
}

// TestCancelledCutStopsTheRestOfThePaste checks that clearing the clipboard while a
// cut paste is in flight stops the paste. The move that is running still lands, but
// the next move of the paste is not started, and the notice says why it stopped.
func TestCancelledCutStopsTheRestOfThePaste(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	pickRows(m, 2)
	settle(t, m, press(m, ctrl('x')))
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	inFlight := runMsg(cmd) // the move of alpha.txt, started but not yet answered
	settle(t, m, press(m, special(tea.KeyEscape)))
	if m.clip != nil {
		t.Fatalf("setup: Esc left the clipboard %+v", m.clip)
	}
	_, cmd = m.Update(inFlight)
	settle(t, m, cmd)
	if want := []string{"move /ws/alpha.txt /other/alpha.txt"}; !equalStrings(ops.calls, want) {
		t.Errorf("calls = %v, want %v: the cancelled cut started its next move", ops.calls, want)
	}
	if m.notice != "Paste stopped: the cut was cancelled" {
		t.Errorf("notice = %q, want the cancelled-cut notice", m.notice)
	}
}
