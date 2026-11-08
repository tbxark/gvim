package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/pluginapi"
)

// This file is the pump between the root model and the plugin runtime. The runtime is
// single-goroutine: every call into it happens on the update loop, and Lua never runs
// anywhere else. What the runtime needs done off the loop comes back as pluginapi.Work,
// which the pump turns into tea.Cmds; their completions return as pluginWorkMsg and go
// to Runtime.Resolve.
//
// Re-entrancy (AMENDMENTS C1). A Host method runs inside a Lua call, so it must never
// call Runtime.Run, Resolve, Control, Veto, Drain or Stop. A Host method that could
// reach one of those validates its arguments, appends a closure to pluginState.later
// and returns. The pump runs the closures after the Lua slice has returned, in order,
// and then drains the runtime. Every call the root makes into the runtime goes through
// pumpAfter, so the pump runs once after each of them (C26).

const (
	// pumpBudget is how long the Drains of one pump may run Lua together (D16): each
	// Drain gets what the pump has left of it.
	pumpBudget = 100 * time.Millisecond
	// pumpRounds caps the later/Drain cascade of one pump; what is left, and what a Drain
	// that reports more work leaves, goes to the next pluginDrainMsg.
	pumpRounds = 8
	// pumpLater bounds the deferred closures one pump runs.
	pumpLater = 1000
	// pendingEvents bounds the events plugEmit queues between two pumps. The newest are
	// dropped: they are the ones a runaway producer made last.
	pendingEvents = 10000
)

// pluginWorkMsg is the completion of one Work, or one item of a stream. next re-arms a
// stream and is nil for everything else.
type pluginWorkMsg struct {
	c    pluginapi.Completion
	next func() tea.Cmd
}

// pluginDrainMsg asks the root to pump again: the runtime had more queued than one
// Drain may run.
type pluginDrainMsg struct{}

// pluginState is everything the root keeps for the plugin host. The zero rt turns the
// host off: every hook is a nil check on rt.
type pluginState struct {
	rt   pluginapi.Runtime
	host *pluginHost
	// pe is the editor's plugin port. It is nil when the app wired none, and the buffer
	// calls of a plugin answer unsupported then.
	pe PluginEditor
	// decor holds the decorations of the plugins (plugin_decor.go); nil with no runtime.
	decor *decorations.Store

	// later holds the closures Host methods deferred; the pump runs them in order at
	// Runtime.InSlice()==false.
	later []func() tea.Cmd
	// quiet is above zero while the root runs something on behalf of a plugin. The macro
	// recorder ignores commands then (C8), so only the command id the user issued is
	// recorded and what the plugin does through command.run is not.
	quiet int
	// direct is above zero while doForPlugin runs an action inside a Lua call. The
	// plugin-command hook of uiAction is skipped then, because such an action must not
	// reach the runtime.
	direct int
	// doBudget is the wall time one Host.Do or DoAll may take; zero means maxDoTime. A test
	// sets it.
	doBudget time.Duration
	// pumping is set while pluginPump runs; a nested pump returns at once, and the outer
	// loop picks up whatever the nested caller queued.
	pumping bool
	// tick is set while a pluginDrainMsg is on its way.
	tick bool
	// events are the events plugEmit queued; the pump hands them to Runtime.Emit.
	events []pluginapi.Event
	// waits are the async Host calls whose result the root has yet to deliver. A wait that
	// is no longer here (CancelWait) has its result dropped.
	waits map[pluginapi.Token]string
	// ui is the state of the prompts, pickers and popups of the plugins (plugin_prompts.go).
	ui pluginUI
	// src says what started the command now running, for the plugin command's ctx.source.
	src pluginapi.CommandSource

	// started is set once the runtime has been started (Init), and readySent once `ready`
	// has been queued.
	started, readySent bool
	// shared, when non-nil, is the daemon-wide host mux this model is bound into: the
	// runtime is shared with every workspace, so this model must not Stop it.
	shared *PluginShared

	// tracking is set while the editor's change tracking is on for buffer_changed.
	tracking bool
	// cmdErr is why the command now running failed, for post_command.
	cmdErr error

	// panicKey is a test seam: when set, a key it accepts counts as a panic chord, so a test
	// can observe the hook sites.
	panicKey func(tea.Key) bool

	// keys is the key layer, the modes, the status items and the idle timer (plugin_keys.go).
	keys pluginKeys
	// panels are the declarative widget panels and sidebar sections plugins mounted
	// (plugin_panels.go).
	panels pluginPanels
	// menus are the rows plugins added to the menu bar dropdowns (plugin_menus.go).
	menus menus
	// groups are the buffer groups and scroll-sync groups (plugin_groups.go).
	groups pluginGroups
	// folds are the plugin folding ranges (plugin_folds.go).
	folds     pluginFolds
	foldOrder int
	// viewState is the tier-1c-C window state: buffer -> plugin -> key (plugin_seams.go).
	viewState map[pluginapi.BufferID]map[string]map[string]any
	// lineTargets are the clickable lines of plugin buffers (plugin_linetargets.go).
	lineTargets map[pluginapi.BufferID]*lineTargetSet
	// explorer is the file-explorer decoration surface (plugin_explorer.go).
	explorer pluginExplorer
	// virtual records who made each virtual buffer (plugin_virtual.go).
	virtual pluginVirtual

	// commands are the registered plugin commands by plugin; order is the order in which
	// the plugins first registered, which is the palette order.
	commands map[string][]pluginapi.CommandInfo
	order    []string
}

