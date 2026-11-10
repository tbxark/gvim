package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// loader is the loader state of a Runtime: what discovery found, the toggles of the
// manager and the batches of plugins that are being loaded. It lives on the UI goroutine.
type loader struct {
	opt    *LoaderOptions // nil: no discovery
	ctx    context.Context
	cancel context.CancelFunc

	begun   bool
	holding bool // `ready` is held back until plugins_loaded has been queued
	held    *pluginapi.Event

	cands  []*candidate // load order, then the plugins that are not running
	byName map[string]*candidate
	legacy []string
	state  pluginState

	stateBusy, stateDirty, stateHooked bool

	// toggles counts the enables and disables of each plugin name; a rescan Work that
	// started before the latest one is out of date.
	toggles map[string]uint64
}

// toggled records that name was enabled or disabled.
func (l *loader) toggled(name string) {
	if l.toggles == nil {
		l.toggles = map[string]uint64{}
	}
	l.toggles[name]++
}

// ldContext is the context of the loader's Work; Stop cancels it.
func (r *Runtime) ldContext() context.Context {
	l := &r.ld
	if l.ctx == nil {
		l.ctx, l.cancel = context.WithCancel(context.Background())
		r.onStop(l.cancel)
	}
	return l.ctx
}

// newWork wraps loader IO as Work: run goes to a Work goroutine and must not touch
// Lua values or Runtime fields; then runs on the UI goroutine with its result.
func (r *Runtime) newWork(run func(ctx context.Context) (any, string), then func(v any, errText string)) pluginapi.Work {
	tok := r.newToken()
	ctx := r.ldContext()
	r.expectRuntime(tok, func(c pluginapi.Completion) {
		if r.stopped {
			return
		}
		var v any
		if len(c.Values) > 0 {
			v = c.Values[0]
		}
		then(v, c.Err)
	})
	return pluginapi.Work{Token: tok, Ctx: ctx, Run: func(ctx context.Context) (c pluginapi.Completion) {
		defer func() {
			if rec := recover(); rec != nil {
				c = pluginapi.Completion{Err: fmt.Sprint(pluginapi.CodeIO, ": internal error: ", rec)}
			}
			c.Token = tok
		}()
		v, e := run(ctx)
		return pluginapi.Completion{Values: []any{v}, Err: e}
	}}
}

// deferRuntime runs f from the inbox, at the next Drain (outside any slice and outside
// unload, which may be on the stack now).
func (r *Runtime) deferRuntime(f func()) {
	tok := r.newToken()
	r.expectRuntime(tok, func(pluginapi.Completion) {
		if !r.stopped {
			f()
		}
	})
	r.inbox = append(r.inbox, pluginapi.Completion{Token: tok})
}

// say shows a notice. Loader notices belong to no plugin.
func (r *Runtime) say(level pluginapi.Level, text string) { r.host.Notify("", level, text) }

// ---------- the toggles of the manager: plugin-state.json ----------

// pluginState is plugin-state.json: the plugins the user enabled or disabled in the
// manager. It is below plugins.<name>.enabled of the configuration in precedence.
type pluginState struct {
	enabled, disabled map[string]bool
	// warnedUnknown holds the plugin names already reported as not installed, so a reload does
	// not report them again.
	warnedUnknown map[string]bool
}

func (s pluginState) clone() pluginState {
	c := pluginState{enabled: map[string]bool{}, disabled: map[string]bool{}, warnedUnknown: map[string]bool{}}
	for k := range s.enabled {
		c.enabled[k] = true
	}
	for k := range s.disabled {
		c.disabled[k] = true
	}
	for k := range s.warnedUnknown {
		c.warnedUnknown[k] = true
	}
	return c
}

func (s *pluginState) set(name string, on bool) {
	if s.enabled == nil {
		s.enabled = map[string]bool{}
	}
	if s.disabled == nil {
		s.disabled = map[string]bool{}
	}
	delete(s.enabled, name)
	delete(s.disabled, name)
	if on {
		s.enabled[name] = true
	} else {
		s.disabled[name] = true
	}
}

func (s pluginState) marshal() []byte {
	list := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	data, _ := json.MarshalIndent(struct {
		Enabled  []string `json:"enabled"`
		Disabled []string `json:"disabled"`
	}{list(s.enabled), list(s.disabled)}, "", "  ")
	return append(data, '\n')
}

