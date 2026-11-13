package tui

import (
	"reflect"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/theme"
)

// TestHostRoutesHiddenModelCommandToSource: a command result from a hidden warm model
// must be delivered to that model, not dropped: dock PTY output/exit and LSP lifecycle
// waits depend on it. It must not touch the active model.
func TestHostRoutesHiddenModelCommandToSource(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.Show(b)

	open := func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} }
	h.Update(wrapModelCmd(ownerA, a, open)())
	if len(a.overlays) != 1 {
		t.Fatalf("hidden source A got %d overlay(s), want its command applied", len(a.overlays))
	}
	if len(b.overlays) != 0 {
		t.Fatalf("stale command from A opened %d overlay(s) in active B", len(b.overlays))
	}
	h.Update(wrapModelCmd(ownerB, b, open)())
	if len(b.overlays) == 0 {
		t.Fatal("the active model's command was dropped")
	}
}

// TestHostHiddenWorkspaceLateCutCannotClearWarmPrompt: a cut starts on workspace A,
// A is hidden for B and CancelTransientWork runs (as the workspace manager does), then
// the late result is delivered to the hidden warm model and A is shown again. The token
// invalidation must keep the still-active prompt and the notice untouched; the host's
// source-routing alone is not enough.
func TestHostHiddenWorkspaceLateCutCannotClearWarmPrompt(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	cb := &promptCutClipboard{}
	a.SetDock(DockPorts{Clipboard: cb})
	a.keys = fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}}
	p := pushPrompt(a, "hidden")
	h := NewWorkspaceHost(a)
	_, cmd := h.Update(ctrl('x'))
	if cmd == nil {
		t.Fatal("the cut started no command")
	}
	// A -> B: A is warm/hidden and its transient work is canceled like the manager does.
	h.Show(b)
	a.CancelTransientWork()
	a.notice = "marker"
	// The late result is delivered to the hidden source A; then A is shown again.
	pumpHostCmd(t, h, cmd)
	h.Show(a)
	if got := p.Value(); got != "hidden" {
		t.Fatalf("late cut from hidden workspace cleared the prompt: %q", got)
	}
	if a.notice != "marker" {
		t.Fatalf("late cut from hidden workspace polluted the notice: %q", a.notice)
	}
}

// TestHostRewrapsBatchCommandResults: tea.Batch results are re-wrapped per command after
// the outer envelope is opened, so every inner async result keeps the same source model
// and can be routed back to it.
func TestHostRewrapsBatchCommandResults(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.Show(b)

	batch := wrapModelCmd(ownerA, a, tea.Batch(
		func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} },
		func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} },
	))
	_, replay := h.Update(batch())
	if replay == nil {
		t.Fatal("a wrapped batch produced no replay command")
	}
	msg := replay()
	batchMsg, ok := msg.(tea.BatchMsg)
	if !ok || len(batchMsg) != 2 {
		t.Fatalf("replay returned %T with %d commands, want tea.BatchMsg with 2", msg, len(batchMsg))
	}
	for _, inner := range batchMsg {
		result := inner()
		owned, ok := result.(modelCmdMsg)
		if !ok {
			t.Fatalf("batch subcommand returned %T, want modelCmdMsg", result)
		}
		if owned.owner != ownerA || owned.source != a {
			t.Fatalf("batch subcommand owner/source = %+v/%p, want %+v/%p", owned.owner, owned.source, ownerA, a)
		}
		h.Update(result)
	}
	if len(a.overlays) == 0 {
		t.Fatal("hidden source A did not receive its batch results")
	}
	if len(b.overlays) != 0 {
		t.Fatalf("B received A's batch results: %d overlay(s)", len(b.overlays))
	}
}

// TestHostDropsClosedModelCommand: after eviction/close the model's runtime is gone, so a
// late result must be discarded instead of keeping the closed model/PTY state alive.
func TestHostDropsClosedModelCommand(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.Show(b)
	a.Close()

	h.Update(wrapModelCmd(ownerA, a, func() tea.Msg {
		return palette.RunCommandMsg{Action: "open_settings"}
	})())
	if len(a.overlays) != 0 {
		t.Fatalf("a closed model applied a late command: %d overlay(s)", len(a.overlays))
	}
	if len(b.overlays) != 0 {
		t.Fatalf("the active model received a closed model's command: %d overlay(s)", len(b.overlays))
	}
}