// newPluginState returns the plugin state for the ports; Plugins may be nil.
func newPluginState(m *Model, ports RootPorts) pluginState {
	p := pluginState{rt: ports.Plugins, pe: ports.PluginEditor, shared: ports.PluginShared}
	if ports.PluginShared != nil {
		p.host = ports.PluginShared.host
	} else if p.rt != nil {
		p.host = &pluginHost{m: m}
	}
	if p.rt != nil {
		p.decor = decorStoreOf(ports.PluginEditor)
	}
	return p
}

// collect runs fn and returns the commands that Host.Do collected into m.pending
// meanwhile. m.pending is restored, so commands a menu row collected before are not
// taken, and the ones of the plugin do not leak into the next menu action.
func (m *Model) collect(fn func()) tea.Cmd {
	held := m.pending
	m.pending = nil
	fn()
	got := m.pending
	m.pending = held
	return got
}

// pumpAfter runs call, which calls into the runtime, and then pumps. It is the only way
// the root calls Runtime.Start, Run, Resolve, Control, Emit and Veto, so the deferred
// closures, the events and the runtime's queues are handled after every one of them. The
// commands that Host.Do collected during call are part of the result.
func (m *Model) pumpAfter(call func()) tea.Cmd {
	p := &m.plug
	if p.rt == nil {
		return nil
	}
	cmd := m.collect(func() {
		p.quiet++
		defer func() { p.quiet-- }()
		call()
	})
	return tea.Batch(cmd, m.pluginPump())
}

// pluginPump runs the deferred closures, flushes the queued events into the runtime and
// drains it, until nothing is left or the round limit is reached. It returns the Work of
// the runtime as commands, the commands the plugins collected, and a drain tick when
// work remains. A nested call returns nil: the outer pump is running and sees what the
// nested caller queued.
func (m *Model) pluginPump() tea.Cmd {
	p := &m.plug
	if p.rt == nil || p.pumping {
		return nil
	}
	p.pumping = true
	defer func() { p.pumping = false }()
	var cmds []tea.Cmd
	more := false
	ran := 0
	deadline := time.Now().Add(pumpBudget)
	cmds = append(cmds, m.collect(func() {
		p.quiet++
		defer func() { p.quiet-- }()
		for range pumpRounds {
			left := time.Until(deadline)
			if left <= 0 {
				break
			}
			m.plugFlushEvents()
			for len(p.later) > 0 && ran < pumpLater {
				fn := p.later[0]
				p.later[0] = nil
				p.later = p.later[1:]
				ran++
				cmds = append(cmds, fn())
			}
			var work []pluginapi.Work
			work, more = p.rt.Drain(left)
			cmds = append(cmds, m.workCmds(work))
			if more || (len(p.later) == 0 && len(p.events) == 0) {
				break
			}
		}
	}))
	if more || len(p.later) > 0 || len(p.events) > 0 {
		cmds = append(cmds, m.drainTick())
	}
	return tea.Batch(cmds...)
}

// drainTick returns the command that sends pluginDrainMsg, or nil when one is already
// on its way.
func (m *Model) drainTick() tea.Cmd {
	if m.plug.tick {
		return nil
	}
	m.plug.tick = true
	return tea.Tick(0, func(time.Time) tea.Msg { return pluginDrainMsg{} })
}

// plugFlushEvents hands the queued events to the runtime, which only queues them.
func (m *Model) plugFlushEvents() {
	p := &m.plug
	if len(p.events) == 0 {
		return
	}
	events := p.events
	p.events = nil
	for _, e := range events {
		p.rt.Emit(e)
	}
}

