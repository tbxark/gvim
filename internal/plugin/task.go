package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// origin says what started a task; it selects the budget class and is kept when the
// task is resumed.
type origin int

const (
	originLoad     origin = iota // the main chunk of a plugin
	originCommand                // palette, key, menu, command.run, mode key handler
	originCallback               // callback after user interaction (prompt, picker, popup, next_key)
	originEvent                  // an event handler
	originTimer                  // timers, process/fs/watch callbacks, status item click
)

func (o origin) eventClass() bool { return o == originEvent || o == originTimer }

// task is a root coroutine of a plugin state: every entry point (main chunk, command,
// handler, timer, callback) runs as one. Only roots are resumed by the host; nested
// coroutines forward their awaits to the root (see lua/prelude.lua).
type task struct {
	id           uint64
	p            *plugin
	root         *lua.LState
	cancelThread context.CancelFunc
	origin       origin
	label        string // for messages: "command foo.bar", "event buffer_activated"
	fn           *lua.LFunction
	gofn         *lua.LFunction // the Go function behind fn (see newTask)

	started, running bool
	done, cancelled  bool
	failed           bool
	results          []lua.LValue // return values when done
	errv             lua.LValue   // error object when failed
	waiting          pluginapi.Token
	waiters          []pluginapi.Token // tasks parked in Task:wait on this one
	sub              *subscription     // the subscription this event task serves
	xdepth           int               // nesting of gvim.plugins calls that started this task
}

// item is a runnable unit: start the task (first item) or resume it with args.
type item struct {
	t     *task
	args  []lua.LValue
	batch uint64 // event handler tasks: the (event, plugin) they belong to; 0 for none
}

// wait is one parked async operation. A wait with a task resumes it; a wait with only
// onItem is a stream or a runtime-level wait.
type wait struct {
	token  pluginapi.Token
	p      *plugin // nil for runtime-level waits
	gen    uint64
	task   *task
	target string                                  // plugin whose state serves a cross-plugin call
	conv   func(pluginapi.Completion) []lua.LValue // resume arguments, leading ok flag
	onItem func(pluginapi.Completion)              // intermediate items (More) and stream/final items
	cancel func()                                  // abandon: unload, panic chord
	work   bool                                    // counts against the Work quota
	vari   bool                                    // the awaiting wrapper takes any number of values
}

// newToken allocates a wait token.
func (r *Runtime) newToken() pluginapi.Token {
	r.nextTok++
	return r.nextTok
}

// expectRuntime registers a runtime-level wait: f is called on the UI goroutine with the
// completion of tok (and with each stream item). Used by the loader for its Work.
func (r *Runtime) expectRuntime(tok pluginapi.Token, f func(pluginapi.Completion)) {
	r.waits[tok] = &wait{token: tok, onItem: f}
}

func (r *Runtime) dropWait(w *wait) {
	if r.waits[w.token] != w {
		return
	}
	delete(r.waits, w.token)
	if w.p != nil {
		if w.task != nil {
			w.p.parked--
		}
		if w.work {
			w.p.inflight--
		}
	}
}

// cancelWait abandons a wait without delivering anything.
func (r *Runtime) cancelWait(w *wait) {
	r.dropWait(w)
	if w.cancel != nil {
		w.cancel()
	}
}

// ---------- budgets ----------

func (r *Runtime) budget(o origin) time.Duration {
	switch {
	case o == originLoad:
		return min(4*r.timeout, maxLoadBudget)
	case o.eventClass():
		return min(r.eventCap, r.timeout)
	}
	return r.timeout
}

// ---------- creating tasks ----------

