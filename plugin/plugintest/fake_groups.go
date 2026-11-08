package plugintest

import (
	"fmt"

	"github.com/tbxark/gvim/pluginapi"
)

// The buffer-group and scroll-sync part of the fake host (plugin API tier 1b, group-lite).
// The fake keeps the groups a plugin created, the content and title writes, the panel
// visibility/focus calls and the scroll-sync groups, so a test can drive the Lua side and
// assert what crossed the host boundary. OwnerGone drops what a plugin owns.

// FakeGroup is one created buffer group.
type FakeGroup struct {
	ID      pluginapi.BufferGroupID
	Plugin  string
	Spec    pluginapi.BufferGroupSpec
	Panels  map[string]pluginapi.BufferGroupPanel
	Order   []string
	Title   []pluginapi.BufferGroupTitleSegment
	Content map[string][]pluginapi.ContentEntry
	Focused string
	Closed  bool
}

// FakeScrollSync is one scroll-sync group.
type FakeScrollSync struct {
	ID      uint64
	Plugin  string
	Left    pluginapi.WinID
	Right   pluginapi.WinID
	Anchors []pluginapi.ScrollAnchor
	Removed bool
}

// FakeGroups is the group state of the fake host.
type FakeGroups struct {
	ByID   map[pluginapi.BufferGroupID]*FakeGroup
	Order  []pluginapi.BufferGroupID
	Scroll map[uint64]*FakeScrollSync
}

// Groups returns the group state of the fake host.
func (f *FakeHost) Groups() *FakeGroups {
	g, ok := f.Ext["groups"].(*FakeGroups)
	if !ok {
		g = &FakeGroups{ByID: map[pluginapi.BufferGroupID]*FakeGroup{}, Scroll: map[uint64]*FakeScrollSync{}}
		f.Ext["groups"] = g
	}
	return g
}

// CreateBufferGroup records the group and the panel buffers it would create.
func (f *FakeHost) CreateBufferGroup(plugin string, id pluginapi.BufferGroupID, o pluginapi.BufferGroupSpec) error {
	f.rec("CreateBufferGroup", plugin, id, o.Name)
	g := f.Groups()
	if g.ByID[id] != nil {
		return pluginapi.Errf(pluginapi.CodeBusy, "group %d is already mounted", id)
	}
	fg := &FakeGroup{ID: id, Plugin: plugin, Spec: o, Panels: map[string]pluginapi.BufferGroupPanel{}, Content: map[string][]pluginapi.ContentEntry{}}
	var walk func(node *pluginapi.BufferGroupLayout)
	walk = func(node *pluginapi.BufferGroupLayout) {
		if node == nil {
			return
		}
		if node.Leaf != nil {
			buf := node.Leaf.Buffer
			if buf == 0 {
				f.nextBuf++
				buf = f.nextBuf
			}
			fg.Panels[node.Leaf.Name] = pluginapi.BufferGroupPanel{Name: node.Leaf.Name, Buffer: buf, Window: pluginapi.WinID(1000 + len(fg.Order)), Visible: true}
			fg.Order = append(fg.Order, node.Leaf.Name)
			return
		}
		walk(node.First)
		walk(node.Second)
	}
	walk(&o.Layout)
	g.ByID[id] = fg
	g.Order = append(g.Order, id)
	return nil
}

