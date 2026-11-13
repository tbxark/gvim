package plugin

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// Options configure a Runtime.
type Options struct {
	// Timeout is plugin_timeout_ms: the budget of a user-triggered slice. Event slices get
	// min(50 ms, Timeout), load slices min(4 x Timeout, 5 s). 0 means DefaultTimeout;
	// values are clamped to 1 ms..MaxTimeout.
	Timeout time.Duration
	// EventBudget is the upper bound of an event, timer or callback slice (0: 50 ms, the
	// value of the spec); the slice gets min(EventBudget, Timeout). The editor leaves it
	// alone. Tests that run real Lua raise it, so that a machine that is busy with other
	// work does not turn a slow slice into a timeout.
	EventBudget time.Duration
	// OS provides files, processes and watchers (nil: hostos.NopOS, everything unsupported).
	OS hostos.OS
	// HTTPClient, when set, is the client behind gvim.http.get (tests inject a TLS
	// httptest client or a scripted RoundTripper). Nil uses the safe production
	// transport: https only, public-address dial checks, no proxy.
	HTTPClient *http.Client
	// Services provides display width, diff, fuzzy, grep, decode, git root (nil:
	// every call that needs one returns CodeUnsupported).
	Services pluginapi.Services
	// Version is gvim.version.
	Version string
	// Platform is gvim.platform ("" means runtime.GOOS).
	Platform string
	// Now is the clock of log entries and notice de-duplication (nil: time.Now).
	Now func() time.Time
	// CheckGoroutine panics when a Runtime method is called from a goroutine other than
	// the one that called Start. GVIM_PLUGIN_DEBUG=1 turns it on as well.
	CheckGoroutine bool
	// HeapLimit is the retained-heap growth in one slice that cancels it
	// (0: 128 MiB). See slice.runGuard.
	HeapLimit int64
	// LogSink, when set, is called with each entry added to the plugin log, for --log-file. It
	// runs on the goroutine that calls the runtime and must not block.
	LogSink func(pluginapi.LogEntry)
	// Loader configures plugin discovery; nil discovers nothing (Start returns no Work and
	// plugins are added with LoadPlugin).
	Loader *LoaderOptions
	// Locale is the editor's locale for gvim.i18n ("" means "en", the only built-in locale
	// today). It is read when a plugin loads, so the value is stable for the plugin's life.
	Locale string
	// WorkspaceStateFile, when set, is the workspace-scoped gvim.state file of a daemon
	// workspace (sessions/<stable-id>/plugin-state.json). Then every plugin's state lives
	// in that ONE versioned file instead of the editor-wide per-plugin files; "" keeps the
	// editor-wide behavior a direct launch has.
	WorkspaceStateFile string
	// WorkspaceState is the parsed content of WorkspaceStateFile, read by the app off the
	// update loop (nil: empty or not read).
	WorkspaceState *WorkspaceState
	// WorkspaceStateReadOnly is set when the file was written by a newer gvim: state writes
	// are refused and the file is never overwritten.
	WorkspaceStateReadOnly bool
	// WorkspaceStateNote, when not empty, is logged once the workspace state is first
	// touched (a damaged file was ignored, or the read-only version guard explains itself).
	WorkspaceStateNote string
}

