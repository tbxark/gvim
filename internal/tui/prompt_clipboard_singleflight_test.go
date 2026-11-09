package tui

import (
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// blockingPromptClipboard is a native writer whose Write waits for release, so a test can
// hold one write in flight and observe whether a second copy/cut reaches the writer.
type blockingPromptClipboard struct {
	started chan string
	release chan struct{}

	mu     sync.Mutex
	active int
	max    int
	writes []string
}

func newBlockingPromptClipboard() *blockingPromptClipboard {
	return &blockingPromptClipboard{started: make(chan string, 8), release: make(chan struct{})}
}

func (c *blockingPromptClipboard) Read() (string, error) { return "", nil }

func (c *blockingPromptClipboard) Write(s string) error {
	c.mu.Lock()
	c.active++
	if c.active > c.max {
		c.max = c.active
	}
	c.writes = append(c.writes, s)
	c.mu.Unlock()
	c.started <- s
	<-c.release
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	return nil
}

func (c *blockingPromptClipboard) snapshot() (max, writes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max, len(c.writes)
}

func firstCutResult(t *testing.T, msgs []tea.Msg) promptCutMsg {
	t.Helper()
	for _, msg := range msgs {
		if m, ok := msg.(promptCutMsg); ok {
			return m
		}
	}
	t.Fatal("no promptCutMsg in the command results")
	return promptCutMsg{}
}

func firstCopyResult(t *testing.T, msgs []tea.Msg) promptCopyMsg {
	t.Helper()
	for _, msg := range msgs {
		if m, ok := msg.(promptCopyMsg); ok {
			return m
		}
	}
	t.Fatal("no promptCopyMsg in the command results")
	return promptCopyMsg{}
}

// runCmdAsync runs a model command as Bubble Tea would (off Update) and returns its result
// messages on a channel.
func runCmdAsync(cmd tea.Cmd) <-chan []tea.Msg {
	done := make(chan []tea.Msg, 1)
	go func() { done <- collect(cmd) }()
	return done
}

// TestPromptClipboardSingleFlight: while a native write is pending, a second copy or cut
// is refused with a notice and never reaches the writer, so writes cannot overlap or land
// out of order. The first result still applies when it arrives.
func TestPromptClipboardSingleFlight(t *testing.T) {
	keys := fakeKeymap{
		"prompt|x|ctrl": {Action: "prompt_cut"},
		"prompt|c|ctrl": {Action: "prompt_copy"},
	}
	tests := []struct {
		name    string
		first   tea.KeyPressMsg
		second  tea.KeyPressMsg
		cutDone bool
	}{
		{name: "cut then cut", first: ctrl('x'), second: ctrl('x'), cutDone: true},
		{name: "cut then copy", first: ctrl('x'), second: ctrl('c'), cutDone: true},
		{name: "copy then cut", first: ctrl('c'), second: ctrl('x')},
		{name: "copy then copy", first: ctrl('c'), second: ctrl('c')},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := newBlockingPromptClipboard()
			m := overlayModel(t, keys)
			m.SetDock(DockPorts{Clipboard: cb})
			p := pushPrompt(m, "one")
			_, cmd := m.Update(tt.first)
			if cmd == nil {
				t.Fatal("the first operation started no command")
			}
			done := runCmdAsync(cmd)
			<-cb.started
			_, second := m.Update(tt.second)
			if second != nil {
				t.Fatal("a second clipboard operation started while the first write was pending")
			}
			if !strings.Contains(m.notice, "still in progress") {
				t.Fatalf("busy notice = %q", m.notice)
			}
			if max, writes := cb.snapshot(); max != 1 || writes != 1 {
				t.Fatalf("writer max concurrency %d writes %d, want one at a time", max, writes)
			}
			close(cb.release)
			msgs := <-done
			if tt.cutDone {
				m.Update(firstCutResult(t, msgs))
				if got := p.Value(); got != "" {
					t.Fatalf("completed cut left %q", got)
				}
			} else {
				m.Update(firstCopyResult(t, msgs))
				if got := p.Value(); got != "one" {
					t.Fatalf("copy changed the prompt to %q", got)
				}
			}
		})
	}
}

