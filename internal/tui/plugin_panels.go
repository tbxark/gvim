package tui

import (
	"image/color"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/pluginpanel"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/pluginapi"
)

// Declarative widget panels and sidebar sections of the plugin host (plugin API tier 1a).
// The pluginapi contract carries the spec; internal/tui/pluginpanel lays it out, paints it
// and reports what the user did; this file owns placement, the focus hand-off and the
// routing of keys and mouse messages.
//
// A panel is mounted by its plugin and owned by it: pluginapi.UIHost carries the plugin name
// on mount/update, and OwnerGone drops everything a plugin left. A panel is either a centred
// floating card, a view over the window that shows the plugin's virtual buffer, or a section
// of the sidebar column.
//
// Focus. A panel takes the keyboard when it is mounted with focus, when the user clicks it,
// or when the host focuses it; keys then go to the panel before the editor keymap, and the
// events of the widgets go to the plugin as widget_events. Escape closes a closable panel
// (a cancel event) or gives the keyboard back. A click outside the panel gives it back too.

// pluginPanels is the state of the mounted panels of all plugins.
type pluginPanels struct {
	order []pluginapi.PanelID
	byID  map[pluginapi.PanelID]*mountedPanel
	// focus is the panel with the keyboard, 0 for none.
	focus pluginapi.PanelID
	// collapsed is the folded state of the sidebar sections, by panel.
	collapsed map[pluginapi.PanelID]bool
}

// mountedPanel is one mounted panel.
type mountedPanel struct {
	id    pluginapi.PanelID
	owner string
	mount pluginapi.WidgetMount
	panel *pluginpanel.Panel
	// rect is the block of the last paint: the float card, the pane of the buffer panel
	// or the body of the sidebar section.
	rect pluginpanel.Rect
}

// get returns the mounted panel id, nil when it is not mounted.
func (ps *pluginPanels) get(id pluginapi.PanelID) *mountedPanel {
	return ps.byID[id]
}

// add stores a mounted panel.
func (ps *pluginPanels) add(mp *mountedPanel) {
	if ps.byID == nil {
		ps.byID = map[pluginapi.PanelID]*mountedPanel{}
	}
	ps.byID[mp.id] = mp
	ps.order = append(ps.order, mp.id)
}

// remove drops a mounted panel from the order and the map.
func (ps *pluginPanels) remove(id pluginapi.PanelID) bool {
	mp := ps.byID[id]
	if mp == nil {
		return false
	}
	delete(ps.byID, id)
	delete(ps.collapsed, id)
	for i, got := range ps.order {
		if got == id {
			ps.order = append(ps.order[:i:i], ps.order[i+1:]...)
			break
		}
	}
	return true
}

// sidebarOrder returns the sidebar panels in mount order: the order their sections stack in.
func (ps *pluginPanels) sidebarOrder() []*mountedPanel {
	var out []*mountedPanel
	for _, id := range ps.order {
		mp := ps.byID[id]
		if mp != nil && mp.mount.Placement == pluginapi.PlacementSidebar {
			out = append(out, mp)
		}
	}
	return out
}

// ---- Host: mount, update, unmount, commands, mutations

