package plugin

import (
	"context"
	"fmt"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"

	"github.com/tbxark/gvim/pluginapi"
)

// Plugin statuses (pluginapi.PluginInfo.Status).
const (
	statusLoading   = "loading"
	statusLoaded    = "loaded"
	statusFailed    = "failed"
	statusSuspended = "suspended"
	statusDisabled  = "disabled"
)

// Plugin source kinds (pluginapi.PluginInfo.Source).
const (
	SourceBundled = "bundled"
	SourceUser    = "user"
	SourceCLI     = "cli"
	SourceInit    = "init"
	SourceBuffer  = "buffer"
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// MaxLuaFile is the largest .lua file the runtime compiles (AMENDMENTS C20).
const MaxLuaFile = 256 << 10

// Compiled is one compiled file of a plugin: a state-independent prototype, or the error
// that stopped it. Compiling happens in Work, never on the UI goroutine.
type Compiled struct {
	Proto *lua.FunctionProto
	Err   error
}

// Compile compiles every .lua file of a plugin (key: path relative to the plugin
// directory, slash-separated). Chunk names are "<plugin>/<relpath>". It is pure and safe to
// call from any goroutine.
func Compile(plugin string, files map[string]string) map[string]*Compiled {
	out := make(map[string]*Compiled, len(files))
	for rel, src := range files {
		if !strings.HasSuffix(rel, ".lua") {
			continue
		}
		proto, err := CompileSource(plugin+"/"+rel, src)
		out[rel] = &Compiled{Proto: proto, Err: err}
	}
	return out
}

// CompileSource compiles one chunk. The name is used verbatim in messages.
func CompileSource(chunk, src string) (*lua.FunctionProto, error) {
	if len(src) > MaxLuaFile {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "%s: source too large (%d bytes, max %d)", chunk, len(src), MaxLuaFile)
	}
	stmts, err := parse.Parse(strings.NewReader(src), chunk)
	if err != nil {
		return nil, readableCompileError(chunk, src, err)
	}
	proto, err := lua.Compile(stmts, chunk)
	if err != nil {
		return nil, readableCompileError(chunk, src, err)
	}
	return proto, nil
}

// Source describes one plugin to load.
type Source struct {
	Name, Version string
	Description   string
	Requires      []string
	Kind          string // SourceBundled ... SourceBuffer
	Dir           string // "" for single-file and buffer plugins
	File          string // absolute path of the entry file; "" for bundled and buffer plugins
	Main          string // entry file, relative to the plugin; "" means init.lua
	// I18nJSON is the plugin's <name>.i18n.json sibling: a JSON object of locale -> key ->
	// text ("" when the plugin has none). Read by the loader in Work, before the plugin
	// runs.
	I18nJSON string
	Compiled map[string]*Compiled
	// Settings and ProjectSettings are plugins.<name>.settings of the user and of the project
	// layer of the config; gvim.settings.define merges them over the defaults (AMENDMENTS C11).
	Settings, ProjectSettings map[string]any
}

// plugin is the runtime state of one plugin: its Lua state, tasks, registrations and
// counters. A tombstone (failed or disabled) keeps only the row for the manager.
type plugin struct {
	r           *Runtime
	name        string
	version     string
	description string
	requires    []string
	dir         string
	file        string // Source.File
	kindStr     string // pluginapi.PluginInfo.Source
	gen         uint64
	status      string
	errMsg      string
	dead        bool

	L   *lua.LState
	tag *lua.LUserData // the unforgeable yield tag
	// awaitFrom is the thread that made the await being served, for error positions.
	awaitFrom *lua.LState
	yielded   bool        // a coroutine yielded since the last resume started
	goPanic   *goPanic    // the last Go panic of a guarded API function in this resume
	pre       *lua.LTable // functions returned by the prelude
	gvim      *lua.LTable

	pctx    context.Context
	pcancel context.CancelCauseFunc

	roots   map[*lua.LState]*task
	tasks   map[*task]struct{}
	mainT   *task
	coDepth int
	// resuming holds the threads that are inside a resume right now (an ancestor of the running
	// thread has status "normal"): resuming one again would recurse without bound in the Go VM.
	resuming map[*lua.LState]struct{}
	parked   int // waits with a parked task
	evq      []item

	inflight int // waits with Work
	strikes  []time.Time
	decorNS  map[string]bool // the decoration namespaces the plugin made (cleared when it is suspended)
	pace     timerPace       // the timer pace budget (timerpace.go)

	stormDrains int // consecutive Drains that hit the cascade cap with this plugin's events queued
	userStorm   int // consecutive cut Drains with this plugin's tasks or commands still queued

	commands map[string]*command
	panels   map[pluginapi.PanelID]bool // the widget panels the plugin mounted (api_panel.go)
	// i18n is the plugin's translation table by locale (api_i18n.go); nil when it has none.
	i18n map[string]map[string]string
	// menuItems are the rows the plugin added to the menu bar (api_menu.go).
	menuItems  []pluginapi.MenuItem
	bindWarned map[string]bool // key bindings of an unknown action already named (lint_bindings.go)
	subs       map[string][]*subscription
	nsubs      int
	susp       *suspendTable
	fnSeq      int

	files    map[string]*Compiled
	main     string
	loaded   map[string]lua.LValue
	loading  map[string]bool
	reqDepth int

	kinds   map[string]*handleKind
	handles map[idHandle]*lua.LUserData
	areas   map[string]*lua.LTable
	mk      map[string]lua.LValue
	mkv     map[string]lua.LValue
	feature map[string]bool

	data dataState // settings, exports and caches of the data API (api_data.go)
	// dashboard are the dashboard sections this plugin registered (api_dashboard.go).
	dashboard    map[string]*dashboardSection // by name
	dashboardSeq int
	unloadFns    []*lua.LFunction
	cleanups     []func()
	format       lua.LValue // the stock string.format, for logging
	tostring     lua.LValue // the stock tostring
}

