package plugin

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.on, once, off, emit and the Subscription handle, plus the delivery rules of the
// event queue that depend on a subscription (AMENDMENTS C25):
//
//   - hot events can be dropped by the cascade cap, lifecycle events never (runtime.go);
//   - hot events of one buffer that are still queued for a subscription are merged into
//     one invocation: buffer_changed by concatenating the edits (sequential coordinates),
//     the others as "earliest previous, latest state";
//   - opts.buf, opts.debounce and opts.after_ms are filters of the subscription.

const kindSubscription = "Subscription"

// knownEvents are the names gvim.on accepts besides "<plugin>:<name>" (FINAL section 6).
var knownEvents = map[string]bool{
	"plugins_loaded": true, "ready": true, "shutdown": true, "resize": true,
	"after_file_open": true, "buffer_activated": true, "buffer_deactivated": true, "buffer_closed": true,
	"before_file_save": true, "after_file_save": true, "after_file_revert": true,
	"buffer_modified": true, "buffer_changed": true, "cursor_moved": true, "selection_changed": true,
	"viewport_changed": true, "language_changed": true, "config_changed": true, "theme_changed": true,
	"pre_command": true, "post_command": true, "idle": true, "mode_changed": true, "diagnostics_updated": true,
	"widget_event": true, "lsp_status_clicked": true, "lsp_server_error": true,
	"action_popup_result": true,
}

// customEventRE is the name of a plugin event: "<plugin>:<name>".
var customEventRE = regexp.MustCompile(`^([a-z][a-z0-9_]{0,39}):[a-z][a-z0-9_]{0,39}$`)

// mergeable events are the hot events that carry a buffer and are merged while queued.
var mergeable = map[string]bool{
	"cursor_moved": true, "selection_changed": true, "viewport_changed": true,
	"buffer_modified": true, "buffer_changed": true,
}

// Limits of the event API.
const (
	maxEmitPayload     = 64 << 10 // bytes of a gvim.emit payload
	maxQueuedEmits     = 1000     // events of one plugin waiting in the queue
	maxDebounce        = 60_000   // ms
	maxIdleAfter       = 3_600_000
	defaultIdleAfter   = 1000
	maxDebounceBuffers = 64        // pending debounced buffers per subscription
	maxMergedEdits     = 256       // records of a merged buffer_changed
	maxMergedBytes     = 256 << 10 // text of a merged buffer_changed
)

// subHandle is the payload of a Subscription handle.
type subHandle struct{ s *subscription }

func (h *subHandle) handleString() string { return h.s.event }