func (r *Runtime) newTask(p *plugin, o origin, fn *lua.LFunction, label string) (*task, error) {
	if p.dead || p.L == nil {
		return nil, pluginapi.Errf(pluginapi.CodeClosed, "plugin %q is not loaded", p.name)
	}
	if len(p.tasks) >= maxTasks {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "too many tasks (%d)", maxTasks)
	}
	root, cancel := p.L.NewThread()
	r.nextTsk++
	t := &task{id: r.nextTsk, p: p, root: root, cancelThread: cancel, origin: o, label: label, fn: fn}
	if fn.IsG {
		// A root whose body is a Go function loses its results and fails with a nil error:
		// run it through the prelude's call, which is a Lua function.
		if call, ok := p.pre.RawGetString("call").(*lua.LFunction); ok {
			t.fn, t.gofn = call, fn
		}
	}
	p.tasks[t] = struct{}{}
	p.roots[root] = t
	return t, nil
}

func (r *Runtime) enqueue(it item) {
	if it.t.origin.eventClass() {
		it.t.p.evq = append(it.t.p.evq, it)
		return
	}
	r.userq = append(r.userq, it)
}

// startCallback creates a task for fn and queues it; the next Drain runs it.
func (r *Runtime) startCallback(p *plugin, o origin, label string, fn *lua.LFunction, args ...lua.LValue) {
	t, err := r.newTask(p, o, fn, label)
	if err != nil {
		r.logf(p.name, levelWarn, "dropped "+label+": "+err.Error())
		return
	}
	r.enqueue(item{t: t, args: args})
}

// startCommand builds the ctx table and runs the first slice of the command now.
func (r *Runtime) startCommand(req cmdReq) {
	p := req.p
	fn, ok := req.cmd.fn.(*lua.LFunction)
	if !ok || p.dead {
		return
	}
	t, err := r.newTask(p, originCommand, fn, "command "+req.cmd.id)
	if err != nil {
		r.logf(p.name, levelWarn, "dropped command "+req.cmd.id+": "+err.Error())
		return
	}
	r.runTask(t, []lua.LValue{p.commandCtx(req)})
}

// commandCtx is the table passed to a command function: {args, count, source, buf}.
func (p *plugin) commandCtx(req cmdReq) *lua.LTable {
	ctx := p.L.CreateTable(0, 4)
	args := p.toLua(req.args)
	if _, ok := args.(*lua.LTable); !ok {
		args = p.L.NewTable()
	}
	ctx.RawSetString("args", args)
	count := lua.LNumber(1)
	if n, ok := wholeNumber(req.args["count"]); ok && n > 0 {
		count = lua.LNumber(n)
	}
	ctx.RawSetString("count", count)
	ctx.RawSetString("source", lua.LString(req.src))
	if id := p.r.host.ActiveBuffer(); id != 0 {
		ctx.RawSetString("buf", p.toLua(id))
	}
	return ctx
}

func (r *Runtime) runItem(it item) {
	t := it.t
	if t.done || t.p.dead {
		return
	}
	if !t.started && t.origin.eventClass() && t.p.muted() {
		r.retire(t)
		return
	}
	r.runTask(t, it.args)
}

// ---------- running ----------

type resumeResult struct {
	// yielded: some coroutine of the plugin yielded during this resume. A failure right
	// after a yield is the signature of a yield inside a Go callback.
	yielded bool
	st      lua.ResumeState
	err     error
	vals    []lua.LValue
	panic   any
	stack   string
}

