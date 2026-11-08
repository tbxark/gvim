package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/theme"
)

// pathAwareEditor is realEditor with the active document's file path forwarded, the way
// the app's adapter does: the reveal and follow read it.
type pathAwareEditor struct{ realEditor }

func (p pathAwareEditor) Active() Document {
	d := p.ed.Active()
	if d == nil {
		return nil
	}
	return filePathDoc{realDoc{d}, d.Path}
}

// filePathDoc is realDoc with FilePath, the optional view the app adapter forwards.
type filePathDoc struct {
	realDoc
	path string
}

// FilePath is the file's absolute path.
func (p filePathDoc) FilePath() string { return p.path }

// explorerModel returns a model over a real editor with path open, a real
// explorer tree rooted at root, sized 100 by 30, with the given explorer options.
func explorerModel(t *testing.T, root, path string, opts explorer.Options) (*Model, explorerTree) {
	t.Helper()
	ed := editor.New(editor.Options{Rows: 27, Cols: 90})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	tree, err := explorer.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	view := DefaultViewOptions()
	view.Explorer = opts
	m := New(Deps{
		Editor: pathAwareEditor{realEditor{ed}}, Explorer: tree, Keys: testKeys(), Theme: theme.Theme{},
		ShowExplorer: true, View: &view,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, tree
}

// explorerTree is the tree surface these tests read.
type explorerTree interface {
	Root() string
	Rows() []explorer.Row
	Selected() (explorer.Row, bool)
}

// fixtureFile writes files under a temporary project and returns the project path
// and the file path asked for.
func explorerFixture(t *testing.T) (root, file string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, file
}

// TestExplorerFollowActiveBufferRevealsTheFile checks file_explorer.follow_active_buffer:
// switching to a file in another folder expands the tree to it and selects its row.
func TestExplorerFollowActiveBufferRevealsTheFile(t *testing.T) {
	root, file := explorerFixture(t)
	m, tree := explorerModel(t, root, file, explorer.Options{FollowActiveBuffer: true})
	// The explorer starts on the root row.
	if sel, _ := tree.Selected(); sel.Path == file {
		t.Fatal("the tree already selected the file before following")
	}
	if cmd := m.followActiveBuffer(); cmd != nil {
		// The parent folder was not loaded: the background reload carries the
		// reveal. Settle it and the selection lands.
		settle(t, m, cmd)
	}
	if sel, _ := tree.Selected(); sel.Path != file {
		t.Errorf("selected = %s, want the active file %s", sel.Path, file)
	}
}

// TestExplorerFollowNeedsTheSettingAndTheTreesOwnFocus checks Fresh's gates: the
// follow does nothing when the setting is off, or while the keyboard is in the
// tree.
func TestExplorerFollowNeedsTheSettingAndTheTreesOwnFocus(t *testing.T) {
	root, file := explorerFixture(t)
	off, tree := explorerModel(t, root, file, explorer.Options{})
	before, _ := tree.Selected()
	if cmd := off.followActiveBuffer(); cmd != nil {
		t.Fatal("follow off returned a command")
	}
	if after, _ := tree.Selected(); after.Path != before.Path {
		t.Errorf("follow off moved the selection to %s", after.Path)
	}
	on, tree2 := explorerModel(t, root, file, explorer.Options{FollowActiveBuffer: true})
	on.explorerFocus = true
	if cmd := on.followActiveBuffer(); cmd != nil {
		t.Fatal("follow while the tree has the keyboard returned a command")
	}
	if sel, _ := tree2.Selected(); sel.Path == file {
		t.Error("follow while the tree has the keyboard moved the selection")
	}
}

// TestExplorerFocusRevealsWithFreshFeedback checks the explicit reveal: focusing
// the explorer with no file to reveal says so, and a file outside the project is
// named in the status row, as Fresh's messages do.
func TestExplorerFocusRevealsWithFreshFeedback(t *testing.T) {
	root, file := explorerFixture(t)
	m, tree := explorerModel(t, root, file, explorer.Options{})
	m.explorerOpen = false
	if cmd := m.focusExplorer(); cmd != nil {
		settle(t, m, cmd)
	}
	if sel, _ := tree.Selected(); sel.Path != file {
		t.Errorf("selected = %s, want the focused file %s", sel.Path, file)
	}

	// An unnamed buffer has no file to reveal.
	unnamed := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	unnamed.explorerOpen = true
	unnamed.focusExplorer()
	if unnamed.notice != revealNoFileNotice {
		t.Errorf("notice = %q, want %q", unnamed.notice, revealNoFileNotice)
	}

	// A file outside the project root is named.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "elsewhere.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	elsewhere, _ := explorerModel(t, root, outside, explorer.Options{})
	elsewhere.notice = ""
	elsewhere.focusExplorer()
	want := "File explorer focused; elsewhere.txt is outside the project"
	if elsewhere.notice != want {
		t.Errorf("notice = %q, want %q", elsewhere.notice, want)
	}
}

// TestExplorerAutoOpenOnLastClose checks file_explorer.auto_open_on_last_buffer_close:
// it opens and focuses the explorer only when the setting is on.
func TestExplorerAutoOpenOnLastClose(t *testing.T) {
	root, file := explorerFixture(t)
	off, _ := explorerModel(t, root, file, explorer.Options{})
	off.explorerOpen = false
	off.view.opts.Explorer.AutoOpenOnLastBufferClose = false
	if cmd := off.autoOpenExplorerOnLastClose(); cmd != nil || off.explorerOpen {
		t.Errorf("auto-open off opened the explorer: cmd %v open %v", cmd != nil, off.explorerOpen)
	}
	on, _ := explorerModel(t, root, file, explorer.Options{AutoOpenOnLastBufferClose: true})
	on.explorerOpen = false
	settle(t, on, on.autoOpenExplorerOnLastClose())
	if !on.explorerOpen || !on.explorerFocus || !on.explorerAutoOpened {
		t.Errorf("auto-open on: open %v focus %v auto %v", on.explorerOpen, on.explorerFocus, on.explorerAutoOpened)
	}
}

// TestExplorerGripKeepsTheWidthForm checks the grip drag against file_explorer.width:
// a configured percentage stays a percentage (the nearest one), a column count stays
// a column count, and the layout renders the dragged width.
func TestExplorerGripKeepsTheWidthForm(t *testing.T) {
	root, file := explorerFixture(t)
	percent, _ := explorerModel(t, root, file, explorer.Options{Width: explorer.PercentWidth(30)})
	percent.Update(expanel.ResizeMsg{Width: 40})
	if percent.explorerWidth.Mode != explorer.WidthPercent || percent.explorerWidth.Value != 40 {
		t.Errorf("percent grip -> %+v, want 40%%", percent.explorerWidth)
	}
	if l := percent.layout(); l.Ex != 40 {
		t.Errorf("layout width after the percent drag = %d, want 40", l.Ex)
	}
	cols, _ := explorerModel(t, root, file, explorer.Options{Width: explorer.ColumnsWidth(20)})
	cols.Update(expanel.ResizeMsg{Width: 24})
	if cols.explorerWidth.Mode != explorer.WidthColumns || cols.explorerWidth.Value != 24 {
		t.Errorf("column grip -> %+v, want 24 columns", cols.explorerWidth)
	}
	if l := cols.layout(); l.Ex != 24 {
		t.Errorf("layout width after the column drag = %d, want 24", l.Ex)
	}
}
