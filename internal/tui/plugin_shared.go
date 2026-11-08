package tui

// The daemon's shared plugin host (#19b shared-runtime slice). One plugin.Runtime runs for
// the whole daemon; another workspace is not a second runtime but a REBINDING of that
// runtime's host and state namespace. This file holds the mux the runtime's host calls
// route through (pluginHost.model reads PluginShared.Active) and the editor-wide
// registration snapshot carried across a switch. Chrome that belongs to one workspace - the
// widget panels and sidebar sections, the prompt/picker/popup overlays, per-buffer
// decorations, folds, groups, view state - stays in the model where it was registered,
// matching Fresh's default window scope for chrome; the workspace's gvim.state is the
// per-workspace namespace the app switches on the runtime.

import (
	"sync"

	"github.com/tbxark/gvim/pluginapi"
)

// PluginShared is the daemon-wide plugin host mux: one pluginHost and the editor-wide
// registrations every workspace model shares. The app creates one per daemon and passes it
// to every workspace model's RootPorts; Bind runs on a workspace switch (on the UI
// goroutine) and snapshots the leaving model's editor-wide registrations before installing
// them into the entering model, so a plugin command/menu/key binding registered once is
// visible in every workspace while per-window chrome stays per window.
type PluginShared struct {
	mu     sync.RWMutex
	active *Model
	host   *pluginHost
	wide   pluginWide
}

// pluginWide is the editor-wide registration snapshot carried across a workspace switch:
// commands, menus and the plugin key/mode/context/status registrations. Panels, overlays and
// buffer-scoped state are deliberately NOT here (they belong to the workspace).
type pluginWide struct {
	commands map[string][]pluginapi.CommandInfo
	order    []string
	menus    menus
	keys     pluginKeysWide
	// chrome is the editor-scoped widget chrome (panels and sidebar sections) every
	// workspace shows. Window-scoped chrome stays model-local; a buffer-scoped mount is
	// never carried, because it belongs to one buffer of one workspace.
	chrome []editorChromeSpec
}

// editorChromeSpec is one editor-scoped mount carried across a workspace switch. The mount
// spec is shared read-only; each entering model builds its own panel instance (the widget
// instance state is not cloned, see the residual in docs/architecture.md).
type editorChromeSpec struct {
	id    pluginapi.PanelID
	owner string
	mount pluginapi.WidgetMount
}

// pluginKeysWide is the editor-wide half of pluginKeys: the registrations and contexts, not
// the live input state (sequence, wait, active mode) of one workspace's update loop.
type pluginKeysWide struct {
	bindings map[string][]pluginapi.Binding
	border   []string
	gated    map[string][]pluginapi.Binding
	modes    map[string][]*uiMode
	morder   []string
	ctx      map[string]map[string]bool
	ctxSeen  map[string]bool
	warned   map[string]bool
	status   statusState
}

// NewPluginShared returns the daemon-wide host mux. The app binds the active model before
// the program starts and on every workspace switch.
func NewPluginShared() *PluginShared {
	s := &PluginShared{}
	s.host = &pluginHost{shared: s}
	return s
}