// plugEmit queues an event for the plugins. It never runs Lua: the next pump hands the
// event to the runtime. An event nobody subscribed to is dropped here, except the
// lifecycle events (FINAL section 6): the plugins load asynchronously, so nobody
// subscribes yet when the first resize arrives. The runtime filters those and holds
// ready until its own plugins_loaded has been delivered.
func (m *Model) plugEmit(name string, data map[string]any) {
	p := &m.plug
	if p.rt == nil || len(p.events) >= pendingEvents || (!unconditionalEvents[name] && !p.rt.Wants(name)) {
		return
	}
	p.events = append(p.events, pluginapi.Event{Name: name, Data: data})
}

// pluginReady queues `ready` once, on the first window size after the runtime was started:
// the screen is laid out and the plugins can ask for its size. Bubble Tea calls Init before
// it delivers the first size. The runtime holds `ready` back until the plugins have loaded
// and plugins_loaded has been raised, so a handler sees them in that order.
func (m *Model) pluginReady() {
	if !m.plug.started || m.plug.readySent {
		return
	}
	m.plug.readySent = true
	m.plugEmit("ready", map[string]any{})
}

// unconditionalEvents are the events the root emits whether or not a plugin subscribed.
// shutdown is not one of them: the root raises none (AMENDMENTS C25), Runtime.Stop runs the
// handlers itself.
var unconditionalEvents = map[string]bool{"ready": true, "resize": true}

// pluginInit starts the runtime. The Work it returns discovers the plugin sources; the
// plugins load when its completion is resolved.
func (m *Model) pluginInit() tea.Cmd {
	p := &m.plug
	if p.rt == nil {
		return nil
	}
	var work []pluginapi.Work
	p.started = true
	cmd := m.pumpAfter(func() { work = p.rt.Start(p.host) })
	return tea.Batch(m.workCmds(work), cmd)
}

// pluginStop stops the runtime: it runs the shutdown handlers, kills the processes the
// plugins started and flushes their state. Close calls it before the dock closes.
func (m *Model) pluginStop() {
	if m.plug.shared != nil {
		// One runtime for the daemon: its lifecycle is the daemon's, not a workspace's.
		return
	}
	if rt := m.plug.rt; rt != nil {
		rt.Stop()
	}
}

// pluginMessage handles the messages of the plugin host. It reports whether it took msg.
func (m *Model) pluginMessage(msg tea.Msg) (tea.Cmd, bool) {
	p := &m.plug
	switch msg := msg.(type) {
	case pluginWorkMsg:
		if p.rt == nil {
			return nil, true
		}
		cmd := m.pumpAfter(func() { p.rt.Resolve(msg.c) })
		if msg.next != nil {
			cmd = tea.Batch(cmd, msg.next())
		}
		return cmd, true
	case pluginDrainMsg:
		p.tick = false
		return m.pumpAfter(func() {}), true
	case pluginOpenedMsg:
		return m.pluginOpened(msg), true
	case pluginSavedMsg:
		return m.pluginSaved(msg), true
	case pluginRevertedMsg:
		return m.pluginReverted(msg), true
	case pluginLSPMsg:
		return m.pluginLSPDone(msg), true
	}
	return m.pluginKeysMessage(msg)
}

// pluginControl asks the runtime for a lifecycle operation (reload, enable, ...). The
// Work it needs becomes commands, and its notices go to the status row.
func (m *Model) pluginControl(c pluginapi.Control) tea.Cmd {
	p := &m.plug
	if p.rt == nil {
		return nil
	}
	var work []pluginapi.Work
	var notices []pluginapi.Notice
	cmd := m.pumpAfter(func() { work, notices = p.rt.Control(c) })
	for _, n := range notices {
		m.notice = n.Text
	}
	return tea.Batch(m.workCmds(work), cmd)
}

// pluginVeto asks the runtime whether the plugins cancel the operation e names. It is
// gated on Wants. The root only gets here at InSlice()==false, because every action that
// can reach a save is deferred by Host.Do (reentrantActions); the runtime also refuses to
// run Lua for a veto asked inside a slice. The command is the pump's, and the caller
// returns it.
func (m *Model) pluginVeto(e pluginapi.Event) (cancel bool, reason string, cmd tea.Cmd) {
	p := &m.plug
	if p.rt == nil || !p.rt.Wants(e.Name) {
		return false, "", nil
	}
	cmd = m.pumpAfter(func() { cancel, reason = p.rt.Veto(e) })
	return cancel, reason, cmd
}