// installEvents installs gvim.on, gvim.once, gvim.off, gvim.emit and the Subscription methods.
func installEvents(e *apiEnv) {
	p, r, g := e.p, e.r, e.gvim

	methods := e.kind(kindSubscription).methods
	e.set(methods, "off", func(L *lua.LState) int {
		p.removeSub(checkObject(L, 1, kindSubscription).(*subHandle).s)
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "active", func(L *lua.LState) int {
		L.Push(lua.LBool(checkObject(L, 1, kindSubscription).(*subHandle).s.active))
		return 1
	})
	e.set(g, "off", func(L *lua.LState) int {
		p.removeSub(checkObject(L, 1, kindSubscription).(*subHandle).s)
		L.Push(lua.LTrue)
		return 1
	})
	for _, once := range []bool{false, true} {
		name := "on"
		if once {
			name = "once"
		}
		e.set(g, name, func(L *lua.LState) int { return p.subscribe(L, name, once) })
	}

	e.set(g, "emit", func(L *lua.LState) int {
		fail := func(err error) int {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		name := L.CheckString(1)
		m := customEventRE.FindStringSubmatch(name)
		if m == nil || m[1] != p.name {
			return fail(pluginapi.Errf(pluginapi.CodeInvalid, "event name must be %q, \"%s:<name>\" with [a-z][a-z0-9_]* names", p.name+":<name>", p.name))
		}
		var data map[string]any
		if v := L.Get(2); v != lua.LNil {
			t, ok := v.(*lua.LTable)
			if !ok {
				L.ArgError(2, "table expected, got "+v.Type().String())
			}
			x, err := p.fromLua(t)
			if err != nil {
				return fail(err)
			}
			if data, ok = x.(map[string]any); !ok {
				L.ArgError(2, "table with string keys expected")
			}
		}
		if payloadSize(data, maxEmitPayload) > maxEmitPayload {
			return fail(pluginapi.Errf(pluginapi.CodeLimit, "event payload is larger than %d KiB", maxEmitPayload>>10))
		}
		if r.queuedFrom(p) >= maxQueuedEmits {
			return fail(pluginapi.Errf(pluginapi.CodeLimit, "too many events waiting (max %d)", maxQueuedEmits))
		}
		r.emit(pluginapi.Event{Name: name, Data: data}, p)
		L.Push(lua.LTrue)
		return 1
	})
}

// eventOpts are the options of gvim.on.
type eventOpts struct {
	buf      uint64
	hasBuf   bool
	debounce time.Duration
	afterMS  float64
}

// subscribe implements gvim.on and gvim.once.
func (p *plugin) subscribe(L *lua.LState, api string, once bool) int {
	event := L.CheckString(1)
	fn := L.CheckFunction(2)
	if !knownEvents[event] && !customEventRE.MatchString(event) {
		L.RaiseError("gvim.%s: unknown event %q", api, clipMsg(event, 48))
	}
	o := parseEventOpts(L, event, 3)
	s, err := p.addSub(event, fn, once)
	if err != nil {
		v, msg := errTuple(err)
		L.Push(v)
		L.Push(msg)
		return 2
	}
	c := &subCtl{r: p.r, p: p, s: s, o: o, pend: map[uint64]*debState{}}
	if c.needsFilter() {
		s.filter = c.accept
	}
	L.Push(p.newObject(kindSubscription, &subHandle{s: s}))
	return 1
}

// parseEventOpts reads opts {buf, debounce, after_ms}; everything else is ignored.
func parseEventOpts(L *lua.LState, event string, n int) eventOpts {
	var o eventOpts
	if event == "idle" {
		o.afterMS = defaultIdleAfter
	}
	t := L.OptTable(n, nil)
	if t == nil {
		return o
	}
	if v := t.RawGetString("buf"); v != lua.LNil {
		ud, ok := v.(*lua.LUserData)
		var h idHandle
		if ok {
			h, ok = ud.Value.(idHandle)
		}
		if !ok || h.kind != kindBuffer {
			L.ArgError(n, "opts.buf must be a Buffer")
		}
		o.buf, o.hasBuf = h.id, true
	}
	if v := t.RawGetString("debounce"); v != lua.LNil {
		ms, ok := v.(lua.LNumber)
		if !ok || !(ms >= 1 && ms <= maxDebounce) {
			L.ArgError(n, "opts.debounce must be a number of milliseconds in 1.."+strconv.Itoa(maxDebounce))
		}
		if event == "before_file_save" {
			L.ArgError(n, "before_file_save handlers cannot be debounced")
		}
		o.debounce = time.Duration(float64(ms) * float64(time.Millisecond))
	}
	if v := t.RawGetString("after_ms"); v != lua.LNil {
		ms, ok := v.(lua.LNumber)
		if !ok || !(ms >= 1 && ms <= maxIdleAfter) {
			L.ArgError(n, "opts.after_ms must be a number of milliseconds in 1.."+strconv.Itoa(maxIdleAfter))
		}
		if event != "idle" {
			L.ArgError(n, "opts.after_ms applies to the idle event only")
		}
		o.afterMS = float64(ms)
	}
	return o
}

// subCtl is the per-subscription delivery state behind subscription.filter.
type subCtl struct {
	r *Runtime
	p *plugin
	s *subscription
	o eventOpts

	lastIdle  float64 // idle: ms of the previous idle event
	period    float64 // idle: "period" of the previous idle event
	hasPeriod bool    // idle: the previous event carried a period
	idleFired bool    // idle: the handler already ran in this idle period
	pend      map[uint64]*debState
	armed     bool // the debounce timer is out
}

// debState is one debounced buffer (id 0: events without a buffer).
type debState struct {
	tbl  *lua.LTable // the payload to deliver, merged while the window is open
	last time.Time   // the latest event
}

func (c *subCtl) needsFilter() bool {
	return c.o.hasBuf || c.o.debounce > 0 || c.s.event == "idle" || mergeable[c.s.event] ||
		(c.s.once && c.s.event == "before_file_save")
}

// accept is subscription.filter. It returns whether the event becomes a new handler
// invocation now: false when the event is filtered out, merged into an invocation that is
// still queued, or held for a debounce timer.
func (c *subCtl) accept(e pluginapi.Event) bool {
	if c.o.hasBuf {
		if id, ok := eventBuffer(e); !ok || id != c.o.buf {
			return false
		}
	}
	if c.s.event == "idle" && !c.idlePasses(e) {
		return false
	}
	switch {
	case c.o.debounce > 0:
		c.debounce(e)
		return false
	case mergeable[e.Name] && c.coalesce(e):
		return false
	}
	if c.s.once && e.Name == "before_file_save" {
		c.p.removeSub(c.s) // Veto does not remove once subscriptions itself
	}
	return true
}

// eventBuffer is the buffer an event is about: Data["buf"], or the old id of buffer_closed.
func eventBuffer(e pluginapi.Event) (uint64, bool) {
	v, ok := e.Data["buf"]
	if !ok && e.Name == "buffer_closed" {
		v, ok = e.Data["id"]
	}
	if !ok {
		return 0, false
	}
	n, ok := wholeNumber(v)
	if !ok || n < 0 {
		return 0, false
	}
	return uint64(n), true
}

// idlePasses applies opts.after_ms. The ui reports idle time as {ms, period}, possibly
// several times per idle period with growing values; the handler runs once per period, the
// first time ms reaches after_ms. A different period number starts a new period. ms is real
// elapsed time and jitters, so it cannot do that alone: the first ms of a short pause can be
// larger than the last of the one before. An event without a period (an emit from a test or
// another source) falls back to the ms rule: a value that does not grow starts a new one.
func (c *subCtl) idlePasses(e pluginapi.Event) bool {
	ms, ok := numberOf(e.Data["ms"])
	if !ok {
		return true
	}
	if per, has := numberOf(e.Data["period"]); has {
		if !c.hasPeriod || per != c.period {
			c.idleFired = false
		}
		c.period, c.hasPeriod = per, true
	} else if ms <= c.lastIdle {
		c.idleFired = false
	}
	c.lastIdle = ms
	if c.idleFired || ms < c.o.afterMS {
		return false
	}
	c.idleFired = true
	return true
}

func numberOf(v any) (float64, bool) {
	if n, ok := wholeNumber(v); ok {
		return float64(n), true
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	}
	return 0, false
}

// coalesce merges e into the invocation of this subscription for the same buffer that has
// not started yet, if there is one.
func (c *subCtl) coalesce(e pluginapi.Event) bool {
	id, ok := eventBuffer(e)
	if !ok {
		return false
	}
	evq := c.p.evq
	for i := len(evq) - 1; i >= 0; i-- {
		it := evq[i]
		if it.t.sub != c.s || it.t.started || it.t.done || len(it.args) != 1 {
			continue
		}
		tbl, ok := it.args[0].(*lua.LTable)
		if !ok {
			continue
		}
		if h, ok := tbl.RawGetString("buf").(*lua.LUserData); ok {
			if ih, ok := h.Value.(idHandle); ok && ih.id == id {
				c.p.mergeEvent(tbl, e)
				return true
			}
		}
	}
	return false
}

// mergeEvent folds e into the payload table of an invocation that has not run: the latest
// state wins and the earliest `previous` is kept; buffer_changed concatenates its edits.
func (p *plugin) mergeEvent(old *lua.LTable, e pluginapi.Event) {
	nt, ok := p.toLua(e.Data).(*lua.LTable)
	if !ok {
		return
	}
	if e.Name == "buffer_changed" {
		mergeChanged(old, nt)
		return
	}
	prev := old.RawGetString("previous")
	var stale []lua.LValue
	old.ForEach(func(k, _ lua.LValue) {
		if nt.RawGet(k) == lua.LNil {
			stale = append(stale, k)
		}
	})
	for _, k := range stale {
		old.RawSet(k, lua.LNil)
	}
	nt.ForEach(func(k, v lua.LValue) { old.RawSet(k, v) })
	old.RawSetString("previous", prev)
}

// mergeChanged appends the edits of nt to old. Records are in sequential coordinates, so
// arrival order is kept. The result overflows (edits=nil, overflow=true) when either side
// did, or when it would exceed maxMergedEdits records or maxMergedBytes of text.
func mergeChanged(old, nt *lua.LTable) {
	over := lua.LVAsBool(old.RawGetString("overflow")) || lua.LVAsBool(nt.RawGetString("overflow"))
	if !over {
		oe, _ := old.RawGetString("edits").(*lua.LTable)
		ne, _ := nt.RawGetString("edits").(*lua.LTable)
		switch {
		case oe == nil:
			if ne != nil {
				old.RawSetString("edits", ne)
				oe = ne
			}
		case ne != nil:
			for i := 1; i <= ne.Len(); i++ {
				oe.Append(ne.RawGetInt(i))
			}
		}
		if oe != nil {
			n, size := editStats(oe)
			over = n > maxMergedEdits || size > maxMergedBytes
		}
	}
	if over {
		old.RawSetString("edits", lua.LNil)
		old.RawSetString("overflow", lua.LTrue)
	}
	undo := lua.LVAsBool(old.RawGetString("undo")) || lua.LVAsBool(nt.RawGetString("undo"))
	nt.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok && (ks == "edits" || ks == "overflow" || ks == "undo") {
			return
		}
		old.RawSet(k, v)
	})
	old.RawSetString("undo", lua.LBool(undo))
}