// TestHostEnvelopeFromRealPaletteCommand: the host wraps a command a real workspace model
// returns from a key press; the result is source/owner-tagged and stays in that model
// after another model becomes active.
func TestHostEnvelopeFromRealPaletteCommand(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b, _ := newLiveModel(t, g)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(b)

	if _, cmd := h.Update(palette.RunCommandMsg{Action: "open_live_grep"}); cmd != nil {
		if msg, ok := cmd().(modelCmdMsg); ok && (msg.source != b || msg.owner != ownerB) {
			t.Fatalf("open_live_grep envelope = %+v source %p, want B", msg, msg.source)
		}
	}
	_, cmd := h.Update(typed("t"))
	if cmd == nil {
		t.Fatal("typing in the palette started no search command")
	}
	msg, ok := cmd().(modelCmdMsg)
	if !ok || msg.source != b || msg.owner != ownerB {
		t.Fatalf("real palette command returned %#v, want modelCmdMsg source B", msg)
	}

	h.Show(a)
	h.Update(msg)
	if len(a.overlays) != 0 {
		t.Fatalf("B's search command opened %d overlay(s) in A", len(a.overlays))
	}
	if len(b.overlays) != 1 {
		t.Fatalf("B's own palette disappeared after routing its result: %d overlay(s)", len(b.overlays))
	}
}

// TestHostProgramMessagesReachProgram: ClearScreen/Quit/SetClipboard results belong to
// Bubble Tea's Program, not Model.Update. An active model's wrapped command must hand them
// back untouched; a hidden model's program effect must not act on the program.
func TestHostProgramMessagesReachProgram(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.Show(b)

	tests := []struct {
		name string
		cmd  tea.Cmd
		typ  reflect.Type
	}{
		{"clear screen", func() tea.Msg { return tea.ClearScreen() }, reflect.TypeOf(tea.ClearScreen())},
		{"quit", func() tea.Msg { return tea.Quit() }, reflect.TypeOf(tea.Quit())},
		{"clipboard", tea.SetClipboard("x"), reflect.TypeOf(tea.SetClipboard("")())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hidden := tagForTest(ownerA, a, tt.cmd)
			if _, got := h.Update(hidden); got != nil {
				t.Fatalf("hidden model program message produced a command")
			}
			active := tagForTest(ownerB, b, tt.cmd)
			_, replay := h.Update(active)
			if replay == nil {
				t.Fatal("active model program message was dropped")
			}
			msg := replay()
			if reflect.TypeOf(msg) != tt.typ {
				t.Fatalf("program received %T, want %v", msg, tt.typ)
			}
		})
	}
}

// tagForTest wraps cmd with source/owner as the host does around a model command.
func tagForTest(owner palette.Owner, source *Model, cmd tea.Cmd) tea.Msg {
	return wrapModelCmd(owner, source, cmd)()
}

// TestHostProgramMessagesInBatch: a Batch containing program-level commands stays wrapped
// per subcommand and each result still reaches the Program unchanged.
func TestHostProgramMessagesInBatch(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.Show(b)

	batch := wrapModelCmd(ownerB, b, tea.Batch(
		func() tea.Msg { return tea.ClearScreen() },
		tea.SetClipboard("x"),
	))
	_, replay := h.Update(batch())
	if replay == nil {
		t.Fatal("wrapped program batch produced no replay")
	}
	msg := replay()
	batchMsg, ok := msg.(tea.BatchMsg)
	if !ok || len(batchMsg) != 2 {
		t.Fatalf("replay = %T with %d commands, want tea.BatchMsg with 2", msg, len(batchMsg))
	}
	var types []reflect.Type
	for _, inner := range batchMsg {
		_, cmd := h.Update(inner())
		if cmd == nil {
			t.Fatal("a program command inside the batch was dropped")
		}
		types = append(types, reflect.TypeOf(cmd()))
	}
	if len(types) != 2 || types[0] != reflect.TypeOf(tea.ClearScreen()) || types[1] != reflect.TypeOf(tea.SetClipboard("")()) {
		t.Fatalf("program batch result types = %v", types)
	}
}