func stringMessages(msgs []tea.Msg) []string {
	var out []string
	for _, msg := range msgs {
		if s, ok := msg.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// TestPromptClipboardUsesOneChannel: when a native writer exists the prompt uses only the
// native path (no OSC 52 in the same command, so the two cannot reorder); without a
// native writer it uses only the OSC 52 request and reports that it is unconfirmed.
func TestPromptClipboardUsesOneChannel(t *testing.T) {
	keys := fakeKeymap{
		"prompt|x|ctrl": {Action: "prompt_cut"},
		"prompt|c|ctrl": {Action: "prompt_copy"},
	}
	t.Run("native cut", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, keys)
		m.SetDock(DockPorts{Clipboard: cb})
		p := pushPrompt(m, "one")
		_, cmd := m.Update(ctrl('x'))
		msgs := collect(cmd)
		if got := stringMessages(msgs); len(got) != 0 {
			t.Fatalf("native cut also sent OSC 52: %q", got)
		}
		if len(cb.writes) != 1 || cb.writes[0] != "one" {
			t.Fatalf("native writes = %q", cb.writes)
		}
		m.Update(firstCutResult(t, msgs))
		if got := p.Value(); got != "" {
			t.Fatalf("native cut did not clear: %q", got)
		}
	})
	t.Run("native copy", func(t *testing.T) {
		cb := &promptCutClipboard{}
		m := overlayModel(t, keys)
		m.SetDock(DockPorts{Clipboard: cb})
		p := pushPrompt(m, "one")
		_, cmd := m.Update(ctrl('c'))
		msgs := collect(cmd)
		if got := stringMessages(msgs); len(got) != 0 {
			t.Fatalf("native copy also sent OSC 52: %q", got)
		}
		if len(cb.writes) != 1 || cb.writes[0] != "one" {
			t.Fatalf("native writes = %q", cb.writes)
		}
		m.Update(firstCopyResult(t, msgs))
		if got := p.Value(); got != "one" {
			t.Fatalf("native copy changed the prompt: %q", got)
		}
	})
	t.Run("no writer cut", func(t *testing.T) {
		m := overlayModel(t, keys)
		m.SetDock(DockPorts{Clipboard: fakeClipboard{}})
		p := pushPrompt(m, "one")
		_, cmd := m.Update(ctrl('x'))
		msgs := collect(cmd)
		if got := clipboardWrites(cmd); len(got) != 1 || got[0] != "one" {
			t.Fatalf("OSC 52 writes = %q", got)
		}
		for _, msg := range msgs {
			if _, ok := msg.(promptCutMsg); ok {
				t.Fatal("no-writer cut produced a native result")
			}
		}
		if got := p.Value(); got != "one" {
			t.Fatalf("unconfirmed cut lost the prompt: %q", got)
		}
	})
	t.Run("no writer copy", func(t *testing.T) {
		m := overlayModel(t, keys)
		m.SetDock(DockPorts{Clipboard: fakeClipboard{}})
		p := pushPrompt(m, "one")
		_, cmd := m.Update(ctrl('c'))
		msgs := collect(cmd)
		if got := clipboardWrites(cmd); len(got) != 1 || got[0] != "one" {
			t.Fatalf("OSC 52 writes = %q", got)
		}
		for _, msg := range msgs {
			if _, ok := msg.(promptCopyMsg); ok {
				t.Fatal("no-writer copy produced a native result")
			}
		}
		if got := p.Value(); got != "one" {
			t.Fatalf("unconfirmed copy changed the prompt: %q", got)
		}
	})
}