// Runtime runs Lua plugins. It implements pluginapi.Runtime and is not safe for use from
// more than one goroutine.
type Runtime struct {
	timeout   time.Duration
	eventCap  time.Duration
	os        hostos.OS
	svc       pluginapi.Services
	version   string
	platform  string
	locale    string
	now       func() time.Time
	heapLimit int64
	check     bool
	debug     bool

	// dashboardAutoOpen is the process-scoped dashboard auto-open override set by
	// gvim.dashboard.set_auto_open: 0 unset, 1 off, 2 on. It is read only from Lua
	// slices on the runtime goroutine.
	dashboardAutoOpen int8
	// workspaceStateFile/workspaceState/workspaceStateReadOnly/workspaceStateNote are the
	// per-workspace gvim.state target (empty: the editor-wide per-plugin files).
	workspaceStateFile     string
	workspaceState         *WorkspaceState
	workspaceStateReadOnly bool
	workspaceStateNote     string

	// wantsMemo remembers the answer of Wants per event. Anything that can change an answer
	// (a subscription made or dropped, a plugin loaded, unloaded, suspended or failed) calls
	// wantsChanged, so the root's per-Update Wants calls cost a map lookup.
	wantsMemo map[string]bool

	host  pluginapi.Host // as given to Start (Unsupported before)
	guard hostGuard      // what the API sees: host with slice pausing

	plugins map[string]*plugin
	order   []*plugin // load order of the plugins in plugins
	nextGen uint64
	nextTok pluginapi.Token
	nextTsk uint64
	waits   map[pluginapi.Token]*wait

	// depth counts Lua frames of this runtime on the Go stack (Resume/Call nesting);
	// InSlice is depth > 0. cur, curPlugin and curTask describe the running slice.
	depth     int
	cur       *slice
	curPlugin *plugin
	curTask   *task

	inbox                []pluginapi.Completion // Resolve calls made in a slice, and internal wake-ups
	cmdq                 []cmdReq               // Run calls made in a slice
	ctlq                 []pluginapi.Control    // Control calls made in a slice
	userq                []item                 // runnable user-class tasks (FIFO)
	evq                  []queuedEvent          // events not yet expanded into handler tasks
	rr                   int                    // round-robin cursor over r.order for event-class tasks
	nextBatch, lastBatch uint64                 // groups the handler tasks of one plugin for one event
	work                 []pluginapi.Work       // Work created, returned by the next Drain
	userSpent, userCap   time.Duration          // wall time of user-class slices in this Drain/Resolve, and its limit

	log       *logRing
	noticeAt  map[string]time.Time
	stopHooks []func()
	ioR       *ioRuntime // the IO areas' runtime-level state (ioruntime.go), created on first use
	http      *httpState // restricted HTTP GET state (http.go); nil only before New fills it
	started   bool
	stopped   bool
	gid       uint64
	closeHook func(*plugin) // tests: called just before a state is closed

	ld loader // discovery, ordering, enablement (discover.go, lifecycle.go)
}

// New creates a Runtime. It starts nothing; call Start with the host.
func New(o Options) *Runtime {
	r := &Runtime{
		timeout:   o.Timeout,
		eventCap:  o.EventBudget,
		os:        o.OS,
		svc:       o.Services,
		version:   o.Version,
		platform:  o.Platform,
		locale:    o.Locale,
		now:       o.Now,
		heapLimit: o.HeapLimit,
		check:     o.CheckGoroutine || os.Getenv("GVIM_PLUGIN_DEBUG") == "1",
		debug:     os.Getenv("GVIM_PLUGIN_DEBUG") == "1",
		plugins:   map[string]*plugin{},
		waits:     map[pluginapi.Token]*wait{},
		http:      newHTTPState(o.HTTPClient),
		log:       &logRing{sink: o.LogSink},
		noticeAt:  map[string]time.Time{},
	}
	if r.locale == "" {
		r.locale = "en"
	}
	switch {
	case r.timeout <= 0:
		r.timeout = DefaultTimeout
	case r.timeout < time.Millisecond:
		r.timeout = time.Millisecond
	case r.timeout > MaxTimeout:
		r.timeout = MaxTimeout
	}
	if r.eventCap <= 0 {
		r.eventCap = eventBudget
	}
	if r.os == nil {
		r.os = hostos.NopOS{}
	}
	if r.svc == nil {
		r.svc = noServices{}
	}
	if r.version == "" {
		r.version = "dev"
	}
	if r.platform == "" {
		r.platform = runtime.GOOS
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.heapLimit <= 0 {
		r.heapLimit = defaultHeapLimit
	}
	if o.Loader != nil {
		opt := *o.Loader // the runtime changes Settings on a config reload
		if o.WorkspaceStateFile != "" {
			// Workspace mode never reads or writes the editor-wide per-plugin state
			// files, whatever the caller's loader options said.
			opt.WorkspaceState = true
		}
		r.ld.opt = &opt
	}
	r.workspaceStateFile = o.WorkspaceStateFile
	r.workspaceState = o.WorkspaceState
	r.workspaceStateReadOnly = o.WorkspaceStateReadOnly
	r.workspaceStateNote = o.WorkspaceStateNote
	r.setHost(pluginapi.Unsupported{})
	return r
}

// WorkspaceStatePath is the per-workspace gvim.state file of this runtime, or "" for the
// editor-wide per-plugin files (direct launch). It is the seam the app and tests observe.
func (r *Runtime) WorkspaceStatePath() string { return r.workspaceStateFile }

// SwitchWorkspaceState makes path/st the active per-workspace gvim.state namespace. It is
// called on the UI goroutine when a daemon workspace becomes active: the current namespace
// is flushed as a Work (no blocking IO here), the in-memory store is replaced by the new
// namespace's seed, and later state reads/writes go to the new file. readOnly marks a file
// written by a newer gvim (writes refused, never overwritten).
func (r *Runtime) SwitchWorkspaceState(path string, st *WorkspaceState, readOnly bool) {
	io := r.io()
	io.wsSwitch(path, st, readOnly)
	r.workspaceStateFile = path
	r.workspaceState = st
	r.workspaceStateReadOnly = readOnly
}

// SetLocale sets the locale plugins translate for: gvim.i18n.locale() returns it and
// gvim.i18n.t falls back to English. The app calls it on the update loop, at startup and
// when the Settings locale row is saved.
func (r *Runtime) SetLocale(locale string) {
	r.locale = locale
}

// Locale is the locale plugins translate for ("" means English, for a runtime built
// outside the app).
func (r *Runtime) Locale() string { return r.locale }

func (r *Runtime) setHost(h pluginapi.Host) {
	r.host = h
	r.guard = hostGuard{Host: h, r: r}
}

// Interface check.
var _ pluginapi.Runtime = (*Runtime)(nil)

// goid returns the id of the calling goroutine (debug use only).
func goid() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i >= 0 {
		b = b[:i]
	}
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}