// resume runs the root until it yields, returns or fails, under sl. A Go panic (also the
// re-panic out of Resume of a corrupted coroutine) is returned, never propagated.
func (r *Runtime) resume(t *task, sl *slice, args []lua.LValue) (res resumeResult) {
	p := t.p
	t.root.SetContext(sl.ctx)
	prevCur, prevP, prevT := r.cur, r.curPlugin, r.curTask
	r.cur, r.curPlugin, r.curTask = sl, p, t
	r.depth++
	t.running = true
	top := p.L.GetTop()
	defer func() {
		t.running = false
		r.depth--
		r.cur, r.curPlugin, r.curTask = prevCur, prevP, prevT
		if rec := recover(); rec != nil {
			res = resumeResult{st: lua.ResumeError, panic: rec, yielded: p.yielded}
			if r.debug {
				res.stack = stackString()
			}
			// A panic out of Resume leaves the main state believing the dead root still
			// runs and with a stack above where it was: put both back.
			p.L.G.CurrentThread = p.L
			p.L.SetTop(top)
		}
	}()
	fn := t.fn
	if !t.started && t.gofn != nil {
		args = append([]lua.LValue{t.gofn}, args...)
	}
	p.yielded = false
	p.goPanic = nil
	if t.started {
		fn = nil
	}
	t.started = true
	res.st, res.err, res.vals = p.L.Resume(t.root, fn, args...)
	res.yielded = p.yielded
	return res
}

// runTask runs one slice of t: resume under a fresh budget, serve inline awaits without
// ending the slice, and stop when the task parks, returns or fails.
func (r *Runtime) runTask(t *task, args []lua.LValue) {
	p := t.p
	sl := r.newSlice(p.pctx, r.budget(t.origin))
	defer sl.finish()
	for {
		res := r.resume(t, sl, args)
		if t.cancelled {
			r.retire(t)
			r.settleWaiters(t)
			return
		}
		switch {
		case res.panic != nil || res.err != nil:
			r.failTask(t, r.classify(t, sl, res))
			return
		case res.st == lua.ResumeOK:
			r.finishTask(t, res.vals)
			return
		}
		next, parked := r.handleYield(t, sl, res.vals)
		if parked {
			return
		}
		args = next
	}
}

// handleYield serves the await the task yielded. It returns the resume arguments when the
// await completed inline, or parked = true when the task is parked or was aborted.
func (r *Runtime) handleYield(t *task, sl *slice, vals []lua.LValue) (args []lua.LValue, parked bool) {
	p := t.p
	kind, ok := yieldKind(p, vals)
	k, known := awaitKinds[kind]
	if !ok || !known {
		r.failTask(t, &runError{kind: failInvalidYield, msg: "invalid yield (not an async API call)"})
		return nil, true
	}
	c := &awaitCall{r: r, p: p, t: t, args: vals[2:]}
	if rec := r.callAwait(k.f, c, sl); rec != nil {
		r.failTask(t, &runError{kind: failPanic, msg: fmt.Sprint("panic in async call ", kind, ": ", rec)})
		return nil, true
	}
	switch {
	case c.park != nil:
		if err := r.park(t, c.park, k.variadic); err != nil {
			if c.park.Cancel != nil {
				c.park.Cancel()
			}
			r.logf(p.name, levelWarn, "await "+kind+": "+err.Error())
			return fixedShape(k, lua.LTrue, lua.LNil, lua.LString(err.Error())), false
		}
		return nil, true
	case c.throw != nil:
		return fixedShape(k, lua.LFalse, c.throw), false
	case c.raise != "":
		return fixedShape(k, lua.LFalse, lua.LString(p.awaitWhere()+c.raise)), false
	case c.err != nil:
		return fixedShape(k, lua.LTrue, lua.LNil, lua.LString(errString(c.err))), false
	}
	return fixedShape(k, append([]lua.LValue{lua.LTrue}, c.vals...)...), false
}

// fixedShape makes the resume arguments for the wrapper of k. The wrapper of an ordinary
// kind reads exactly three values (ok, value, err) into locals: gopher-lua leaves a slot
// the resume did not fill uninitialised and a later read crashes, so the arguments are
// padded with nil (and cut) to three. A variadic wrapper takes any number.
func fixedShape(k awaitKind, vals ...lua.LValue) []lua.LValue {
	if k.variadic {
		return vals
	}
	out := make([]lua.LValue, 3)
	for i := range out {
		out[i] = lua.LNil
		if i < len(vals) {
			out[i] = vals[i]
		}
	}
	return out
}

