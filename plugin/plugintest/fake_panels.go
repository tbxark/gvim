package plugintest

import (
	"fmt"

	"github.com/tbxark/gvim/pluginapi"
)

// The widget-panel part of the fake host (plugin API tier 1a). The fake keeps every panel
// a plugin mounted, its last spec and the commands and mutations it received, so a test can
// drive the Lua side without the TUI: mount in Lua, then assert the recorded request, and
// answer the widget_event the way the ui would by emitting it.
//
// Like the real ui it keys panels by id and drops what a plugin owns in OwnerGone.

// FakePanel is one mounted widget panel or sidebar section.
type FakePanel struct {
	ID     pluginapi.PanelID
	Plugin string
	Mount  pluginapi.WidgetMount
	// Panel is the latest spec (the mount's, then every update).
	Panel pluginapi.WidgetPanel
	// Commands and Mutations are the widget_command and widget_mutate calls, in order.
	Commands  []pluginapi.WidgetAction
	Mutations []pluginapi.WidgetMutation
	// Open is false once the panel was unmounted.
	Open bool
}

// FakePanels is the panel state of the fake host.
type FakePanels struct {
	// ByID holds every mounted panel, in mount order when iterated through Order.
	ByID  map[pluginapi.PanelID]*FakePanel
	Order []pluginapi.PanelID
}

// Panels returns the panel state of the fake host.
func (f *FakeHost) Panels() *FakePanels {
	p, ok := f.Ext["panels"].(*FakePanels)
	if !ok {
		p = &FakePanels{ByID: map[pluginapi.PanelID]*FakePanel{}}
		f.Ext["panels"] = p
	}
	return p
}

// Mounted returns the panels that are still open, in mount order.
func (p *FakePanels) Mounted() []*FakePanel {
	var out []*FakePanel
	for _, id := range p.Order {
		if fp := p.ByID[id]; fp != nil && fp.Open {
			out = append(out, fp)
		}
	}
	return out
}

// MountWidgetPanel records the mount.
func (f *FakeHost) MountWidgetPanel(plugin string, id pluginapi.PanelID, o pluginapi.WidgetMount) error {
	f.rec("MountWidgetPanel", plugin, id, o.Placement, o.Title)
	ps := f.Panels()
	if fp := ps.ByID[id]; fp != nil {
		fp.Plugin, fp.Mount, fp.Panel = plugin, o, o.Panel
		fp.Open = true
		return nil
	}
	ps.ByID[id] = &FakePanel{ID: id, Plugin: plugin, Mount: o, Panel: o.Panel, Open: true}
	ps.Order = append(ps.Order, id)
	return nil
}

// UpdateWidgetPanel records the new spec.
func (f *FakeHost) UpdateWidgetPanel(plugin string, id pluginapi.PanelID, p pluginapi.WidgetPanel) error {
	f.rec("UpdateWidgetPanel", plugin, id)
	fp := f.Panels().ByID[id]
	if fp == nil || !fp.Open {
		return pluginapi.Errf(pluginapi.CodeClosed, "panel %d is not mounted", id)
	}
	fp.Panel = p
	return nil
}

// UnmountWidgetPanel closes the panel.
func (f *FakeHost) UnmountWidgetPanel(id pluginapi.PanelID) {
	f.rec("UnmountWidgetPanel", id)
	if fp := f.Panels().ByID[id]; fp != nil {
		fp.Open = false
	}
}

// WidgetCommand records the action.
func (f *FakeHost) WidgetCommand(id pluginapi.PanelID, a pluginapi.WidgetAction) error {
	f.rec("WidgetCommand", id, a.Kind)
	fp := f.Panels().ByID[id]
	if fp == nil || !fp.Open {
		return pluginapi.Errf(pluginapi.CodeClosed, "panel %d is not mounted", id)
	}
	fp.Commands = append(fp.Commands, a)
	return nil
}

// WidgetMutate records the mutation.
func (f *FakeHost) WidgetMutate(id pluginapi.PanelID, m pluginapi.WidgetMutation) error {
	f.rec("WidgetMutate", id, m.Kind)
	fp := f.Panels().ByID[id]
	if fp == nil || !fp.Open {
		return pluginapi.Errf(pluginapi.CodeClosed, "panel %d is not mounted", id)
	}
	fp.Mutations = append(fp.Mutations, m)
	return nil
}

// DropPanels removes the open panels of one plugin: the OwnerGone hook.
func (f *FakeHost) dropPanels(plugin string) {
	for _, fp := range f.Panels().ByID {
		if fp.Plugin == plugin {
			fp.Open = false
		}
	}
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		var out []string
		for _, fp := range f.Panels().Mounted() {
			out = append(out, fmt.Sprintf("panel:%s/%d", fp.Mount.Placement, fp.ID))
		}
		return out
	})
	OnOwnerGone(func(f *FakeHost, plugin string, gen uint64) { f.dropPanels(plugin) })
}