// enter is deferred at the top of every public entry point: `defer r.enter("Drain")()`. It
// enforces the single-goroutine rule when checking is on (that panic is meant to
// propagate) and returns the function that stops any other panic from leaving the entry
// point: a bug in the runtime or in a closure it runs (an OS or Host callback) is logged and
// the ui goes on. The depth counter is put back to what it was on entry.
func (r *Runtime) enter(name string) func() {
	r.checkG()
	depth, cur, curP, curT := r.depth, r.cur, r.curPlugin, r.curTask
	return func() {
		if rec := recover(); rec != nil {
			r.depth, r.cur, r.curPlugin, r.curTask = depth, cur, curP, curT
			r.logf("", levelError, fmt.Sprint(name, ": internal error: ", rec, "\n", stackString()))
		}
	}
}

// checkG enforces the single-goroutine rule when checking is on.
func (r *Runtime) checkG() {
	if r.check && r.started && goid() != r.gid {
		panic("plugin: Runtime called from a goroutine other than the one that called Start")
	}
}

// ---------- pluginapi.Runtime ----------

// Start attaches the host and returns the Work that discovers, reads and compiles the
// plugins (nil without Options.Loader). The plugins load when its completion is resolved.
func (r *Runtime) Start(h pluginapi.Host) []pluginapi.Work {
	if h == nil {
		h = pluginapi.Unsupported{}
	}
	r.setHost(h)
	if !r.started {
		r.started = true
		r.gid = goid()
	}
	return r.startLoader()
}

// Stop shuts the runtime down: it runs the shutdown handlers (200 ms in total for all
// plugins, min(50 ms, remaining) each, no yield), runs the stop hooks (kill processes,
// flush state), then closes every state. It is idempotent and may do IO.
func (r *Runtime) Stop() {
	if r.stopped {
		return
	}
	defer r.enter("Stop")()
	r.stopped = true
	end := time.Now().Add(shutdownBudget)
	for _, p := range append([]*plugin(nil), r.order...) {
		if p.dead || p.L == nil {
			continue
		}
		for _, s := range append([]*subscription(nil), p.subs["shutdown"]...) {
			left := time.Until(end)
			if left <= 0 {
				break
			}
			if !s.active {
				continue
			}
			_, rerr := r.callSync(p, s.fn, min(shutdownEach, left), "shutdown handler", p.toLua(map[string]any{}))
			if rerr != nil {
				r.logf(p.name, levelWarn, "shutdown handler: "+rerr.msg)
			}
		}
	}
	for _, h := range r.stopHooks {
		h()
	}
	for _, p := range append([]*plugin(nil), r.order...) {
		r.unload(p, unloadOptions{quiet: true})
	}
}