// editStats counts the records of an edits array and the bytes of their text.
func editStats(edits *lua.LTable) (n, size int) {
	n = edits.Len()
	for i := 1; i <= n && size <= maxMergedBytes; i++ {
		if rec, ok := edits.RawGetInt(i).(*lua.LTable); ok {
			if s, ok := rec.RawGetString("text").(lua.LString); ok {
				size += len(s)
			}
			if s, ok := rec.RawGetString("removed").(lua.LString); ok {
				size += len(s)
			}
		}
	}
	return n, size
}

// debounce holds e until the subscription has seen no event for the debounce time for this
// buffer, then delivers the merged payload. One timer per subscription is out at a time,
// armed for the earliest buffer; fire re-arms it for the remainder.
func (c *subCtl) debounce(e pluginapi.Event) {
	id, _ := eventBuffer(e)
	st := c.pend[id]
	if st == nil {
		if len(c.pend) >= maxDebounceBuffers {
			// Too many buffers waiting: deliver this one at once rather than hold unbounded state.
			tbl, _ := c.p.toLua(e.Data).(*lua.LTable)
			c.r.queueHandler(c.s, tbl)
			return
		}
		st = &debState{}
		c.pend[id] = st
	}
	st.last = c.r.now()
	if st.tbl != nil && mergeable[e.Name] {
		c.p.mergeEvent(st.tbl, e)
	} else {
		st.tbl, _ = c.p.toLua(e.Data).(*lua.LTable)
	}
	if !c.armed && !c.arm(c.o.debounce) {
		// Over the Work quota: deliver at once rather than hold a timer.
		delete(c.pend, id)
		c.deliver(st)
	}
}

