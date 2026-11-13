package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// This file is the runtime-level part of the IO areas: the plugin-level waits
// timers, watchers and processes are built on, the set of live processes Stop kills, and
// the name-keyed state store with its serial writer.

// ioRuntime is the IO state of one Runtime. It lives on the runtime, not on a plugin
// generation, so state writes and process bookkeeping survive reloads.
type ioRuntime struct {
	r      *Runtime
	states map[string]*stateEntry
	procs  map[*process]struct{}

	// The workspace-scoped state target: one versioned file for every plugin's
	// gvim.state instead of the editor-wide per-plugin files. Empty path keeps the old
	// behavior byte-identical.
	wsPath     string
	wsReadOnly bool
	wsDirty    bool
	wsArmed    bool
	wsBusy     bool
	// wsSeq is the latest write sequence per file. A queued write is invalidated when a
	// newer write (or the Stop flush) of the same file owns it; a stale Work skips its
	// write instead of overwriting newer content. A workspace switch does not bump it: the
	// writes of the file it leaves are still the newest ones for that file.
	wsSeqMu sync.Mutex
	wsSeq   map[string]uint64
}

// wsNextSeq claims the next write sequence of path.
func (io *ioRuntime) wsNextSeq(path string) uint64 {
	io.wsSeqMu.Lock()
	defer io.wsSeqMu.Unlock()
	if io.wsSeq == nil {
		io.wsSeq = map[string]uint64{}
	}
	io.wsSeq[path]++
	return io.wsSeq[path]
}

// wsStale reports whether a newer write of path claimed a sequence after seq.
func (io *ioRuntime) wsStale(path string, seq uint64) bool {
	io.wsSeqMu.Lock()
	defer io.wsSeqMu.Unlock()
	return io.wsSeq[path] != seq
}

// io returns the IO state of the runtime, creating it (and the stop hook) on first use.
func (r *Runtime) io() *ioRuntime {
	if r.ioR == nil {
		io := &ioRuntime{r: r, states: map[string]*stateEntry{}, procs: map[*process]struct{}{}}
		io.wsPath = r.workspaceStateFile
		io.wsReadOnly = r.workspaceStateReadOnly
		io.seedWorkspace(r.workspaceState)
		if r.workspaceStateNote != "" {
			r.logf("", levelWarn, r.workspaceStateNote)
		}
		r.ioR = io
		r.onStop(r.ioR.stop)
	}
	return r.ioR
}

// stop is the Stop hook: SIGKILL every live process at once, then flush the state files.
func (io *ioRuntime) stop() {
	for pr := range io.procs {
		if pr.osp != nil {
			_ = pr.osp.Kill("KILL")
		}
	}
	if io.wsPath != "" {
		// Workspace-scoped state: one file, flushed before the OS is shut down.
		io.wsFlush()
	}
	io.r.os.Shutdown()
	if io.wsPath != "" {
		return
	}
	names := make([]string, 0, len(io.states))
	for n := range io.states {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		io.states[n].flush()
	}
}

// ---------- plugin-level waits ----------

// newWait registers a wait of the plugin that has no task: onItem receives every item of
// the stream and the final completion; cancel runs when the wait is abandoned (unload,
// Watcher:stop). The wait dies with the plugin generation.
func (p *plugin) newWait(onItem func(pluginapi.Completion), cancel func()) (pluginapi.Token, *wait) {
	r := p.r
	tok := r.newToken()
	w := &wait{token: tok, p: p, gen: p.gen, onItem: onItem, cancel: cancel}
	r.waits[tok] = w
	return tok, w
}

// queueWork hands w to the next Drain with the identity of the plugin filled in.
func (p *plugin) queueWork(tok pluginapi.Token, w pluginapi.Work) {
	w.Token, w.Plugin, w.Gen = tok, p.name, p.gen
	if w.Ctx == nil {
		w.Ctx = p.pctx
	}
	p.r.work = append(p.r.work, w)
}