// muted plugins receive no events, timers or key layers (failed, suspended or gone).
func (p *plugin) muted() bool {
	return p.dead || p.status == statusSuspended || p.status == statusFailed
}

func (p *plugin) info() pluginapi.PluginInfo {
	n := 0
	for _, l := range p.subs {
		for _, s := range l {
			if s.active {
				n++
			}
		}
	}
	return pluginapi.PluginInfo{
		Name: p.name, Version: p.version, Description: p.description, Requires: append([]string(nil), p.requires...),
		Source: p.kindStr, Dir: p.dir, File: p.file,
		Status: p.status, Err: p.errMsg,
		Commands: len(p.commands), Subscriptions: n,
		Enabled: p.status != statusDisabled,
	}
}

func (p *plugin) mainFinished() {
	if p.status == statusLoading {
		p.status = statusLoaded
	}
}

// addCleanup registers a function unload calls (in reverse order) to release what an API
// area holds for the plugin: timers, watchers, processes.
func (p *plugin) addCleanup(f func()) { p.cleanups = append(p.cleanups, f) }

// ---------- commands ----------

// command is a plugin command; the function stays here, the host only sees CommandInfo.
type command struct {
	id, name string
	fn       lua.LValue
	info     pluginapi.CommandInfo
}

// findCommand resolves the action id "<plugin>.<name>".
func (r *Runtime) findCommand(id string) (*command, *plugin) {
	name, rest, ok := strings.Cut(id, ".")
	if !ok {
		return nil, nil
	}
	p := r.plugins[name]
	if p == nil || p.dead || p.L == nil {
		return nil, nil
	}
	c := p.commands[rest]
	if c == nil {
		return nil, nil
	}
	return c, p
}

// addCommand registers (or replaces) a command and publishes the plugin's command set.
func (p *plugin) addCommand(c *command) error {
	if _, ok := p.commands[c.name]; !ok && len(p.commands) >= QuotaCommands {
		return pluginapi.Errf(pluginapi.CodeLimit, "command quota (%d)", QuotaCommands)
	}
	p.commands[c.name] = c
	p.syncCommands()
	return nil
}

func (p *plugin) removeCommand(name string) {
	if _, ok := p.commands[name]; ok {
		delete(p.commands, name)
		p.syncCommands()
	}
}

