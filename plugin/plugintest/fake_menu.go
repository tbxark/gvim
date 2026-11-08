package plugintest

import (
	"fmt"

	"github.com/tbxark/gvim/pluginapi"
)

// The menu-bar part of the fake host (plugin API tier 1a): the rows a plugin added, as the ui
// would keep them, whole-set replace per plugin and dropped by OwnerGone.

// FakeMenu is the menu registration state of the fake host.
type FakeMenu struct {
	ByPlugin map[string][]pluginapi.MenuItem
	Order    []string // plugins in the order they first registered a row
}

// Menu returns the menu state of the fake host.
func (f *FakeHost) Menu() *FakeMenu {
	m, ok := f.Ext["menu"].(*FakeMenu)
	if !ok {
		m = &FakeMenu{ByPlugin: map[string][]pluginapi.MenuItem{}}
		f.Ext["menu"] = m
	}
	return m
}

// SetMenuItems replaces the rows of the plugin.
func (f *FakeHost) SetMenuItems(plugin string, items []pluginapi.MenuItem) {
	f.rec("SetMenuItems", plugin, len(items))
	m := f.Menu()
	if len(items) == 0 {
		delete(m.ByPlugin, plugin)
		for i, p := range m.Order {
			if p == plugin {
				m.Order = append(m.Order[:i:i], m.Order[i+1:]...)
				break
			}
		}
		return
	}
	if _, ok := m.ByPlugin[plugin]; !ok {
		m.Order = append(m.Order, plugin)
	}
	m.ByPlugin[plugin] = append([]pluginapi.MenuItem(nil), items...)
}

// dropMenus removes the rows of a plugin: the OwnerGone hook.
func (f *FakeHost) dropMenus(plugin string) {
	f.SetMenuItems(plugin, nil)
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		var out []string
		m := f.Menu()
		for _, plugin := range m.Order {
			for _, it := range m.ByPlugin[plugin] {
				out = append(out, fmt.Sprintf("menu:%s/%s", plugin, it.Label))
			}
		}
		return out
	})
	OnOwnerGone(func(f *FakeHost, plugin string, gen uint64) { f.dropMenus(plugin) })
}
