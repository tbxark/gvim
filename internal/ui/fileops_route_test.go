package ui

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
)

// fakeOps records the disk operations the root asks for. Each call returns the
// path the real adapter would, and fails with err when it is set. A copy into an
// existing destination reports ErrConflict under the cancel policy when
// conflict is set.
type fakeOps struct {
	calls    []string
	policies []expanel.Conflict
	renamed  [][2]string
	closed   []string
	conflict bool
	err      error
	// failSrc, when set, limits err to a copy or move of that source path.
	failSrc string
	// refuse is what CheckRename returns; nil lets every rename or move through.
	refuse error
	// checked records the (old, new) pairs CheckRename was asked about.
	checked [][2]string
}

// CheckRename records the question and returns refuse.
func (f *fakeOps) CheckRename(oldPath, newPath string) error {
	f.checked = append(f.checked, [2]string{oldPath, newPath})
	return f.refuse
}

// errFor is the error a copy or move of src returns: err, unless failSrc names
// another source.
func (f *fakeOps) errFor(src string) error {
	if f.failSrc != "" && f.failSrc != src {
		return nil
	}
	return f.err
}

func (f *fakeOps) CreateFile(_ context.Context, dir, name string) (string, error) {
	f.calls = append(f.calls, "create "+filepath.Join(dir, name))
	return filepath.Join(dir, name), f.err
}

func (f *fakeOps) Mkdir(_ context.Context, dir, name string) (string, error) {
	f.calls = append(f.calls, "mkdir "+filepath.Join(dir, name))
	return filepath.Join(dir, name), f.err
}

func (f *fakeOps) Rename(_ context.Context, path, newName string) (string, error) {
	np := filepath.Join(filepath.Dir(path), newName)
	f.calls = append(f.calls, "rename "+path+" "+np)
	return np, f.err
}

func (f *fakeOps) Move(_ context.Context, src, dstDir string) (string, error) {
	dst := filepath.Join(dstDir, filepath.Base(src))
	f.calls = append(f.calls, "move "+src+" "+dst)
	return dst, f.errFor(src)
}

func (f *fakeOps) Copy(_ context.Context, src, dstDir string, c expanel.Conflict) (string, error) {
	dst := filepath.Join(dstDir, filepath.Base(src))
	f.calls = append(f.calls, "copy "+src+" "+dst)
	f.policies = append(f.policies, c)
	if f.conflict && c == expanel.ConflictCancel {
		return "", errors.Join(expanel.ErrConflict, errors.New("exists"))
	}
	return dst, f.errFor(src)
}

func (f *fakeOps) Trash(_ context.Context, path string) error {
	f.calls = append(f.calls, "trash "+path)
	return f.err
}

func (f *fakeOps) FollowRename(oldPath, newPath string) error {
	f.renamed = append(f.renamed, [2]string{oldPath, newPath})
	return nil
}

func (f *fakeOps) CloseDeleted(path string) error {
	f.closed = append(f.closed, path)
	return nil
}

// newOpsModel returns a model with the explorer over a fake tree and the fake
// operations wired.
func newOpsModel(t *testing.T, ops *fakeOps) (*Model, *fakeEditor) {
	t.Helper()
	ed := newFakeEditor("hello")
	ed.quitAction = "quit"
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(3),
		Keys:     testKeys(),
		Ops:      ops,
		Theme:    theme.Theme{},
		Clock:    testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, ed
}

// settle runs cmd and feeds each message it returns back into m, until no command
// is left. It fails the test if the commands do not end.
func settle(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for range 20 {
		if cmd == nil {
			return
		}
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = m.Update(msg)
	}
	t.Fatal("commands did not settle")
}

// type feeds the text to the top overlay, one key press per rune.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(typed(string(r)))
	}
}

func TestNewFileOpensPromptAndCreates(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.NewFileMsg{Dir: "/ws"})
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the name prompt", len(m.overlays))
	}
	typeText(m, "x.txt")
	_, cmd := m.Update(special(tea.KeyEnter))
	settle(t, m, cmd)
	if want := []string{"create /ws/x.txt"}; !equalStrings(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
	if len(m.overlays) != 0 {
		t.Errorf("prompt still open after submit")
	}
}

func TestNewFolderCallsMkdir(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.NewFolderMsg{Dir: "/ws/sub"})
	typeText(m, "d")
	_, cmd := m.Update(special(tea.KeyEnter))
	settle(t, m, cmd)
	if want := []string{"mkdir /ws/sub/d"}; !equalStrings(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
}

func TestNameWithSlashKeepsPromptOpen(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.NewFileMsg{Dir: "/ws"})
	typeText(m, "a/b")
	_, cmd := m.Update(special(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("a name with a slash was submitted")
	}
	if len(m.overlays) != 1 {
		t.Fatal("prompt closed on an invalid name")
	}
	if len(ops.calls) != 0 {
		t.Fatalf("calls = %v, want none", ops.calls)
	}
}

func TestRenameMovesOpenTabs(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.RenameMsg{Path: "/ws/f0"})
	// The prompt starts with the current name. Delete the last rune and type a new one.
	m.Update(special(tea.KeyBackspace))
	typeText(m, "2")
	_, cmd := m.Update(special(tea.KeyEnter))
	settle(t, m, cmd)
	if want := []string{"rename /ws/f0 /ws/f2"}; !equalStrings(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
	if len(ops.renamed) != 1 || ops.renamed[0] != [2]string{"/ws/f0", "/ws/f2"} {
		t.Fatalf("FollowRename = %v, want /ws/f0 to /ws/f2", ops.renamed)
	}
}

