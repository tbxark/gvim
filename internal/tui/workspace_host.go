package tui

// WorkspaceHost is the daemon's program model: one Bubble Tea program hosts several
// workspace models and swaps the active one without restarting the program. The app owns
// the workspace lifecycle; it calls Show from the UI goroutine and Do to marshal work from
// other goroutines (the daemon's socket handlers) onto the UI goroutine.

import (
	"reflect"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// hostCallState is the cancellation handshake of one Do call. Do and the UI goroutine
// race on its mutex: either the UI claims the call (started) and Do waits for completion,
// or Do times out first and marks it canceled so the queued fn can never run later.
type hostCallState struct {
	mu       sync.Mutex
	started  bool
	canceled bool
	done     chan struct{}
}

func newHostCallState() *hostCallState { return &hostCallState{done: make(chan struct{})} }

// tryStart claims the call for execution. It reports false when Do already timed out.
func (s *hostCallState) tryStart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.canceled {
		return false
	}
	s.started = true
	return true
}

// timeout gives up on a Do wait. It reports started=true when the fn already began (the
// caller must wait for done); otherwise it cancels the call so a later queued hostCallMsg
// is a no-op.
func (s *hostCallState) timeout() (started bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return true
	}
	s.canceled = true
	return false
}

func (s *hostCallState) finish() { close(s.done) }

// hostCallMsg runs fn on the program's UI goroutine unless its Do call timed out first.
type hostCallMsg struct {
	fn    func()
	state *hostCallState
}

// notifyMsg is a status notice queued through the host's bounded outbound queue. Notify is
// callable from any goroutine: the model is only touched when host Update applies this on
// the UI goroutine.
type notifyMsg struct{ text string }

// modelCmdMsg is the host-level envelope around a command result. The workspace models
// share one program; a command started by the model that was active at the time can
// return after a switch. Palette messages carry their own palette.Owner, but searchpanel,
// dock, LSP and plugin async messages do not. The envelope keeps the originating model
// pointer AND its owner: a result for a hidden warm model is delivered to that model on
// the UI goroutine (it must keep receiving PTY output/exit and lifecycle work), not
// dropped; only a closed model loses its late results.
type modelCmdMsg struct {
	owner  palette.Owner
	source *Model
	msg    tea.Msg
}

// wrapModelCmd tags one command result with its source model and owner. Nil commands and
// nil messages stay nil, so a model that starts no work adds no envelope.
func wrapModelCmd(owner palette.Owner, source *Model, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		// A queued Init/Queue command can outlive an eviction/close. Check the atomic
		// closed flag before running it so a closed model's work cannot have side effects
		// (the result would also be dropped by deliverModelCmd). A Close that races after
		// this check can still let an already-started command run; its result is dropped.
		if source != nil && source.Closed() {
			return nil
		}
		msg := cmd()
		if msg == nil {
			return nil
		}
		return modelCmdMsg{owner: owner, source: source, msg: msg}
	}
}

// programMsgTypes are the Bubble Tea messages the Program handles itself instead of
// passing to Model.Update: clear-screen, clipboard, terminal queries, raw/print output and
// exec requests. A host envelope must unwrap these back to the Program; delivering them to
// Model.Update would silently lose them (including tea.Quit).
var programMsgTypes = func() map[reflect.Type]struct{} {
	msgs := []tea.Msg{
		tea.ClearScreen(), tea.ReadClipboard(), tea.ReadPrimaryClipboard(),
		tea.RequestBackgroundColor(), tea.RequestForegroundColor(), tea.RequestCursorColor(),
		tea.RequestCursorPosition(), tea.RequestWindowSize(),
		tea.RequestProgramStatusSupport(), tea.RequestTerminalVersion(),
		tea.SetClipboard("")(), tea.SetPrimaryClipboard("")(),
		tea.Println("")(), tea.Printf("")(),
		tea.Exec(nil, nil)(),
		tea.Raw("")(),
	}
	out := make(map[reflect.Type]struct{}, len(msgs))
	for _, msg := range msgs {
		out[reflect.TypeOf(msg)] = struct{}{}
	}
	return out
}()

