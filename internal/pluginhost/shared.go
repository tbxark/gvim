package pluginhost

// The daemon's shared plugin runtime (#19b follow-up): one plugin.Runtime for every LOCAL
// daemon workspace, with the per-workspace parts rebound on a switch instead of a runtime
// per workspace. A direct launch keeps the one-runtime-per-session model (New). Remote /
// authority-spec workspaces are excluded from sharing in this slice: their plugin OS and
// trust gate are a different host, so they keep their own runtime.

import (
	"context"
	"sync"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// MuxOS is the shared runtime's plugin OS: every call runs on the ACTIVE workspace's OS
// instance, so files, processes, watchers, gvim.dirs and the spawn gate follow the active
// workspace. Shutdown is deferred: it shuts down every workspace OS registered with it
// (the runtime calls it once, at daemon exit).
type MuxOS struct {
	mu     sync.RWMutex
	active hostos.OS
	all    []hostos.OS
}

// SetActive binds the active workspace's OS.
func (m *MuxOS) SetActive(osys hostos.OS) {
	m.mu.Lock()
	m.active = osys
	m.mu.Unlock()
}

// Register adds a workspace OS to the set Shutdown ends.
func (m *MuxOS) Register(osys hostos.OS) {
	if osys == nil {
		return
	}
	m.mu.Lock()
	m.all = append(m.all, osys)
	m.mu.Unlock()
}

func (m *MuxOS) current() hostos.OS {
	m.mu.RLock()
	osys := m.active
	m.mu.RUnlock()
	if osys == nil {
		return hostos.NopOS{}
	}
	return osys
}

func (m *MuxOS) ReadFile(ctx context.Context, path string, max int64) ([]byte, error) {
	return m.current().ReadFile(ctx, path, max)
}
func (m *MuxOS) WriteFile(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error {
	return m.current().WriteFile(ctx, path, data, o)
}
func (m *MuxOS) ReadDir(ctx context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	return m.current().ReadDir(ctx, path, hidden)
}
func (m *MuxOS) Stat(ctx context.Context, path string) (hostos.FileStat, error) {
	return m.current().Stat(ctx, path)
}
func (m *MuxOS) Mkdir(ctx context.Context, path string, parents bool) error {
	return m.current().Mkdir(ctx, path, parents)
}
func (m *MuxOS) Remove(ctx context.Context, path string, recursive bool) error {
	return m.current().Remove(ctx, path, recursive)
}
func (m *MuxOS) Rename(ctx context.Context, from, to string) error {
	return m.current().Rename(ctx, from, to)
}
func (m *MuxOS) Copy(ctx context.Context, from, to string) error {
	return m.current().Copy(ctx, from, to)
}
func (m *MuxOS) RealPath(ctx context.Context, path string) (string, error) {
	return m.current().RealPath(ctx, path)
}
func (m *MuxOS) Walk(ctx context.Context, root string, o hostos.WalkOptions) (hostos.WalkResult, error) {
	return m.current().Walk(ctx, root, o)
}
func (m *MuxOS) Watch(ctx context.Context, path string, o hostos.WatchOptions) (hostos.Watch, error) {
	return m.current().Watch(ctx, path, o)
}
func (m *MuxOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	return m.current().Run(ctx, argv, o)
}
func (m *MuxOS) Start(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error) {
	return m.current().Start(ctx, argv, o)
}
func (m *MuxOS) LookPath(name string) (string, bool) { return m.current().LookPath(name) }
func (m *MuxOS) Getenv(name string) (string, bool)   { return m.current().Getenv(name) }
func (m *MuxOS) Dirs() hostos.Dirs                   { return m.current().Dirs() }
func (m *MuxOS) Now() time.Time                      { return m.current().Now() }

// Shutdown ends every registered workspace OS (live plugin processes and watchers).
func (m *MuxOS) Shutdown() {
	m.mu.Lock()
	all := append([]hostos.OS(nil), m.all...)
	m.all = nil
	m.mu.Unlock()
	for _, osys := range all {
		osys.Shutdown()
	}
}

// Workspace is one daemon workspace's plugin-side state: its own OS (root/env/gate), the
// editor as plugins see it, the keymap builder, and the gvim.state namespace. It refers to
// the daemon's one shared Runtime.
type Workspace struct {
	OS     hostos.OS
	Editor pluginapi.BufferEditor
	Keys   func(name string, bs []pluginapi.Binding) (*keymap.Map, error)
	// StatePath is sessions/<stable-id>/plugin-state.json; State is its parsed content
	// (nil empty) and ReadOnly marks a file written by a newer gvim.
	StatePath string
	State     *plugin.WorkspaceState
	ReadOnly  bool
	// StateNote explains a damaged or newer state file ("" when it parsed cleanly).
	StateNote string
}

// Daemon is one daemon's shared plugin runtime. Ensure builds it on the first workspace
// build (from that build's config; per-workspace project plugin config is a documented
// residual), Activate rebinds the shared runtime to a workspace, and Stop ends it once at
// daemon exit.
type Daemon struct {
	mu      sync.Mutex
	mux     *MuxOS
	runtime *plugin.Runtime
	keys    func(name string, bs []pluginapi.Binding) (*keymap.Map, error)
}

// NewDaemon returns a daemon with no runtime yet.
func NewDaemon() *Daemon {
	return &Daemon{mux: &MuxOS{}}
}

// Mux is the plugin OS of the shared runtime.
func (d *Daemon) Mux() *MuxOS { return d.mux }

// Runtime is the daemon's one plugin runtime, or nil before the first Ensure.
func (d *Daemon) Runtime() *plugin.Runtime {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.runtime
}

// Ensure builds the shared runtime on the first call and returns it with the keymap builder.
// Later calls ignore cfg/o (the daemon launch configuration owns the runtime).
func (d *Daemon) Ensure(cfg config.Config, o Options) (*plugin.Runtime, func(string, []pluginapi.Binding) (*keymap.Map, error)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.runtime == nil {
		ld := loaderOptions(cfg, o)
		ld.WorkspaceState = true
		d.runtime = plugin.New(plugin.Options{
			Timeout:            cfg.PluginTimeout,
			OS:                 d.mux,
			Services:           pluginServices{},
			Version:            o.Version,
			LogSink:            o.LogSink,
			Loader:             ld,
			Locale:             i18n.Locale(),
			WorkspaceStateFile: o.WorkspaceStateFile,
		})
		d.keys = pluginKeysFor(cfg, o)
	}
	return d.runtime, d.keys
}

// NewWorkspace builds the per-workspace plugin parts without a runtime: the plugin OS (its
// own root/env/gate, registered with the daemon's mux), the editor adapter, the keymap
// builder and the parsed gvim.state namespace. buildRuntime calls it for every local daemon
// workspace.
func NewWorkspace(cfg config.Config, ed *editor.Editor, markEdited func(*editor.Document), o Options) *Workspace {
	osys := newPluginOS(pluginDirs(configDir(), o.Root), o.SpawnEnv)
	osys.gate = o.Gate
	ws := &Workspace{
		OS:     osys,
		Editor: newPluginEditor(ed, markEdited),
		Keys:   pluginKeysFor(cfg, o),
	}
	if o.WorkspaceStateFile != "" {
		ws.StatePath = o.WorkspaceStateFile
		st, readOnly, err := parsedWorkspaceState(o)
		switch {
		case err != nil:
			ws.StateNote = "per-workspace plugin state ignored: " + err.Error()
		case readOnly:
			ws.ReadOnly = true
			ws.StateNote = "per-workspace plugin state is read-only: it was written by a newer gvim"
		default:
			ws.State = st
		}
	}
	return ws
}

// Activate rebinds the shared runtime to ws: its OS becomes the mux target and its
// gvim.state namespace becomes the runtime's store (the previous namespace is flushed as
// Work, not IO on the update loop).
func (d *Daemon) Activate(ws *Workspace) {
	if ws == nil {
		return
	}
	d.mux.SetActive(ws.OS)
	d.mux.Register(ws.OS)
	d.mu.Lock()
	rt := d.runtime
	d.mu.Unlock()
	if rt != nil && ws.StatePath != "" {
		rt.SwitchWorkspaceState(ws.StatePath, ws.State, ws.ReadOnly)
	}
}

// Stop ends the shared runtime (shutdown handlers, plugin processes, state flush) once.
func (d *Daemon) Stop() {
	d.mu.Lock()
	rt := d.runtime
	d.mu.Unlock()
	if rt != nil {
		rt.Stop()
		return
	}
	if d.mux != nil {
		d.mux.Shutdown()
	}
}

func (d *Daemon) Wired(ws *Workspace) *Wiring {
	d.mu.Lock()
	rt := d.runtime
	d.mu.Unlock()
	w := &Wiring{Runtime: rt, Editor: ws.Editor, OS: ws.OS, Keys: ws.Keys}
	return w
}

// configDir is config.Dir() as a string ("" when unavailable).
func configDir() string {
	dir, _ := config.Dir()
	return dir
}