// pluginSaveGate runs the before_file_save veto of a save that source started: "manual",
// "autosave" or "api". It reports whether a plugin cancelled the save and why, and
// returns the pump's command, which the caller returns with its own. The runtime is never
// called from inside a Lua slice, so a save a plugin asks for runs through Host.Save,
// which defers it.
func (m *Model) pluginSaveGate(source string) (cancel bool, reason string, cmd tea.Cmd) {
	if m.plug.rt == nil {
		return false, "", nil
	}
	data := map[string]any{"source": source}
	if pe := m.plug.pe; pe != nil {
		id := pe.ActiveBuffer()
		data["buf"] = id
		if info, ok := pe.Buffer(id); ok {
			data["path"] = info.Path
		}
	}
	cancel, reason, cmd = m.pluginVeto(pluginapi.Event{Name: "before_file_save", Data: data})
	if cancel {
		if reason == "" {
			reason = "Save cancelled by a plugin"
		}
		m.notice = reason
	}
	return cancel, reason, cmd
}

// workCmds turns the runtime's Work into commands.
func (m *Model) workCmds(ws []pluginapi.Work) tea.Cmd {
	if len(ws) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(ws))
	for _, w := range ws {
		cmds = append(cmds, workCmd(w))
	}
	return tea.Batch(cmds...)
}

// workCmd returns the command that runs one Work. Run is a one-shot, Stream is re-armed
// for each item the way the dock waits for its events, and a Work with neither is a
// timer. The Work goroutine touches no Lua value, no Host and no root state: it gets
// what it needs in the closure and returns a Completion.
func workCmd(w pluginapi.Work) tea.Cmd {
	ctx := w.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	switch {
	case w.Run != nil:
		return func() tea.Msg { return pluginWorkMsg{c: runWork(ctx, w)} }
	case w.Stream != nil:
		return streamCmd(ctx, w)
	}
	// Not tea.Tick: it cannot be cancelled, and a long timer of a plugin that was reloaded
	// would park a goroutine until its time was up. A cancelled timer delivers nothing.
	return func() tea.Msg {
		t := time.NewTimer(w.After)
		defer t.Stop()
		select {
		case <-t.C:
			return pluginWorkMsg{c: pluginapi.Completion{Token: w.Token}}
		case <-ctx.Done():
			return nil
		}
	}
}

// runWork runs w.Run. A panic in a plugin's Work must not end the program, so it comes
// back as an error completion.
func runWork(ctx context.Context, w pluginapi.Work) (c pluginapi.Completion) {
	defer func() {
		if r := recover(); r != nil {
			c = pluginapi.Completion{Token: w.Token, Err: fmt.Sprintf("io: work panicked: %v", r)}
		}
	}()
	c = w.Run(ctx)
	if c.Token == 0 {
		c.Token = w.Token
	}
	return c
}

// streamCmd waits for the next item of a stream. An item with More set carries the
// command that waits for the one after it. A stream that ends delivers its last
// completion, if it gave one, as the final item.
func streamCmd(ctx context.Context, w pluginapi.Work) tea.Cmd {
	return func() tea.Msg {
		c, ok := nextItem(ctx, w)
		if c.Token == 0 {
			c.Token = w.Token
		}
		if !ok {
			if c.Token == 0 || (c.Values == nil && c.Err == "" && c.Kind == "") {
				return nil
			}
			c.More = false
			return pluginWorkMsg{c: c}
		}
		msg := pluginWorkMsg{c: c}
		if c.More {
			msg.next = func() tea.Cmd { return streamCmd(ctx, w) }
		}
		return msg
	}
}

// nextItem calls w.Stream, turning a panic into an error item that ends the stream.
func nextItem(ctx context.Context, w pluginapi.Work) (c pluginapi.Completion, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			c, ok = pluginapi.Completion{Token: w.Token, Err: fmt.Sprintf("io: stream panicked: %v", r)}, false
		}
	}()
	return w.Stream(ctx)
}

// plugWait records an async Host call. A token that is already waiting is refused, so a
// runtime bug cannot make two calls share a result.
func (m *Model) plugWait(t pluginapi.Token, kind string) error {
	p := &m.plug
	if _, dup := p.waits[t]; dup {
		return pluginapi.Errf(pluginapi.CodeInvalid, "token %d is already waiting", t)
	}
	if p.waits == nil {
		p.waits = make(map[pluginapi.Token]string)
	}
	p.waits[t] = kind
	return nil
}

// plugFinish delivers the result of the wait c.Token to the runtime, unless the wait
// was cancelled meanwhile.
func (m *Model) plugFinish(c pluginapi.Completion) tea.Cmd {
	p := &m.plug
	if _, ok := p.waits[c.Token]; !ok || p.rt == nil {
		return nil
	}
	delete(p.waits, c.Token)
	return m.pumpAfter(func() { p.rt.Resolve(c) })
}
