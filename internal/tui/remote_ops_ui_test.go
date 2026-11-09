package tui

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// remoteCapTree is a writable tree with the clipboard, the remote explorer's capability
// shape after task-148.
type remoteCapTree struct {
	rows     []tree.Row
	selected int
}

func (t *remoteCapTree) Root() string        { return "/r" }
func (t *remoteCapTree) Rows() []tree.Row    { return t.rows }
func (t *remoteCapTree) Move(d int)          { t.selected = min(max(t.selected+d, 0), len(t.rows)-1) }
func (t *remoteCapTree) Toggle(string) error { return nil }
func (t *remoteCapTree) Reload() error       { return nil }
func (t *remoteCapTree) Reveal(string) error { return nil }
func (t *remoteCapTree) ShowHidden() bool    { return false }
func (t *remoteCapTree) SetShowHidden(bool)  {}
func (t *remoteCapTree) ScrollBy(int)        {}
func (t *remoteCapTree) ScrollOffset() int   { return 0 }
func (t *remoteCapTree) ShowIgnored() bool   { return false }
func (t *remoteCapTree) SetShowIgnored(bool) {}
func (t *remoteCapTree) Selected() (tree.Row, bool) {
	if len(t.rows) == 0 {
		return tree.Row{}, false
	}
	return t.rows[min(t.selected, len(t.rows)-1)], true
}

// fakeRemoteOps is a remote explorerOps: RemoteTarget drives the confirmations and the
// clipboard methods record what the UI runs.
type fakeRemoteOps struct {
	target string
	copies []string
	moves  []string
	// conflict makes a cancel-policy copy/move report explorer.ErrConflict; conflictFor
	// limits it to one source and fail makes one source return an error.
	conflict    bool
	conflictFor string
	fail        map[string]error
	policies    []explorer.Conflict
	copyAs      [][3]string // src, dstDir, name
	moveAs      []string    // dstDir/name of a move
}

// sourceConflict reports the cancel-policy conflict of one source.
func (f *fakeRemoteOps) sourceConflict(src string, c explorer.Conflict) error {
	if !f.conflict || c != explorer.ConflictCancel {
		return nil
	}
	if f.conflictFor != "" && f.conflictFor != src {
		return nil
	}
	return errors.Join(explorer.ErrConflict, errors.New("exists"))
}

// sourceFailure reports an injected error of one source.
func (f *fakeRemoteOps) sourceFailure(src string) error {
	if f.fail == nil {
		return nil
	}
	return f.fail[src]
}

func (f *fakeRemoteOps) RemoteTarget() string { return f.target }
func (f *fakeRemoteOps) CreateFile(context.Context, string, string) (string, error) {
	return "", errors.New("not called")
}
func (f *fakeRemoteOps) Mkdir(context.Context, string, string) (string, error) {
	return "", errors.New("not called")
}
func (f *fakeRemoteOps) Move(_ context.Context, src, dstDir string) (string, error) {
	f.moves = append(f.moves, src+" -> "+dstDir)
	return dstDir + "/" + path.Base(src), nil
}
func (f *fakeRemoteOps) MoveAs(_ context.Context, src, dstDir, name string, c explorer.Conflict) (string, error) {
	f.policies = append(f.policies, c)
	if err := f.sourceFailure(src); err != nil {
		return "", err
	}
	if err := f.sourceConflict(src, c); err != nil {
		return "", err
	}
	if name == "" {
		name = path.Base(src)
	}
	dst := dstDir + "/" + name
	f.moveAs = append(f.moveAs, dst)
	f.moves = append(f.moves, src+" -> "+dstDir)
	return dst, nil
}
func (f *fakeRemoteOps) Copy(_ context.Context, src, dstDir string, c explorer.Conflict) (string, error) {
	f.policies = append(f.policies, c)
	if err := f.sourceFailure(src); err != nil {
		return "", err
	}
	if err := f.sourceConflict(src, c); err != nil {
		return "", err
	}
	f.copies = append(f.copies, src+" -> "+dstDir)
	return dstDir + "/" + path.Base(src), nil
}
func (f *fakeRemoteOps) CopyAs(_ context.Context, src, dstDir, name string, c explorer.Conflict) (string, error) {
	f.policies = append(f.policies, c)
	f.copyAs = append(f.copyAs, [3]string{src, dstDir, name})
	return dstDir + "/" + name, nil
}
func (f *fakeRemoteOps) Rename(context.Context, string, string) (string, error) {
	return "", errors.New("not called")
}
func (f *fakeRemoteOps) Trash(context.Context, string) error { return errors.New("not called") }
func (f *fakeRemoteOps) FollowRename(string, string) error   { return nil }
func (f *fakeRemoteOps) CloseDeleted(string) error           { return nil }
func (f *fakeRemoteOps) CheckRename(string, string) error    { return nil }