// yieldKind checks the tag of a yield and returns its kind.
func yieldKind(p *plugin, vals []lua.LValue) (string, bool) {
	if len(vals) < 2 || vals[0] != lua.LValue(p.tag) {
		return "", false
	}
	k, ok := vals[1].(lua.LString)
	return string(k), ok
}

// callAwait runs an await handler on the UI goroutine under the slice, recovering panics.
func (r *Runtime) callAwait(f awaitFunc, c *awaitCall, sl *slice) (rec any) {
	prevCur, prevP, prevT := r.cur, r.curPlugin, r.curTask
	r.cur, r.curPlugin, r.curTask = sl, c.p, c.t
	r.depth++
	defer func() {
		r.depth--
		r.cur, r.curPlugin, r.curTask = prevCur, prevP, prevT
		rec = recover()
	}()
	f(c)
	return nil
}

// park registers the wait of a task and queues the Work, enforcing the quotas.
func (r *Runtime) park(t *task, s *parkSpec, variadic bool) error {
	p := t.p
	if err := p.checkPark(s.Work != nil); err != nil {
		return err
	}
	tok := s.Token
	if tok == 0 {
		tok = r.newToken()
	}
	conv := s.Convert
	if conv == nil {
		conv = p.defaultConvert
	}
	w := &wait{token: tok, p: p, gen: p.gen, task: t, target: s.Target, conv: conv, onItem: s.OnItem, cancel: s.Cancel, work: s.Work != nil, vari: variadic}
	r.waits[tok] = w
	t.waiting = tok
	p.parked++
	if s.Work != nil {
		wk := *s.Work
		wk.Token, wk.Plugin, wk.Gen, wk.Ctx = tok, p.name, p.gen, p.pctx
		// The closures run on a tea.Cmd goroutine: a panic there would take the editor down, so
		// it becomes an error completion.
		if run := wk.Run; run != nil {
			wk.Run = func(ctx context.Context) (c pluginapi.Completion) {
				defer func() {
					if rec := recover(); rec != nil {
						c = pluginapi.Completion{Err: fmt.Sprint(pluginapi.CodeIO, ": internal error: ", rec)}
					}
					c.Token = tok
				}()
				return run(ctx)
			}
		}
		if stream := wk.Stream; stream != nil {
			wk.Stream = func(ctx context.Context) (c pluginapi.Completion, ok bool) {
				defer func() {
					if rec := recover(); rec != nil {
						c, ok = pluginapi.Completion{Err: fmt.Sprint(pluginapi.CodeIO, ": internal error: ", rec)}, false
					}
					c.Token = tok
				}()
				return stream(ctx)
			}
		}
		p.inflight++
		r.work = append(r.work, wk)
	}
	return nil
}

// checkPark reports whether the plugin may park another task (and queue one more Work).
func (p *plugin) checkPark(withWork bool) error {
	if p.parked >= QuotaParkedTasks {
		return pluginapi.Errf(pluginapi.CodeLimit, "parked task quota (%d)", QuotaParkedTasks)
	}
	if withWork && p.inflight >= QuotaWork {
		return pluginapi.Errf(pluginapi.CodeLimit, "in-flight work quota (%d)", QuotaWork)
	}
	return nil
}

// defaultConvert turns a completion into resume arguments: ok, then the values, or
// ok, nil, err.
func (p *plugin) defaultConvert(c pluginapi.Completion) []lua.LValue {
	if c.Err != "" {
		return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(c.Err)}
	}
	out := make([]lua.LValue, 1, 1+len(c.Values))
	out[0] = lua.LTrue
	for _, v := range c.Values {
		out = append(out, p.toLua(v))
	}
	return out
}

// ---------- finishing ----------

// retire removes a finished or aborted task from its plugin.
func (r *Runtime) retire(t *task) {
	t.done = true
	p := t.p
	delete(p.tasks, t)
	delete(p.roots, t.root)
	if t.waiting != 0 {
		if w := r.waits[t.waiting]; w != nil {
			r.cancelWait(w)
		}
		t.waiting = 0
	}
	if t.cancelThread != nil {
		t.cancelThread()
	}
}