// MountWidgetPanel mounts a declarative widget panel. It validates the spec against the
// host caps and the placement rules, and stores the panel; the paint happens on the next
// frame. Mounting an id that is already mounted replaces its content and placement.
func (h *pluginHost) MountWidgetPanel(plugin string, id pluginapi.PanelID, o pluginapi.WidgetMount) error {
	m := h.m
	if id == 0 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a panel id is not 0")
	}
	if o.Placement == "" {
		o.Placement = pluginapi.PlacementFloat
	}
	switch o.Placement {
	case pluginapi.PlacementFloat:
	case pluginapi.PlacementBuffer:
		if o.Buffer == 0 {
			return pluginapi.Errf(pluginapi.CodeInvalid, "a buffer panel needs the virtual buffer it is shown over")
		}
		if m.plug.virtual.owner[o.Buffer] != plugin {
			return pluginapi.Errf(pluginapi.CodeInvalid, "buffer %d is not a virtual buffer this plugin made", o.Buffer)
		}
		if m.windowOfBuffer(o.Buffer) < 0 {
			return pluginapi.Errf(pluginapi.CodeNotFound, "buffer %d is not shown in a window", o.Buffer)
		}
	case pluginapi.PlacementSidebar:
		if o.Rows < 0 || o.Rows > sidebarMaxPluginRows {
			return pluginapi.Errf(pluginapi.CodeInvalid, "a section asks for 0 to %d rows, not %d", sidebarMaxPluginRows, o.Rows)
		}
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "placement must be \"float\", \"buffer\" or \"sidebar\", not %q", string(o.Placement))
	}
	if len(o.Title) > pluginpanel.MaxTitle {
		return pluginapi.Errf(pluginapi.CodeLimit, "a panel title is at most %d bytes", pluginpanel.MaxTitle)
	}
	if err := pluginpanel.Validate(o.Panel); err != nil {
		return err
	}
	o.Title = cleanText(o.Title)
	opts := pluginpanel.Options{
		Chrome:         m.styles.Overlay,
		Resolve:        m.widgetColor,
		Title:          o.Title,
		Framed:         o.Placement == pluginapi.PlacementFloat,
		Closable:       o.Closable,
		Page:           o.Page,
		AutoFocusFirst: o.AutoFocusFirst,
	}
	if old := m.plug.panels.get(id); old != nil {
		old.owner, old.mount = plugin, o
		old.panel.Update(o.Panel)
		if o.Focus {
			m.focusPluginPanel(id)
		}
		return nil
	}
	mp := &mountedPanel{id: id, owner: plugin, mount: o, panel: pluginpanel.New(o.Panel, opts)}
	m.plug.panels.add(mp)
	if o.Placement == pluginapi.PlacementSidebar {
		if !m.explorerOpen && m.tree != nil {
			m.explorerOpen = true
			m.syncViewport()
		}
		m.syncViewport()
	}
	if o.Focus {
		m.focusPluginPanel(id)
	}
	return nil
}

// sidebarMaxPluginRows is the most rows a plugin section can ask for.
const sidebarMaxPluginRows = 100

// UpdateWidgetPanel replaces the content of a mounted panel, keeping the instance state of
// its keyed widgets and where the user was.
func (h *pluginHost) UpdateWidgetPanel(plugin string, id pluginapi.PanelID, p pluginapi.WidgetPanel) error {
	mp := h.m.plug.panels.get(id)
	if mp == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "panel %d is not mounted", id)
	}
	if mp.owner != plugin {
		return pluginapi.Errf(pluginapi.CodeInvalid, "panel %d belongs to a different plugin", id)
	}
	if err := pluginpanel.Validate(p); err != nil {
		return err
	}
	mp.panel.Update(p)
	return nil
}

// UnmountWidgetPanel removes a mounted panel. It is idempotent.
func (h *pluginHost) UnmountWidgetPanel(id pluginapi.PanelID) {
	m := h.m
	mp := m.plug.panels.get(id)
	if mp == nil {
		return
	}
	if m.plug.panels.focus == id {
		m.plug.panels.focus = 0
		m.plugEmitWidget(pluginapi.WidgetEvent{Panel: uint64(id), Event: "focus_lost", Index: -1})
	}
	m.plug.panels.remove(id)
	m.syncViewport()
}

// WidgetCommand performs an action on the focused widget of a panel.
func (h *pluginHost) WidgetCommand(id pluginapi.PanelID, a pluginapi.WidgetAction) error {
	m := h.m
	mp := m.plug.panels.get(id)
	if mp == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "panel %d is not mounted", id)
	}
	switch a.Kind {
	case "focusAdvance", "activate", "selectMove", "textInputKey", "textInputChar", "key":
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "unknown widget action %q", a.Kind)
	}
	mp.panel.SetFocused(true)
	res := mp.panel.HandleAction(a)
	m.emitWidgetResult(id, res)
	return nil
}

// WidgetMutate applies a targeted mutation to a mounted panel.
func (h *pluginHost) WidgetMutate(id pluginapi.PanelID, mu pluginapi.WidgetMutation) error {
	m := h.m
	mp := m.plug.panels.get(id)
	if mp == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "panel %d is not mounted", id)
	}
	switch mu.Kind {
	case "setChecked", "setSelectedIndex", "setValue", "setNumber", "setDropdown", "setCompletions",
		"setDualIncluded", "setItems", "setExpandedKeys", "setCheckedKeys", "appendTreeNodes",
		"setRawEntries", "setFocusKey", "previewTreeMove", "endTreeMovePreview":
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "unknown widget mutation %q", mu.Kind)
	}
	if !mp.panel.Mutate(mu) {
		return pluginapi.Errf(pluginapi.CodeNotFound, "no widget with key %q", mu.WidgetKey)
	}
	return nil
}