// arm starts the timer of the subscription and reports whether it did. The timer is
// Work{After}; its wait belongs to the plugin, so unload cancels it, and it counts against
// QuotaWork like any other in-flight Work. It returns false when the plugin is at the quota.
func (c *subCtl) arm(d time.Duration) bool {
	r, p := c.r, c.p
	if p.inflight >= QuotaWork {
		return false
	}
	tok := r.newToken()
	c.armed = true
	p.inflight++
	r.waits[tok] = &wait{
		token: tok, p: p, gen: p.gen, work: true,
		onItem: func(pluginapi.Completion) { c.fire() },
		cancel: func() { c.armed = false },
	}
	r.work = append(r.work, pluginapi.Work{Token: tok, Plugin: p.name, Gen: p.gen, Ctx: p.pctx, After: d})
	return true
}

// fire is the timer of a debounced subscription: it delivers the buffers whose window has
// passed and re-arms for the earliest of the others.
func (c *subCtl) fire() {
	c.armed = false
	s, p := c.s, c.p
	if !s.active || p.muted() {
		clear(c.pend)
		return
	}
	now := c.r.now()
	var due []uint64
	next := time.Duration(0)
	for id, st := range c.pend {
		if left := c.o.debounce - now.Sub(st.last); left > 0 {
			if next == 0 || left < next {
				next = left
			}
		} else {
			due = append(due, id)
		}
	}
	slices.SortFunc(due, func(a, b uint64) int {
		if d := c.pend[a].last.Compare(c.pend[b].last); d != 0 {
			return d
		}
		return cmp.Compare(a, b)
	})
	for _, id := range due {
		st := c.pend[id]
		delete(c.pend, id)
		c.deliver(st)
		if s.once {
			clear(c.pend) // a once subscription runs once, for the first buffer
			return
		}
	}
	if len(c.pend) == 0 || !s.active {
		return
	}
	if !c.arm(next) {
		// At the Work quota: nothing may wait any longer.
		for _, id := range slices.Sorted(maps.Keys(c.pend)) {
			st := c.pend[id]
			delete(c.pend, id)
			c.deliver(st)
		}
	}
}

