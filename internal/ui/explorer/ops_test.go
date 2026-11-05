package explorer

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/ui/style"
)

// fakeTree is a Tree with fixed rows and a movable selection. It records what
// the panel asks of it, so the tests can check the selection without a disk.
type fakeTree struct {
	root string
	rows []tree.Row
	sel  int
}

func (f *fakeTree) Root() string        { return f.root }
func (f *fakeTree) Rows() []tree.Row    { return f.rows }
func (f *fakeTree) ScrollOffset() int   { return 0 }
func (f *fakeTree) ScrollBy(int)        {}
func (f *fakeTree) Toggle(string) error { return nil }
func (f *fakeTree) Move(delta int) {
	f.sel = min(max(f.sel+delta, 0), len(f.rows)-1)
}

func (f *fakeTree) Selected() (tree.Row, bool) {
	if len(f.rows) == 0 || f.sel < 0 {
		return tree.Row{}, false
	}
	return f.rows[f.sel], true
}

// opsTree is a project with the directory a/ holding a file x.txt, and a file
// b.txt at the root. The selection starts on a/.
func opsTree(sel int) *fakeTree {
	return &fakeTree{
		root: "/r",
		rows: []tree.Row{
			{Name: "a", Path: "/r/a", IsDir: true, Depth: 0},
			{Name: "x.txt", Path: "/r/a/x.txt", Depth: 1},
			{Name: "b.txt", Path: "/r/b.txt", Depth: 0},
		},
		sel: sel,
	}
}

// act runs one action on a panel over t, with the explorer geometry the tests
// share.
func act(t *testing.T, tr Tree, action string) (tea.Msg, bool) {
	t.Helper()
	p := New(tr, style.Styles{}, nil)
	_, cmd, ok := p.Action(action, Geometry{Width: 16, Bottom: 9, Focused: true})
	return run(cmd), ok
}

func TestFileOperationKeysSendMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		sel    int
		action string
		want   tea.Msg
	}{
		{"new file in a selected directory", 0, "file_explorer_new_file", NewFileMsg{Dir: "/r/a"}},
		{"new file under a selected file goes to its parent", 1, "file_explorer_new_file", NewFileMsg{Dir: "/r/a"}},
		{"new folder under a selected file", 1, "file_explorer_new_directory", NewFolderMsg{Dir: "/r/a"}},
		{"rename the selection", 2, "file_explorer_rename", RenameMsg{Path: "/r/b.txt"}},
		{"delete the selection", 1, "file_explorer_delete", DeleteMsg{Path: "/r/a/x.txt"}},
		{"copy the selection", 2, "copy", CopyMsg{Path: "/r/b.txt"}},
		{"cut the selection", 1, "cut", CutMsg{Path: "/r/a/x.txt"}},
		{"paste into the selected directory", 0, "paste", PasteMsg{Dir: "/r/a"}},
		{"paste with no row selected goes to the root", -1, "paste", PasteMsg{Dir: "/r"}},
		{"new file with no row selected goes to the root", -1, "file_explorer_new_file", NewFileMsg{Dir: "/r"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := act(t, opsTree(tt.sel), tt.action)
			if !ok {
				t.Fatalf("%s not handled", tt.action)
			}
			if got != tt.want {
				t.Errorf("%s sent %#v, want %#v", tt.action, got, tt.want)
			}
		})
	}
}

func TestSelectionActionsWithNoRowSendNothing(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"file_explorer_rename", "file_explorer_delete", "copy", "cut"} {
		got, ok := act(t, opsTree(-1), action)
		if !ok {
			t.Errorf("%s not handled with no row", action)
		}
		if got != nil {
			t.Errorf("%s with no row sent %#v", action, got)
		}
	}
}

func TestFileOperationActionsNeedATree(t *testing.T) {
	t.Parallel()
	p := New(nil, style.Styles{}, nil)
	for _, action := range []string{"file_explorer_new_file", "copy", "paste"} {
		if _, _, ok := p.Action(action, Geometry{Width: 16, Bottom: 9}); ok {
			t.Errorf("%s handled with no tree", action)
		}
	}
}

func TestEditorActionsAreNotHandledByThePanel(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"undo", "save", "select_all"} {
		if _, ok := act(t, opsTree(0), action); ok {
			t.Errorf("%s handled by the explorer", action)
		}
	}
}