// plugPanelsGone is OwnerGone for the panels: every panel the plugin mounted goes.
func (m *Model) plugPanelsGone(plugin string, gen uint64) {
	_ = gen
	for _, id := range append([]pluginapi.PanelID(nil), m.plug.panels.order...) {
		mp := m.plug.panels.get(id)
		if mp != nil && mp.owner == plugin {
			m.plug.panels.remove(id)
		}
	}
	if m.plug.panels.focus != 0 && m.plug.panels.get(m.plug.panels.focus) == nil {
		m.plug.panels.focus = 0
	}
	m.syncViewport()
}

// ---- focus and events

// focusPluginPanel gives the keyboard to a mounted panel.
func (m *Model) focusPluginPanel(id pluginapi.PanelID) {
	mp := m.plug.panels.get(id)
	if mp == nil {
		return
	}
	ps := &m.plug.panels
	if ps.focus != 0 && ps.focus != id {
		if old := ps.get(ps.focus); old != nil {
			old.panel.SetFocused(false)
			m.plugEmitWidget(pluginapi.WidgetEvent{Panel: uint64(old.id), Event: "focus_lost", Index: -1})
		}
	}
	ps.focus = id
	mp.panel.SetFocused(true)
	m.focusEditor()
	m.explorerFocus = false
	m.contentsFocus = false
	m.tabFocus = false
	m.dock.SetFocus(false)
	m.plugEmitWidget(pluginapi.WidgetEvent{Panel: uint64(id), Event: "focus_gained", Index: -1})
	if ev := mp.panel.FocusEvent(); ev.Event == "focus" {
		m.emitWidgetResult(id, pluginpanel.Result{Handled: true, Events: []pluginapi.WidgetEvent{ev}})
	}
}

// blurPluginPanels takes the keyboard from the mounted panels.
func (m *Model) blurPluginPanels() {
	ps := &m.plug.panels
	if ps.focus == 0 {
		return
	}
	if mp := ps.get(ps.focus); mp != nil {
		mp.panel.SetFocused(false)
		m.plugEmitWidget(pluginapi.WidgetEvent{Panel: uint64(ps.focus), Event: "focus_lost", Index: -1})
	}
	ps.focus = 0
	// The editor has the keyboard again.
	m.plugEmit("focus_gained", map[string]any{})
}

// focusPluginSection gives a sidebar section the keyboard.
func (m *Model) focusPluginSection(mp *mountedPanel) {
	if mp == nil {
		return
	}
	m.focusPluginPanel(mp.id)
}

// unmountPanel removes a mounted panel; a model without a plugin host has none.
func (m *Model) unmountPanel(id pluginapi.PanelID) {
	if m.plug.host != nil {
		m.plug.host.UnmountWidgetPanel(id)
	}
}

// emitWidgetResult reports the events of one panel input to the plugin.
func (m *Model) emitWidgetResult(id pluginapi.PanelID, res pluginpanel.Result) {
	if res.Close {
		m.plugEmitWidget(pluginapi.WidgetEvent{Panel: uint64(id), Event: "cancel", Index: -1})
		m.unmountPanel(id)
		return
	}
	focusKey := ""
	if mp := m.plug.panels.get(id); mp != nil {
		focusKey = mp.panel.FocusedKey()
	}
	for _, ev := range res.Events {
		ev.Panel = uint64(id)
		ev.FocusKey = focusKey
		m.plugEmitWidget(ev)
	}
}