// TestPromptClipboardCrossModelWriterRejected: two workspace models share the app's
// system clipboard permission. While A's native write is in flight, B's copy/cut is
// refused instead of queued; after A's canceled write reports, B can start its own write.
// There is never more than one writer and no unbounded waiting queue.
func TestPromptClipboardCrossModelWriterRejected(t *testing.T) {
	keys := fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}}
	cb := newBlockingPromptClipboard()
	released := false
	defer func() {
		if !released {
			close(cb.release)
		}
	}()
	a := overlayModel(t, keys)
	a.SetDock(DockPorts{Clipboard: cb})
	pa := pushPrompt(a, "old")
	_, oldCmd := a.Update(ctrl('x'))
	if oldCmd == nil {
		t.Fatal("the old model's cut started no command")
	}
	oldDone := runCmdAsync(oldCmd)
	<-cb.started
	a.CancelTransientWork()

	b := overlayModel(t, keys)
	b.SetDock(DockPorts{Clipboard: cb})
	pb := pushPrompt(b, "new")
	_, refused := b.Update(ctrl('x'))
	if refused != nil {
		t.Fatal("the new model's cut was queued behind the old native write")
	}
	if !strings.Contains(b.notice, "still in progress") {
		t.Fatalf("new model busy notice = %q", b.notice)
	}
	if max, writes := cb.snapshot(); max != 1 || writes != 1 {
		t.Fatalf("writer max %d writes %d while old write in flight", max, writes)
	}

	close(cb.release)
	released = true
	a.Update(firstCutResult(t, <-oldDone))
	if got := pa.Value(); got != "old" {
		t.Fatalf("canceled old-model cut cleared its prompt: %q", got)
	}

	// A's result released the process-wide permission; B's next attempt starts and runs.
	_, next := b.Update(ctrl('x'))
	if next == nil {
		t.Fatal("the new model's cut did not start after the old write reported")
	}
	b.Update(firstCutResult(t, collect(next)))
	if got := pb.Value(); got != "" {
		t.Fatalf("new model's cut did not clear its prompt: %q", got)
	}
	if max, writes := cb.snapshot(); max != 1 || writes != 2 {
		t.Fatalf("writer max %d writes %d, want two sequential writes", max, writes)
	}
}

// TestPromptClipboardCancelBeforeCommandSkipsWrite: permission is acquired when the
// operation starts, but a cancellation that lands before the command runs makes the command
// check the operation and skip ClipboardWriter.Write entirely. The skipped result still
// releases the process-wide permission, so the next cut can run.
func TestPromptClipboardCancelBeforeCommandSkipsWrite(t *testing.T) {
	keys := fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}}
	cb := &promptCutClipboard{}
	m := overlayModel(t, keys)
	m.SetDock(DockPorts{Clipboard: cb})
	p := pushPrompt(m, "one")
	_, cmd := m.Update(ctrl('x'))
	if cmd == nil {
		t.Fatal("the cut started no command")
	}
	m.CancelTransientWork()
	m.notice = "marker"
	msgs := collect(cmd)
	if len(cb.writes) != 0 {
		t.Fatalf("canceled-before-start command wrote %q", cb.writes)
	}
	msg := firstCutResult(t, msgs)
	if !msg.skipped {
		t.Fatalf("canceled-before-start result = %+v, want skipped", msg)
	}
	m.Update(msg)
	if got := p.Value(); got != "one" {
		t.Fatalf("skipped cut changed the prompt: %q", got)
	}
	if m.notice != "marker" {
		t.Fatalf("skipped cut changed the notice: %q", m.notice)
	}
	// The skipped result released the gate: a fresh cut starts and writes once.
	_, next := m.Update(ctrl('x'))
	if next == nil {
		t.Fatal("a cut after the skipped one did not start")
	}
	m.Update(firstCutResult(t, collect(next)))
	if got := p.Value(); got != "" {
		t.Fatalf("fresh cut did not clear: %q", got)
	}
	if len(cb.writes) != 1 || cb.writes[0] != "one" {
		t.Fatalf("native writes after skip = %q", cb.writes)
	}
}