// afterTimer calls f on the UI goroutine once d has passed (Work{After}, so the manual
// clock of the test harness drives it). The returned stop function abandons the timer. d
// is raised to 1 ms: a timer of zero is a yield to the event loop.
func (p *plugin) afterTimer(d time.Duration, f func()) (stop func()) {
	// The timer's own context, so a stopped timer also ends the host's wait for it
	// instead of parking a goroutine until its time is up.
	ctx, cancel := context.WithCancel(p.pctx)
	tok, w := p.newWait(func(pluginapi.Completion) {
		cancel()
		f()
	}, nil)
	p.queueWork(tok, pluginapi.Work{After: max(d, time.Millisecond), Ctx: ctx})
	return func() {
		p.r.dropWait(w)
		cancel()
	}
}

// afterTask calls f on the UI goroutine when t has finished (returned, failed or been
// cancelled).
func (p *plugin) afterTask(t *task, f func()) {
	tok, _ := p.newWait(func(pluginapi.Completion) { f() }, nil)
	t.waiters = append(t.waiters, tok)
}

// timerTask starts fn as an event-class task (the budget of timers and of process, fs and
// watch callbacks) and queues it for the next Drain.
func (p *plugin) timerTask(label string, fn *lua.LFunction, args ...lua.LValue) (*task, error) {
	t, err := p.r.newTask(p, originTimer, fn, label)
	if err != nil {
		return nil, err
	}
	p.r.enqueue(item{t: t, args: args})
	return t, nil
}

// ---------- state store ----------

const (
	// maxState is the size cap of one plugin's state (keys plus encoded values).
	maxState = 1 << 20
	// stateDebounce is the write-behind delay.
	stateDebounce = 500 * time.Millisecond
	// stateFlushWait bounds how long Stop waits for a write already in flight.
	stateFlushWait = 2 * time.Second
	maxStateKey    = 256
)

// stateEntry is the state of one plugin name: the values as encoded JSON, and the writer
// chain that persists them. One write is in flight at a time; changes made meanwhile
// coalesce into the next write.
type stateEntry struct {
	io      *ioRuntime
	name    string
	vals    map[string]json.RawMessage
	size    int
	seeded  bool                // vals were read from the file (SeedState)
	touched map[string]struct{} // keys changed here; an unseeded file is merged around them

	dirty    bool
	armed    bool        // a debounce timer is pending
	inflight *stateWrite // the write Work in flight; nil when none
}

// stateWrite is one write Work. flush must not race with it: it either abandons a Work
// that never started, or waits for the one that did.
type stateWrite struct {
	phase atomic.Int32 // 0 queued, 1 running, 2 abandoned
	done  chan struct{}
}

// state returns the entry of name.
func (io *ioRuntime) state(name string) *stateEntry {
	e := io.states[name]
	if e == nil {
		e = &stateEntry{io: io, name: name, vals: map[string]json.RawMessage{}, touched: map[string]struct{}{}}
		io.states[name] = e
	}
	return e
}

// StateFile is the path of the state file of a plugin ("" when the OS knows no config
// directory). The loader reads it in its discovery Work and passes the bytes to SeedState.
func (r *Runtime) StateFile(name string) string {
	if r.workspaceStateFile != "" {
		// Workspace mode uses one file for every plugin; the per-plugin files are not
		// read or written this way (WorkspaceStatePath names the file).
		return ""
	}
	return stateFilePath(r.os.Dirs().Config, name)
}

// stateFilePath is the state file of plugin name below the configuration directory cfg.
func stateFilePath(cfg, name string) string {
	if cfg == "" {
		return ""
	}
	return filepath.Join(cfg, "plugin-data", name, "state.json")
}

// SeedState loads the content of a plugin's state file (nil or empty: no file). Call it on
// the UI goroutine before the plugin's main chunk runs. Values already set in memory are
// kept. Without a seed the state starts empty and the first write merges the file in the
// Work, so keys the plugin never touched survive.
func (r *Runtime) SeedState(name string, data []byte) error {
	e := r.io().state(name)
	e.seeded = true
	if len(data) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("state file of %s: %w", name, err)
	}
	for k, v := range m {
		if _, mine := e.touched[k]; mine {
			continue
		}
		if e.size+len(k)+len(v) > maxState {
			continue
		}
		e.put(k, v)
	}
	return nil
}

func (e *stateEntry) put(k string, v json.RawMessage) {
	if old, ok := e.vals[k]; ok {
		e.size -= len(k) + len(old)
	}
	e.vals[k] = v
	e.size += len(k) + len(v)
}