// plugEmitWidget queues one widget_event. The panel id is a plain number; index is 1-based
// in Lua, -1 when the event carries none.
func (m *Model) plugEmitWidget(ev pluginapi.WidgetEvent) {
	if m.plug.rt == nil || !m.plug.rt.Wants("widget_event") {
		return
	}
	data := map[string]any{"panel": ev.Panel, "event": ev.Event}
	if ev.Widget != "" {
		data["widget"] = ev.Widget
	}
	if ev.Index >= 0 {
		data["index"] = ev.Index + 1
	}
	if ev.Event == "toggle" || ev.Event == "expand" {
		data["checked"] = ev.Checked
	}
	if ev.Value != "" {
		data["value"] = ev.Value
	}
	if ev.CursorByte >= 0 {
		data["cursor_byte"] = ev.CursorByte
	}
	if ev.Included != nil {
		data["included"] = ev.Included
	}
	if ev.FocusKey != "" {
		data["focus_key"] = ev.FocusKey
	}
	if ev.TargetKey != "" || ev.Event == "drag" || ev.Event == "drop" {
		data["target_key"] = ev.TargetKey
	}
	if ev.TargetIndex >= 0 {
		data["target_index"] = ev.TargetIndex + 1
	}
	if len(ev.Event) > 6 && ev.Event[:6] == "mouse_" {
		data["x"], data["y"] = ev.X, ev.Y
		if ev.Button != "" {
			data["button"] = ev.Button
		}
		if ev.Wheel != 0 {
			data["wheel"] = ev.Wheel
		}
	}
	m.plugEmit("widget_event", data)
}

// ---- placement

// widgetPanelRect is the block a panel is painted in for the current frame, zero when it has
// no place (a hidden sidebar, a closed buffer window).
func (m *Model) widgetPanelRect(mp *mountedPanel) pluginpanel.Rect {
	switch mp.mount.Placement {
	case pluginapi.PlacementFloat:
		return m.floatPanelRect(mp.mount)
	case pluginapi.PlacementBuffer:
		l := m.layout()
		i := m.windowOfBuffer(mp.mount.Buffer)
		if i < 0 || i >= len(l.Panes) {
			return pluginpanel.Rect{}
		}
		p := l.Panes[i]
		return pluginpanel.Rect{X: p.TextX, Y: p.Y, W: max(p.TextW-1, 0), H: p.H}
	case pluginapi.PlacementSidebar:
		sec, ok := m.pluginSidebarSection(mp)
		if !ok || sec.Collapsed || sec.Rows() == 0 {
			return pluginpanel.Rect{}
		}
		l := m.layout()
		return pluginpanel.Rect{X: l.ExX + 1, Y: sec.BodyTop, W: max(l.Ex-2, 0), H: sec.Rows()}
	}
	return pluginpanel.Rect{}
}

// floatPanelRect is the centred card of a floating panel: WidthPct/HeightPct of the frame,
// at least a minimum size, never closer than space.Margin to the left and right edges and
// one row to the top and bottom (docs/ui.md section 2).
func (m *Model) floatPanelRect(o pluginapi.WidgetMount) pluginpanel.Rect {
	if m.width <= 0 || m.height <= 0 {
		return pluginpanel.Rect{}
	}
	wp, hp := o.WidthPct, o.HeightPct
	if wp <= 0 {
		wp = 60
	}
	if hp <= 0 {
		hp = 70
	}
	wp = min(max(wp, 10), 100)
	hp = min(max(hp, 10), 100)
	margin := space.Margin(m.width)
	w := m.width * wp / 100
	h := m.height * hp / 100
	w = min(max(w, 20), max(m.width-2*margin, 20))
	h = min(max(h, 5), max(m.height-2, 5))
	return pluginpanel.Rect{X: max((m.width-w)/2, 0), Y: max((m.height-h)/2, 1), W: w, H: h}
}

// windowOfBuffer returns the index of the window (in reading order) that shows buffer id,
// -1 when none does.
func (m *Model) windowOfBuffer(id pluginapi.BufferID) int {
	tab := m.plug.host.tabIndex(id)
	if tab < 0 {
		return -1
	}
	for i, n := range m.windowLeaves() {
		if m.leafTab(n) == tab {
			return i
		}
	}
	return -1
}

// ---- paint

// paintPluginPanels paints the floating and buffer panels, and the bodies of the sidebar
// sections. The sidebar sections' headers are painted by paintSidebar; the bodies are drawn
// here so the panel state stays in one place.
func (m *Model) paintPluginPanels(g *grid.Grid, l layout) {
	for _, id := range m.plug.panels.order {
		mp := m.plug.panels.get(id)
		if mp == nil {
			continue
		}
		switch mp.mount.Placement {
		case pluginapi.PlacementSidebar:
			// Painted by paintSidebar, in the column's stack.
			continue
		default:
			r := m.widgetPanelRect(mp)
			if r.W <= 0 || r.H <= 0 {
				mp.rect = pluginpanel.Rect{}
				continue
			}
			mp.rect = r
			if mp.mount.Placement == pluginapi.PlacementBuffer {
				for y := r.Y; y < r.Y+r.H; y++ {
					g.Fill(r.X, r.X+r.W, y, m.styles.Overlay.Box)
				}
			}
			mp.panel.Draw(g, r)
		}
	}
}