// isProgramMsg reports whether msg belongs to the Program rather than a Model.Update.
// Exported lifecycle messages are matched by type; the internal ones by their reflect
// type. A wrapped command must hand these back to the Program untouched.
func isProgramMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.QuitMsg, tea.SuspendMsg, tea.ResumeMsg, tea.InterruptMsg, tea.RawMsg:
		return true
	}
	_, ok := programMsgTypes[reflect.TypeOf(msg)]
	return ok
}

// deliverModelCmd routes an envelope to the model that started the command. A hidden
// warm model receives its own result on the UI goroutine and keeps its terminal/LSP
// lifecycle armed; a closed model is ignored. The active model's messages follow the
// normal host path. A BatchMsg is re-wrapped per subcommand with the same source so the
// inner results keep their provenance.
func (h *WorkspaceHost) deliverModelCmd(env modelCmdMsg) (tea.Model, tea.Cmd) {
	if env.source == nil || env.source.Closed() {
		return h, nil
	}
	if _, ok := env.msg.(workspaceQuitNextMsg); ok {
		p, ok := env.source.workspace.(workspaceQuitPort)
		if !ok {
			return h, nil
		}
		target, index := p.QuitNextDirty()
		if target == nil {
			return h, wrapModelCmd(env.owner, env.source, env.source.quit())
		}
		if err := p.QuitActivate(target); err != nil {
			env.source.notice = err.Error()
			return h, nil
		}
		if len(target.large.saves) > 0 {
			target.notice = "Wait for the save to finish, then quit again"
			return h, nil
		}
		t := target.tabPort()
		if t == nil {
			target.notice = "Cannot save workspace"
			return h, nil
		}
		t.ActivateTab(index)
		target.syncViewport()
		flow := flow{then: nextQuit, index: index}
		if !hasPath(target.ed.Active()) {
			target.askSaveAs(flow)
			return h, nil
		}
		return h, wrapModelCmd(target.WorkspaceOwner(), target, target.saveThen(flow))
	}
	if batch, ok := env.msg.(tea.BatchMsg); ok {
		cmds := make([]tea.Cmd, 0, len(batch))
		for _, c := range batch {
			cmds = append(cmds, wrapModelCmd(env.owner, env.source, c))
		}
		return h, tea.Batch(cmds...)
	}
	if isProgramMsg(env.msg) {
		if env.source != h.Active() {
			// A hidden/closed model's program-level message must not quit, suspend,
			// clear or write the clipboard of the program currently showing another
			// workspace. Its model-lifecycle messages still route to it below.
			return h, nil
		}
		// Hand the message back to the Program untouched: Model.Update ignores
		// clear-screen, clipboard and quit messages.
		return h, func() tea.Msg { return env.msg }
	}
	if env.source == h.Active() {
		return h.Update(env.msg)
	}
	// A hidden warm model: deliver without changing the active model, then tag the
	// commands it starts with the same source.
	_, c := env.source.Update(env.msg)
	return h, wrapModelCmd(env.owner, env.source, c)
}

// WorkspaceHost implements tea.Model over the active workspace model.
type WorkspaceHost struct {
	mu      sync.Mutex
	active  *Model
	program *tea.Program
	// pendingShow marks a model shown since the last Update; pendingCmd carries its first
	// Init (nil when the model has nothing to start). The next Update replays the known
	// window size so the swapped-in workspace paints at the client's size. A warm model is
	// not Init'ed again: its runtime and state continue where the switch left them.
	pendingShow bool
	pendingCmd  tea.Cmd
	// initialized marks the models whose Init already ran (the first is the program's own
	// Init; every later one is started by Show).
	initialized map[*Model]bool
	// width/height remember the last window size the program saw, so a model shown on a
	// workspace switch is sized (a daemon host has no terminal to ask).
	width, height int
	// outbound is the bounded queue of host calls. ONE sender goroutine drains it into
	// p.Send: a blocked program queue therefore costs one goroutine, not one per Do.
	outbound      chan tea.Msg
	stopCh        chan struct{}
	senderStarted bool
	stopped       bool
	// pendingCount bounds QueueFor work so a stopped/closed/backed-up UI cannot grow
	// pendingCmd without limit.
	pendingCount int
}

