package plugin

import (
	"encoding/json"
	"sync/atomic"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.buf buffer groups and gvim.win scroll-sync groups (plugin API tier 1b, group-lite).
//
//	gvim.buf.create_group(name, layout_json) -> id | nil, err
//	gvim.buf.set_panel_content(id, panel, entries) -> true | nil, err
//	gvim.buf.set_group_title(id, { {text=, panel=}, ... }) -> true | nil, err
//	gvim.buf.set_panel_visible(id, panel, visible) -> true | nil, err
//	gvim.buf.focus_panel(id, panel) -> true | nil, err
//	gvim.buf.close_group(id) -> true
//	gvim.buf.group_panels(id) -> { {name=, buf=, window=, visible=}, ... }
//	gvim.win.create_scroll_sync(id, left, right) -> true | nil, err
//	gvim.win.set_scroll_sync_anchors(id, { {left, right}, ... }) -> true | nil, err
//	gvim.win.remove_scroll_sync(id) -> true
//
// A layout is Fresh's JSON: a leaf is {"type":"scrollable","id":"name"} or
// {"type":"fixed","id":"name","height":n} (the fixed height is accepted and ignored: the
// split ratios place panels in gvim); a split is
// {"type":"split","direction":"h"|"v","ratio":0.5,"first":...,"second":...}. A leaf may take
// "bufferId" to show an existing buffer; otherwise the host makes a read-only panel buffer
// the group owns. There is no synthetic group tab yet, so the title segments are stored for
// the ports and not painted in a tab (tier 1c).

// groupSeq numbers the buffer groups of all plugins.
var groupSeq atomic.Uint64

// maxGroupLayoutJSON bounds the layout JSON a plugin may send.
const maxGroupLayoutJSON = 64 << 10

// maxFixedGroupHeight bounds a fixed layout leaf's height.
const maxFixedGroupHeight = 100

// bgNode is one node of the layout JSON.
type bgNode struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Height    int     `json:"height"`
	Direction string  `json:"direction"`
	Ratio     float64 `json:"ratio"`
	First     *bgNode `json:"first"`
	Second    *bgNode `json:"second"`
	BufferID  *uint64 `json:"bufferId"`
}