// syncCommands replaces the host's command set of this plugin.
func (p *plugin) syncCommands() {
	infos := make([]pluginapi.CommandInfo, 0, len(p.commands))
	for _, c := range p.commands {
		infos = append(infos, c.info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	p.r.host.SetCommands(p.name, infos)
}

// ---------- subscriptions ----------

// subscription is a registered event handler.
type subscription struct {
	id       uint64
	p        *plugin
	event    string
	fn       *lua.LFunction
	once     bool
	active   bool
	failures int
	filter   func(pluginapi.Event) bool // nil: every event
}

func (s *subscription) accepts(e pluginapi.Event) bool { return s.filter == nil || s.filter(e) }

func (p *plugin) addSub(event string, fn *lua.LFunction, once bool) (*subscription, error) {
	if p.nsubs >= QuotaSubscriptions {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "subscription quota (%d)", QuotaSubscriptions)
	}
	p.fnSeq++
	s := &subscription{id: uint64(p.fnSeq), p: p, event: event, fn: fn, once: once, active: true}
	p.subs[event] = append(p.subs[event], s)
	p.nsubs++
	p.r.wantsChanged()
	return s, nil
}

func (p *plugin) removeSub(s *subscription) {
	if !s.active {
		return
	}
	s.active = false
	p.nsubs--
	p.r.wantsChanged()
	l := p.subs[s.event]
	for i, x := range l {
		if x == s {
			p.subs[s.event] = append(l[:i:i], l[i+1:]...)
			break
		}
	}
	if len(p.subs[s.event]) == 0 {
		delete(p.subs, s.event)
	}
}

// ---------- loading ----------

// LoadPlugin creates the state of a plugin and starts its main chunk as a task (budget
// class "load"). The main chunk may await; the status is "loading" until it returns. A
// failure to start, or later in the main chunk, leaves the plugin failed with everything it
// registered rolled back; see Plugins. All files must be compiled (Compile).
//
// Call it on the UI goroutine. Discovery, ordering and dependencies are the loader's job
// (stage S10); this is the entry it builds on.
func (r *Runtime) LoadPlugin(s Source) error {
	defer r.enter("LoadPlugin")()
	if r.stopped {
		return pluginapi.Errf(pluginapi.CodeClosed, "the plugin runtime is stopped")
	}
	if !nameRE.MatchString(s.Name) {
		return pluginapi.Errf(pluginapi.CodeInvalid, "invalid plugin name %q (want [a-z][a-z0-9_]*, at most 40 characters)", s.Name)
	}
	if old := r.plugins[s.Name]; old != nil {
		if !old.dead {
			return pluginapi.Errf(pluginapi.CodeConflict, "plugin %q is already loaded", s.Name)
		}
		r.forget(old) // a tombstone (failed or disabled): replace it
	}
	main := s.Main
	if main == "" {
		main = "init.lua"
	}
	r.nextGen++
	p := &plugin{
		r: r, name: s.Name, version: s.Version, description: s.Description, requires: append([]string(nil), s.Requires...),
		dir: s.Dir, file: s.File, kindStr: s.Kind, gen: r.nextGen,
		status: statusLoading, main: main, files: s.Compiled,
		roots: map[*lua.LState]*task{}, tasks: map[*task]struct{}{},
		commands: map[string]*command{}, subs: map[string][]*subscription{},
		loaded: map[string]lua.LValue{}, loading: map[string]bool{},
		kinds: map[string]*handleKind{}, handles: map[idHandle]*lua.LUserData{},
		areas: map[string]*lua.LTable{}, mk: map[string]lua.LValue{}, mkv: map[string]lua.LValue{},
		feature: map[string]bool{},
		data:    dataState{user: s.Settings, project: s.ProjectSettings},
	}
	if s.I18nJSON != "" {
		p.i18n = parseI18n(r, p.name, s.I18nJSON)
	}
	r.plugins[p.name] = p
	r.order = append(r.order, p)
	r.wantsChanged()

	c := s.Compiled[main]
	switch {
	case c == nil:
		return r.failStart(p, pluginapi.Errf(pluginapi.CodeNotFound, "%s/%s not found", s.Name, main))
	case c.Err != nil:
		return r.failStart(p, c.Err)
	}
	p.pctx, p.pcancel = context.WithCancelCause(context.Background())
	if err := r.initState(p); err != nil {
		return r.failStart(p, err)
	}
	fn := p.L.NewFunctionFromProto(c.Proto)
	t, err := r.newTask(p, originLoad, fn, "load")
	if err != nil {
		return r.failStart(p, err)
	}
	p.mainT = t
	if r.depth > 0 {
		r.enqueue(item{t: t})
	} else {
		r.runTask(t, nil)
	}
	return nil
}

// failStart records a plugin that could not start as failed and returns the error.
func (r *Runtime) failStart(p *plugin, err error) error {
	r.logf(p.name, levelError, "load failed: "+err.Error())
	r.unload(p, unloadOptions{final: statusFailed, err: err.Error(), quiet: true})
	return err
}

// initState builds the sandboxed state of p, installs the API and keeps panics inside.
func (r *Runtime) initState(p *plugin) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("creating the plugin state: %v\n%s", rec, debug.Stack())
		}
	}()
	return p.buildState()
}

