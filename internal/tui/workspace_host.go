package tui

// WorkspaceHost is the daemon's #19a program model: one Bubble Tea program hosts several
// workspace models and swaps the active one without restarting the program. The app owns
// the workspace lifecycle; it calls Show from the UI goroutine and Do to marshal work from
// other goroutines (the daemon's socket handlers) onto the UI goroutine.

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// hostCallMsg runs fn on the program's UI goroutine and closes done after it returns.
type hostCallMsg struct {
	fn   func()
	done chan struct{}
}

// WorkspaceHost implements tea.Model over the active workspace model.
type WorkspaceHost struct {
	mu      sync.Mutex
	active  *Model
	program *tea.Program
	// pendingCmd is batched into the next returned command: the first Init of a model
	// shown since the last Update, and a window-size request so a swapped-in workspace
	// repaints at the client's size. A warm model is not Init'ed again: its runtime and
	// state continue where the switch left them.
	pendingCmd tea.Cmd
	// initialized marks the models whose Init already ran (the first is the program's own
	// Init; every later one is started by Show).
	initialized map[*Model]bool
}

// NewWorkspaceHost returns a host over active.
func NewWorkspaceHost(active *Model) *WorkspaceHost {
	return &WorkspaceHost{active: active, initialized: map[*Model]bool{active: true}}
}

// Active is the model the program currently shows.
func (h *WorkspaceHost) Active() *Model {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}

// Show makes m the active workspace model and arranges its Init to run on the next
// update. It must be called on the program's UI goroutine (Update, or inside Do).
func (h *WorkspaceHost) Show(m *Model) {
	if m == nil {
		return
	}
	cmd := func() tea.Msg { return tea.RequestWindowSize() }
	h.mu.Lock()
	h.active = m
	if !h.initialized[m] {
		h.initialized[m] = true
		cmd = tea.Batch(m.Init(), cmd)
	}
	h.pendingCmd = cmd
	h.mu.Unlock()
}

// Ready reports whether the program exists (RunWorkspaceHost has set it). Before that,
// the app's workspace calls run inline: no program means no concurrent update loop.
func (h *WorkspaceHost) Ready() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.program != nil
}

// Do runs fn on the program's UI goroutine and waits for it. It reports false when the
// program is not running yet or does not answer within the timeout, so a socket handler
// never blocks the daemon forever.
func (h *WorkspaceHost) Do(fn func()) bool {
	h.mu.Lock()
	p := h.program
	h.mu.Unlock()
	if p == nil {
		return false
	}
	done := make(chan struct{})
	p.Send(hostCallMsg{fn: fn, done: done})
	select {
	case <-done:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

// Queue adds cmd to the batch the next Update returns (for work that must run after a
// model switch, such as starting the imported session's terminal in the new dock).
func (h *WorkspaceHost) Queue(cmd WorkspaceCmd) {
	if cmd == nil {
		return
	}
	h.mu.Lock()
	h.pendingCmd = tea.Batch(h.pendingCmd, cmd)
	h.mu.Unlock()
}

// Notify shows a notice on the active workspace's status row. The app calls it for the
// outcome of asynchronous workspace work.
func (h *WorkspaceHost) Notify(text string) {
	if text == "" {
		return
	}
	h.Do(func() { h.Active().Notify(text) })
}

// PostOpenFile sends a client's open-file request to the active model through the program
// queue (call it after Show, so the request lands in the workspace that took the file).
func (h *WorkspaceHost) PostOpenFile(path string) {
	h.mu.Lock()
	p := h.program
	h.mu.Unlock()
	if p != nil {
		p.Send(OpenFileMessage(path))
	}
}

// stop marks the program as over, so late asynchronous work (a capture write finishing
// after quit) does not block on the dead message queue.
func (h *WorkspaceHost) stop() {
	h.mu.Lock()
	h.program = nil
	h.mu.Unlock()
}

// setProgram is called by RunWorkspaceHost before the program starts.
func (h *WorkspaceHost) setProgram(p *tea.Program) {
	h.mu.Lock()
	h.program = p
	h.mu.Unlock()
}

// Init implements tea.Model.
func (h *WorkspaceHost) Init() tea.Cmd { return h.Active().Init() }

// Update implements tea.Model: host messages run here, everything else goes to the active
// workspace model. A model shown during this update starts with its Init.
func (h *WorkspaceHost) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case hostCallMsg:
		msg.fn()
		close(msg.done)
	default:
		next, c := h.Active().Update(msg)
		if m, ok := next.(*Model); ok {
			h.mu.Lock()
			h.active = m
			h.mu.Unlock()
		}
		cmd = c
	}
	h.mu.Lock()
	pending := h.pendingCmd
	h.pendingCmd = nil
	h.mu.Unlock()
	if pending != nil {
		return h, tea.Batch(pending, cmd)
	}
	return h, cmd
}

// View implements tea.Model.
func (h *WorkspaceHost) View() tea.View { return h.Active().View() }