// installGroups installs the buffer-group and scroll-sync functions.
func installGroups(e *apiEnv) {
	p := e.p
	buf := e.area("buf")
	win := e.area("win")

	e.set(buf, "create_group", func(L *lua.LState) int {
		name := clipText(L.CheckString(1), maxPanelTitle)
		raw := L.CheckString(2)
		if len(raw) > maxGroupLayoutJSON {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "a group layout is at most %d KiB", maxGroupLayoutJSON>>10))
		}
		layout, err := groupLayoutFromJSON(raw)
		if err != nil {
			return bufRet(L, err)
		}
		id := pluginapi.BufferGroupID(groupSeq.Add(1))
		if err := p.r.guard.CreateBufferGroup(p.name, id, pluginapi.BufferGroupSpec{Name: name, Layout: layout}); err != nil {
			return bufRet(L, err)
		}
		if p.groups == nil {
			p.groups = map[pluginapi.BufferGroupID]bool{}
		}
		p.groups[id] = true
		L.Push(lua.LNumber(uint64(id)))
		return 1
	})

	e.set(buf, "set_panel_content", func(L *lua.LState) int {
		id := pluginapi.BufferGroupID(groupIDFromLua(L, 1))
		panel := L.CheckString(2)
		entries, err := p.contentFromLua(L, L.CheckTable(3))
		if err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, p.r.guard.SetPanelContent(p.name, id, panel, entries))
	})

	e.set(buf, "set_group_title", func(L *lua.LState) int {
		id := pluginapi.BufferGroupID(groupIDFromLua(L, 1))
		list := L.CheckTable(2)
		if list.Len() > 16 {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "a group title has at most 16 segments"))
		}
		segs := make([]pluginapi.BufferGroupTitleSegment, 0, list.Len())
		for i := 1; i <= list.Len(); i++ {
			row, ok := list.RawGetInt(i).(*lua.LTable)
			if !ok {
				return bufRet(L, invalidErr("title segment %d must be a table", i))
			}
			o := bufTableOpts{t: row}
			segs = append(segs, pluginapi.BufferGroupTitleSegment{
				Text:  clipText(optString(&o, "text"), maxPanelTitle),
				Panel: clipText(optString(&o, "panel"), maxPanelKey),
			})
			if o.err != "" {
				return bufRet(L, invalidErr("title segment %d: %s", i, o.err))
			}
		}
		if err := p.ownsGroup(id); err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, p.r.guard.SetBufferGroupTitle(id, segs))
	})

	e.set(buf, "set_panel_visible", func(L *lua.LState) int {
		id := pluginapi.BufferGroupID(groupIDFromLua(L, 1))
		panel := L.CheckString(2)
		visible := bool(L.CheckBool(3))
		if err := p.ownsGroup(id); err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, p.r.guard.SetBufferGroupPanelVisible(id, panel, visible))
	})

	e.set(buf, "focus_panel", func(L *lua.LState) int {
		id := pluginapi.BufferGroupID(groupIDFromLua(L, 1))
		if err := p.ownsGroup(id); err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, p.r.guard.FocusBufferGroupPanel(id, L.CheckString(2)))
	})

	e.set(buf, "close_group", func(L *lua.LState) int {
		id := pluginapi.BufferGroupID(groupIDFromLua(L, 1))
		if err := p.ownsGroup(id); err != nil {
			return bufRet(L, err)
		}
		p.r.guard.CloseBufferGroup(id)
		delete(p.groups, id)
		L.Push(lua.LTrue)
		return 1
	})

	e.set(buf, "group_panels", func(L *lua.LState) int {
		id := pluginapi.BufferGroupID(groupIDFromLua(L, 1))
		var panels []pluginapi.BufferGroupPanel
		if p.groups[id] {
			panels = p.r.guard.BufferGroupPanels(id)
		}
		if panels == nil {
			v, msg := errTuple(pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id))
			L.Push(v)
			L.Push(msg)
			return 2
		}
		out := L.CreateTable(len(panels), 0)
		for _, panel := range panels {
			row := L.CreateTable(0, 4)
			row.RawSetString("name", lua.LString(panel.Name))
			row.RawSetString("buf", p.toLua(panel.Buffer))
			if panel.Window != 0 {
				row.RawSetString("window", p.toLua(pluginapi.WinID(panel.Window)))
			}
			row.RawSetString("visible", lua.LBool(panel.Visible))
			out.Append(row)
		}
		L.Push(out)
		return 1
	})

	e.set(win, "create_scroll_sync", func(L *lua.LState) int {
		id := uint64(L.CheckInt64(1))
		if id == 0 || id > 1<<53 {
			return bufRet(L, invalidErr("a scroll sync id must be a positive integer"))
		}
		left := pluginapi.WinID(checkID(L, 2, kindWindow))
		right := pluginapi.WinID(checkID(L, 3, kindWindow))
		if err := p.r.guard.CreateScrollSyncGroup(p.name, id, left, right); err != nil {
			return bufRet(L, err)
		}
		if p.scrolls == nil {
			p.scrolls = map[uint64]bool{}
		}
		p.scrolls[id] = true
		L.Push(lua.LTrue)
		return 1
	})

	e.set(win, "set_scroll_sync_anchors", func(L *lua.LState) int {
		id := uint64(L.CheckInt64(1))
		list := L.CheckTable(2)
		if list.Len() > 10_000 {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "a scroll sync group takes at most 10000 anchors"))
		}
		if !p.scrolls[id] {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeClosed, "scroll sync group %d is not mounted by this plugin", id))
		}
		anchors := make([]pluginapi.ScrollAnchor, 0, list.Len())
		for i := 1; i <= list.Len(); i++ {
			row, ok := list.RawGetInt(i).(*lua.LTable)
			if !ok || row.Len() < 2 {
				continue
			}
			left, lok := row.RawGetInt(1).(lua.LNumber)
			right, rok := row.RawGetInt(2).(lua.LNumber)
			if !lok || !rok {
				return bufRet(L, invalidErr("scroll sync anchor %d must be {left, right} line numbers", i))
			}
			anchors = append(anchors, pluginapi.ScrollAnchor{Left: clampInt(float64(left)), Right: clampInt(float64(right))})
		}
		return bufTrue(L, p.r.guard.SetScrollSyncAnchors(id, anchors))
	})

	e.set(win, "remove_scroll_sync", func(L *lua.LState) int {
		id := uint64(L.CheckInt64(1))
		if !p.scrolls[id] {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeClosed, "scroll sync group %d is not mounted by this plugin", id))
		}
		p.r.guard.RemoveScrollSyncGroup(id)
		delete(p.scrolls, id)
		L.Push(lua.LTrue)
		return 1
	})
}