// TestRemoteExplorerPanelCapabilities checks the writable remote tree allows create,
// rename, delete and the clipboard actions.
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
	for action, want := range map[string]any{
		"cut":   explorer.CutMsg{},
		"copy":  explorer.CopyMsg{},
		"paste": explorer.PasteMsg{},
	} {
		_, cmd, ok := p.Action(action, explorer.Geometry{Top: 1, Width: 16, Bottom: 9})
		if !ok || cmd == nil {
			t.Fatalf("%s not handled", action)
		}
		msg := cmd()
		if _, isErr := msg.(explorer.ErrorMsg); isErr {
			t.Fatalf("%s still refused: %#v", action, msg)
		}
		switch want.(type) {
		case explorer.CutMsg:
			if _, ok := msg.(explorer.CutMsg); !ok {
				t.Fatalf("cut = %T", msg)
			}
		case explorer.CopyMsg:
			if _, ok := msg.(explorer.CopyMsg); !ok {
				t.Fatalf("copy = %T", msg)
			}
		case explorer.PasteMsg:
			if _, ok := msg.(explorer.PasteMsg); !ok {
				t.Fatalf("paste = %T", msg)
			}
		}
	}
}

// TestRemoteExplorerMenuHasClipboard checks the remote context menu has New/Rename/
// Delete, Cut/Copy/Paste/Duplicate and the path copies.
func TestRemoteExplorerMenuHasClipboard(t *testing.T) {
	tr := &remoteCapTree{rows: []tree.Row{{Name: "a.go", Path: "/r/a.go"}}}
	m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tr})
	m.ops = &fakeRemoteOps{target: "SSH:u@h:/r"}
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
	if containsAction(root, "paste") == false {
		t.Fatalf("root menu = %v, missing paste", root)
	}
	single := actions(explorer.ContextTarget{Paths: []string{"/r/a.go"}})
	for _, want := range []string{"file_explorer_new_file", "file_explorer_rename", "file_explorer_delete", "cut", "copy", "paste", "duplicate", "copy_full_path", "copy_relative_path"} {
		if !containsAction(single, want) {
			t.Fatalf("single menu = %v, missing %s", single, want)
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

// TestRemoteExplorerClipboardRefreshesWithoutBlocking checks a successful remote copy/cut
// queues the same remote tree reload without reading during the handler.
func TestRemoteExplorerClipboardRefreshesWithoutBlocking(t *testing.T) {
	for _, kind := range []jobKind{jobCopy, jobMove} {
		loader := (&fakeRemoteLoader{}).set("/srv/proj", RemoteDirEntry{Name: "a.go"}, RemoteDirEntry{Name: "a copy.go"})
		tr := NewRemoteExplorerTree("/srv/proj", loader)
		m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tr, ShowExplorer: true})
		m.ops = &fakeRemoteOps{target: "SSH:u@h:/srv/proj"}
		cmd := m.fileDone(fileResultMsg{job: fileJob{kind: kind, path: "/srv/proj/a.go", dir: "/srv/proj"}, out: "/srv/proj/a copy.go"})
		if cmd == nil {
			t.Fatalf("a successful remote %v queued no refresh", kind)
		}
		if loader.callCount() != 0 {
			t.Fatalf("the %v handler read remote directories instead of queueing a command", kind)
		}
		_ = cmd()
	}
}

// TestRemoteExplorerWriteRefreshesWithoutBlocking checks a successful remote create queues a
// remote tree reload in a command: no loader call happens during the handler, only when the
// command runs.
func TestRemoteExplorerWriteRefreshesWithoutBlocking(t *testing.T) {
	loader := (&fakeRemoteLoader{}).set("/srv/proj", RemoteDirEntry{Name: "x"})
	tr := NewRemoteExplorerTree("/srv/proj", loader)
	m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tr, ShowExplorer: true})
	m.ops = &fakeRemoteOps{target: "SSH:u@h:/srv/proj"}
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

// remotePasteModel builds a root with a writable remote tree and the fake remote ops.
func remotePasteModel(t *testing.T, ops *fakeRemoteOps) *Model {
	t.Helper()
	tr := &remoteCapTree{rows: []tree.Row{{Name: "a.go", Path: "/r/other/a.go"}}}
	m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tr, ShowExplorer: true})
	m.ops = ops
	return m
}

func remotePasteConflictErr() error { return errors.Join(explorer.ErrConflict, errors.New("exists")) }