// hostOutboundQueue bounds the host calls waiting to be handed to the program. A full
// queue makes DoWithTimeout fail fast (the callback never ran) instead of growing
// goroutines while the UI is stuck.
const hostOutboundQueue = 64

// hostPendingCap bounds queued QueueFor commands waiting for a later Update.
const hostPendingCap = 64

// defaultPostOpenTimeout bounds how long PostOpenFileAt may wait for a slot in the host
// queue. It must never block a socket/connection goroutine indefinitely when the UI is
// stuck; false means the open was not delivered.
const defaultPostOpenTimeout = 2 * time.Second

// NewWorkspaceHost returns a host over active.
func NewWorkspaceHost(active *Model) *WorkspaceHost {
	return &WorkspaceHost{
		active:      active,
		initialized: map[*Model]bool{active: true},
		outbound:    make(chan tea.Msg, hostOutboundQueue),
		stopCh:      make(chan struct{}),
	}
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
	h.mu.Lock()
	h.active = m
	h.pendingShow = true
	if !h.initialized[m] {
		h.initialized[m] = true
		h.pendingCmd = tea.Batch(h.pendingCmd, wrapModelCmd(m.WorkspaceOwner(), m, m.Init()))
	}
	h.mu.Unlock()
}

// Ready reports whether the program exists (RunWorkspaceHost has set it). Before that,
// the app's workspace calls run inline: no program means no concurrent update loop.
func (h *WorkspaceHost) Ready() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.program != nil
}

// defaultHostDoTimeout is Do's normal wait. DoWithTimeout lets tests and callers choose a
// shorter bound; false still guarantees the callback never ran.
const defaultHostDoTimeout = 10 * time.Second

// Do runs fn on the program's UI goroutine with the default wait.
func (h *WorkspaceHost) Do(fn func()) bool {
	return h.DoWithTimeout(defaultHostDoTimeout, fn)
}