// onStop registers a function Stop calls before the states are closed.
func (r *Runtime) onStop(f func()) { r.stopHooks = append(r.stopHooks, f) }

// maxWantsMemo bounds the memo: event names come from plugins (gvim.emit), so the set of
// names asked about is not fixed.
const maxWantsMemo = 512

// wantsChanged forgets the remembered answers of Wants.
func (r *Runtime) wantsChanged() { r.wantsMemo = nil }

// Wants reports whether a live, unsuspended plugin subscribes to event.
func (r *Runtime) Wants(event string) bool {
	if v, ok := r.wantsMemo[event]; ok {
		return v
	}
	v := r.computeWants(event)
	if r.wantsMemo == nil || len(r.wantsMemo) >= maxWantsMemo {
		r.wantsMemo = make(map[string]bool)
	}
	r.wantsMemo[event] = v
	return v
}

func (r *Runtime) computeWants(event string) bool {
	for _, p := range r.order {
		if p.muted() {
			continue
		}
		for _, s := range p.subs[event] {
			if s.active {
				return true
			}
		}
	}
	return false
}

// Emit queues an event for delivery by the next Drain. It never runs Lua.
func (r *Runtime) Emit(e pluginapi.Event) {
	defer r.enter("Emit")()
	r.emit(e, nil)
}

// emit queues e; from is the plugin whose handler produced it (nil for the ui).
func (r *Runtime) emit(e pluginapi.Event, from *plugin) {
	if r.holdReady(e) {
		return
	}
	if e.Name == "buffer_closed" {
		// The handle of a closed buffer leaves every state's cache whether or not anyone
		// listens (a plugin that still holds it sees a dead handle). The handler gets the
		// old id as a plain number, not a handle: converting it would re-create the entry
		// just dropped. Data["id"] may be any integer type or a BufferID.
		if id, ok := wholeNumber(e.Data["id"]); ok && id > 0 {
			for _, p := range r.order {
				p.dropHandle(kindBuffer, uint64(id))
			}
			data := make(map[string]any, len(e.Data))
			for k, v := range e.Data {
				data[k] = v
			}
			data["id"] = id
			e.Data = data
		}
	}
	if e.Target != "" {
		if p := r.plugins[e.Target]; p == nil || len(p.subs[e.Name]) == 0 {
			return
		}
	} else if !r.Wants(e.Name) {
		return
	}
	r.evq = append(r.evq, queuedEvent{e: e, from: from, hot: hotEvents[e.Name]})
}

// Veto runs the before_file_save handlers synchronously (25 ms each, no yield) and
// reports whether one of them cancelled the action. Called in a slice it returns
// (false, "") without running Lua.
func (r *Runtime) Veto(e pluginapi.Event) (cancel bool, reason string) {
	defer r.enter("Veto")()
	if r.depth > 0 {
		r.logf("", levelWarn, "veto skipped in slice: "+e.Name)
		return false, ""
	}
	for _, p := range append([]*plugin(nil), r.order...) {
		if p.muted() || (e.Target != "" && e.Target != p.name) {
			continue
		}
		for _, s := range append([]*subscription(nil), p.subs[e.Name]...) {
			if !s.active || !s.accepts(e) {
				continue
			}
			vals, rerr := r.callSync(p, s.fn, vetoBudget, "veto handler "+e.Name, p.toLua(e.Data))
			if rerr != nil {
				r.noteFailure(p, rerr)
				continue
			}
			if len(vals) > 0 && vals[0] == lua.LFalse {
				if len(vals) > 1 {
					if msg, ok := vals[1].(lua.LString); ok {
						reason = string(msg)
					}
				}
				return true, reason
			}
		}
	}
	return false, ""
}

// Owns reports whether action is the id of a command of a loaded plugin.
func (r *Runtime) Owns(action string) bool {
	c, _ := r.findCommand(action)
	return c != nil
}

// InSlice reports whether Lua of this runtime is on the Go stack right now.
func (r *Runtime) InSlice() bool { return r.depth > 0 }

