package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/theme"
)

// remoteClipboardModel is a model with the writable remote tree and adapter.
func remoteClipboardModel(t *testing.T, target string) (*Model, *fakeRemoteOps) {
	t.Helper()
	tr := &remoteCapTree{rows: []tree.Row{{Name: "a.go", Path: "/r/a.go"}, {Name: "sub", Path: "/r/sub", IsDir: true}}}
	m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tr})
	ops := &fakeRemoteOps{target: target}
	m.ops = ops
	return m, ops
}

// answerYes delivers the pending confirmation's Yes answer and runs its command.
func answerYes(t *testing.T, m *Model) {
	t.Helper()
	if len(m.overlays) == 0 {
		t.Fatal("no confirmation overlay is open")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("the confirmation produced no answer")
	}
	// Deliver the answer (and any disk job it starts) back through Update.
	for round := 0; cmd != nil && round < 20; round++ {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = m.Update(msg)
	}
}

// TestRemoteClipboardConfirmations checks cut/copy/paste/duplicate ask first on a
// remote workspace, the answer runs the action, and the question names the target.
func TestRemoteClipboardConfirmations(t *testing.T) {
	t.Parallel()
	target := "SSH:u@h:/r"

	// Cut: the clipboard is not armed until Yes.
	m, _ := remoteClipboardModel(t, target)
	m.Update(explorer.CutMsg{Path: "/r/a.go"})
	if len(m.overlays) != 1 || m.clip != nil {
		t.Fatalf("cut overlays=%d clip=%+v", len(m.overlays), m.clip)
	}
	answerYes(t, m)
	if m.clip == nil || !m.clip.cut || len(m.clip.paths) != 1 || m.clip.paths[0] != "/r/a.go" {
		t.Fatalf("remote cut clipboard = %+v", m.clip)
	}

	// Copy: then a paste asks naming the destination and runs the copy job.
	m2, ops := remoteClipboardModel(t, target)
	m2.Update(explorer.CopyMsg{Path: "/r/a.go"})
	answerYes(t, m2)
	if m2.clip == nil || m2.clip.cut {
		t.Fatalf("remote copy clipboard = %+v", m2.clip)
	}
	m2.Update(explorer.PasteMsg{Dir: "/r/sub"})
	if len(m2.overlays) != 1 {
		t.Fatal("remote paste did not confirm")
	}
	answerYes(t, m2)
	if len(ops.copies) != 1 || !strings.Contains(ops.copies[0], "/r/a.go -> /r/sub") {
		t.Fatalf("paste did not run the remote copy: %v", ops.copies)
	}

	// Duplicate: the menu action confirms, then copies next to the source.
	m3, ops3 := remoteClipboardModel(t, target)
	if cmd := m3.explorerMenuAction("duplicate", []string{"/r/a.go"}); cmd != nil {
		t.Fatal("a remote duplicate ran before the confirmation")
	}
	if len(m3.overlays) != 1 {
		t.Fatal("remote duplicate did not confirm")
	}
	answerYes(t, m3)
	if len(ops3.copies) != 1 || !strings.Contains(ops3.copies[0], "/r/a.go -> /r") {
		t.Fatalf("duplicate did not copy next to the source: %v", ops3.copies)
	}
}

// TestRemoteClipboardQuestionNamesTarget checks the wording of each confirmation.
func TestRemoteClipboardQuestionNamesTarget(t *testing.T) {
	t.Parallel()
	target := "SSH:u@h:/remote/w"
	for _, kind := range []string{remoteClipboardCopy, remoteClipboardCut, remoteClipboardDuplicate} {
		got := remoteClipboardQuestion(kind, []string{"/remote/w/a.go"}, "", target)
		if !strings.Contains(got, "a.go") || !strings.Contains(got, target) {
			t.Errorf("%s question = %q", kind, got)
		}
	}
	if got := pasteQuestion(2, "/remote/w/dst", target); !strings.Contains(got, "2 entries") || !strings.Contains(got, "/remote/w/dst") || !strings.Contains(got, target) {
		t.Errorf("paste question = %q", got)
	}
}

// TestLocalClipboardStillActsDirectly checks the local workspace keeps the old
// no-confirmation clipboard behavior.
func TestLocalClipboardStillActsDirectly(t *testing.T) {
	t.Parallel()
	m, _ := remoteClipboardModel(t, "")
	m.Update(explorer.CutMsg{Path: "/r/a.go"})
	if len(m.overlays) != 0 {
		t.Fatal("a local cut asked for confirmation")
	}
	if m.clip == nil || !m.clip.cut {
		t.Fatalf("local cut clipboard = %+v", m.clip)
	}
}