// enqueueHostMsg puts msg on the bounded outbound queue, waiting at most timeout. It
// reports false when the program is gone, the queue stays full until the timeout, or the
// host stops. The caller must not fall back to a direct p.Send (which can block forever).
func (h *WorkspaceHost) enqueueHostMsg(msg tea.Msg, timeout time.Duration) bool {
	h.mu.Lock()
	p := h.program
	stop := h.stopCh
	h.mu.Unlock()
	// No program means no sender: accepting the message would report a delivery that can
	// never happen (or be drained by a later program).
	if p == nil || stop == nil {
		return false
	}
	if timeout <= 0 {
		timeout = defaultPostOpenTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	// Stop may already be closed while both channels are ready; prefer it.
	select {
	case <-stop:
		return false
	default:
	}
	select {
	case h.outbound <- msg:
		// accepted != delivered: the sender/program may stop before it runs. But once
		// stop is already closed this call must not report acceptance.
		select {
		case <-stop:
			return false
		default:
			return true
		}
	case <-timer.C:
		return false
	case <-stop:
		return false
	}
}

// DoWithTimeout runs fn on the program's UI goroutine and waits for it. The return value
// is a completion guarantee: true means fn ran to completion (even past the nominal
// timeout, when it had already started); false means fn definitively never ran and never
// will, because the timeout canceled the queued hostCallMsg before the UI claimed it. It
// reports false immediately when no program is running.
func (h *WorkspaceHost) DoWithTimeout(timeout time.Duration, fn func()) bool {
	h.mu.Lock()
	p := h.program
	stop := h.stopCh
	h.mu.Unlock()
	if p == nil || stop == nil {
		return false
	}
	state := newHostCallState()
	if timeout <= 0 {
		timeout = defaultHostDoTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	call := hostCallMsg{fn: fn, state: state}
	// Enqueue into the host-owned bounded queue, never p.Send directly: p.Send can block
	// forever when the program's message queue is full (the UI is inside an earlier host
	// call). One sender goroutine handles all calls; a full queue means this call never
	// ran and false is safe.
	select {
	case h.outbound <- call:
	case <-timer.C:
		return false
	case <-stop:
		return false
	}
	select {
	case <-state.done:
		return true
	case <-timer.C:
		if state.timeout() {
			// fn already started: wait for it to finish rather than returning while it
			// mutates workspace state.
			<-state.done
			return true
		}
		return false
	case <-stop:
		if state.timeout() {
			<-state.done
			return true
		}
		return false
	}
}

// runSender is the host's single program-send goroutine. It may block in p.Send while the
// UI is stuck; the bounded outbound queue and the stop channel cap that cost at one
// goroutine plus hostOutboundQueue queued calls. Calls that time out while queued are
// canceled and skipped by host Update.
func (h *WorkspaceHost) runSender(p *tea.Program, stop <-chan struct{}) {
	for {
		// Prefer stop when both are ready, so a stopped host does not take another
		// queued message into p.Send.
		select {
		case <-stop:
			return
		default:
		}
		select {
		case <-stop:
			return
		case msg := <-h.outbound:
			p.Send(msg)
		}
	}
}

// Queue adds cmd to the batch the next Update returns (for work that must run after a
// model switch, such as starting the imported session's terminal in the new dock).
func (h *WorkspaceHost) Queue(cmd WorkspaceCmd) bool {
	return h.QueueFor(h.Active(), cmd)
}

// QueueFor queues cmd as work of source: its results are routed back to source even after
// another workspace becomes active. A nil source (or the active model) keeps the ordinary
// active binding. The command is wrapped when queued, not when the model is shown, so
// several Show/Queue calls in one update keep their own source.
func (h *WorkspaceHost) QueueFor(source *Model, cmd WorkspaceCmd) bool {
	if cmd == nil {
		return false
	}
	if source == nil {
		source = h.Active()
	}
	if source == nil || source.Closed() {
		return false
	}
	wrapped := wrapModelCmd(source.WorkspaceOwner(), source, cmd)
	h.mu.Lock()
	if h.stopped || h.pendingCount >= hostPendingCap {
		h.mu.Unlock()
		return false
	}
	h.pendingCount++
	h.pendingCmd = tea.Batch(h.pendingCmd, wrapped)
	h.mu.Unlock()
	return true
}

// Notify queues a notice for the active workspace's status row. It never calls Do or
// touches a model from the caller's goroutine: the bounded outbound sender hands the
// notice to host Update, which writes the model on the UI goroutine. It reports false if
// the host is stopped/not ready or its queue is full; the caller may ignore that (a notice
// is best-effort) but must not retry in a loop.
func (h *WorkspaceHost) Notify(text string) bool {
	if text == "" {
		return true
	}
	h.mu.Lock()
	p := h.program
	stop := h.stopCh
	h.mu.Unlock()
	if p == nil || stop == nil {
		return false
	}
	select {
	case <-stop:
		return false
	default:
	}
	select {
	case h.outbound <- notifyMsg{text: text}:
		select {
		case <-stop:
			return false
		default:
			return true
		}
	default:
		return false
	}
}

// openFileAtMsg is a client open-file request with an optional 1-based position.
type openFileAtMsg struct {
	path      string
	line, col int
	// source is the model the open was meant for; nil means the active model. Binding it
	// prevents a switch between the manager's select and the program delivery from
	// opening the file in the newly active workspace.
	source *Model
}

// PostOpenFile sends a client's open-file request to the active model through the program
// queue. The manager uses the target-bound PostOpenFileAt for workspace-routed opens.
func (h *WorkspaceHost) PostOpenFile(path string) bool {
	return h.PostOpenFileAt(h.Active(), path, 0, 0)
}

// PostOpenFileAt is PostOpenFile with a position: line/col are moved to after the file is
// focused (line 0 keeps the restored/existing caret). source is the model the manager
// selected for this open; binding it makes a later switch before delivery harmless. It
// returns false when the bounded host queue did not accept the request (no program, stuck
// UI, or timeout), so a socket goroutine never blocks indefinitely.
func (h *WorkspaceHost) PostOpenFileAt(source *Model, path string, line, col int) bool {
	return h.PostOpenFileAtWithTimeout(source, path, line, col, defaultPostOpenTimeout)
}

// PostOpenFileAtWithTimeout is PostOpenFileAt with an explicit queue-wait bound.
func (h *WorkspaceHost) PostOpenFileAtWithTimeout(source *Model, path string, line, col int, timeout time.Duration) bool {
	return h.enqueueHostMsg(openFileAtMsg{path: path, line: line, col: col, source: source}, timeout)
}

// stop marks the program as over, so late asynchronous work (a capture write finishing
// after quit) does not block on the dead message queue.
func (h *WorkspaceHost) stop() {
	h.mu.Lock()
	h.program = nil
	h.stopped = true
	h.pendingCmd = nil
	h.pendingCount = 0
	stop := h.stopCh
	h.stopCh = nil
	h.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// setProgram is called by RunWorkspaceHost before the program starts. It starts the one
// host-owned sender goroutine; the queue it drains is bounded.
func (h *WorkspaceHost) setProgram(p *tea.Program) {
	h.mu.Lock()
	h.program = p
	stop := h.stopCh
	if stop == nil {
		stop = make(chan struct{})
		h.stopCh = stop
	}
	first := !h.senderStarted
	h.senderStarted = true
	h.mu.Unlock()
	if first {
		go h.runSender(p, stop)
	}
}

// Init implements tea.Model. The first Init's results are tagged with the active model's
// owner so a switch before they land cannot apply them to another model.
func (h *WorkspaceHost) Init() tea.Cmd {
	m := h.Active()
	if m == nil {
		return nil
	}
	return wrapModelCmd(m.WorkspaceOwner(), m, m.Init())
}

// Update implements tea.Model: host messages run here, everything else goes to the active
// workspace model. A model shown during this update starts with its Init.
func (h *WorkspaceHost) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if owned, ok := msg.(modelCmdMsg); ok {
		return h.deliverModelCmd(owned)
	}
	var cmd tea.Cmd
	if size, ok := msg.(tea.WindowSizeMsg); ok && size.Width > 0 && size.Height > 0 {
		// Ignore a 0x0 size: a daemon's program output is not a terminal, so a window
		// size request can come back as 0x0 and must not erase the client's size.
		h.mu.Lock()
		h.width, h.height = size.Width, size.Height
		h.mu.Unlock()
	}
	switch msg := msg.(type) {
	case notifyMsg:
		if active := h.Active(); active != nil {
			active.Notify(msg.text)
		}
		return h, nil
	case hostCallMsg:
		state := msg.state
		if state == nil {
			state = newHostCallState()
		}
		if state.tryStart() {
			// finish is deferred so a panicking fn still releases a waiting Do.
			func() {
				defer state.finish()
				msg.fn()
			}()
		} else {
			state.finish()
		}
	case openFileAtMsg:
		target := msg.source
		if target == nil {
			target = h.Active()
		}
		if target == nil || target.Closed() {
			return h, nil
		}
		cmd := wrapModelCmd(target.WorkspaceOwner(), target, target.openFileCmd(msg.path, false, msg.line, msg.col))
		target.focusEditor()
		return h, cmd
	default:
		active := h.Active()
		next, c := active.Update(msg)
		if m, ok := next.(*Model); ok {
			h.mu.Lock()
			h.active = m
			h.mu.Unlock()
		}
		cmd = wrapModelCmd(active.WorkspaceOwner(), active, c)
	}
	h.mu.Lock()
	pending, shown := h.pendingCmd, h.pendingShow
	h.pendingCmd, h.pendingShow = nil, false
	h.pendingCount = 0
	width, height := h.width, h.height
	h.mu.Unlock()
	if shown {
		// The shown model has no window size of its own: replay the last one so it paints
		// at the attached client's size. This must happen even when Init is nil.
		if width > 0 && height > 0 {
			// Bind the replay to the model this update showed: another Show before the
			// command runs must not size the wrong workspace.
			shown := h.Active()
			if shown != nil {
				cmd = tea.Batch(cmd, wrapModelCmd(shown.WorkspaceOwner(), shown, func() tea.Msg {
					return tea.WindowSizeMsg{Width: width, Height: height}
				}))
			}
		}
	}
	if pending != nil {
		// Queued/Init commands were wrapped with their own source when queued.
		cmd = tea.Batch(pending, cmd)
	}
	return h, cmd
}

// View implements tea.Model.
func (h *WorkspaceHost) View() tea.View { return h.Active().View() }