// TestPromptClipboardOwnerChangeSkipsWrite: a workspace owner generation change cancels
// a not-yet-started native request just like CancelTransientWork; the command checks the
// operation before Write, so the stale generation never reaches the clipboard.
func TestPromptClipboardOwnerChangeSkipsWrite(t *testing.T) {
	keys := fakeKeymap{"prompt|x|ctrl": {Action: "prompt_cut"}}
	cb := &promptCutClipboard{}
	m := overlayModel(t, keys)
	m.SetDock(DockPorts{Clipboard: cb})
	m.SetWorkspaceOwner("ws", 1)
	p := pushPrompt(m, "old generation")
	_, cmd := m.Update(ctrl('x'))
	if cmd == nil {
		t.Fatal("the cut started no command")
	}
	m.notice = "marker"
	m.SetWorkspaceOwner("ws", 2)
	msgs := collect(cmd)
	if len(cb.writes) != 0 {
		t.Fatalf("stale-generation command wrote %q", cb.writes)
	}
	msg := firstCutResult(t, msgs)
	if !msg.skipped {
		t.Fatalf("stale-generation result = %+v, want skipped", msg)
	}
	m.Update(msg)
	if got := p.Value(); got != "old generation" {
		t.Fatalf("stale-generation cut changed the prompt: %q", got)
	}
	if m.notice != "marker" {
		t.Fatalf("stale-generation cut changed the notice: %q", m.notice)
	}
}

// TestPromptClipboardCancelWaitsForInFlightWrite: CancelTransientWork invalidates the
// hidden prompt's late cut, but the single-flight gate stays set until the old write
// reports. A new copy/cut is refused meanwhile, so the old and new writes cannot run
// together; once the old result clears the gate, the next operation proceeds.
func TestPromptClipboardCancelWaitsForInFlightWrite(t *testing.T) {
	keys := fakeKeymap{
		"prompt|x|ctrl": {Action: "prompt_cut"},
		"prompt|c|ctrl": {Action: "prompt_copy"},
	}
	cb := newBlockingPromptClipboard()
	m := overlayModel(t, keys)
	m.SetDock(DockPorts{Clipboard: cb})
	p := pushPrompt(m, "one")
	_, cmd := m.Update(ctrl('x'))
	if cmd == nil {
		t.Fatal("the cut started no command")
	}
	done := runCmdAsync(cmd)
	<-cb.started

	m.CancelTransientWork()
	_, refused := m.Update(ctrl('c'))
	if refused != nil {
		t.Fatal("a copy started while the canceled write was still in flight")
	}
	if !strings.Contains(m.notice, "still in progress") {
		t.Fatalf("busy notice after cancel = %q", m.notice)
	}
	if max, writes := cb.snapshot(); max != 1 || writes != 1 {
		t.Fatalf("writer max %d writes %d after cancel, want one in flight", max, writes)
	}

	close(cb.release)
	msgs := <-done
	m.Update(firstCutResult(t, msgs))
	if got := p.Value(); got != "one" {
		t.Fatalf("canceled late cut cleared the prompt: %q", got)
	}
	if m.notice == "Cut" {
		t.Fatal("canceled late cut reported Cut")
	}

	// The old command reported, so the single-flight gate is free again.
	_, next := m.Update(ctrl('x'))
	if next == nil {
		t.Fatal("the next cut did not start after the old write completed")
	}
	m.Update(firstCutResult(t, collect(next)))
	if got := p.Value(); got != "" {
		t.Fatalf("the next cut did not clear: %q", got)
	}
	if max, writes := cb.snapshot(); max != 1 || writes != 2 {
		t.Fatalf("writer max %d writes %d, want two sequential writes", max, writes)
	}
}
