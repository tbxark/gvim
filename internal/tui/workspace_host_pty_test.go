package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// startHiddenDock starts A's dock shell and returns the fake terminal. It drives the
// dock's own start messages, not a Model action, because this test is about the host's
// command-result routing.
func startHiddenDock(t *testing.T, a *Model, starter *shellsStarter) *fakeTerm {
	t.Helper()
	start := a.dock.NewTab()
	for start != nil {
		msg := start()
		if msg == nil {
			break
		}
		var next tea.Cmd
		next, _ = a.dock.Update(msg)
		start = next
	}
	if len(starter.terms) != 1 || !a.dock.Running() {
		t.Fatalf("A dock did not start: terms=%d running=%v", len(starter.terms), a.dock.Running())
	}
	return starter.terms[0]
}

// routeHostResult feeds one wrapped command result to the host and, when the host
// returns a re-wrapped batch, runs each inner command once and routes its result. The
// follow-up waits the model then arms are intentionally not executed.
func routeHostResult(h *WorkspaceHost, msg tea.Msg) {
	_, next := h.Update(msg)
	if next == nil {
		return
	}
	decoded := next()
	batch, ok := decoded.(tea.BatchMsg)
	if !ok {
		if decoded != nil {
			h.Update(decoded)
		}
		return
	}
	for _, inner := range batch {
		if result := inner(); result != nil {
			h.Update(result)
		}
	}
}

// TestHostRoutesInactiveDockExitToSource checks the PTY exit path: A's dock owns a
// waitEvent command for output/exit. After the host switches to B, that command's exit
// result is still delivered to A's model (not dropped), marks A's shell exited and leaves
// B untouched; warm Show never re-Init'ed A, so dropping this result would leave A's dock
// armed and its PTY/exit channel unread.
func TestHostRoutesInactiveDockExitToSource(t *testing.T) {
	t.Parallel()
	starterA := &shellsStarter{}
	a := newTerminalModel(t, newFakeTree(1), dockKeys(), starterA)
	b := newTerminalModel(t, newFakeTree(1), dockKeys(), &shellsStarter{})
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)

	term := startHiddenDock(t, a, starterA)
	term.exitCode, term.exitOK = 7, true
	close(term.done)
	wait := a.dock.Sync()
	if wait == nil {
		t.Fatal("A's dock Sync armed no wait")
	}
	h := NewWorkspaceHost(b)
	// The Sync command is a wait for the exit; run it once and route the result.
	result := make(chan tea.Msg, 1)
	go func() { result <- wrapModelCmd(ownerA, a, wait)() }()
	var msg tea.Msg
	select {
	case msg = <-result:
	case <-time.After(3 * time.Second):
		t.Fatal("A's exit wait never returned")
	}
	routeHostResult(h, msg)
	if !a.dock.Exited() {
		t.Fatal("A's dock exit was dropped/not applied while B was active")
	}
	if b.dock.Open() || b.dock.Running() || b.dock.Exited() {
		t.Fatal("B's dock changed while routing A's exit")
	}
}

// TestHostRoutesInactiveDockOutputAndRearms checks the output side of the same PTY
// lifecycle: A's armed wait for output must be delivered to hidden A, which then arms
// its next wait (the returned command), never B. A dropped outputMsg would leave
// p.armed set and silently stop A's PTY output chain.
func TestHostRoutesInactiveDockOutputAndRearms(t *testing.T) {
	t.Parallel()
	starterA := &shellsStarter{}
	a := newTerminalModel(t, newFakeTree(1), dockKeys(), starterA)
	b := newTerminalModel(t, newFakeTree(1), dockKeys(), &shellsStarter{})
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)

	term := startHiddenDock(t, a, starterA)
	wait := a.dock.Sync()
	if wait == nil {
		t.Fatal("A's dock Sync armed no wait")
	}
	h := NewWorkspaceHost(b)
	result := make(chan tea.Msg, 1)
	go func() { result <- wrapModelCmd(ownerA, a, wait)() }()
	var msg tea.Msg
	deadline := time.After(3 * time.Second)
	for msg == nil {
		select {
		case msg = <-result:
		case <-deadline:
			t.Fatal("A's output wait never returned")
		case <-time.After(2 * time.Millisecond):
			// The fake PTY has no writer; pump a screen change so the armed wait
			// returns an outputMsg. (An earlier notification may have been consumed.)
			select {
			case term.updates <- struct{}{}:
			default:
			}
		}
	}
	_, next := h.Update(msg)
	if next == nil {
		t.Fatal("A's dock did not arm the next output wait after the hidden output")
	}
	if a.dock.Exited() {
		t.Fatal("an output message marked A exited")
	}
	if b.dock.Open() || b.dock.Running() || b.dock.Exited() {
		t.Fatal("B's dock changed while routing A's output")
	}
}