// set stores the encoded value; it reports a limit error when the state would exceed
// maxState.
func (e *stateEntry) set(k string, v json.RawMessage) error {
	if e.io.wsReadOnly {
		return pluginapi.Errf(pluginapi.CodeInvalid, "workspace plugin state %s is read-only: it was written by a newer gvim", e.io.wsPath)
	}
	extra := len(k) + len(v)
	if old, ok := e.vals[k]; ok {
		extra -= len(k) + len(old)
	}
	if e.size+extra > maxState {
		return pluginapi.Errf(pluginapi.CodeLimit, "state is limited to %d bytes", maxState)
	}
	e.put(k, v)
	e.touched[k] = struct{}{}
	e.changed()
	return nil
}

func (e *stateEntry) delete(k string) {
	if e.io.wsReadOnly {
		return
	}
	if old, ok := e.vals[k]; ok {
		e.size -= len(k) + len(old)
		delete(e.vals, k)
	}
	e.touched[k] = struct{}{}
	e.changed()
}

// changed marks the state dirty and starts the debounce when no write is pending.
func (e *stateEntry) changed() {
	if e.io.wsPath != "" {
		e.io.wsStateChanged()
		return
	}
	e.dirty = true
	e.arm()
}

// arm starts the debounce timer unless one is pending or a write is in flight (its
// completion arms again when the state changed meanwhile).
func (e *stateEntry) arm() {
	r := e.io.r
	if e.armed || e.inflight != nil || r.stopped || r.StateFile(e.name) == "" {
		return
	}
	e.armed = true
	tok := r.newToken()
	r.expectRuntime(tok, func(pluginapi.Completion) {
		e.armed = false
		e.write()
	})
	r.work = append(r.work, pluginapi.Work{Token: tok, Plugin: e.name, After: stateDebounce, Ctx: context.Background()})
}

// snapshot encodes the values as the JSON object that goes to disk.
func (e *stateEntry) snapshot() []byte {
	b, _ := json.Marshal(e.vals)
	return b
}

// write starts the write Work for the current content. Called from the debounce timer.
func (e *stateEntry) write() {
	r := e.io.r
	path := r.StateFile(e.name)
	if !e.dirty || e.inflight != nil || path == "" {
		return
	}
	e.dirty = false
	job := &stateWrite{done: make(chan struct{})}
	e.inflight = job
	data := e.snapshot()
	merge, mine := e.mergeSet()
	o := r.os
	tok := r.newToken()
	r.expectRuntime(tok, func(c pluginapi.Completion) {
		e.inflight = nil
		if c.Err != "" {
			r.logf(e.name, levelWarn, "saving state: "+c.Err)
		}
		if e.dirty {
			e.arm()
		}
	})
	r.work = append(r.work, pluginapi.Work{Token: tok, Plugin: e.name, Ctx: context.Background(), Run: func(ctx context.Context) pluginapi.Completion {
		defer close(job.done)
		if !job.phase.CompareAndSwap(0, 1) {
			return pluginapi.Completion{Token: tok} // abandoned by flush, which wrote newer data
		}
		if err := writeStateFile(ctx, o, path, data, merge, mine); err != nil {
			return pluginapi.Completion{Token: tok, Err: errString(err)}
		}
		return pluginapi.Completion{Token: tok}
	}})
}

// mergeSet returns, for an unseeded state, the keys this process touched and their values
// (nil, nil for a seeded one): writeStateFile keeps the other keys of the file.
func (e *stateEntry) mergeSet() (map[string]struct{}, map[string]json.RawMessage) {
	if e.seeded {
		return nil, nil
	}
	merge := make(map[string]struct{}, len(e.touched))
	mine := make(map[string]json.RawMessage, len(e.touched))
	for k := range e.touched {
		merge[k] = struct{}{}
		if v, ok := e.vals[k]; ok {
			mine[k] = v
		}
	}
	return merge, mine
}