// TestRemotePasteConflictOffersOverwriteRenameCancel checks a remote paste conflict opens
// the overwrite/rename choice: No opens the prefilled name prompt, submitting a new name
// reruns the copy as that name; Yes reruns it with overwrite; Esc cancels with nothing
// changed.
func TestRemotePasteConflictOffersOverwriteRenameCancel(t *testing.T) {
	job := func() fileJob {
		return fileJob{kind: jobCopy, path: "/r/other/a.go", dir: "/r/dst", policy: explorer.ConflictCancel}
	}

	// Rename.
	ops := &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true}
	m := remotePasteModel(t, ops)
	_, cmd := m.Update(fileResultMsg{job: job(), err: remotePasteConflictErr()})
	settle(t, m, cmd)
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the remote conflict prompt", len(m.overlays))
	}
	_, cmd = m.Update(typed("n"))
	settle(t, m, cmd)
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the rename prompt", len(m.overlays))
	}
	// The prompt is prefilled with the source name: replace it with b.go.
	for range len("a.go") {
		m.Update(special(tea.KeyBackspace))
	}
	typeText(m, "b.go")
	_, cmd = m.Update(special(tea.KeyEnter))
	settle(t, m, cmd)
	if len(ops.copyAs) != 1 || ops.copyAs[0] != [3]string{"/r/other/a.go", "/r/dst", "b.go"} {
		t.Fatalf("renamed copy = %v", ops.copyAs)
	}
	if len(m.overlays) != 0 {
		t.Fatal("the rename prompt stayed open")
	}

	// Overwrite.
	ops = &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true}
	m = remotePasteModel(t, ops)
	_, cmd = m.Update(fileResultMsg{job: job(), err: remotePasteConflictErr()})
	settle(t, m, cmd)
	_, cmd = m.Update(typed("y"))
	settle(t, m, cmd)
	if len(ops.policies) == 0 || ops.policies[len(ops.policies)-1] != explorer.ConflictOverwrite {
		t.Fatalf("policies = %v, want the overwrite retry", ops.policies)
	}

	// Cancel.
	ops = &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true}
	m = remotePasteModel(t, ops)
	_, cmd = m.Update(fileResultMsg{job: job(), err: remotePasteConflictErr()})
	settle(t, m, cmd)
	m.Update(special(tea.KeyEscape))
	if len(m.overlays) != 0 || len(ops.copies) != 0 || len(ops.copyAs) != 0 {
		t.Fatalf("cancel left overlays %d, copies %v, copyAs %v", len(m.overlays), ops.copies, ops.copyAs)
	}
}

// TestRemoteCutPasteConflictOverwrite checks a remote cut paste's conflict also offers the
// choice and the overwrite retry goes through MoveAs with the chosen policy.
func TestRemoteCutPasteConflictOverwrite(t *testing.T) {
	ops := &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true}
	m := remotePasteModel(t, ops)
	_, cmd := m.Update(fileResultMsg{job: fileJob{kind: jobMove, path: "/r/other/a.go", dir: "/r/dst"}, err: remotePasteConflictErr()})
	settle(t, m, cmd)
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the cut conflict prompt", len(m.overlays))
	}
	_, cmd = m.Update(typed("y"))
	settle(t, m, cmd)
	if len(ops.policies) == 0 || ops.policies[len(ops.policies)-1] != explorer.ConflictOverwrite {
		t.Fatalf("policies = %v, want the overwrite move retry", ops.policies)
	}
	if len(ops.moveAs) != 1 || ops.moveAs[0] != "/r/dst/a.go" {
		t.Fatalf("moveAs = %v", ops.moveAs)
	}
}

// startRemotePaste starts a paste of paths and runs it until the first conflict overlay
// (or completion).
func startRemotePaste(t *testing.T, m *Model, ops *fakeRemoteOps, paths []string, cut bool) {
	t.Helper()
	m.clip = newClipboard("", paths, cut)
	cmd := m.paste("/r/dst")
	if cmd == nil {
		t.Fatal("the paste started no jobs")
	}
	settle(t, m, cmd)
	if len(m.overlays) == 0 {
		t.Fatal("the paste did not stop at a conflict")
	}
}

// chooseRemoteConflict answers the top conflict overlay with a mnemonic key.
func chooseRemoteConflict(t *testing.T, m *Model, mnemonic string) {
	t.Helper()
	_, cmd := m.Update(typed(mnemonic))
	settle(t, m, cmd)
}

