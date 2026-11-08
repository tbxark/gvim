package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// remoteCapTree is a writable tree without clipboard operations, the remote explorer's
// capability shape.
type remoteCapTree struct {
	rows     []tree.Row
	selected int
}

func (t *remoteCapTree) Root() string            { return "/r" }
func (t *remoteCapTree) Rows() []tree.Row        { return t.rows }
func (t *remoteCapTree) Move(d int)              { t.selected = min(max(t.selected+d, 0), len(t.rows)-1) }
func (t *remoteCapTree) Toggle(string) error     { return nil }
func (t *remoteCapTree) Reload() error           { return nil }
func (t *remoteCapTree) Reveal(string) error     { return nil }
func (t *remoteCapTree) ShowHidden() bool        { return false }
func (t *remoteCapTree) SetShowHidden(bool)      {}
func (t *remoteCapTree) ScrollBy(int)            {}
func (t *remoteCapTree) ScrollOffset() int       { return 0 }
func (t *remoteCapTree) ShowIgnored() bool       { return false }
func (t *remoteCapTree) SetShowIgnored(bool)     {}
func (t *remoteCapTree) ClipboardDisabled() bool { return true }
func (t *remoteCapTree) Selected() (tree.Row, bool) {
	if len(t.rows) == 0 {
		return tree.Row{}, false
	}
	return t.rows[min(t.selected, len(t.rows)-1)], true
}

// fakeRemoteOps is a remote explorerOps: only RemoteTarget is read by the UI tests; the
// disk methods are never called.
type fakeRemoteOps struct{ target string }

func (f fakeRemoteOps) RemoteTarget() string { return f.target }
func (f fakeRemoteOps) CreateFile(context.Context, string, string) (string, error) {
	return "", errors.New("not called")
}
func (f fakeRemoteOps) Mkdir(context.Context, string, string) (string, error) {
	return "", errors.New("not called")
}
func (f fakeRemoteOps) Move(context.Context, string, string) (string, error) {
	return "", errors.New("not called")
}
func (f fakeRemoteOps) Copy(context.Context, string, string, explorer.Conflict) (string, error) {
	return "", errors.New("not called")
}
func (f fakeRemoteOps) Rename(context.Context, string, string) (string, error) {
	return "", errors.New("not called")
}
func (f fakeRemoteOps) Trash(context.Context, string) error { return errors.New("not called") }
func (f fakeRemoteOps) FollowRename(string, string) error   { return nil }
func (f fakeRemoteOps) CloseDeleted(string) error           { return nil }
func (f fakeRemoteOps) CheckRename(string, string) error    { return nil }

// TestRemoteExplorerPanelCapabilities checks the writable remote tree allows create,
// rename and delete but refuses cut/paste before any adapter call.
func TestRemoteExplorerPanelCapabilities(t *testing.T) {
	tr := &remoteCapTree{rows: []tree.Row{{Name: "a.go", Path: "/r/a.go"}, {Name: "sub", Path: "/r/sub", IsDir: true}}}
	p := explorer.New(tr, style.Styles{}, nil)
	for _, tc := range []struct {
		action string
		want   any
	}{
		{"file_explorer_new_file", explorer.NewFileMsg{}},
		{"file_explorer_new_directory", explorer.NewFolderMsg{}},
		{"file_explorer_rename", explorer.RenameMsg{}},
		{"file_explorer_delete", explorer.DeleteMsg{}},
	} {
		_, cmd, ok := p.Action(tc.action, explorer.Geometry{Top: 1, Width: 16, Bottom: 9})
		if !ok || cmd == nil {
			t.Fatalf("%s not handled or no command", tc.action)
		}
		msg := cmd()
		switch tc.want.(type) {
		case explorer.NewFileMsg:
			if _, ok := msg.(explorer.NewFileMsg); !ok {
				t.Fatalf("%s returned %T", tc.action, msg)
			}
		case explorer.NewFolderMsg:
			if _, ok := msg.(explorer.NewFolderMsg); !ok {
				t.Fatalf("%s returned %T", tc.action, msg)
			}
		case explorer.RenameMsg:
			if _, ok := msg.(explorer.RenameMsg); !ok {
				t.Fatalf("%s returned %T", tc.action, msg)
			}
		case explorer.DeleteMsg:
			if _, ok := msg.(explorer.DeleteMsg); !ok {
				t.Fatalf("%s returned %T", tc.action, msg)
			}
		}
	}
	for _, action := range []string{"cut", "paste"} {
		_, cmd, ok := p.Action(action, explorer.Geometry{Top: 1, Width: 16, Bottom: 9})
		if !ok || cmd == nil {
			t.Fatalf("%s not handled", action)
		}
		msg, ok := cmd().(explorer.ErrorMsg)
		if !ok || !errors.Is(msg.Err, explorer.ErrClipboardDisabled) {
			t.Fatalf("%s = %#v, want the clipboard-disabled error", action, cmd())
		}
	}
}