// TestRenameOntoAnOpenTabIsRefusedInUpdate checks that the root asks the editor
// before the command starts: a refused rename never reaches the disk, and the
// refusal shows in the notice row.
func TestRenameOntoAnOpenTabIsRefusedInUpdate(t *testing.T) {
	refused := errors.New("rename: file already open in another tab")
	ops := &fakeOps{refuse: refused}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.RenameMsg{Path: "/ws/f0"})
	m.Update(special(tea.KeyBackspace))
	typeText(m, "2")
	_, cmd := m.Update(special(tea.KeyEnter))
	settle(t, m, cmd)
	if want := [][2]string{{"/ws/f0", "/ws/f2"}}; !reflect.DeepEqual(ops.checked, want) {
		t.Fatalf("CheckRename asked %v, want %v", ops.checked, want)
	}
	for _, c := range ops.calls {
		if strings.HasPrefix(c, "rename ") {
			t.Fatalf("refused rename reached the disk: %v", ops.calls)
		}
	}
	if m.notice != refused.Error() {
		t.Fatalf("notice = %q, want %q", m.notice, refused.Error())
	}
}

func TestDeleteAsksAndTrashesOnYes(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.DeleteMsg{Path: "/ws/f0"})
	if len(m.overlays) != 1 {
		t.Fatal("delete did not ask")
	}
	_, cmd := m.Update(typed("y"))
	settle(t, m, cmd)
	if want := []string{"trash /ws/f0"}; !equalStrings(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
	if !equalStrings(ops.closed, []string{"/ws/f0"}) {
		t.Fatalf("CloseDeleted = %v, want /ws/f0", ops.closed)
	}
}

func TestDeleteDefaultsToNo(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.DeleteMsg{Path: "/ws/f0"})
	// The focus starts on No, so Enter cancels.
	_, cmd := m.Update(special(tea.KeyEnter))
	settle(t, m, cmd)
	if len(ops.calls) != 0 || len(ops.closed) != 0 {
		t.Fatalf("delete ran without a yes: calls %v, closed %v", ops.calls, ops.closed)
	}
}

func TestCopyPasteAsksBeforeOverwrite(t *testing.T) {
	ops := &fakeOps{conflict: true}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.CopyMsg{Path: "/ws/f0"})
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	if len(m.overlays) != 1 {
		t.Fatal("a conflict did not ask to overwrite")
	}
	_, cmd = m.Update(typed("y"))
	settle(t, m, cmd)
	want := []expanel.Conflict{expanel.ConflictCancel, expanel.ConflictOverwrite}
	if len(ops.policies) != 2 || ops.policies[0] != want[0] || ops.policies[1] != want[1] {
		t.Fatalf("policies = %v, want %v", ops.policies, want)
	}
}

func TestCopyPasteNoSkipsOverwrite(t *testing.T) {
	ops := &fakeOps{conflict: true}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.CopyMsg{Path: "/ws/f0"})
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	_, cmd = m.Update(typed("n"))
	settle(t, m, cmd)
	if len(ops.policies) != 2 || ops.policies[1] != expanel.ConflictSkip {
		t.Fatalf("policies = %v, want a second attempt with skip", ops.policies)
	}
}

func TestCutPasteMovesAndClearsClipboard(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.CutMsg{Path: "/ws/f0"})
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	settle(t, m, cmd)
	if want := []string{"move /ws/f0 /other/f0"}; !equalStrings(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
	if len(ops.renamed) != 1 || ops.renamed[0] != [2]string{"/ws/f0", "/other/f0"} {
		t.Fatalf("FollowRename = %v", ops.renamed)
	}
	if m.clip != nil {
		t.Errorf("clipboard kept after a cut paste: %+v", m.clip)
	}
}

func TestPasteWithoutClipboardSaysSo(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	_, cmd := m.Update(expanel.PasteMsg{Dir: "/other"})
	if cmd != nil || len(ops.calls) != 0 {
		t.Fatalf("paste with an empty clipboard ran %v", ops.calls)
	}
	if m.notice != "Nothing to paste" {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestDiskErrorGoesToNotice(t *testing.T) {
	ops := &fakeOps{err: errors.New("disk full")}
	m, _ := newOpsModel(t, ops)
	m.Update(expanel.NewFileMsg{Dir: "/ws"})
	typeText(m, "x")
	_, cmd := m.Update(special(tea.KeyEnter))
	settle(t, m, cmd)
	if m.notice != "disk full" {
		t.Fatalf("notice = %q, want the disk error", m.notice)
	}
}

func TestWithoutOpsFileKeysSayNotSupported(t *testing.T) {
	ed := newFakeEditor("hello")
	m := New(Deps{Editor: ed, Explorer: newFakeTree(2), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(expanel.NewFileMsg{Dir: "/ws"})
	if len(m.overlays) != 0 {
		t.Fatal("prompt opened without file operations")
	}
	if !strings.Contains(m.notice, "not supported") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestExplorerKeysReachTheRoot(t *testing.T) {
	ops := &fakeOps{}
	m, _ := newOpsModel(t, ops)
	press(m, ctrl('e')) // focus the explorer
	cmd := press(m, ctrl('c'))
	if got, ok := runMsg(cmd).(expanel.CopyMsg); !ok || got.Path != "/ws/f0" {
		t.Fatalf("ctrl+c in the explorer sent %#v, want CopyMsg for /ws/f0", runMsg(cmd))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func runMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}
