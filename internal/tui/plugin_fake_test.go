package tui

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// fakeRuntime is a pluginapi.Runtime for the host tests. It records every call. While a
// "slice" runs (onRun, onResolve, onStart, Drain's hook) InSlice reports true, as the
// real runtime does. With strictSlice set, a call that the contract forbids inside a
// slice fails the test: Run, Resolve, Control, Veto, Drain and Stop.
type fakeRuntime struct {
	t *testing.T

	owned   map[string]bool
	wants   map[string]bool
	plugins []pluginapi.PluginInfo // what Plugins reports, in load order

	inSlice     bool
	strictSlice bool

	host pluginapi.Host

	onStart   func() []pluginapi.Work
	onRun     func(action string, args map[string]any)
	onResolve func(c pluginapi.Completion)
	onDrain   func() ([]pluginapi.Work, bool)
	onControl func(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice)
	veto      func(e pluginapi.Event) (bool, string)
	runErr    error

	log       []string // "start", "run:x", "resolve", "drain", "control:op", "veto:name", "emit:name", "stop"
	runs      []string
	runArgs   []map[string]any
	runSrc    []pluginapi.CommandSource
	resolved  []pluginapi.Completion
	emitted   []pluginapi.Event
	vetoed    []pluginapi.Event
	controls  []pluginapi.Control
	vetoDepth []bool // InSlice at each Veto call
	stops     int
	drains    int
	budgets   []time.Duration // the budget of each Drain
}

var _ pluginapi.Runtime = (*fakeRuntime)(nil)

func newFakeRuntime(t *testing.T, owned ...string) *fakeRuntime {
	r := &fakeRuntime{t: t, owned: map[string]bool{}, wants: map[string]bool{}}
	for _, o := range owned {
		r.owned[o] = true
	}
	return r
}

// slice runs fn as Lua would run: with InSlice true.
func (r *fakeRuntime) slice(fn func()) {
	was := r.inSlice
	r.inSlice = true
	defer func() { r.inSlice = was }()
	fn()
}

func (r *fakeRuntime) forbid(call string) {
	r.t.Helper()
	if r.strictSlice && r.inSlice {
		r.t.Errorf("Runtime.%s was called while a slice was running", call)
	}
}

func (r *fakeRuntime) Start(h pluginapi.Host) []pluginapi.Work {
	r.log = append(r.log, "start")
	r.host = h
	if r.onStart == nil {
		return nil
	}
	var w []pluginapi.Work
	r.slice(func() { w = r.onStart() })
	return w
}

func (r *fakeRuntime) Stop() {
	r.forbid("Stop")
	r.stops++
	r.log = append(r.log, "stop")
}

func (r *fakeRuntime) Wants(e string) bool { return r.wants[e] }

func (r *fakeRuntime) Emit(e pluginapi.Event) {
	r.emitted = append(r.emitted, e)
	r.log = append(r.log, "emit:"+e.Name)
}

func (r *fakeRuntime) Veto(e pluginapi.Event) (bool, string) {
	r.forbid("Veto")
	r.vetoed = append(r.vetoed, e)
	r.vetoDepth = append(r.vetoDepth, r.inSlice)
	r.log = append(r.log, "veto:"+e.Name)
	if r.veto == nil {
		return false, ""
	}
	return r.veto(e)
}

func (r *fakeRuntime) Owns(action string) bool { return r.owned[action] }
func (r *fakeRuntime) InSlice() bool           { return r.inSlice }

func (r *fakeRuntime) Run(action string, args map[string]any, src pluginapi.CommandSource) error {
	r.forbid("Run")
	r.runs = append(r.runs, action)
	r.runArgs = append(r.runArgs, args)
	r.runSrc = append(r.runSrc, src)
	r.log = append(r.log, "run:"+action)
	if r.onRun != nil {
		r.slice(func() { r.onRun(action, args) })
	}
	return r.runErr
}

func (r *fakeRuntime) Resolve(c pluginapi.Completion) {
	r.forbid("Resolve")
	r.resolved = append(r.resolved, c)
	r.log = append(r.log, "resolve")
	if r.onResolve != nil {
		r.slice(func() { r.onResolve(c) })
	}
}

func (r *fakeRuntime) Drain(budget time.Duration) ([]pluginapi.Work, bool) {
	r.forbid("Drain")
	r.drains++
	r.budgets = append(r.budgets, budget)
	r.log = append(r.log, "drain")
	if r.onDrain == nil {
		return nil, false
	}
	var w []pluginapi.Work
	var more bool
	r.slice(func() { w, more = r.onDrain() })
	return w, more
}

func (r *fakeRuntime) Plugins() []pluginapi.PluginInfo { return r.plugins }
func (r *fakeRuntime) Log() []pluginapi.LogEntry       { return nil }

func (r *fakeRuntime) Control(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
	r.forbid("Control")
	r.controls = append(r.controls, c)
	r.log = append(r.log, "control:"+c.Op)
	if r.onControl == nil {
		return nil, nil
	}
	return r.onControl(c)
}

// newPluginModel returns a sized model over a fake editor with rt as its plugin runtime.
func newPluginModel(t *testing.T, rt pluginapi.Runtime) (*Model, *fakeEditor) {
	t.Helper()
	ed := newFakeEditor("hello", "world")
	ed.quitAction = "quit"
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	return m, ed
}

// useHost lets a fake runtime call the model's host without a Start, as Lua would after one.
func useHost(rt pluginapi.Runtime, m *Model) {
	if fr, ok := rt.(*fakeRuntime); ok {
		fr.host = m.plug.host
	}
}