// TestRemotePasteOverwriteAll checks Overwrite All overwrites the current conflict and
// applies the overwrite policy to every remaining job, so no further prompt appears.
func TestRemotePasteOverwriteAll(t *testing.T) {
	ops := &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true}
	m := remotePasteModel(t, ops)
	startRemotePaste(t, m, ops, []string{"/r/a.go", "/r/b.go"}, false)
	chooseRemoteConflict(t, m, "O")
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d after Overwrite All", len(m.overlays))
	}
	if len(ops.copies) != 2 {
		t.Fatalf("copies = %v, want both items", ops.copies)
	}
	if len(ops.policies) != 3 || ops.policies[0] != explorer.ConflictCancel || ops.policies[1] != explorer.ConflictOverwrite || ops.policies[2] != explorer.ConflictOverwrite {
		t.Fatalf("policies = %v, want cancel then two overwrites", ops.policies)
	}
}

// TestRemotePasteSkipKeepsAsking checks Skip skips only the current conflict and asks
// again on the next one; skipping all conflicts finishes without running them.
func TestRemotePasteSkipKeepsAsking(t *testing.T) {
	ops := &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true}
	m := remotePasteModel(t, ops)
	startRemotePaste(t, m, ops, []string{"/r/a.go", "/r/b.go"}, false)
	chooseRemoteConflict(t, m, "s")
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the second conflict prompt", len(m.overlays))
	}
	chooseRemoteConflict(t, m, "s")
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d after the last skip", len(m.overlays))
	}
	if len(ops.copies) != 0 {
		t.Fatalf("copies = %v, want none after skips", ops.copies)
	}
}

// TestRemotePasteSkipAllStopsRemaining checks Skip All skips the current and every
// remaining conflict at once.
func TestRemotePasteSkipAllStopsRemaining(t *testing.T) {
	ops := &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true}
	m := remotePasteModel(t, ops)
	startRemotePaste(t, m, ops, []string{"/r/a.go", "/r/b.go", "/r/c.go"}, false)
	chooseRemoteConflict(t, m, "S")
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d after Skip All", len(m.overlays))
	}
	if len(ops.copies) != 0 || len(ops.policies) != 1 {
		t.Fatalf("copies = %v policies = %v, want only the first attempt", ops.copies, ops.policies)
	}
	if !strings.Contains(m.notice, "Paste skipped") {
		t.Fatalf("notice = %q, want the skip notice", m.notice)
	}
}

// TestRemotePasteCancelKeepsCompletedItems checks Cancel after one item already completed
// stops the remaining jobs, keeps the completed copy, and says how many items completed.
func TestRemotePasteCancelKeepsCompletedItems(t *testing.T) {
	ops := &fakeRemoteOps{target: "SSH:u@h:/r", conflict: true, conflictFor: "/r/b.go"}
	m := remotePasteModel(t, ops)
	startRemotePaste(t, m, ops, []string{"/r/a.go", "/r/b.go", "/r/c.go"}, false)
	chooseRemoteConflict(t, m, "c")
	if len(ops.copies) != 1 || !strings.Contains(ops.copies[0], "/r/a.go") {
		t.Fatalf("copies = %v, want the completed a.go only", ops.copies)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d after Cancel", len(m.overlays))
	}
	if !strings.Contains(m.notice, "Paste cancelled") || !strings.Contains(m.notice, "1 item(s)") {
		t.Fatalf("notice = %q, want cancelled plus the completed count", m.notice)
	}
}

// TestRemotePasteFailureKeepsCompletedCount checks a failure later in an Overwrite All
// chain stops the rest, reports the error with the already-pasted count, and leaves the
// completed copy alone.
func TestRemotePasteFailureKeepsCompletedCount(t *testing.T) {
	ops := &fakeRemoteOps{
		target:   "SSH:u@h:/r",
		conflict: true,
		fail:     map[string]error{"/r/b.go": errors.New("disk full")},
	}
	m := remotePasteModel(t, ops)
	startRemotePaste(t, m, ops, []string{"/r/a.go", "/r/b.go", "/r/c.go"}, false)
	chooseRemoteConflict(t, m, "O")
	if len(ops.copies) != 1 || !strings.Contains(ops.copies[0], "/r/a.go") {
		t.Fatalf("copies = %v, want a.go only", ops.copies)
	}
	if len(ops.policies) != 3 {
		t.Fatalf("policies = %v, want a's cancel+overwrite and b's overwrite", ops.policies)
	}
	if !strings.Contains(m.notice, "disk full") || !strings.Contains(m.notice, "1 item(s)") {
		t.Fatalf("notice = %q, want the failure plus the completed count", m.notice)
	}
	if len(m.overlays) != 0 {
		t.Fatal("a failure left a conflict overlay open")
	}
}