// groupIDFromLua reads a group id argument.
func groupIDFromLua(L *lua.LState, n int) uint64 {
	f := float64(L.CheckNumber(n))
	if f < 0 || f != float64(uint64(f)) {
		L.ArgError(n, "a group id is a positive integer")
	}
	return uint64(f)
}

// groupLayoutFromJSON parses the layout JSON of create_group.
func groupLayoutFromJSON(raw string) (pluginapi.BufferGroupLayout, error) {
	var node bgNode
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return pluginapi.BufferGroupLayout{}, invalidErr("layout: %s", err.Error())
	}
	return groupLayoutNode(&node, 0)
}

// groupLayoutNode converts one layout node to the host shape.
func groupLayoutNode(node *bgNode, depth int) (pluginapi.BufferGroupLayout, error) {
	if node == nil {
		return pluginapi.BufferGroupLayout{}, invalidErr("layout: a node is missing")
	}
	if depth > 6 {
		return pluginapi.BufferGroupLayout{}, pluginapi.Errf(pluginapi.CodeLimit, "layout is too deep")
	}
	switch node.Type {
	case "scrollable", "fixed":
		if node.ID == "" {
			return pluginapi.BufferGroupLayout{}, invalidErr("layout: a leaf needs an id")
		}
		if len(node.ID) > maxPanelKey {
			return pluginapi.BufferGroupLayout{}, pluginapi.Errf(pluginapi.CodeLimit, "layout: a panel name is too long")
		}
		leaf := &pluginapi.BufferGroupLeaf{Name: node.ID}
		if node.BufferID != nil {
			leaf.Buffer = pluginapi.BufferID(*node.BufferID)
		}
		if node.Type == "fixed" {
			if node.Height < 1 || node.Height > maxFixedGroupHeight {
				return pluginapi.BufferGroupLayout{}, pluginapi.Errf(pluginapi.CodeInvalid, "layout: a fixed leaf height must be 1 to %d rows", maxFixedGroupHeight)
			}
			leaf.Fixed, leaf.Height = true, node.Height
		}
		return pluginapi.BufferGroupLayout{Leaf: leaf}, nil
	case "split":
		if node.Direction != "h" && node.Direction != "v" {
			return pluginapi.BufferGroupLayout{}, invalidErr(`layout: a split needs direction "h" or "v"`)
		}
		first, err := groupLayoutNode(node.First, depth+1)
		if err != nil {
			return pluginapi.BufferGroupLayout{}, err
		}
		second, err := groupLayoutNode(node.Second, depth+1)
		if err != nil {
			return pluginapi.BufferGroupLayout{}, err
		}
		ratio := node.Ratio
		if ratio < 0 || ratio > 1 {
			ratio = 0
		}
		return pluginapi.BufferGroupLayout{Dir: node.Direction, Ratio: ratio, First: &first, Second: &second}, nil
	}
	return pluginapi.BufferGroupLayout{}, invalidErr("layout: unknown node type %q", node.Type)
}

// ownsGroup refuses a buffer group id the plugin did not create: group ids are global.
func (p *plugin) ownsGroup(id pluginapi.BufferGroupID) error {
	if !p.groups[id] {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted by this plugin", id)
	}
	return nil
}