// Run starts the plugin command action. The first slice runs synchronously (so a macro
// replay keeps its order) unless InSlice, in which case the request is queued FIFO and
// runs from Drain. The error is for an unknown command; an error inside the command is
// reported as a notice.
func (r *Runtime) Run(action string, args map[string]any, src pluginapi.CommandSource) error {
	defer r.enter("Run")()
	c, p := r.findCommand(action)
	if c == nil {
		return pluginapi.Errf(pluginapi.CodeNotFound, "unknown plugin command %q", action)
	}
	req := cmdReq{p: p, gen: p.gen, cmd: c, args: args, src: src}
	if r.depth > 0 {
		r.cmdq = append(r.cmdq, req)
		return nil
	}
	r.startCommand(req)
	return nil
}

// Resolve completes an async wait, or delivers one stream item. In a slice, and for
// event-class waits (timers, streams), it is queued and delivered by Drain; a
// user-class wait resumes its task at once.
func (r *Runtime) Resolve(c pluginapi.Completion) {
	defer r.enter("Resolve")()
	if r.depth > 0 {
		r.inbox = append(r.inbox, c)
		return
	}
	w := r.waits[c.Token]
	if w == nil {
		return
	}
	if w.task != nil && !w.task.origin.eventClass() && !c.More {
		r.startUser(userFactor * perUpdateCap)
		r.deliver(c)
		r.runUser()
		return
	}
	r.inbox = append(r.inbox, c)
}

// Drain runs what is queued: completions, queued commands, controls, then event and timer
// handlers round-robin over the plugins. Event-class work stops when budget (the
// per-Update cap, normally 100 ms) is spent; the remainder stays queued and more is true.
// User-triggered work is not subject to that cap, but a chain of user-class tasks that
// keeps re-queueing itself (gvim.async.spawn, command.run from a slice) is: after
// userFactor x budget of it Drain stops starting user-class slices and reports more, so the
// rest runs on later Updates. The returned Work was created meanwhile.
func (r *Runtime) Drain(budget time.Duration) (work []pluginapi.Work, more bool) {
	defer r.enter("Drain")()
	if r.depth > 0 {
		return nil, true
	}
	if budget <= 0 {
		budget = perUpdateCap
	}
	r.startUser(userFactor * budget)
	var spent time.Duration
	invocations := 0
	rounds := 0
	for rounds < maxDrainRounds && r.hasPending() {
		rounds++
		r.drainInbox()
		r.runUser()
		r.drainCmdq()
		r.runUser()
		r.drainCtl()
		r.expandEvents(&invocations)
		spent += r.runEvents(budget - spent)
		if spent >= budget || invocations >= maxInvocations || r.userExhausted() {
			break
		}
	}
	if (rounds >= maxDrainRounds || invocations >= maxInvocations) && spent < budget && !r.userExhausted() {
		r.dropHotEvents()
		r.noteStorm()
	} else if rounds < maxDrainRounds && invocations < maxInvocations {
		r.endStorms()
	}
	r.noteUserStorm(r.userExhausted() || rounds >= maxDrainRounds)
	work, r.work = r.work, nil
	return work, r.hasPending()
}

// Plugins lists the plugins for the manager, in load order.
func (r *Runtime) Plugins() []pluginapi.PluginInfo { return r.pluginRows() }

// Log returns the plugin log, oldest first.
func (r *Runtime) Log() []pluginapi.LogEntry { return r.log.snapshot() }

// Control runs a lifecycle request. In a slice it only queues the request and returns
// (nil, nil); the outcome then goes out as notices.
func (r *Runtime) Control(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
	defer r.enter("Control")()
	if r.depth > 0 {
		r.ctlq = append(r.ctlq, c)
		return nil, nil
	}
	w, n := r.doControl(c)
	w = append(w, r.work...)
	r.work = nil
	return w, n
}

// ---------- queues ----------

type cmdReq struct {
	p    *plugin
	gen  uint64
	cmd  *command
	args map[string]any
	src  pluginapi.CommandSource
}

type queuedEvent struct {
	e    pluginapi.Event
	from *plugin
	hot  bool
}

// hotEvents may be dropped by the cascade cap and are computed only while subscribed;
// every other event is lifecycle and is never dropped.
var hotEvents = map[string]bool{
	"cursor_moved": true, "selection_changed": true, "viewport_changed": true,
	"buffer_changed": true, "buffer_modified": true, "pre_command": true,
	"post_command": true, "idle": true,
}