// Active is the model the runtime's host calls route to, or nil before the first Bind.
func (s *PluginShared) Active() *Model {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// Bind makes m the active host target on the UI goroutine. It snapshots the editor-wide
// registrations of the model being left (if any) into the shared snapshot, installs the
// snapshot into m, and only then makes m active: a plugin loaded once keeps its commands,
// menus and key bindings in every workspace.
func (s *PluginShared) Bind(m *Model) {
	if m == nil {
		return
	}
	s.mu.Lock()
	prev := s.active
	if prev != nil {
		s.wide.capture(prev)
	}
	s.active = m
	wide := s.wide.clone()
	s.mu.Unlock()
	if prev == nil {
		// First bind: the snapshot is empty; keep the model's own registrations (there are
		// none before the runtime starts).
		wide.install(m)
		return
	}
	m.seedFocusAnnouncement(prev)
	// Announce the entering workspace's window/buffer now (queued, no IO): the previous
	// workspace's focus is already known, so this is the switch transition.
	m.announceFocus()
	wide.install(m)
}

// capture snapshots the editor-wide registrations of m.
func (w *pluginWide) capture(m *Model) {
	w.commands = cloneCommands(m.plug.commands)
	w.order = append([]string(nil), m.plug.order...)
	w.menus = menus{
		byPlugin: cloneMenuRows(m.plug.menus.byPlugin),
		order:    append([]string(nil), m.plug.menus.order...),
	}
	ks := &m.plug.keys
	w.keys = pluginKeysWide{
		bindings: cloneBindings(ks.bindings),
		border:   append([]string(nil), ks.border...),
		gated:    cloneBindings(ks.gated),
		modes:    cloneModes(ks.modes),
		morder:   append([]string(nil), ks.morder...),
		ctx:      cloneCtx(ks.ctx),
		ctxSeen:  cloneBoolMap(ks.ctxSeen),
		warned:   cloneBoolMap(ks.warned),
		status: statusState{
			items: cloneStatusItems(ks.status.items),
			order: append([]string(nil), ks.status.order...),
		},
	}
	w.chrome = captureEditorChrome(m)
}

// install puts the snapshot into m (after a switch) and rebuilds the plugin keymap.
func (w *pluginWide) install(m *Model) {
	m.plug.commands = cloneCommands(w.commands)
	m.plug.order = append([]string(nil), w.order...)
	m.plug.menus = menus{
		byPlugin: cloneMenuRows(w.menus.byPlugin),
		order:    append([]string(nil), w.menus.order...),
	}
	ks := &m.plug.keys
	ks.bindings = cloneBindings(w.keys.bindings)
	ks.border = append([]string(nil), w.keys.border...)
	ks.gated = cloneBindings(w.keys.gated)
	ks.modes = cloneModes(w.keys.modes)
	ks.morder = append([]string(nil), w.keys.morder...)
	ks.ctx = cloneCtx(w.keys.ctx)
	ks.ctxSeen = cloneBoolMap(w.keys.ctxSeen)
	ks.warned = cloneBoolMap(w.keys.warned)
	ks.status = statusState{
		items: cloneStatusItems(w.keys.status.items),
		order: append([]string(nil), w.keys.status.order...),
	}
	// The keymap was built from the leaving workspace's bindings; rebuild it over this
	// model's preset. Direct call: no Lua is on the stack during a switch.
	m.plugRebuildKeymap()
	// Editor-scoped chrome belongs to every workspace: replace whatever this model carried
	// with the snapshot's specs (window/buffer chrome is untouched).
	m.clearEditorChrome()
	m.installEditorChrome(w.chrome)
}

func (w *pluginWide) clone() pluginWide {
	out := pluginWide{
		commands: cloneCommands(w.commands),
		order:    append([]string(nil), w.order...),
		menus: menus{
			byPlugin: cloneMenuRows(w.menus.byPlugin),
			order:    append([]string(nil), w.menus.order...),
		},
	}
	out.chrome = cloneEditorChrome(w.chrome)
	out.keys = pluginKeysWide{
		bindings: cloneBindings(w.keys.bindings),
		border:   append([]string(nil), w.keys.border...),
		gated:    cloneBindings(w.keys.gated),
		modes:    cloneModes(w.keys.modes),
		morder:   append([]string(nil), w.keys.morder...),
		ctx:      cloneCtx(w.keys.ctx),
		ctxSeen:  cloneBoolMap(w.keys.ctxSeen),
		warned:   cloneBoolMap(w.keys.warned),
		status: statusState{
			items: cloneStatusItems(w.keys.status.items),
			order: append([]string(nil), w.keys.status.order...),
		},
	}
	return out
}

// captureEditorChrome reads the editor-scoped panels of m in mount order.
func captureEditorChrome(m *Model) []editorChromeSpec {
	if m == nil {
		return nil
	}
	var out []editorChromeSpec
	for _, id := range m.plug.panels.order {
		mp := m.plug.panels.get(id)
		if mp == nil || mp.mount.Scope != pluginapi.ScopeEditor {
			continue
		}
		out = append(out, editorChromeSpec{id: id, owner: mp.owner, mount: mp.mount})
	}
	return out
}

// cloneEditorChrome copies the snapshot slice (the widget specs stay read-only shared).
func cloneEditorChrome(in []editorChromeSpec) []editorChromeSpec {
	if in == nil {
		return nil
	}
	return append([]editorChromeSpec(nil), in...)
}

// noteEditorChromeChanged refreshes the shared snapshot after an editor-scoped mount,
// update, unmount or owner-gone on the active model. It is a no-op for a direct launch.
func (m *Model) noteEditorChromeChanged() {
	s := m.root.ports.PluginShared
	if s == nil {
		return
	}
	s.captureEditorChrome(m)
}

// captureEditorChrome replaces the shared editor-scoped chrome snapshot with m's, if m is
// the active host target.
func (s *PluginShared) captureEditorChrome(m *Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != m {
		return
	}
	s.wide.chrome = captureEditorChrome(m)
}

// clearEditorChrome drops the editor-scoped panels of m. The entering model is rebuilt from
// the snapshot, so no stale carried section survives a switch.
func (m *Model) clearEditorChrome() {
	ps := &m.plug.panels
	for _, id := range append([]pluginapi.PanelID(nil), ps.order...) {
		mp := ps.get(id)
		if mp == nil || mp.mount.Scope != pluginapi.ScopeEditor {
			continue
		}
		if ps.focus == id {
			ps.focus = 0
		}
		ps.remove(id)
	}
}

// installEditorChrome builds this model's copies of the editor-scoped mounts. They are not
// given the keyboard on install: a switch keeps the entering model's own focus.
func (m *Model) installEditorChrome(specs []editorChromeSpec) {
	for _, spec := range specs {
		if old := m.plug.panels.get(spec.id); old != nil {
			m.plug.panels.remove(spec.id)
		}
		mp := newMountedPanel(m, spec.id, spec.owner, spec.mount)
		m.plug.panels.add(mp)
		if spec.mount.Placement == pluginapi.PlacementSidebar && !m.explorerOpen && m.tree != nil {
			m.explorerOpen = true
		}
	}
	m.syncViewport()
}

func cloneCommands(in map[string][]pluginapi.CommandInfo) map[string][]pluginapi.CommandInfo {
	if in == nil {
		return nil
	}
	out := make(map[string][]pluginapi.CommandInfo, len(in))
	for k, v := range in {
		out[k] = append([]pluginapi.CommandInfo(nil), v...)
	}
	return out
}

func cloneMenuRows(in map[string][]pluginapi.MenuItem) map[string][]pluginapi.MenuItem {
	if in == nil {
		return nil
	}
	out := make(map[string][]pluginapi.MenuItem, len(in))
	for k, v := range in {
		out[k] = append([]pluginapi.MenuItem(nil), v...)
	}
	return out
}

func cloneBindings(in map[string][]pluginapi.Binding) map[string][]pluginapi.Binding {
	if in == nil {
		return nil
	}
	out := make(map[string][]pluginapi.Binding, len(in))
	for k, v := range in {
		out[k] = append([]pluginapi.Binding(nil), v...)
	}
	return out
}

func cloneModes(in map[string][]*uiMode) map[string][]*uiMode {
	if in == nil {
		return nil
	}
	out := make(map[string][]*uiMode, len(in))
	for k, v := range in {
		out[k] = append([]*uiMode(nil), v...)
	}
	return out
}

func cloneCtx(in map[string]map[string]bool) map[string]map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]bool, len(in))
	for k, v := range in {
		out[k] = cloneBoolMap(v)
	}
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStatusItems(in map[string][]pluginapi.StatusItem) map[string][]pluginapi.StatusItem {
	if in == nil {
		return nil
	}
	out := make(map[string][]pluginapi.StatusItem, len(in))
	for k, v := range in {
		out[k] = append([]pluginapi.StatusItem(nil), v...)
	}
	return out
}