// deliver queues the handler with the merged payload of st.
func (c *subCtl) deliver(st *debState) {
	if c.s.once {
		c.p.removeSub(c.s)
	}
	c.r.queueHandler(c.s, st.tbl)
}

// queueHandler starts the handler of s with tbl as the payload, as expandEvent does for an
// event that passed the filter.
func (r *Runtime) queueHandler(s *subscription, tbl *lua.LTable) {
	p := s.p
	if tbl == nil {
		tbl = p.L.NewTable()
	}
	t, err := r.newTask(p, originEvent, s.fn, "event "+s.event)
	if err != nil {
		r.logf(p.name, levelWarn, "dropped event "+s.event+": "+err.Error())
		return
	}
	t.sub = s
	r.nextBatch++
	p.evq = append(p.evq, item{t: t, args: []lua.LValue{tbl}, batch: r.nextBatch})
}

// queuedFrom counts the queued events that plugin p produced. The queue is only walked
// when it is long enough for the limit to matter.
func (r *Runtime) queuedFrom(p *plugin) int {
	if len(r.evq) < maxQueuedEmits {
		return len(r.evq)
	}
	n := 0
	for _, qe := range r.evq {
		if qe.from == p {
			n++
		}
	}
	return n
}

// payloadSize estimates the size in bytes of a payload and stops counting once it is above
// limit.
func payloadSize(v any, limit int) int {
	n := 0
	var walk func(v any)
	walk = func(v any) {
		if n > limit {
			return
		}
		switch x := v.(type) {
		case string:
			n += len(x) + 8
		case []any:
			n += 8
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			n += 8
			for k, e := range x {
				n += len(k) + 8
				walk(e)
			}
		default:
			n += 8
		}
	}
	walk(v)
	return n
}