func (r *Runtime) finishTask(t *task, vals []lua.LValue) {
	t.results = vals
	r.retire(t)
	if t.sub != nil {
		t.sub.failures = 0
	}
	if t.origin == originLoad {
		t.p.mainFinished()
	}
	r.settleWaiters(t)
}

// settleWaiters wakes the tasks parked in Task:wait on t.
func (r *Runtime) settleWaiters(t *task) {
	for _, tok := range t.waiters {
		r.inbox = append(r.inbox, pluginapi.Completion{Token: tok})
	}
	t.waiters = nil
}

// cancelTask is Task:cancel.
func (r *Runtime) cancelTask(t *task) {
	if t.done || t.cancelled {
		return
	}
	t.cancelled = true
	if t.running {
		return // dropped when the slice ends
	}
	r.retire(t)
	r.settleWaiters(t)
}

// ---------- failures ----------

type failKind int

const (
	failLua failKind = iota
	failTimeout
	failMemory
	failCancelled
	failPanic
	failInvalidYield
	failYieldCallback
)

// runError describes why a slice ended abnormally.
type runError struct {
	kind  failKind
	msg   string
	trace string
	slow  bool // a deadline that was mostly spent inside Host calls: not a strike
	obj   lua.LValue
	limit time.Duration
}

func (e *runError) Error() string { return e.msg }

// classify maps the outcome of a failed resume to a runError, using the cancellation
// cause of the slice instead of gopher-lua's own message (AMENDMENTS C10).
func (r *Runtime) classify(t *task, sl *slice, res resumeResult) *runError {
	re := &runError{kind: failLua, obj: lua.LNil, limit: sl.budget}
	var ae *lua.ApiError
	switch {
	case res.panic != nil:
		re.kind = failPanic
		re.msg = fmt.Sprint("panic: ", res.panic)
		re.trace = res.stack
	case t.p.goPanic != nil:
		// gopher-lua turned a panic of one of our Go functions into a Lua error; guarded()
		// noted it.
		re.kind = failPanic
		re.msg = fmt.Sprint("panic: ", t.p.goPanic.value)
		re.trace = t.p.goPanic.stack
	case errors.As(res.err, &ae):
		re.obj = ae.Object
		re.msg = errorText(ae.Object)
		re.trace = ae.StackTrace
		if re.trace == "" {
			re.trace = traceback(t.root)
		}
	default:
		re.msg = res.err.Error()
	}
	// A yield inside a function gopher-lua calls from Go (sort comparator, gsub callback,
	// metamethod, for-in iterator) corrupts the call: it surfaces as "can not yield" or, after
	// the resume, as a nil dereference. Report what the plugin did.
	if strings.Contains(re.msg, "can not yield from outside of a coroutine") ||
		(res.yielded && strings.Contains(re.msg, "invalid memory address or nil pointer dereference")) {
		re.kind = failYieldCallback
		re.msg = errAwaitInGoCallback + " [" + re.msg + "]"
		return re
	}
	if res.panic != nil {
		return re
	}
	pos := ""
	if i := strings.Index(re.msg, ": context "); i > 0 {
		pos = re.msg[:i] + ": "
	}
	switch cause := context.Cause(sl.ctx); {
	case errors.Is(cause, errSliceDeadline), errors.Is(cause, errHardDeadline):
		re.kind = failTimeout
		re.slow = sl.hostTime > 0 && sl.hostTime*2 > sl.elapsed()
		if re.slow {
			re.msg = pos + "slow host call (" + fmt.Sprint(sl.hostTime.Milliseconds()) + " ms in the editor)"
		} else {
			re.msg = pos + "timed out (" + fmt.Sprint(sl.budget.Milliseconds()) + " ms)"
		}
	case errors.Is(cause, errMemory):
		re.kind = failMemory
		re.msg = pos + "memory limit"
	case errors.Is(cause, errCancelled):
		re.kind = failCancelled
		re.msg = pos + "cancelled"
	}
	return re
}

