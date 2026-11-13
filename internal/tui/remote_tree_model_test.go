package tui

import (
	"strings"

	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// remoteExplorerModel builds the root with the read-only remote tree shown.
func remoteExplorerModel(t *testing.T, loader *fakeRemoteLoader) (*Model, *RemoteExplorerTree) {
	t.Helper()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	m := New(Deps{Editor: newFakeEditor("x"), Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, Explorer: tree, ShowExplorer: true})
	return m, tree
}

// TestRemoteExplorerUpdateReadsNothing checks the update loop never calls the remote
// loader: the tree's command does the listing, and the update only applies its message.
func TestRemoteExplorerUpdateReadsNothing(t *testing.T) {
	loader := remoteTreeTestLoader()
	m, tree := remoteExplorerModel(t, loader)
	// The initial load is a command as well.
	init := m.remoteExplorerReloadCmd()
	if init == nil {
		t.Fatal("a remote tree got no initial load command")
	}
	if loader.callCount() != 0 {
		t.Fatalf("the model read %d directories before the command ran", loader.callCount())
	}
	msg := init()
	if loader.callCount() != 1 {
		t.Fatalf("the initial command read %d directories, want one", loader.callCount())
	}
	if _, cmd := m.Update(msg); cmd != nil {
		// The application may start the reveal/follow work; it must not read remote
		// directories on the update loop.
		for range deliver(cmd) {
		}
	}
	if loader.callCount() != 1 {
		t.Fatalf("Update started %d more remote reads", loader.callCount()-1)
	}
	wantRemoteRows(t, tree, "proj", "cmd", "main.go")
}

// TestRemoteExplorerReconnectReloads checks a successful authority reconnect schedules a
// remote tree reload in a command, so a disconnected error is cleared.
func TestRemoteExplorerReconnectReloads(t *testing.T) {
	loader := remoteTreeTestLoader()
	m, tree := remoteExplorerModel(t, loader)
	applyRemoteInitial(t, tree)
	loader.mu.Lock()
	loader.err = ErrRemoteExplorerDisconnected
	loader.mu.Unlock()
	if msg := tree.ReloadCmd("")(); msg != nil {
		_ = msg
	}
	loader.mu.Lock()
	loader.err = nil
	loader.mu.Unlock()

	_, cmd := m.Update(authorityReconnectedMsg{})
	if cmd == nil {
		t.Fatal("a successful reconnect scheduled no command")
	}
	before := loader.callCount()
	msgs := deliver(cmd)
	if len(msgs) == 0 {
		t.Fatal("the reconnect command produced no messages")
	}
	for _, msg := range msgs {
		_, _ = m.Update(msg)
	}
	if loader.callCount() <= before {
		t.Fatalf("reconnect reloaded nothing (calls %d -> %d)", before, loader.callCount())
	}
}

// TestRemoteExplorerDisconnectedNotice checks a disconnected listing becomes the explicit
// notice on the status row and the tree keeps its rows for the reconnect reload.
func TestRemoteExplorerDisconnectedNotice(t *testing.T) {
	loader := remoteTreeTestLoader()
	m, tree := remoteExplorerModel(t, loader)
	applyRemoteInitial(t, tree)
	loader.mu.Lock()
	loader.err = ErrRemoteExplorerDisconnected
	loader.mu.Unlock()
	msg := tree.ReloadCmd("")()
	_, _ = m.Update(msg)
	if !strings.Contains(m.notice, "remote workspace is disconnected") {
		t.Fatalf("notice = %q, want the disconnected explanation", m.notice)
	}
	// The cached rows stay visible, so the reconnect can refresh them.
	wantRemoteRows(t, tree, "proj", "cmd", "main.go")
}

// TestRemoteExplorerLoadNoticeSurfaced checks a degraded loader notice (unsupported
// directory watching or incomplete ignore filtering) reaches the status row through the
// remote tree load handler.
func TestRemoteExplorerLoadNoticeSurfaced(t *testing.T) {
	loader := remoteTreeTestLoader()
	loader.mu.Lock()
	loader.notice = "remote directory watching is not supported by this backend; refresh manually (Ctrl+R)"
	loader.mu.Unlock()
	m, tree := remoteExplorerModel(t, loader)
	_, _ = m.Update(remoteTreeLoadedMsg{tree: tree})
	if !strings.Contains(m.notice, "not supported by this backend") {
		t.Fatalf("notice = %q, want the loader degradation notice", m.notice)
	}
	if got := tree.TakeLoadNotice(); got != "" {
		t.Fatalf("the notice was not consumed once: %q", got)
	}
}

// TestRemoteWatchEventRefreshesTheTree checks a directory-watch event schedules the remote
// tree reload in a command (the event itself reads nothing on the update loop), completing
// the watch -> refresh path for the remote explorer.
func TestRemoteWatchEventRefreshesTheTree(t *testing.T) {
	loader := remoteTreeTestLoader()
	m, tree := remoteExplorerModel(t, loader)
	applyRemoteInitial(t, tree)
	before := loader.callCount()
	cmd := func() tea.Cmd {
		_, c := m.Update(watchMsg{dirs: []string{"/srv/proj"}})
		return c
	}()
	if cmd == nil {
		t.Fatal("a watch event scheduled no tree refresh")
	}
	if loader.callCount() != before {
		t.Fatalf("the watch handler read remote directories (%d -> %d)", before, loader.callCount())
	}
	_ = cmd()
	if loader.callCount() <= before {
		t.Fatal("the scheduled refresh did not list the remote tree")
	}
}