func (r *Runtime) hasPending() bool {
	if len(r.inbox)+len(r.cmdq)+len(r.ctlq)+len(r.userq)+len(r.evq) > 0 {
		return true
	}
	for _, p := range r.order {
		if len(p.evq) > 0 {
			return true
		}
	}
	return false
}

func (r *Runtime) drainInbox() {
	in := r.inbox
	r.inbox = nil
	for _, c := range in {
		r.deliver(c)
	}
}

func (r *Runtime) drainCmdq() {
	q := r.cmdq
	r.cmdq = nil
	for i, req := range q {
		if r.userExhausted() {
			// Keep FIFO order: what is left goes before what the commands just queued.
			r.cmdq = append(append([]cmdReq(nil), q[i:]...), r.cmdq...)
			return
		}
		if req.p.dead || req.p.gen != req.gen {
			continue
		}
		start := time.Now()
		r.startCommand(req)
		r.userSpent += time.Since(start)
	}
}

func (r *Runtime) drainCtl() {
	q := r.ctlq
	r.ctlq = nil
	for _, c := range q {
		w, notices := r.doControl(c)
		r.work = append(r.work, w...)
		for _, n := range notices {
			lvl := pluginapi.LevelInfo
			if n.Warn {
				lvl = pluginapi.LevelWarn
			}
			r.host.Notify("", lvl, n.Text)
		}
	}
}

// userFactor times the per-Update cap is the wall time of user-class slices one Drain (or
// one user-class Resolve) may start. A single slice is never cut short by it.
const userFactor = 2

// startUser begins the accounting of user-class work with limit as its cap.
func (r *Runtime) startUser(limit time.Duration) {
	r.userSpent, r.userCap = 0, limit
}

func (r *Runtime) userExhausted() bool { return r.userSpent >= r.userCap }

// runUser runs the user-class tasks until none is left or the user-class time of this
// Drain is spent; the rest stays queued.
func (r *Runtime) runUser() {
	for n := 0; len(r.userq) > 0 && n < 100_000 && !r.userExhausted(); n++ {
		it := r.userq[0]
		r.userq = r.userq[1:]
		start := time.Now()
		r.runItem(it)
		r.userSpent += time.Since(start)
	}
}

// expandEvents turns queued events into handler tasks, at most maxInvocations per Drain.
func (r *Runtime) expandEvents(invocations *int) {
	q := r.evq
	r.evq = nil
	for i, qe := range q {
		if *invocations >= maxInvocations {
			r.evq = append(r.evq, q[i:]...)
			return
		}
		r.expandEvent(qe, invocations)
	}
}

func (r *Runtime) expandEvent(qe queuedEvent, invocations *int) {
	r.nextBatch++
	batch := r.nextBatch
	for _, p := range append([]*plugin(nil), r.order...) {
		if p.muted() || (qe.e.Target != "" && qe.e.Target != p.name) {
			continue
		}
		for _, s := range append([]*subscription(nil), p.subs[qe.e.Name]...) {
			if !s.active || !s.accepts(qe.e) {
				continue
			}
			if s.once {
				p.removeSub(s)
			}
			t, err := r.newTask(p, originEvent, s.fn, "event "+qe.e.Name)
			if err != nil {
				r.logf(p.name, levelWarn, "dropped event "+qe.e.Name+": "+err.Error())
				continue
			}
			t.sub = s
			p.evq = append(p.evq, item{t: t, args: []lua.LValue{p.toLua(qe.e.Data)}, batch: batch})
			*invocations++
		}
	}
}

// runEvents runs event-class tasks one per plugin in turn until none is left or left of
// the cap is spent. It returns the wall time used.
func (r *Runtime) runEvents(left time.Duration) time.Duration {
	var spent time.Duration
	for spent < left {
		ran := false
		for i := 0; i < len(r.order); i++ {
			n := len(r.order)
			p := r.order[(r.rr+i)%n]
			if p.dead || len(p.evq) == 0 {
				continue
			}
			r.rr = (r.rr + i + 1) % n
			start := time.Now()
			// The handlers of one plugin for one event run together, in subscription order;
			// plugins take turns per event.
			for first := true; len(p.evq) > 0 && (first || (r.lastBatch != 0 && p.evq[0].batch == r.lastBatch && time.Since(start)+spent < left)); first = false {
				it := p.evq[0]
				p.evq = p.evq[1:]
				r.lastBatch = it.batch
				r.runItem(it)
			}
			spent += time.Since(start)
			ran = true
			break
		}
		if !ran {
			break
		}
	}
	return spent
}