// failTask ends a task after an error and reports it.
func (r *Runtime) failTask(t *task, re *runError) {
	t.failed = true
	t.errv = re.obj
	if t.errv == nil || t.errv == lua.LNil {
		t.errv = lua.LString(re.msg)
	}
	r.retire(t)
	p := t.p
	if t.sub != nil {
		t.sub.failures++
		if t.sub.failures >= maxFailureStreak && t.sub.active {
			p.removeSub(t.sub)
			r.logf(p.name, levelWarn, fmt.Sprintf("unsubscribed %s after %d consecutive failures", t.sub.event, t.sub.failures))
		}
	}
	if t.origin == originLoad {
		r.logf(p.name, levelError, "load failed: "+re.msg+traceSuffix(re.trace))
		r.unload(p, unloadOptions{final: statusFailed, err: re.msg})
	} else {
		r.report(p, t.label, re, t.budgetLimit())
	}
	r.settleWaiters(t)
}

func (t *task) budgetLimit() time.Duration { return t.p.r.budget(t.origin) }

// noteFailure reports the failure of a synchronous call (veto, validate, shutdown).
func (r *Runtime) noteFailure(p *plugin, re *runError) { r.report(p, "callback", re, re.limit) }

// report logs a failure, applies the strike rules and tells the user.
func (r *Runtime) report(p *plugin, label string, re *runError, limit time.Duration) {
	r.logf(p.name, levelError, label+": "+re.msg+traceSuffix(re.trace))
	switch re.kind {
	case failCancelled:
		return
	case failTimeout, failMemory, failPanic, failInvalidYield:
		if re.slow {
			r.notify(p, pluginapi.LevelWarn, p.name+": "+label+": "+firstLine(re.msg))
			return
		}
		n, suspended := r.strike(p, label)
		text := ""
		switch re.kind {
		case failTimeout:
			text = fmt.Sprintf("%s: %s timed out (%d ms), strike %d/%d", p.name, label, limit.Milliseconds(), min(n, strikeLimit), strikeLimit)
		case failMemory:
			text = fmt.Sprintf("%s: %s stopped by the memory limit, strike %d/%d", p.name, label, min(n, strikeLimit), strikeLimit)
		default:
			text = fmt.Sprintf("%s: %s failed (%s), strike %d/%d", p.name, label, firstLine(re.msg), min(n, strikeLimit), strikeLimit)
		}
		r.notify(p, pluginapi.LevelError, text)
		if suspended {
			r.notify(p, pluginapi.LevelError, fmt.Sprintf("%s suspended after %d strikes (Plugin: Manager)", p.name, strikeLimit))
		}
	default:
		r.notify(p, pluginapi.LevelError, p.name+": "+firstLine(re.msg))
	}
}

func traceSuffix(trace string) string {
	if trace == "" {
		return ""
	}
	return "\n" + trace
}

// notify shows a notice, once per (plugin, text) per noticeDedupe.
func (r *Runtime) notify(p *plugin, level pluginapi.Level, text string) {
	key := p.name + "\x00" + text
	now := r.now()
	if at, ok := r.noticeAt[key]; ok && now.Sub(at) < noticeDedupe {
		return
	}
	if len(r.noticeAt) > 256 {
		clear(r.noticeAt)
	}
	r.noticeAt[key] = now
	r.host.Notify(p.name, level, clipMsg(text, maxNotify))
}