// paintPluginSection paints the body of one sidebar section.
func (m *Model) paintPluginSection(g *grid.Grid, l layout, mp *mountedPanel) {
	sec, ok := m.pluginSidebarSection(mp)
	if !ok || sec.Collapsed || sec.Rows() == 0 {
		mp.rect = pluginpanel.Rect{}
		return
	}
	r := pluginpanel.Rect{X: l.ExX + 1, Y: sec.BodyTop, W: max(l.Ex-2, 0), H: sec.Rows()}
	mp.rect = r
	for y := r.Y; y < r.Y+r.H; y++ {
		g.Fill(l.ExX+1, l.ExX+l.Ex-1, y, m.styles.Overlay.Box)
	}
	mp.panel.Draw(g, r)
}

// pluginSidebarSection returns the section a sidebar panel landed in.
func (m *Model) pluginSidebarSection(mp *mountedPanel) (sidebar.Section, bool) {
	order := m.plug.panels.sidebarOrder()
	k := -1
	for i, got := range order {
		if got.id == mp.id {
			k = i
			break
		}
	}
	if k < 0 {
		return sidebar.Section{}, false
	}
	seen := 0
	for _, sec := range m.layout().Sections {
		if sec.Kind != sidebar.KindPlugin {
			continue
		}
		if seen == k {
			return sec, true
		}
		seen++
	}
	return sidebar.Section{}, false
}

// ---- input

// pluginPanelKey routes one key press to the panel with the keyboard, the panel's widgets
// first: it reports whether the panel took the message. A panel swallows the keys it does
// not handle, so a keypress meant for a panel never edits the buffer under it.
func (m *Model) pluginPanelKey(msg tea.Msg) (tea.Cmd, bool) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || m.plug.panels.focus == 0 {
		return nil, false
	}
	mp := m.plug.panels.get(m.plug.panels.focus)
	if mp == nil || !mp.panel.Focused() {
		m.plug.panels.focus = 0
		return nil, false
	}
	if cmd, panicked := m.pluginPanicKey(tea.Key(k)); panicked {
		return cmd, true
	}
	pk := panelKeyOf(tea.Key(k))
	if pk.Name == "" || pk.Ctrl || pk.Alt {
		return nil, true
	}
	if pk.Name == "escape" && !mp.panel.Closable() {
		m.blurPluginPanels()
		m.focusEditor()
		return nil, true
	}
	res := mp.panel.HandleKey(pk)
	m.emitWidgetResult(mp.id, res)
	return nil, true
}

// panelKeyOf converts a key press to the panel key vocabulary, using the plugin key names.
func panelKeyOf(k tea.Key) pluginpanel.Key {
	pk := pluginKeyOf(k)
	name := pk.Key
	if name == " " {
		name = "space"
	}
	return pluginpanel.Key{Name: name, Text: pk.Text, Ctrl: pk.Ctrl, Alt: pk.Alt, Shift: pk.Shift}
}