// dropHotEvents is the cascade cap: hot events still queued are dropped with a log line and
// a strike for the plugin whose handler produced them; lifecycle events stay.
func (r *Runtime) dropHotEvents() {
	kept := r.evq[:0]
	dropped := map[*plugin]int{}
	total := 0
	for _, qe := range r.evq {
		if !qe.hot {
			kept = append(kept, qe)
			continue
		}
		total++
		if qe.from != nil {
			dropped[qe.from]++
		}
	}
	r.evq = kept
	if total == 0 {
		return
	}
	r.logf("", levelWarn, "event storm: dropped "+strconv.Itoa(total)+" hot events")
	for p := range dropped {
		if !p.dead {
			r.strike(p, "event storm")
		}
	}
}

// noteStorm watches plugin events (lifecycle: never dropped) that are still queued after the
// cascade cap was hit. A finite chain ends within a few Drains; a plugin whose events are
// still queued after stormDrains Drains in a row is feeding itself: it gets a strike (and a
// log line), so the 3-strike breaker can suspend it. The events stay queued.
func (r *Runtime) noteStorm() {
	feeding := map[*plugin]bool{}
	for _, qe := range r.evq {
		if qe.from != nil {
			feeding[qe.from] = true
		}
	}
	for _, p := range r.order {
		if p.dead || !feeding[p] {
			p.stormDrains = 0
			continue
		}
		if p.stormDrains++; p.stormDrains < stormDrains {
			continue
		}
		p.stormDrains = 0
		r.logf(p.name, levelWarn, "event storm: its own events keep feeding the queue")
		r.strike(p, "event storm")
	}
}

// noteUserStorm is noteStorm for user-class work (tasks, spawn chains, command.run chains),
// which the event cascade cap does not cover. cut says this Drain stopped because its
// user-class time or its rounds were spent; a plugin that still has tasks or commands queued
// after userStormDrains such Drains in a row is feeding itself: it gets a strike, and the
// strike that suspends it also drops the work it has queued, which ends the chain.
func (r *Runtime) noteUserStorm(cut bool) {
	feeding := map[*plugin]bool{}
	if cut {
		for _, it := range r.userq {
			feeding[it.t.p] = true
		}
		for _, c := range r.cmdq {
			feeding[c.p] = true
		}
	}
	for _, p := range r.order {
		if p.dead || !feeding[p] {
			p.userStorm = 0
			continue
		}
		if p.userStorm++; p.userStorm < userStormDrains {
			continue
		}
		p.userStorm = 0
		r.logf(p.name, levelWarn, "task storm: its own tasks keep feeding the queue")
		if _, suspended := r.strike(p, "task storm"); suspended {
			r.dropQueuedUser(p)
		}
	}
}

// dropQueuedUser retires the queued tasks and drops the queued commands of p.
func (r *Runtime) dropQueuedUser(p *plugin) {
	for _, it := range r.userq {
		if it.t.p == p {
			r.retire(it.t)
		}
	}
	r.userq = filterItems(r.userq, p)
	r.cmdq = filterCmds(r.cmdq, p)
}

// endStorms ends every streak: a Drain finished under the cascade cap.
func (r *Runtime) endStorms() {
	for _, p := range r.order {
		p.stormDrains = 0
	}
}

// deliver hands a completion to its wait.
func (r *Runtime) deliver(c pluginapi.Completion) {
	w := r.waits[c.Token]
	if w == nil {
		return
	}
	if w.p != nil && (w.p.dead || w.p.gen != w.gen) {
		r.dropWait(w)
		return
	}
	if c.More {
		if w.onItem != nil {
			w.onItem(c)
		}
		return
	}
	r.dropWait(w)
	switch {
	case w.task != nil:
		t := w.task
		t.waiting = 0
		r.enqueue(item{t: t, args: fixedShape(awaitKind{variadic: w.vari}, w.conv(c)...)})
	case w.onItem != nil:
		w.onItem(c)
	}
}