// strike records a timeout, panic or memory cancel of p and suspends it at strikeLimit
// strikes within strikeWindow. It returns the strikes in the window.
func (r *Runtime) strike(p *plugin, why string) (n int, suspended bool) {
	now := r.now()
	kept := p.strikes[:0]
	for _, at := range p.strikes {
		if now.Sub(at) < strikeWindow {
			kept = append(kept, at)
		}
	}
	p.strikes = append(kept, now)
	n = len(p.strikes)
	if n >= strikeLimit && p.status != statusSuspended && p.status != statusFailed {
		p.status = statusSuspended
		r.wantsChanged()
		p.evq = nil
		_ = r.host.SetInputMode(p.name, "")
		r.logf(p.name, levelError, "suspended: "+why)
		return n, true
	}
	return n, false
}

// traceback renders the frames of a (dead) thread, for coroutine errors whose ApiError
// carries no stack trace.
func traceback(th *lua.LState) string {
	var sb strings.Builder
	for lvl := 0; lvl < 30; lvl++ {
		dbg, ok := th.GetStack(lvl)
		if !ok {
			break
		}
		if _, err := th.GetInfo("Sln", dbg, lua.LNil); err != nil {
			break
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "\t%s:%d: in %s %q", dbg.Source, dbg.CurrentLine, dbg.What, dbg.Name)
	}
	return sb.String()
}

// awaitWhere is the position of the plugin code that made the await being served: the
// thread that yielded is parked with its frames intact.
func (p *plugin) awaitWhere() string {
	if p.awaitFrom == nil {
		return ""
	}
	return callerWhere(p.awaitFrom)
}

// callerWhere returns "<chunk>:<line>: " of the innermost plugin frame of a parked
// thread, skipping the runtime's own prelude wrappers; "" when unknown.
func callerWhere(th *lua.LState) string {
	for lvl := 0; lvl < 8; lvl++ {
		dbg, ok := th.GetStack(lvl)
		if !ok {
			return ""
		}
		if _, err := th.GetInfo("Sl", dbg, lua.LNil); err != nil {
			return ""
		}
		if dbg.What != "G" && dbg.Source != preludeChunk && dbg.CurrentLine > 0 {
			return fmt.Sprintf("%s:%d: ", dbg.Source, dbg.CurrentLine)
		}
	}
	return ""
}

// ---------- synchronous calls ----------

// callSync runs fn to completion on a fresh root within budget. A yield is an error: the
// callers (veto, validate, shutdown, on_unload) must not suspend.
func (r *Runtime) callSync(p *plugin, fn *lua.LFunction, budget time.Duration, label string, args ...lua.LValue) ([]lua.LValue, *runError) {
	if r.depth > 0 {
		return nil, &runError{kind: failLua, msg: "busy: synchronous call inside a slice"}
	}
	t, err := r.newTask(p, originCallback, fn, label)
	if err != nil {
		return nil, &runError{kind: failLua, msg: err.Error()}
	}
	sl := r.newSlice(p.pctx, budget)
	defer sl.finish()
	res := r.resume(t, sl, args)
	if res.panic == nil && res.err == nil && res.st == lua.ResumeYield {
		r.retire(t)
		return nil, &runError{kind: failYieldCallback, msg: "async call inside a Go callback: " + label + " must not yield"}
	}
	if res.panic != nil || res.err != nil {
		re := r.classify(t, sl, res)
		r.retire(t)
		return nil, re
	}
	r.retire(t)
	return res.vals, nil
}

// callNested calls fn from a Go function running on L (an API function that takes a
// callback, such as undo_group) on a fresh coroutine under L's context. The callback
// must not yield; a yield is an error.
func (p *plugin) callNested(L *lua.LState, fn *lua.LFunction, args ...lua.LValue) ([]lua.LValue, error) {
	if p.coDepth >= maxCoDepth {
		return nil, errors.New("stack overflow: coroutines are nested too deeply")
	}
	th, cancel := L.NewThread()
	if cancel != nil {
		defer cancel()
	}
	if ctx := L.Context(); ctx != nil {
		th.SetContext(ctx)
	}
	p.coDepth++
	defer func() { p.coDepth-- }()
	if fn.IsG {
		if call, ok := p.pre.RawGetString("call").(*lua.LFunction); ok {
			fn, args = call, append([]lua.LValue{fn}, args...)
		}
	}
	st, err, vals := L.Resume(th, fn, args...)
	switch {
	case err != nil:
		return nil, err
	case st == lua.ResumeYield:
		return nil, errors.New("async call inside a Go callback: the callback must not yield")
	}
	return vals, nil
}