// pumpHostCmd executes cmd and feeds every message (including batches and envelopes) back
// to the host until it asks for no more work.
func pumpHostCmd(t *testing.T, h *WorkspaceHost, cmd tea.Cmd) {
	t.Helper()
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		switch m := msg.(type) {
		case tea.BatchMsg:
			for _, sub := range m {
				walk(sub)
			}
		default:
			_, next := h.Update(msg)
			walk(next)
		}
	}
	walk(cmd)
}

// TestHostQueueForBindsTheHiddenSource: work queued for a workspace model while another
// is active carries that model as its source; its result is delivered to the queued
// model, not to the active one.
func TestHostQueueForBindsTheHiddenSource(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.initialized[b] = true // do not run B's Init in this unit test
	h.Show(b)

	h.QueueFor(a, func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} })
	_, cmd := h.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	pumpHostCmd(t, h, cmd)
	if len(a.overlays) != 1 {
		t.Fatalf("queued A work got %d overlay(s), want 1", len(a.overlays))
	}
	if len(b.overlays) != 0 {
		t.Fatalf("queued A work landed in active B: %d overlay(s)", len(b.overlays))
	}
}

// TestHostMultipleShowAndQueueInOneUpdate: several Show/Queue calls in one UI callback keep
// each command's own source; the last Show is active and no queued command is rebound to
// it.
func TestHostMultipleShowAndQueueInOneUpdate(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.initialized[a] = true
	h.initialized[b] = true
	open := func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} }

	_, cmd := h.Update(hostCallMsg{fn: func() {
		h.Show(a)
		h.QueueFor(a, open)
		h.Show(b)
		h.QueueFor(b, open)
	}, state: newHostCallState()})
	pumpHostCmd(t, h, cmd)
	if h.Active() != b {
		t.Fatal("the last Show did not become active")
	}
	if len(a.overlays) != 1 || len(b.overlays) != 1 {
		t.Fatalf("queued command overlays A/B = %d/%d, want 1/1", len(a.overlays), len(b.overlays))
	}
}

