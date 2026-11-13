package explorer

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/style"
)

// asyncFakeTree is a Tree whose directory read happens in ToggleCmd, like the remote
// tree: the panel must return that command, not read synchronously.
type asyncFakeTree struct {
	rows      []tree.Row
	selected  int
	scroll    int
	toggles   []string // synchronous Toggle calls
	cmdCalls  []string // ToggleCmd calls
	toggleErr error
}

func (t *asyncFakeTree) Root() string        { return "/r" }
func (t *asyncFakeTree) Rows() []tree.Row    { return t.rows }
func (t *asyncFakeTree) Move(delta int)      { t.selected = min(max(t.selected+delta, 0), len(t.rows)-1) }
func (t *asyncFakeTree) ScrollBy(n int)      { t.scroll += n }
func (t *asyncFakeTree) ScrollOffset() int   { return 0 }
func (t *asyncFakeTree) ShowIgnored() bool   { return false }
func (t *asyncFakeTree) SetShowIgnored(bool) {}
func (t *asyncFakeTree) Toggle(path string) error {
	t.toggles = append(t.toggles, path)
	return t.toggleErr
}
func (t *asyncFakeTree) Selected() (tree.Row, bool) {
	if len(t.rows) == 0 {
		return tree.Row{}, false
	}
	return t.rows[min(t.selected, len(t.rows)-1)], true
}

// asyncToggledMsg is what the fake async toggle command returns.
type asyncToggledMsg struct{ path string }

func (t *asyncFakeTree) ToggleCmd(path string) tea.Cmd {
	t.cmdCalls = append(t.cmdCalls, path)
	return func() tea.Msg { return asyncToggledMsg{path: path} }
}

// TestPanelAsyncToggleReturnsTheCommand checks the panel does not read an async tree's
// directory on the update loop: the toggle is the tree's command, and the synchronous
// Toggle is never called.
func TestPanelAsyncToggleReturnsTheCommand(t *testing.T) {
	tr := &asyncFakeTree{rows: []tree.Row{{Name: "dir", Path: "/r/dir", IsDir: true}}}
	p := New(tr, style.Styles{}, nil)
	_, cmd, ok := p.Action("file_explorer_open", testGeometry)
	if !ok {
		t.Fatal("file_explorer_open not handled")
	}
	if cmd == nil {
		t.Fatal("the async open returned no command")
	}
	if len(tr.toggles) != 0 {
		t.Fatalf("synchronous Toggle calls = %v, want none", tr.toggles)
	}
	if len(tr.cmdCalls) != 1 || tr.cmdCalls[0] != "/r/dir" {
		t.Fatalf("ToggleCmd calls = %v, want the selected directory", tr.cmdCalls)
	}
	msg := run(cmd)
	if got, ok := msg.(asyncToggledMsg); !ok || got.path != "/r/dir" {
		t.Fatalf("toggle command returned %#v", msg)
	}
}

// TestPanelAsyncExpandAndCollapseUseTheCommand checks the keyboard expand/collapse paths
// also return the async command.
func TestPanelAsyncExpandAndCollapseUseTheCommand(t *testing.T) {
	tr := &asyncFakeTree{rows: []tree.Row{{Name: "dir", Path: "/r/dir", IsDir: true, Expanded: true}}}
	p := New(tr, style.Styles{}, nil)
	_, cmd, _ := p.Action("file_explorer_collapse", testGeometry)
	if cmd == nil || len(tr.cmdCalls) != 1 || tr.cmdCalls[0] != "/r/dir" {
		t.Fatalf("collapse cmd calls = %v, want the directory", tr.cmdCalls)
	}
	if _, ok := run(cmd).(asyncToggledMsg); !ok {
		t.Fatal("collapse command did not return the toggle message")
	}
	tr.rows[0].Expanded = false
	tr.selected = 0
	_, cmd, _ = p.Action("file_explorer_expand", testGeometry)
	if cmd == nil || len(tr.cmdCalls) != 2 {
		t.Fatalf("expand cmd calls = %v, want a second one", tr.cmdCalls)
	}
}