// ---------- await handlers ----------

// awaitFunc serves one kind of async API call on the UI goroutine. It must finish the call
// with exactly one of Return, Fail, Bad or Park.
type awaitFunc func(c *awaitCall)

// awaitKind is a registered kind: the handler, and whether its Lua function returns any
// number of values (asyncV) instead of at most two (async).
type awaitKind struct {
	f        awaitFunc
	variadic bool
}

// awaitKinds is the allow-list of yield kinds. API files register their kinds from init.
var awaitKinds = map[string]awaitKind{}

// registerAwait registers a kind whose Lua function returns (value, err).
func registerAwait(kind string, f awaitFunc) { registerKind(kind, awaitKind{f: f}) }

// registerAwaitV registers a kind whose Lua function returns any number of values.
func registerAwaitV(kind string, f awaitFunc) { registerKind(kind, awaitKind{f: f, variadic: true}) }

func registerKind(kind string, k awaitKind) {
	if _, dup := awaitKinds[kind]; dup {
		panic("plugin: await kind registered twice: " + kind)
	}
	awaitKinds[kind] = k
}

// awaitCall is one async API call as the handler sees it.
type awaitCall struct {
	r    *Runtime
	p    *plugin
	t    *task
	args []lua.LValue // the arguments of the Lua call

	vals  []lua.LValue
	err   error
	raise string
	throw lua.LValue
	park  *parkSpec
}

// parkSpec parks the calling task until a Completion for its token arrives.
type parkSpec struct {
	// Token is allocated with NewToken before a Host async call (the host resolves it);
	// 0 lets the runtime allocate one.
	Token pluginapi.Token
	// Work is IO for the ui to run; the runtime fills in its token, plugin and context.
	Work *pluginapi.Work
	// Convert makes the resume arguments (leading ok flag) from the completion; nil means
	// the default: ok, values... or ok, nil, err.
	Convert func(pluginapi.Completion) []lua.LValue
	// OnItem receives intermediate items (Completion.More) of an interaction stream.
	OnItem func(pluginapi.Completion)
	// Cancel abandons the operation (unload, panic chord), e.g. Host.CancelWait.
	Cancel func()
	// Target names the plugin whose state serves a cross-plugin call; unloading it
	// resolves the wait with a closed error.
	Target string
}

// Return completes the call at once with up to two values.
func (c *awaitCall) Return(vals ...lua.LValue) { c.vals = vals }

// Fail completes the call with the expected failure `nil, "<code>: <msg>"`.
func (c *awaitCall) Fail(err error) { c.err = err }

// Bad raises a programmer error in the calling task: bad argument #n to 'name' (msg).
func (c *awaitCall) Bad(n int, name, msg string) {
	c.raise = fmt.Sprintf("bad argument #%d to '%s' (%s)", n, name, msg)
}

// Throw raises v (any Lua value, for example a task's error object) in the calling task.
func (c *awaitCall) Throw(v lua.LValue) { c.throw = v }

// Park parks the task.
func (c *awaitCall) Park(s *parkSpec) { c.park = s }

// NewToken allocates a token for a Host async call.
func (c *awaitCall) NewToken() pluginapi.Token { return c.r.newToken() }

// CheckPark reports the quota error when the plugin may not park another task. Call it
// before a Host async call.
func (c *awaitCall) CheckPark(withWork bool) error { return c.p.checkPark(withWork) }