// writeStateFile writes data atomically. With merge set (an unseeded state) the file is
// read first and the keys the plugin never touched are kept.
func writeStateFile(ctx context.Context, o hostos.OS, path string, data []byte, merge map[string]struct{}, mine map[string]json.RawMessage) error {
	if merge != nil {
		if old, err := o.ReadFile(ctx, path, maxStateFile); err == nil {
			var disk map[string]json.RawMessage
			if json.Unmarshal(old, &disk) == nil {
				for k, v := range disk {
					if _, touched := merge[k]; !touched {
						mine[k] = v
					}
				}
				data, _ = json.Marshal(mine)
			}
		}
	}
	return o.WriteFile(ctx, path, data, hostos.WriteOptions{Atomic: true, Parents: true, Mode: 0o600})
}

// flush writes pending changes synchronously, after a write in flight finished. Stop calls it.
func (e *stateEntry) flush() {
	r := e.io.r
	path := r.StateFile(e.name)
	redo := false
	if job := e.inflight; job != nil {
		if job.phase.CompareAndSwap(0, 2) {
			redo = true // the Work never started (the program is ending): write the content now
		} else {
			select {
			case <-job.done:
			case <-time.After(stateFlushWait):
			}
		}
	}
	if (!e.dirty && !redo) || path == "" {
		return
	}
	e.dirty = false
	merge, mine := e.mergeSet()
	ctx, cancel := context.WithTimeout(context.Background(), stateFlushWait)
	defer cancel()
	if err := writeStateFile(ctx, r.os, path, e.snapshot(), merge, mine); err != nil {
		r.logf(e.name, levelWarn, "saving state: "+errString(err))
	}
}

// ---------- workspace-scoped state

// queueWorkspaceWrite writes one snapshot of a workspace state namespace to path. Used when
// a namespace is left: the content captured at the switch is written to ITS file, whereas
// writes of the new namespace go to the new path.
func (io *ioRuntime) queueWorkspaceWrite(path string, data []byte) {
	r := io.r
	if path == "" || len(data) == 0 {
		return
	}
	if len(data) > maxStateFile {
		r.logf("", levelWarn, fmt.Sprintf("workspace plugin state is larger than %d bytes; not saved", maxStateFile))
		return
	}
	tok := r.newToken()
	r.expectRuntime(tok, func(c pluginapi.Completion) {
		if c.Err != "" {
			r.logf("", levelWarn, "saving workspace plugin state: "+c.Err)
		}
	})
	osys := r.os
	seq := io.wsNextSeq(path)
	r.work = append(r.work, pluginapi.Work{Token: tok, Ctx: context.Background(), Run: func(ctx context.Context) pluginapi.Completion {
		if io.wsStale(path, seq) {
			return pluginapi.Completion{Token: tok} // a newer write of the file owns it
		}
		if err := writeStateFile(ctx, osys, path, data, nil, nil); err != nil {
			return pluginapi.Completion{Token: tok, Err: errString(err)}
		}
		return pluginapi.Completion{Token: tok}
	}})
}

// wsSwitch leaves the current workspace state namespace and enters the next one. The old
// namespace's pending content is queued to its file (as Work, never blocking the caller);
// its in-memory entries are dropped and the new namespace's seed becomes the store. A
// pending debounce of the old namespace is disarmed (its completion finds no dirty state of
// the new namespace and writes nothing).
func (io *ioRuntime) wsSwitch(path string, st *WorkspaceState, readOnly bool) {
	if io.wsPath != "" && io.wsDirty {
		data := io.wsSnapshot()
		io.wsDirty = false
		io.wsArmed = false
		io.queueWorkspaceWrite(io.wsPath, data)
	}
	io.wsPath, io.wsReadOnly = path, readOnly
	io.wsDirty, io.wsArmed, io.wsBusy = false, false, false
	io.states = map[string]*stateEntry{}
	io.seedWorkspace(st)
}

// seedWorkspace loads the parsed workspace state into the plugin entries. It runs when the
// runtime first touches its IO state (on Start/plugin load, on the UI goroutine) and does no
// IO: the app read and parsed the file off the update loop. Values over the per-plugin quota
// or with an invalid key are skipped, exactly like a damaged per-plugin file is.
func (io *ioRuntime) seedWorkspace(st *WorkspaceState) {
	if st == nil || io.wsPath == "" {
		return
	}
	names := make([]string, 0, len(st.Plugins))
	for name := range st.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		vals := st.Plugins[name]
		if len(vals) == 0 {
			continue
		}
		e := &stateEntry{io: io, name: name, vals: map[string]json.RawMessage{}, touched: map[string]struct{}{}, seeded: true}
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := vals[k]
			if k == "" || len(k) > maxStateKey {
				continue
			}
			if e.size+len(k)+len(v) > maxState {
				continue
			}
			e.put(k, v)
		}
		io.states[name] = e
	}
}