// readPluginState reads the state file; a missing file is an empty state, a damaged one
// is ignored with a note.
func readPluginState(ctx context.Context, osys hostos.OS, path string, notes *[]note) pluginState {
	st := pluginState{enabled: map[string]bool{}, disabled: map[string]bool{}}
	if path == "" {
		return st
	}
	data, err := osys.ReadFile(ctx, path, maxStateFile)
	if err != nil {
		switch pluginapi.CodeOf(err) {
		case pluginapi.CodeNotFound, pluginapi.CodeUnsupported:
		default:
			if !errors.Is(err, fs.ErrNotExist) {
				*notes = append(*notes, note{text: "cannot read plugin-state.json: " + err.Error(), warn: true})
			}
		}
		return st
	}
	var f struct {
		Enabled  []string `json:"enabled"`
		Disabled []string `json:"disabled"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		*notes = append(*notes, note{text: "plugin-state.json is damaged and ignored: " + err.Error(), warn: true})
		return st
	}
	for _, n := range f.Enabled {
		st.enabled[n] = true
	}
	for _, n := range f.Disabled {
		st.disabled[n] = true // disabled wins when a name is in both
		delete(st.enabled, n)
	}
	return st
}

// saveState writes the toggles. One write is in flight at most; changes that arrive
// meanwhile are written by the next one (the latest value wins, nothing is dropped).
func (r *Runtime) saveState() {
	l := &r.ld
	if l.opt == nil || l.opt.StateFile == "" {
		return
	}
	l.stateDirty = true
	if !l.stateBusy {
		r.writeState()
	}
}

func (r *Runtime) writeState() {
	l := &r.ld
	if !l.stateBusy && !l.stateDirty {
		return
	}
	if !l.stateHooked {
		l.stateHooked = true
		r.onStop(r.flushState)
	}
	l.stateDirty, l.stateBusy = false, true
	data, path, osys := l.state.marshal(), l.opt.StateFile, r.os
	r.work = append(r.work, r.newWork(func(ctx context.Context) (any, string) {
		if err := osys.WriteFile(ctx, path, data, hostos.WriteOptions{Atomic: true, Parents: true}); err != nil {
			return nil, errString(err)
		}
		return nil, ""
	}, func(_ any, errText string) {
		l.stateBusy = false
		if errText != "" {
			r.say(pluginapi.LevelWarn, "cannot save plugin-state.json: "+errText)
		}
		if l.stateDirty {
			r.writeState()
		}
	}))
}

// flushState is Stop's synchronous write of toggles that have not reached the disk.
func (r *Runtime) flushState() {
	l := &r.ld
	if l.opt == nil || l.opt.StateFile == "" || (!l.stateDirty && !l.stateBusy) {
		return
	}
	l.stateDirty, l.stateBusy = false, false
	if err := r.os.WriteFile(context.Background(), l.opt.StateFile, l.state.marshal(), hostos.WriteOptions{Atomic: true, Parents: true}); err != nil {
		r.logf("", levelWarn, "cannot save plugin-state.json: "+errString(err))
	}
}

// ---------- startup ----------

// startLoader returns the discovery Work (nil when nothing is to be discovered).
func (r *Runtime) startLoader() []pluginapi.Work {
	l := &r.ld
	if l.opt == nil || l.begun {
		return nil
	}
	l.begun = true
	l.byName = map[string]*candidate{}
	if l.opt.Disabled && len(l.opt.Paths) == 0 {
		return nil
	}
	l.holding = true
	return []pluginapi.Work{r.discover(nil, func(d *discovery) { r.applyDiscovery(d, true, nil) })}
}

// holdReady keeps `ready` back until the loader has queued plugins_loaded: handlers see
// plugins_loaded first even when the first window size arrives while plugins load.
func (r *Runtime) holdReady(e pluginapi.Event) bool {
	if e.Name != "ready" || !r.ld.holding {
		return false
	}
	r.ld.held = &e
	return true
}

// depLookup tells planOrder whether a required plugin that is not part of the set is
// available (running) and why not otherwise.
func (r *Runtime) depLookup(name string) (bool, string) {
	if p := r.plugins[name]; p != nil && !p.dead {
		return true, ""
	}
	switch c := r.ld.byName[name]; {
	case c == nil:
		return false, "not installed"
	case !c.enabled:
		return false, "disabled"
	case c.err != "":
		return false, "failed: " + firstLine(c.err)
	case c.reason != "":
		return false, "not running: " + firstLine(c.reason)
	}
	return false, "not loaded"
}

// applyDiscovery takes the result of the discovery Work. At startup it loads every enabled
// plugin; for reload_all (startup false) it first unloads the plugins that restart, keeps
// the running version of one whose new source does not compile, and drops the ones that
// are gone.
func (r *Runtime) applyDiscovery(d *discovery, startup bool, done func(*batch)) {
	l := &r.ld
	l.state = d.state
	l.legacy = d.legacy
	for _, n := range d.notes {
		lvl := pluginapi.LevelInfo
		if n.warn {
			lvl = pluginapi.LevelWarn
		}
		r.logf("", levelWarn, n.text)
		if !startup {
			r.say(lvl, n.text)
		}
	}

	prev := l.byName
	var kept []*candidate
	if !startup {
		d.cands, kept = unreadPlugins(d.cands, prev, d.unread)
	}
	b := r.newBatch(startup, done)
	if startup {
		// Said by the flush, in the same update as the load errors (see flush).
		for _, n := range d.notes {
			b.lead = append(b.lead, n.text)
		}
		if len(d.legacy) > 0 {
			b.lead = append(b.lead, legacyNotice(d.legacy))
			r.logf("", levelWarn, legacyNotice(d.legacy))
		}
	}
	var set []*candidate
	restart := map[string]bool{}
	for i, c := range d.cands {
		cur := r.plugins[c.name]
		running := cur != nil && !cur.dead
		if len(c.overrides) > 0 {
			r.logf(c.name, levelInfo, "overrides the "+strings.Join(c.overrides, ", ")+" plugin of the same name")
		}
		if len(c.man.Unknown) > 0 {
			r.logf(c.name, levelDebug, "plugin.json: unknown fields ignored: "+strings.Join(c.man.Unknown, ", "))
		}
		switch {
		case !c.enabled:
		case c.err != "" || len(c.problems) > 0:
			msg := c.err
			if msg == "" {
				msg = c.problems[0]
			}
			if !startup && running && prev[c.name] != nil {
				b.errs = append(b.errs, fmt.Sprintf("plugin '%s': reload aborted: %s (the running version is kept)", c.name, statusText(msg)))
				r.logf(c.name, levelError, "reload aborted: "+msg)
				d.cands[i] = prev[c.name]
				continue
			}
			if c.err != "" {
				c.reason = c.err
				b.errs = append(b.errs, fmt.Sprintf("plugin '%s': %s", c.name, statusText(c.err)))
				r.logf(c.name, levelError, "cannot load: "+c.err)
				continue
			}
			// A module that does not compile fails when it is required, not at startup.
			for _, p := range c.problems {
				r.logf(c.name, levelWarn, "does not compile: "+p)
			}
			set = append(set, c)
			restart[c.name] = true
		default:
			set = append(set, c)
			restart[c.name] = true
		}
	}
	d.cands = withKept(d.cands, kept)
	l.byName = make(map[string]*candidate, len(d.cands))
	for _, c := range d.cands {
		l.byName[c.name] = c
	}
	if !startup {
		r.unloadForReload(restart, l.byName, prev)
	}
	for _, c := range d.cands {
		if p := r.plugins[c.name]; p != nil && p.dead {
			r.forget(p)
		}
	}
	l.cands = nil
	r.prepare(b, set)
	r.orderCands(d.cands, b)
	b.advance()
}

// unreadPlugins returns the previous plugins of every source that could not be read, which a
// reload keeps as they are: it must not take an unreadable directory for an empty one. The
// candidates of the new discovery that only appeared because such a plugin was no longer
// there to shadow them (a bundled plugin a user plugin overrode) are dropped from cands.
func unreadPlugins(cands []*candidate, prev map[string]*candidate, unread map[string]string) ([]*candidate, []*candidate) {
	var names []string
	for name, pc := range prev {
		if _, ok := unread[pc.kind]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var kept []*candidate
	for _, name := range names {
		i := slices.IndexFunc(cands, func(c *candidate) bool { return c.name == name })
		switch {
		case i < 0:
		case cands[i].kind == SourceBundled:
			cands = slices.Delete(slices.Clone(cands), i, i+1)
		default:
			continue
		}
		kept = append(kept, prev[name])
	}
	return cands, kept
}

// withKept adds the plugins unreadPlugins kept to the candidates in source order.
func withKept(cands, kept []*candidate) []*candidate {
	if len(kept) == 0 {
		return cands
	}
	out := append(slices.Clone(cands), kept...)
	rank := map[string]int{SourceBundled: 0, SourceUser: 1, SourceCLI: 2, SourceInit: 3}
	slices.SortStableFunc(out, func(a, b *candidate) int { return rank[a.kind] - rank[b.kind] })
	for i, c := range out {
		c.seq = i
	}
	return out
}

// unloadForReload unloads, in reverse load order, every running plugin that restarts and
// every running plugin the new discovery no longer lists or now disables. Plugins loaded
// from a buffer stay.
func (r *Runtime) unloadForReload(restart map[string]bool, now, prev map[string]*candidate) {
	for i := len(r.order) - 1; i >= 0; i-- {
		p := r.order[i]
		if p.dead || p.kindStr == SourceBuffer {
			continue
		}
		c := now[p.name]
		switch {
		case restart[p.name]:
		case c == nil && prev[p.name] != nil: // gone from disk
		case c != nil && !c.enabled:
		default:
			continue
		}
		r.unload(p, unloadOptions{})
	}
}

// orderCands records the load order for the manager: the plugins of the batch in the order
// they were planned, then everything else in discovery order.
func (r *Runtime) orderCands(all []*candidate, b *batch) {
	l := &r.ld
	seen := map[*candidate]bool{}
	for _, it := range b.items {
		l.cands = append(l.cands, it.c)
		seen[it.c] = true
	}
	for _, c := range all {
		if !seen[c] {
			l.cands = append(l.cands, c)
		}
	}
}

func legacyNotice(names []string) string {
	shown := names
	if len(shown) > 5 {
		shown = shown[:5]
	}
	text := fmt.Sprintf("%d JavaScript plugin(s) ignored (gvim plugins are Lua now): %s", len(names), strings.Join(shown, ", "))
	if len(names) > len(shown) {
		text += fmt.Sprintf(" and %d more", len(names)-len(shown))
	}
	return text
}

// ---------- batches ----------

type itemState int

const (
	itemWaiting itemState = iota
	itemLoading           // main chunk started, parked
	itemOK
	itemFailed
	itemSkipped
)

// bitem is one plugin of a batch.
type bitem struct {
	c        *candidate
	st       itemState
	gen      uint64
	tok      pluginapi.Token
	replayed bool
}

// batch loads a set of plugins in dependency order: a plugin starts when everything it
// requires (or must come after) has finished its main chunk. Failures of one plugin are
// collected and shown together at the end; a failed required plugin skips its dependants.
//
// A main chunk that stays parked past the load budget does not hold the batch back for ever:
// the batch is released, which raises plugins_loaded and `ready`, replays the open files to
// the plugins that loaded and shows the errors so far. What is not done yet (the parked
// chunk, its dependants) completes later and is reported then.
type batch struct {
	r          *Runtime
	items      []*bitem
	by         map[string]*bitem
	startup    bool
	errs       []string
	done       func(*batch)
	finished   bool
	released   bool
	announced  bool     // plugins_loaded was raised
	shown      int      // errs reported so far
	noticed    int      // of those, shown by name in the message
	lead       []string // startup notices not shown yet: discovery notes, ignored JavaScript files
	releaseTok pluginapi.Token
}

func (r *Runtime) newBatch(startup bool, done func(*batch)) *batch {
	return &batch{r: r, startup: startup, by: map[string]*bitem{}, done: done}
}

// prepare plans set and records what cannot load; b.advance starts the first plugins.
func (r *Runtime) prepare(b *batch, set []*candidate) {
	order, blocked := planOrder(set, r.depLookup)
	for _, c := range set {
		if why := blocked[c]; why != "" {
			c.reason = why
			b.errs = append(b.errs, fmt.Sprintf("plugin '%s': %s", c.name, why))
			r.logf(c.name, levelWarn, "not loaded: "+why)
		}
	}
	for _, c := range order {
		c.reason = ""
		it := &bitem{c: c}
		b.items = append(b.items, it)
		b.by[c.name] = it
	}
}

// advance starts every plugin that is ready and finishes the batch when none is left.
func (b *batch) advance() {
	if b.finished {
		return
	}
	for progress := true; progress; {
		progress = false
		for _, it := range b.items {
			if it.st != itemWaiting {
				continue
			}
			ready, fail := b.deps(it)
			switch {
			case fail != "":
				b.skip(it, fail)
				progress = true
			case ready:
				b.start(it)
				progress = true
			}
		}
	}
	if b.released {
		b.flush()
	}
	for _, it := range b.items {
		if it.st == itemWaiting || it.st == itemLoading {
			return
		}
	}
	b.finish()
}

// deps says whether it may start, or why it never will.
func (b *batch) deps(it *bitem) (ready bool, fail string) {
	ready = true
	for _, req := range it.c.man.Requires {
		if d := b.by[req]; d != nil {
			switch d.st {
			case itemOK:
			case itemFailed, itemSkipped:
				return false, fmt.Sprintf("requires %q, which failed to load", req)
			default:
				ready = false
			}
			continue
		}
		if p := b.r.plugins[req]; p == nil || p.dead {
			return false, fmt.Sprintf("requires %q (not loaded)", req)
		}
	}
	for _, name := range it.c.man.After {
		if d := b.by[name]; d != nil && (d.st == itemWaiting || d.st == itemLoading) {
			ready = false
		}
	}
	return ready, ""
}

func (b *batch) start(it *bitem) {
	r := b.r
	c := it.c
	before := r.nextGen
	if e := r.io().state(c.name); !e.seeded && c.stateData != nil {
		if err := r.SeedState(c.name, c.stateData); err != nil {
			r.logf(c.name, levelWarn, err.Error())
		}
	}
	err := r.LoadPlugin(c.source())
	p := r.plugins[c.name]
	switch {
	case p == nil || p.gen <= before: // refused (name taken, runtime stopped): not our plugin
		b.fail(it, errText(err, "could not start"))
		return
	case p.status == statusFailed:
		b.fail(it, p.errMsg)
		return
	}
	it.gen = p.gen
	p.addCleanup(func() { b.gone(it) })
	if p.mainT == nil || p.mainT.done {
		b.ok(it)
		return
	}
	// The main chunk is parked: wake when it finishes or fails (settleWaiters).
	it.st = itemLoading
	it.tok = r.newToken()
	r.expectRuntime(it.tok, func(pluginapi.Completion) { b.settle(it) })
	p.mainT.waiters = append(p.mainT.waiters, it.tok)
	b.armRelease()
}

// armRelease starts the clock that releases the batch when a main chunk stays parked.
func (b *batch) armRelease() {
	r := b.r
	if b.releaseTok != 0 || b.released || b.finished {
		return
	}
	b.releaseTok = r.newToken()
	r.expectRuntime(b.releaseTok, func(pluginapi.Completion) {
		b.releaseTok = 0
		b.release()
	})
	r.work = append(r.work, pluginapi.Work{Token: b.releaseTok, After: r.budget(originLoad), Ctx: r.ldContext()})
}

// release reports what the batch has so far although a main chunk is still parked.
func (b *batch) release() {
	if b.released || b.finished {
		return
	}
	b.released = true
	for _, it := range b.items {
		if it.st == itemLoading {
			b.r.logf(it.c.name, levelWarn, "the main chunk is still waiting; plugins_loaded does not wait for it")
		}
	}
	b.flush()
}

func errText(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

// gone is the unload cleanup of a plugin: when it is unloaded while its main chunk is
// still parked, nothing would wake the batch, so settle it from the inbox.
func (b *batch) gone(it *bitem) {
	if it.st != itemLoading {
		return
	}
	if w := b.r.waits[it.tok]; w != nil {
		b.r.dropWait(w)
	}
	b.r.deferRuntime(func() { b.settle(it) })
}

// settle runs when a parked main chunk ended. A plugin that is not there any more (it was
// unloaded or replaced meanwhile) ends the item without an error.
func (b *batch) settle(it *bitem) {
	if it.st != itemLoading {
		return
	}
	p := b.r.plugins[it.c.name]
	switch {
	case p != nil && p.gen == it.gen && p.status == statusFailed:
		b.fail(it, p.errMsg)
	case p == nil || p.gen != it.gen || p.dead:
		it.st = itemSkipped
	default:
		b.ok(it)
	}
	b.advance()
}

func (b *batch) ok(it *bitem) {
	it.st = itemOK
	it.c.reason = ""
	if p := b.r.plugins[it.c.name]; p != nil && !p.dead {
		p.warnSettingsWithoutSchema()
	}
}

func (b *batch) fail(it *bitem, msg string) {
	it.st = itemFailed
	it.c.reason = msg
	b.errs = append(b.errs, fmt.Sprintf("plugin '%s': %s", it.c.name, statusText(msg)))
}

func (b *batch) skip(it *bitem, why string) {
	it.st = itemSkipped
	it.c.reason = why
	b.errs = append(b.errs, fmt.Sprintf("plugin '%s': %s", it.c.name, why))
	b.r.logf(it.c.name, levelWarn, "not loaded: "+why)
}

// finish ends the batch: it reports what flush has not, then calls done.
func (b *batch) finish() {
	if b.finished {
		return
	}
	b.finished = true
	if w := b.r.waits[b.releaseTok]; b.releaseTok != 0 && w != nil {
		b.r.dropWait(w)
	}
	b.flush()
	b.r.lintBindings()
	if b.done != nil {
		b.done(b)
	}
}

// flush reports what is known: it raises plugins_loaded at startup (once) and releases the
// held `ready`, replays the open files to the plugins that loaded since the last flush, and
// shows the new errors.
func (b *batch) flush() {
	r := b.r
	l := &r.ld
	if b.startup && !b.announced {
		b.announced = true
		r.emit(pluginapi.Event{Name: "plugins_loaded", Data: map[string]any{}}, nil)
		l.holding = false
		if held := l.held; held != nil {
			l.held = nil
			r.emit(*held, nil)
		}
	}
	for _, it := range b.items {
		if it.st == itemOK && !it.replayed {
			it.replayed = true
			r.replay(it.c.name)
		}
	}
	// The notices of one flush reach the host together: the ui shows them as one status
	// message, which is cut at the width of the status row, and keeps each in the warnings
	// log. The errors come first, since they are what the user has to act on; the startup
	// notices that were held back (ignored JavaScript files) follow.
	more := 0
	for _, e := range b.errs[b.shown:] {
		if b.noticed < 3 {
			b.noticed++
			r.say(pluginapi.LevelError, e)
		} else {
			more++
		}
	}
	b.shown = len(b.errs)
	if more > 0 {
		noun := "errors"
		if more == 1 {
			noun = "error"
		}
		r.say(pluginapi.LevelError, fmt.Sprintf("%d more plugin %s (Plugin: Show Log)", more, noun))
	}
	for _, n := range b.lead {
		r.say(pluginapi.LevelWarn, n)
	}
	b.lead = nil
}

// okNames lists the plugins of the batch that loaded.
func (b *batch) okNames() []string {
	var out []string
	for _, it := range b.items {
		if it.st == itemOK {
			out = append(out, it.c.name)
		}
	}
	return out
}

// replay tells a plugin that loaded after files were opened about them (AMENDMENTS C21 4):
// after_file_open{buf, path, replay=true} for each tab with a path, in tab order, then
// buffer_activated{buf, replay=true} for the active one, only for names it subscribed to.
func (r *Runtime) replay(name string) {
	p := r.plugins[name]
	if p == nil || p.dead || p.muted() {
		return
	}
	if len(p.subs["after_file_open"]) == 0 && len(p.subs["buffer_activated"]) == 0 {
		return
	}
	if len(p.subs["after_file_open"]) > 0 {
		for _, bi := range r.host.ListBuffers() {
			if bi.Path == "" || bi.Virtual {
				continue
			}
			r.emit(pluginapi.Event{Name: "after_file_open", Target: name,
				Data: map[string]any{"buf": bi.ID, "path": bi.Path, "replay": true}}, nil)
		}
	}
	if id := r.host.ActiveBuffer(); id != 0 && len(p.subs["buffer_activated"]) > 0 {
		r.emit(pluginapi.Event{Name: "buffer_activated", Target: name,
			Data: map[string]any{"buf": id, "replay": true}}, nil)
	}
}

// ---------- the manager's view ----------

// pluginRows is Runtime.Plugins: the plugins in load order, those that are not running
// (disabled, failed, blocked) from what discovery knew, plugins loaded without the loader
// last, then one row per ignored JavaScript file.
func (r *Runtime) pluginRows() []pluginapi.PluginInfo {
	l := &r.ld
	out := make([]pluginapi.PluginInfo, 0, len(r.order)+len(l.cands)+len(l.legacy))
	seen := map[string]bool{}
	for _, c := range l.cands {
		seen[c.name] = true
		p := r.plugins[c.name]
		switch {
		case p != nil && !p.dead:
			info := p.info()
			info.Locked = c.locked
			out = append(out, info)
		case p != nil && c.enabled && c.err == "" && c.reason == "":
			out = append(out, p.info())
		default:
			out = append(out, c.row())
		}
	}
	for _, p := range r.order {
		if !seen[p.name] {
			out = append(out, p.info())
		}
	}
	dir := ""
	if l.opt != nil {
		dir = l.opt.UserDir
	}
	for _, name := range l.legacy {
		out = append(out, pluginapi.PluginInfo{Name: name, Source: SourceUser, Dir: dir, Status: "legacy",
			Locked: "JavaScript plugins are not supported"})
	}
	return out
}

// row is the manager row of a plugin that is not running.
func (c *candidate) row() pluginapi.PluginInfo {
	info := pluginapi.PluginInfo{Name: c.name, Version: c.version, Description: c.man.Description, Requires: append([]string(nil), c.man.Requires...),
		Source: c.kind, Dir: c.dir, File: c.entry(),
		Locked: c.locked, Enabled: c.enabled}
	switch {
	case !c.enabled:
		info.Status = statusDisabled
	default:
		info.Status = statusFailed
		info.Err = c.err
		if info.Err == "" {
			info.Err = c.reason
		}
	}
	return info
}

// loadedDependants lists the running plugins that require name, directly or not, in load
// order.
func (r *Runtime) loadedDependants(name string) []string {
	set := map[string]bool{name: true}
	var out []string
	for changed := true; changed; {
		changed = false
		for _, p := range r.order {
			if p.dead || set[p.name] {
				continue
			}
			c := r.ld.byName[p.name]
			if c == nil {
				continue
			}
			for _, req := range c.man.Requires {
				if set[req] {
					set[p.name] = true
					out = append(out, p.name)
					changed = true
					break
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return r.orderIndex(out[i]) < r.orderIndex(out[j]) })
	return out
}

func (r *Runtime) orderIndex(name string) int {
	for i, p := range r.order {
		if p.name == name {
			return i
		}
	}
	return len(r.order)
}

// stoppedDependants lists candidates that are enabled and readable but not running and
// that require name, directly or not: they were held back by it.
func (r *Runtime) stoppedDependants(name string) []*candidate {
	l := &r.ld
	set := map[string]bool{name: true}
	var out []*candidate
	for changed := true; changed; {
		changed = false
		for _, c := range l.cands {
			if set[c.name] || !c.enabled || c.err != "" || c.compiled == nil {
				continue
			}
			if p := r.plugins[c.name]; p != nil && !p.dead {
				continue
			}
			for _, req := range c.man.Requires {
				if set[req] {
					set[c.name] = true
					out = append(out, c)
					changed = true
					break
				}
			}
		}
	}
	return out
}

// unloadNamed unloads the running plugins of names in the order given and forgets stale
// tombstones.
func (r *Runtime) unloadNamed(names []string, o unloadOptions) {
	for _, n := range names {
		if p := r.plugins[n]; p != nil {
			if p.dead {
				r.forget(p)
				continue
			}
			r.unload(p, o)
		}
	}
}

// install records nc as the candidate of its name, in place of an older one.
func (l *loader) install(nc *candidate) {
	if l.byName == nil {
		l.byName = map[string]*candidate{}
	}
	if old := l.byName[nc.name]; old != nil {
		for i, c := range l.cands {
			if c == old {
				l.cands[i] = nc
				break
			}
		}
		nc.seq = old.seq
	} else {
		l.cands = append(l.cands, nc)
	}
	l.byName[nc.name] = nc
}

// updateSettings records new config settings for name, for the next time the plugin is read
// or started. Work in flight holds the old map, so the map is replaced, not changed.
func (l *loader) updateSettings(name string, st PluginSettings) {
	if c := l.byName[name]; c != nil {
		c.settings = st
	}
	if l.opt == nil {
		return
	}
	m := make(map[string]PluginSettings, len(l.opt.Settings)+1)
	maps.Copy(m, l.opt.Settings)
	m[name] = st
	l.opt.Settings = m
}
