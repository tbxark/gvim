package tui

import (
	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/pluginapi"
)

// Plugin menu rows (plugin API tier 1a): a plugin adds rows to a menu bar dropdown. The
// host keeps them per plugin, whole-set replace like the other registrations, and hands
// them to the menu bar on every paint; an unload drops them with the rest of the plugin's
// registrations.

// menus is the plugin menu registration state of the root.
type menus struct {
	byPlugin map[string][]pluginapi.MenuItem
	order    []string // plugins in the order they first registered a row
}

// SetMenuItems replaces the menu rows of plugin.
func (h *pluginHost) SetMenuItems(plugin string, items []pluginapi.MenuItem) {
	m := h.model()
	ms := &m.plug.menus
	if len(items) == 0 {
		delete(ms.byPlugin, plugin)
		ms.order = removePluginOrder(ms.order, plugin)
		return
	}
	if ms.byPlugin == nil {
		ms.byPlugin = map[string][]pluginapi.MenuItem{}
	}
	if _, ok := ms.byPlugin[plugin]; !ok {
		ms.order = append(ms.order, plugin)
	}
	clean := make([]pluginapi.MenuItem, 0, len(items))
	for _, it := range items {
		clean = append(clean, pluginapi.MenuItem{
			Menu: cleanText(it.Menu), Label: cleanText(it.Label), Action: cleanText(it.Action),
			Checkbox: cleanText(it.Checkbox), When: cleanText(it.When),
			After: cleanText(it.After), Before: cleanText(it.Before),
		})
	}
	ms.byPlugin[plugin] = clean
}

// removePluginOrder drops name from the order slice.
func removePluginOrder(order []string, name string) []string {
	for i, got := range order {
		if got == name {
			return append(order[:i:i], order[i+1:]...)
		}
	}
	return order
}

// plugMenusGone is OwnerGone for the menu rows.
func (m *Model) plugMenusGone(plugin string) {
	h := m.plug.host
	if h != nil {
		h.SetMenuItems(plugin, nil)
		return
	}
	delete(m.plug.menus.byPlugin, plugin)
	m.plug.menus.order = removePluginOrder(m.plug.menus.order, plugin)
}

// plugMenuRows returns the registered plugin rows in registration order, for the menu bar.
func (m *Model) plugMenuRows() []menubar.PluginRow {
	ms := &m.plug.menus
	if len(ms.byPlugin) == 0 {
		return nil
	}
	var out []menubar.PluginRow
	for _, plugin := range ms.order {
		for _, it := range ms.byPlugin[plugin] {
			out = append(out, menubar.PluginRow{
				Menu: it.Menu, Label: it.Label, Action: it.Action,
				Checkbox: it.Checkbox, When: it.When, After: it.After, Before: it.Before,
			})
		}
	}
	return out
}