// SetPanelContent records the panel content.
func (f *FakeHost) SetPanelContent(plugin string, id pluginapi.BufferGroupID, panel string, entries []pluginapi.ContentEntry) error {
	f.rec("SetPanelContent", plugin, id, panel, len(entries))
	g := f.Groups().ByID[id]
	if g == nil || g.Closed {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	if _, ok := g.Panels[panel]; !ok {
		return pluginapi.Errf(pluginapi.CodeNotFound, "no panel %q in group %d", panel, id)
	}
	g.Content[panel] = append([]pluginapi.ContentEntry(nil), entries...)
	return nil
}

// SetBufferGroupTitle records the title segments.
func (f *FakeHost) SetBufferGroupTitle(id pluginapi.BufferGroupID, segments []pluginapi.BufferGroupTitleSegment) error {
	f.rec("SetBufferGroupTitle", id, len(segments))
	g := f.Groups().ByID[id]
	if g == nil || g.Closed {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	g.Title = append([]pluginapi.BufferGroupTitleSegment(nil), segments...)
	return nil
}

// SetBufferGroupPanelVisible records the visibility change.
func (f *FakeHost) SetBufferGroupPanelVisible(id pluginapi.BufferGroupID, panel string, visible bool) error {
	f.rec("SetBufferGroupPanelVisible", id, panel, visible)
	g := f.Groups().ByID[id]
	if g == nil || g.Closed {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	p, ok := g.Panels[panel]
	if !ok {
		return pluginapi.Errf(pluginapi.CodeNotFound, "no panel %q in group %d", panel, id)
	}
	p.Visible = visible
	g.Panels[panel] = p
	return nil
}

// FocusBufferGroupPanel records the focus call.
func (f *FakeHost) FocusBufferGroupPanel(id pluginapi.BufferGroupID, panel string) error {
	f.rec("FocusBufferGroupPanel", id, panel)
	g := f.Groups().ByID[id]
	if g == nil || g.Closed {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	g.Focused = panel
	return nil
}

// CloseBufferGroup closes the group.
func (f *FakeHost) CloseBufferGroup(id pluginapi.BufferGroupID) {
	f.rec("CloseBufferGroup", id)
	if g := f.Groups().ByID[id]; g != nil {
		g.Closed = true
	}
}

// BufferGroupPanels lists the group's panels.
func (f *FakeHost) BufferGroupPanels(id pluginapi.BufferGroupID) []pluginapi.BufferGroupPanel {
	f.rec("BufferGroupPanels", id)
	g := f.Groups().ByID[id]
	if g == nil || g.Closed {
		return nil
	}
	out := make([]pluginapi.BufferGroupPanel, 0, len(g.Order))
	for _, name := range g.Order {
		out = append(out, g.Panels[name])
	}
	return out
}

// CreateScrollSyncGroup records a scroll-sync group; windows 0 are refused like the ui.
func (f *FakeHost) CreateScrollSyncGroup(plugin string, id uint64, left, right pluginapi.WinID) error {
	f.rec("CreateScrollSyncGroup", plugin, id, left, right)
	g := f.Groups()
	if left == 0 || right == 0 || left == right {
		return pluginapi.Errf(pluginapi.CodeInvalid, "two different windows are needed")
	}
	if g.Scroll[id] != nil {
		return pluginapi.Errf(pluginapi.CodeBusy, "scroll sync group %d is already mounted", id)
	}
	g.Scroll[id] = &FakeScrollSync{ID: id, Plugin: plugin, Left: left, Right: right}
	return nil
}

// SetScrollSyncAnchors records the anchors.
func (f *FakeHost) SetScrollSyncAnchors(id uint64, anchors []pluginapi.ScrollAnchor) error {
	f.rec("SetScrollSyncAnchors", id, len(anchors))
	g := f.Groups()
	sg := g.Scroll[id]
	if sg == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "scroll sync group %d is not mounted", id)
	}
	sg.Anchors = append([]pluginapi.ScrollAnchor(nil), anchors...)
	return nil
}

// RemoveScrollSyncGroup removes a scroll-sync group.
func (f *FakeHost) RemoveScrollSyncGroup(id uint64) {
	f.rec("RemoveScrollSyncGroup", id)
	if sg := f.Groups().Scroll[id]; sg != nil {
		sg.Removed = true
	}
}

// dropGroups removes what a plugin owns: the OwnerGone hook.
func (f *FakeHost) dropGroups(plugin string) {
	g := f.Groups()
	for _, id := range g.Order {
		if fg := g.ByID[id]; fg != nil && fg.Plugin == plugin {
			fg.Closed = true
		}
	}
	for _, sg := range g.Scroll {
		if sg.Plugin == plugin {
			sg.Removed = true
		}
	}
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		var out []string
		g := f.Groups()
		for _, id := range g.Order {
			if fg := g.ByID[id]; fg != nil && !fg.Closed {
				out = append(out, fmt.Sprintf("group:%s/%d", fg.Plugin, id))
			}
		}
		for _, sg := range g.Scroll {
			if !sg.Removed {
				out = append(out, fmt.Sprintf("scroll_sync:%s/%d", sg.Plugin, sg.ID))
			}
		}
		return out
	})
	OnOwnerGone(func(f *FakeHost, plugin string, gen uint64) { f.dropGroups(plugin) })
}