// fakePluginEditor is the editor port of the plugin host: a list of buffers in tab
// order, the active one, and a record of the calls the host makes.
type fakePluginEditor struct {
	pluginapi.Unsupported
	bufs   []pluginapi.BufferInfo
	active pluginapi.BufferID
	ed     *fakeEditor // its tabs follow Close and MoveTab

	activations []pluginapi.BufferID
	closed      []pluginapi.BufferID
	moves       []int
	groups      int
	cursors     []pluginapi.Pos
	contents    map[pluginapi.BufferID][]pluginapi.ContentEntry
	created     []pluginapi.BufferID
}

// CreateBuffer makes a virtual buffer for the tests: it joins the port's list and the fake
// editor's tabs, so the split tree can show it.
func (f *fakePluginEditor) CreateBuffer(o pluginapi.CreateBuffer) (pluginapi.BufferID, error) {
	id := pluginapi.BufferID(len(f.bufs) + 1)
	for _, b := range f.bufs {
		id = max(id, b.ID+1)
	}
	f.bufs = append(f.bufs, pluginapi.BufferInfo{ID: id, Name: o.Name, Virtual: true, ReadOnly: o.ReadOnly})
	if f.ed != nil {
		f.ed.tabs = append(f.ed.tabs, TabInfo{Name: o.Name})
	}
	f.created = append(f.created, id)
	return id, nil
}

// SetContent stores a virtual buffer's content.
func (f *fakePluginEditor) SetContent(id pluginapi.BufferID, entries []pluginapi.ContentEntry) error {
	if _, ok := f.Buffer(id); !ok {
		return pluginapi.Errf(pluginapi.CodeClosed, "no buffer %d", id)
	}
	if f.contents == nil {
		f.contents = map[pluginapi.BufferID][]pluginapi.ContentEntry{}
	}
	f.contents[id] = append([]pluginapi.ContentEntry(nil), entries...)
	return nil
}

func (f *fakePluginEditor) ListBuffers() []pluginapi.BufferInfo { return f.bufs }
func (f *fakePluginEditor) ActiveBuffer() pluginapi.BufferID    { return f.active }
func (f *fakePluginEditor) Buffer(id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	for _, b := range f.bufs {
		if b.ID == id {
			return b, true
		}
	}
	return pluginapi.BufferInfo{}, false
}

func (f *fakePluginEditor) FindBuffer(path string) pluginapi.BufferID {
	for _, b := range f.bufs {
		if b.Path == path {
			return b.ID
		}
	}
	return 0
}

func (f *fakePluginEditor) Activate(id pluginapi.BufferID) error {
	if _, ok := f.Buffer(id); !ok {
		return pluginapi.Errf(pluginapi.CodeClosed, "no buffer %d", id)
	}
	f.active = id
	f.activations = append(f.activations, id)
	return nil
}

func (f *fakePluginEditor) Close(id pluginapi.BufferID, force bool) error {
	for i, b := range f.bufs {
		if b.ID == id {
			f.bufs = append(f.bufs[:i:i], f.bufs[i+1:]...)
			if f.ed != nil {
				f.ed.tabs = append(f.ed.tabs[:i:i], f.ed.tabs[i+1:]...)
			}
			f.closed = append(f.closed, id)
			return nil
		}
	}
	return pluginapi.Errf(pluginapi.CodeClosed, "no buffer %d", id)
}

func (f *fakePluginEditor) MoveTab(id pluginapi.BufferID, delta int) error {
	for i, b := range f.bufs {
		if b.ID != id {
			continue
		}
		j := min(max(i+delta, 0), len(f.bufs)-1)
		f.bufs = slices.Delete(f.bufs, i, i+1)
		f.bufs = slices.Insert(f.bufs, j, b)
		if f.ed != nil {
			t := f.ed.tabs[i]
			f.ed.tabs = slices.Delete(f.ed.tabs, i, i+1)
			f.ed.tabs = slices.Insert(f.ed.tabs, j, t)
		}
		f.moves = append(f.moves, delta)
		return nil
	}
	return pluginapi.Errf(pluginapi.CodeClosed, "no buffer %d", id)
}

func (f *fakePluginEditor) UndoGroup(id pluginapi.BufferID, fn func() error) error {
	f.groups++
	return fn()
}

func (f *fakePluginEditor) SetCursor(id pluginapi.BufferID, p pluginapi.Pos, extend, reveal, center bool) error {
	f.cursors = append(f.cursors, p)
	return nil
}

// newPluginEditorModel is newPluginModel with a plugin editor port of three buffers.
func newPluginEditorModel(t *testing.T, rt pluginapi.Runtime) (*Model, *fakeEditor, *fakePluginEditor) {
	t.Helper()
	ed := newFakeEditor("hello", "world")
	ed.quitAction = "quit"
	ed.tabs = []TabInfo{{Name: "a.txt", Path: "/w/a.txt", Active: true}, {Name: "b.txt", Path: "/w/b.txt"}, {Name: "c.txt", Path: "/w/c.txt"}}
	pe := &fakePluginEditor{ed: ed, active: 1, bufs: []pluginapi.BufferInfo{
		{ID: 1, Path: "/w/a.txt", Name: "a.txt", Active: true},
		{ID: 2, Path: "/w/b.txt", Name: "b.txt"},
		{ID: 3, Path: "/w/c.txt", Name: "c.txt"},
	}}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	return m, ed, pe
}