// TestRemoteExplorerMenuHasWritesWithoutClipboard checks the remote context menu keeps
// New/Rename/Delete and the path copies, and drops Cut/Copy/Paste/Duplicate.
func TestRemoteExplorerMenuHasWritesWithoutClipboard(t *testing.T) {
	tr := &remoteCapTree{rows: []tree.Row{{Name: "a.go", Path: "/r/a.go"}}}
	m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tr})
	m.ops = fakeRemoteOps{target: "SSH:u@h:/r"}
	actions := func(target explorer.ContextTarget) []string {
		var out []string
		for _, it := range m.explorerMenuItems(target) {
			if it.Separator {
				continue
			}
			out = append(out, it.Action)
		}
		return out
	}
	root := actions(explorer.ContextTarget{Root: true})
	for _, want := range []string{"file_explorer_new_file", "file_explorer_new_directory"} {
		if !containsAction(root, want) {
			t.Fatalf("root menu = %v, missing %s", root, want)
		}
	}
	if containsAction(root, "paste") {
		t.Fatalf("root menu = %v, has paste on a clipboard-disabled tree", root)
	}
	single := actions(explorer.ContextTarget{Paths: []string{"/r/a.go"}})
	for _, want := range []string{"file_explorer_new_file", "file_explorer_rename", "file_explorer_delete", "copy_full_path", "copy_relative_path"} {
		if !containsAction(single, want) {
			t.Fatalf("single menu = %v, missing %s", single, want)
		}
	}
	for _, bad := range []string{"cut", "copy", "paste", "duplicate"} {
		if containsAction(single, bad) {
			t.Fatalf("single menu = %v, has %s on a clipboard-disabled tree", single, bad)
		}
	}
}

func containsAction(items []string, action string) bool {
	for _, a := range items {
		if a == "explorer:"+action {
			return true
		}
	}
	return false
}

// TestRemoteConfirmationsNameTheTarget checks the name/rename/delete prompts state the
// remote target, and the delete states that it is permanent.
func TestRemoteConfirmationsNameTheTarget(t *testing.T) {
	target := "SSH:u@h:/remote/w"
	if got := newEntryLabel(false, target); !strings.Contains(got, "New file") || !strings.Contains(got, target) {
		t.Fatalf("file label = %q", got)
	}
	if got := newEntryLabel(true, target); !strings.Contains(got, "New folder") || !strings.Contains(got, target) {
		t.Fatalf("folder label = %q", got)
	}
	if got := renamePromptLabel(target); !strings.Contains(got, target) || !strings.Contains(got, "Rename") {
		t.Fatalf("rename label = %q", got)
	}
	one := deleteQuestion([]string{"/remote/w/a.go"}, target)
	for _, want := range []string{"Permanently delete", "a.go", target, "no trash"} {
		if !strings.Contains(one, want) {
			t.Fatalf("delete question = %q, missing %q", one, want)
		}
	}
	many := deleteQuestion([]string{"/remote/w/a.go", "/remote/w/b.go"}, target)
	for _, want := range []string{"Permanently delete 2 items", target, "no trash"} {
		if !strings.Contains(many, want) {
			t.Fatalf("multi delete question = %q, missing %q", many, want)
		}
	}
	// The local workspace keeps the old wording.
	if got := deleteQuestion([]string{"/w/a.go"}, ""); got != "Delete a.go?" {
		t.Fatalf("local delete question = %q", got)
	}
	if got := renamePromptLabel(""); got != "Rename to:" {
		t.Fatalf("local rename label = %q", got)
	}
}

// TestRemoteExplorerWriteRefreshesWithoutBlocking checks a successful remote create queues a
// remote tree reload in a command: no loader call happens during the handler, only when the
// command runs.
func TestRemoteExplorerWriteRefreshesWithoutBlocking(t *testing.T) {
	loader := (&fakeRemoteLoader{}).set("/srv/proj", RemoteDirEntry{Name: "x"})
	tr := NewRemoteExplorerTree("/srv/proj", loader)
	m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tr, ShowExplorer: true})
	m.ops = fakeRemoteOps{target: "SSH:u@h:/srv/proj"}
	cmd := m.fileDone(fileResultMsg{job: fileJob{kind: jobCreate, dir: "/srv/proj", name: "x"}, out: "/srv/proj/x"})
	if cmd == nil {
		t.Fatal("a successful remote create queued no refresh")
	}
	if loader.callCount() != 0 {
		t.Fatalf("the handler read %d remote directories instead of queueing a command", loader.callCount())
	}
	_ = cmd()
	if loader.callCount() == 0 {
		t.Fatal("the queued refresh read nothing")
	}
}