// TestHostPostOpenIsBoundToTheSelectedModel: an open-file request posted for A after a
// switch to B must open in A, never in the newly active B.
func TestHostPostOpenIsBoundToTheSelectedModel(t *testing.T) {
	t.Parallel()
	edA := &lineEditor{fakeEditor: newFakeEditor("a")}
	edB := &lineEditor{fakeEditor: newFakeEditor("b")}
	a := New(Deps{Editor: edA, Explorer: newFakeTree(1), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	b := New(Deps{Editor: edB, Explorer: newFakeTree(1), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	b.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(b)

	h.Update(openFileAtMsg{path: "/ws/a.txt", line: 3, col: 2, source: a})
	if len(edA.opened) != 1 || edA.opened[0] != "/ws/a.txt" {
		t.Fatalf("A opened %v, want /ws/a.txt", edA.opened)
	}
	if !slices.Equal(edA.gotos[0][:], []int{3, 2}) {
		t.Fatalf("A goto = %v, want line 3 col 2", edA.gotos)
	}
	if len(edB.opened) != 0 {
		t.Fatalf("the active B opened A's file: %v", edB.opened)
	}
}

// TestHostClosedSourceQueuedCommandDoesNotRun: a queued command for a model that is
// marked closed before execution must not run (wrapModelCmd checks Closed before cmd()).
// This is the post-teardown side of the pending/Init path.
func TestHostClosedSourceQueuedCommandDoesNotRun(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.initialized[b] = true
	h.Show(b)

	ran := false
	h.QueueFor(a, func() tea.Msg {
		ran = true
		return palette.RunCommandMsg{Action: "open_settings"}
	})
	a.MarkClosed()
	_, cmd := h.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	pumpHostCmd(t, h, cmd)
	if ran {
		t.Fatal("a closed model's queued command still ran")
	}
	if len(a.overlays) != 0 || len(b.overlays) != 0 {
		t.Fatalf("closed queued command painted A/B = %d/%d", len(a.overlays), len(b.overlays))
	}
}

// TestWrapModelCmdClosedSourceDoesNotExecute: the closed-source check is inside the
// wrapper before cmd(), so both a generic queued command and an open-file command that
// were created before MarkClosed cannot run/model-open afterwards.
func TestWrapModelCmdClosedSourceDoesNotExecute(t *testing.T) {
	t.Parallel()
	owner := palette.Owner{Workspace: "ws-a", Generation: 1}
	ed := &lineEditor{fakeEditor: newFakeEditor("a")}
	m := New(Deps{Editor: ed, Explorer: newFakeTree(1), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.SetWorkspaceOwner(owner.Workspace, owner.Generation)

	ran := false
	wrapped := wrapModelCmd(owner, m, func() tea.Msg {
		ran = true
		return palette.RunCommandMsg{Action: "open_settings"}
	})
	opened := false
	open := wrapModelCmd(owner, m, func() tea.Msg {
		opened = true
		return palette.RunCommandMsg{Action: "open_settings"}
	})
	m.MarkClosed()

	if msg := wrapped(); msg != nil || ran {
		t.Fatalf("closed generic command ran: msg %#v ran %v", msg, ran)
	}
	if msg := open(); msg != nil || opened {
		t.Fatalf("closed open-file-like command ran: msg %#v opened %v", msg, opened)
	}
	_ = ed
}

// TestWindowReplayIsBoundToShownModel: the size replay generated after Show belongs to the
// model that was shown, not to a model that becomes active before the command runs.
func TestWindowReplayIsBoundToShownModel(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	b := newTestModel(t, newFakeEditor("b"), newFakeTree(1), 80, 24)
	ownerA := palette.Owner{Workspace: "ws-a", Generation: 1}
	ownerB := palette.Owner{Workspace: "ws-b", Generation: 2}
	a.SetWorkspaceOwner(ownerA.Workspace, ownerA.Generation)
	b.SetWorkspaceOwner(ownerB.Workspace, ownerB.Generation)
	h := NewWorkspaceHost(a)
	h.initialized[b] = true // isolate the replayed size command
	h.Show(b)
	_, cmd := h.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd == nil {
		t.Fatal("Show(B) produced no replay")
	}
	// Switch before the replay command runs: a wrong (untagged) replay would size A.
	h.Show(a)
	pumpHostCmd(t, h, cmd)
	if b.width != 100 || b.height != 30 {
		t.Fatalf("shown B size = %dx%d, want 100x30", b.width, b.height)
	}
	if a.width == 100 || a.height == 30 {
		t.Fatalf("the replay sized the newly active A: %dx%d", a.width, a.height)
	}
}

// TestHostCallTimeoutDiscardsQueuedWork: a Do call that times out before the UI claims it
// marks the state canceled; the queued hostCallMsg is then a no-op instead of running a
// late MarkClosed/quiesce closure.
func TestHostCallTimeoutDiscardsQueuedWork(t *testing.T) {
	t.Parallel()
	h := NewWorkspaceHost(newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24))
	state := newHostCallState()
	ran := false
	msg := hostCallMsg{fn: func() { ran = true }, state: state}
	if started := state.timeout(); started {
		t.Fatal("timeout reported a started call")
	}
	h.Update(msg)
	if ran {
		t.Fatal("a timed-out queued host call executed later")
	}
	select {
	case <-state.done:
	default:
		t.Fatal("the discarded call did not close its done channel")
	}
}

// TestHostCallTimeoutWaitsForStartedCall: once the UI has claimed a call, Do's timeout path
// must wait for completion instead of returning while the closure mutates model state.
func TestHostCallTimeoutWaitsForStartedCall(t *testing.T) {
	t.Parallel()
	state := newHostCallState()
	if !state.tryStart() {
		t.Fatal("the first claim was refused")
	}
	if started := state.timeout(); !started {
		t.Fatal("timeout did not report the started call")
	}
	state.finish()
	select {
	case <-state.done:
	default:
		t.Fatal("finished call is not done")
	}
}

// TestPostOpenFileAtBoundedWhenQueueFull: the open-file path goes through the same bounded
// outbound queue as Do; with the queue full and no sender progress it fails within the
// timeout instead of blocking a socket goroutine in a direct p.Send.
func TestPostOpenFileAtBoundedWhenQueueFull(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	h := NewWorkspaceHost(a)
	for i := 0; i < hostOutboundQueue; i++ {
		h.outbound <- modelCmdMsg{}
	}
	start := time.Now()
	if h.PostOpenFileAtWithTimeout(a, "/ws/a.txt", 1, 1, 25*time.Millisecond) {
		t.Fatal("an open was accepted into a full host queue")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("PostOpenFileAt blocked for %v", elapsed)
	}
}

// TestPostOpenFileAtWithoutProgramIsRejected: before the host has a program there is no
// sender; PostOpenFileAt must return false and leave the queue empty instead of reporting
// a delivery that will never happen.
func TestPostOpenFileAtWithoutProgramIsRejected(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	h := NewWorkspaceHost(a)
	if h.PostOpenFileAt(a, "/ws/a.txt", 1, 1) {
		t.Fatal("PostOpenFileAt accepted an open with no program")
	}
	if len(h.outbound) != 0 {
		t.Fatalf("post-open queued %d item(s) with no program", len(h.outbound))
	}
}

// TestHostCallFinishIsDeferred: a panicking UI callback must still close the host call's
// done channel, so Do cannot wait forever and a later call is not blocked.
func TestHostCallFinishIsDeferred(t *testing.T) {
	t.Parallel()
	h := NewWorkspaceHost(newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24))
	state := newHostCallState()
	func() {
		defer func() { _ = recover() }()
		h.Update(hostCallMsg{fn: func() { panic("boom") }, state: state})
	}()
	select {
	case <-state.done:
	default:
		t.Fatal("panicking host call did not finish its done channel")
	}
}

// TestQueueForRejectsClosedSource: a closed model must not accumulate queued work.
func TestQueueForRejectsClosedSource(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	h := NewWorkspaceHost(a)
	a.MarkClosed()
	if h.QueueFor(a, func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} }) {
		t.Fatal("queued work for a closed source")
	}
}

// TestQueueForRejectsStoppedHost: after stop, QueueFor must not append to pending work.
func TestQueueForRejectsStoppedHost(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	h := NewWorkspaceHost(a)
	h.stop()
	if h.QueueFor(a, func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} }) {
		t.Fatal("queued work on a stopped host")
	}
	h.mu.Lock()
	pending := h.pendingCmd
	count := h.pendingCount
	h.mu.Unlock()
	if pending != nil || count != 0 {
		t.Fatal("stopped host kept pending work")
	}
}