func stackString() string { return string(debug.Stack()) }

// UnloadPlugin removes a plugin completely: its on_unload handlers run, then everything it
// owns goes (see unload). Unknown names are ignored.
func (r *Runtime) UnloadPlugin(name string) {
	defer r.enter("UnloadPlugin")()
	if p := r.plugins[name]; p != nil {
		r.unload(p, unloadOptions{})
	}
}

// unloadOptions tune unload.
type unloadOptions struct {
	final string // status of the tombstone left behind ("" removes the plugin from the list)
	err   string // error text of a failed tombstone
	quiet bool   // skip on_unload (Stop, failed starts)
}

// unload releases everything a plugin owns, in this order: on_unload handlers (200 ms in all,
// no yield); cancel the plugin context and bump the generation (parked tasks, Work and timers
// can no longer be resumed); drop waits and queued work, resolve the waits other plugins
// have into this one; run the cleanups of the API areas (timers, watchers, processes);
// tell the host to drop commands, bindings, modes, overlays, decorations and key waits;
// close the state. Afterwards the host resource set and the runtime registry equal their
// state before the load.
func (r *Runtime) unload(p *plugin, o unloadOptions) {
	if p.dead {
		return
	}
	if p.L != nil && !o.quiet && r.depth == 0 {
		end := time.Now().Add(unloadBudget) // one budget for all the hooks together
		for _, fn := range p.unloadFns {
			left := time.Until(end)
			if left <= 0 {
				r.logf(p.name, levelWarn, "on_unload: budget spent, remaining hooks skipped")
				break
			}
			if _, rerr := r.callSync(p, fn, left, "on_unload"); rerr != nil {
				r.logf(p.name, levelWarn, "on_unload: "+rerr.msg)
			}
		}
	}
	p.dead = true
	r.wantsChanged()
	if p.pcancel != nil {
		p.pcancel(errCancelled)
	}
	// Waits: its own are abandoned, other plugins' calls into it fail.
	for _, w := range r.waits {
		switch {
		case w.p == p:
			r.cancelWait(w)
		case w.target == p.name && w.p != nil && !w.p.dead:
			r.inbox = append(r.inbox, pluginapi.Completion{Token: w.token, Err: "closed: plugin reloaded"})
		}
	}
	for t := range p.tasks {
		t.done = true
		if t.cancelThread != nil {
			t.cancelThread()
		}
	}
	p.tasks = map[*task]struct{}{}
	p.roots = map[*lua.LState]*task{}
	p.evq = nil
	r.userq = filterItems(r.userq, p)
	r.cmdq = filterCmds(r.cmdq, p)
	for i := len(p.cleanups) - 1; i >= 0; i-- {
		p.cleanups[i]()
	}
	p.cleanups = nil
	r.host.OwnerGone(p.name, p.gen)
	p.commands = map[string]*command{}
	p.subs = map[string][]*subscription{}
	p.nsubs = 0
	p.unloadFns = nil
	p.handles = map[idHandle]*lua.LUserData{}
	if p.L != nil {
		if r.closeHook != nil {
			r.closeHook(p)
		}
		func() {
			defer func() { _ = recover() }()
			p.L.Close()
		}()
		p.L = nil
	}
	p.gvim, p.pre, p.tag = nil, nil, nil
	p.kinds, p.areas, p.mk, p.mkv = map[string]*handleKind{}, map[string]*lua.LTable{}, map[string]lua.LValue{}, map[string]lua.LValue{}
	p.loaded, p.loading, p.files = map[string]lua.LValue{}, map[string]bool{}, nil

	if o.final == "" {
		r.forget(p)
		return
	}
	p.status, p.errMsg = o.final, o.err
}

// forget removes p from the registry.
func (r *Runtime) forget(p *plugin) {
	r.wantsChanged()
	if r.plugins[p.name] == p {
		delete(r.plugins, p.name)
	}
	for i, x := range r.order {
		if x == p {
			r.order = append(r.order[:i:i], r.order[i+1:]...)
			break
		}
	}
}

func filterItems(q []item, p *plugin) []item {
	out := q[:0]
	for _, it := range q {
		if it.t.p != p {
			out = append(out, it)
		}
	}
	return out
}

func filterCmds(q []cmdReq, p *plugin) []cmdReq {
	out := q[:0]
	for _, c := range q {
		if c.p != p {
			out = append(out, c)
		}
	}
	return out
}