// pluginPanelMouse routes a mouse message to the panel under the pointer, a mounted panel's
// rectangle or a plugin sidebar section. It reports whether it took the message.
func (m *Model) pluginPanelMouse(msg tea.Msg) (tea.Cmd, bool) {
	var mo tea.Mouse
	release := false
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		mo = tea.Mouse(msg)
	case tea.MouseMotionMsg:
		mo = tea.Mouse(msg)
	case tea.MouseWheelMsg:
		mo = tea.Mouse(msg)
	case tea.MouseReleaseMsg:
		mo, release = tea.Mouse(msg), true
	default:
		return nil, false
	}
	// The floating and buffer panels first, newest on top.
	ps := &m.plug.panels
	for i := len(ps.order) - 1; i >= 0; i-- {
		mp := ps.get(ps.order[i])
		if mp == nil || mp.mount.Placement == pluginapi.PlacementSidebar {
			continue
		}
		if !mp.rect.Contains(mo.X, mo.Y) && !mp.panel.Dragging() {
			continue
		}
		return m.panelMouseInput(mp, mo, false, release), true
	}
	// A plugin sidebar section: its header folds it and focuses it, its close button
	// unmounts it, and its body is the panel.
	t := m.layout().Hit(mo.X, mo.Y)
	switch t.Kind {
	case hitSidebarHeader:
		mp := m.pluginSectionAt(t.Section)
		if mp == nil {
			return nil, false
		}
		if _, isClick := msg.(tea.MouseClickMsg); isClick {
			m.togglePluginSection(mp)
			m.focusPluginSection(mp)
		}
		return nil, true
	case hitSidebarClose:
		mp := m.pluginSectionAt(t.Section)
		if mp == nil {
			return nil, false
		}
		if _, isClick := msg.(tea.MouseClickMsg); isClick {
			m.plugEmitWidget(pluginapi.WidgetEvent{Panel: uint64(mp.id), Event: "cancel", Index: -1})
			m.unmountPanel(mp.id)
		}
		return nil, true
	case hitSidebarRow, hitSidebarBody:
		mp := m.pluginSectionAt(t.Section)
		if mp == nil {
			return nil, false
		}
		if _, isClick := msg.(tea.MouseClickMsg); isClick {
			m.focusPluginSection(mp)
		}
		return m.panelMouseInput(mp, mo, true, release), true
	}
	return nil, false
}

// panelMouseInput hands a mouse message to one panel. sectional says the panel is a sidebar
// section, whose body coordinates are the panel's own rectangle.
func (m *Model) panelMouseInput(mp *mountedPanel, mo tea.Mouse, sectional, release bool) tea.Cmd {
	if !sectional && !mp.rect.Contains(mo.X, mo.Y) && !mp.panel.Dragging() {
		return nil
	}
	in := pluginpanel.Mouse{X: mo.X, Y: mo.Y, Release: release}
	switch mo.Button {
	case tea.MouseLeft:
		in.Button = "left"
	case tea.MouseRight:
		in.Button = "right"
	case tea.MouseMiddle:
		in.Button = "middle"
	}
	switch mo.Button {
	case tea.MouseWheelUp:
		in.Wheel = -1
	case tea.MouseWheelDown:
		in.Wheel = 1
	}
	if in.Button == "right" {
		return nil
	}
	res, _ := mp.panel.HandleMouse(in)
	m.emitWidgetResult(mp.id, res)
	return nil
}

// pluginSectionAt returns the sidebar panel of section index, nil when it is not a plugin
// section or the panel is gone.
func (m *Model) pluginSectionAt(index int) *mountedPanel {
	if index < 0 || index >= len(m.layout().Sections) {
		return nil
	}
	if m.layout().Sections[index].Kind != sidebar.KindPlugin {
		return nil
	}
	k := 0
	for _, sec := range m.layout().Sections {
		if sec.Index >= index {
			break
		}
		if sec.Kind == sidebar.KindPlugin {
			k++
		}
	}
	order := m.plug.panels.sidebarOrder()
	if k < 0 || k >= len(order) {
		return nil
	}
	return order[k]
}

// togglePluginSection folds or unfolds a plugin sidebar section.
func (m *Model) togglePluginSection(mp *mountedPanel) {
	if m.plug.panels.collapsed == nil {
		m.plug.panels.collapsed = map[pluginapi.PanelID]bool{}
	}
	m.plug.panels.collapsed[mp.id] = !m.plug.panels.collapsed[mp.id]
	m.syncViewport()
}

// pluginSidebarSpecs returns the sidebar section specs of the mounted plugin sections.
func (m *Model) pluginSidebarSpecs() []sidebar.Spec {
	order := m.plug.panels.sidebarOrder()
	if len(order) == 0 {
		return nil
	}
	out := make([]sidebar.Spec, 0, len(order))
	for _, mp := range order {
		out = append(out, sidebar.Spec{
			Kind:      sidebar.KindPlugin,
			Rows:      mp.mount.Rows,
			Collapsed: m.plug.panels.collapsed[mp.id],
			Title:     mp.panel.Title(),
			NoClose:   !mp.mount.Closable,
		})
	}
	return out
}

// widgetColor resolves a plugin widget color against the active theme.
func (m *Model) widgetColor(c pluginapi.Color) color.Color {
	if c.Key == "" && !c.Set {
		return nil
	}
	return style.ResolveColor(m.theme, decorations.Color{Key: c.Key, R: c.R, G: c.G, B: c.B, Set: c.Set})
}