// TestQueueForIsBounded: queued work is capped so a backed-up/never-drained UI cannot grow
// pendingCmd without limit; accepted commands stop at hostPendingCap.
func TestQueueForIsBounded(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	h := NewWorkspaceHost(a)
	cmd := func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} }
	for i := 0; i < hostPendingCap; i++ {
		if !h.QueueFor(a, cmd) {
			t.Fatalf("queue rejected command %d before the cap", i)
		}
	}
	if h.QueueFor(a, cmd) {
		t.Fatal("queue accepted work past hostPendingCap")
	}
	h.mu.Lock()
	count := h.pendingCount
	h.mu.Unlock()
	if count != hostPendingCap {
		t.Fatalf("pending count = %d, want %d", count, hostPendingCap)
	}
}

// TestHostQueueWithoutShowStillRuns: work queued in a host call that shows no other
// workspace belongs to the next Update's batch, as Queue documents, instead of being dropped.
func TestHostQueueWithoutShowStillRuns(t *testing.T) {
	t.Parallel()
	a := newTestModel(t, newFakeEditor("a"), newFakeTree(1), 80, 24)
	a.SetWorkspaceOwner("ws-a", 1)
	h := NewWorkspaceHost(a)
	open := func() tea.Msg { return palette.RunCommandMsg{Action: "open_settings"} }

	_, cmd := h.Update(hostCallMsg{fn: func() { h.QueueFor(a, open) }, state: newHostCallState()})
	pumpHostCmd(t, h, cmd)
	if len(a.overlays) != 1 {
		t.Fatalf("queued work without a Show ran %d time(s), want 1", len(a.overlays))
	}
}