// wsStateChanged marks the workspace file dirty and arms the write-behind. Every plugin's
// change coalesces into ONE write of the whole file.
func (io *ioRuntime) wsStateChanged() {
	if io.wsPath == "" || io.wsReadOnly {
		return
	}
	io.wsDirty = true
	io.wsArm()
}

// wsArm starts the debounce timer unless a write is pending or in flight.
func (io *ioRuntime) wsArm() {
	r := io.r
	if io.wsPath == "" || io.wsReadOnly || io.wsArmed || io.wsBusy || r.stopped {
		return
	}
	io.wsArmed = true
	tok := r.newToken()
	r.expectRuntime(tok, func(pluginapi.Completion) {
		io.wsArmed = false
		io.wsWrite()
	})
	r.work = append(r.work, pluginapi.Work{Token: tok, After: stateDebounce, Ctx: context.Background()})
}

// wsWrite queues one atomic write of the whole workspace state file. The file is bounded by
// maxStateFile: over the bound the write is skipped with a warning (the in-memory state
// stays, the file keeps its previous content).
func (io *ioRuntime) wsWrite() {
	r := io.r
	if io.wsPath == "" || io.wsReadOnly || !io.wsDirty || io.wsBusy {
		return
	}
	data := io.wsSnapshot()
	if len(data) > maxStateFile {
		io.wsDirty = false
		r.logf("", levelWarn, fmt.Sprintf("workspace plugin state is larger than %d bytes; not saved", maxStateFile))
		return
	}
	io.wsDirty = false
	io.wsBusy = true
	path, osys := io.wsPath, r.os
	seq := io.wsNextSeq(path)
	tok := r.newToken()
	r.expectRuntime(tok, func(c pluginapi.Completion) {
		io.wsBusy = false
		if c.Err != "" {
			r.logf("", levelWarn, "saving workspace plugin state: "+c.Err)
		}
		if io.wsDirty {
			io.wsArm()
		}
	})
	r.work = append(r.work, pluginapi.Work{Token: tok, Ctx: context.Background(), Run: func(ctx context.Context) pluginapi.Completion {
		if io.wsStale(path, seq) {
			return pluginapi.Completion{Token: tok} // a newer write (flush) owns the file
		}
		if err := writeStateFile(ctx, osys, path, data, nil, nil); err != nil {
			return pluginapi.Completion{Token: tok, Err: errString(err)}
		}
		return pluginapi.Completion{Token: tok}
	}})
}

// wsSnapshot encodes every plugin's state as the versioned workspace file. Map keys are
// marshalled sorted, so the file is deterministic.
func (io *ioRuntime) wsSnapshot() []byte {
	plugins := make(map[string]map[string]json.RawMessage, len(io.states))
	for name, e := range io.states {
		if len(e.vals) == 0 {
			continue
		}
		plugins[name] = e.vals
	}
	data, err := json.Marshal(WorkspaceState{Version: WorkspaceStateVersion, Plugins: plugins})
	if err != nil {
		return nil
	}
	return data
}

// wsFlush writes the workspace file synchronously on Stop. It invalidates a queued write so
// an older snapshot cannot land after it.
func (io *ioRuntime) wsFlush() {
	if io.wsPath == "" || io.wsReadOnly || !io.wsDirty {
		return
	}
	io.wsNextSeq(io.wsPath)
	io.wsArmed, io.wsBusy, io.wsDirty = false, false, false
	data := io.wsSnapshot()
	if len(data) == 0 || len(data) > maxStateFile {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), stateFlushWait)
	defer cancel()
	if err := writeStateFile(ctx, io.r.os, io.wsPath, data, nil, nil); err != nil {
		io.r.logf("", levelWarn, "saving workspace plugin state: "+errString(err))
	}
}
