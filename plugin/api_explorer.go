package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// The file explorer surface:
//
//	local ns = "git_explorer"
//	gvim.explorer.set_decorations(ns, {{path=, symbol=, color=, priority=}, ...}) -> true | nil, err
//	gvim.explorer.clear_decorations(ns) -> true
//	gvim.explorer.set_slots(ns, {{path=, leading={text=,color=,min_width=}?, suppress_leading=?,
//	  trailing={text=,color=}?, suppress_trailing=?, name_color=?, suppress_name_color=?,
//	  priority=}, ...}) -> true | nil, err
//	gvim.explorer.clear_slots(ns) -> true
//
// Decorations give explorer rows a trailing symbol and colour; slots override the leading
// and trailing slots and the filename colour. Namespaces are isolated per plugin, so two
// plugins may reuse a label. Positions are explorer rows, not buffer lines; a path outside
// the workspace root is ignored.

// installExplorer installs gvim.explorer.
func installExplorer(e *apiEnv) {
	p := e.p
	area := e.area("explorer")
	host := func() pluginapi.Host { return p.r.guard }

	e.set(area, "set_decorations", func(L *lua.LState) int {
		ns := L.CheckString(1)
		list := L.CheckTable(2)
		if list.Len() > pluginapi.MaxExplorerEntries {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "a namespace takes at most %d decorations", pluginapi.MaxExplorerEntries))
		}
		items := make([]pluginapi.FileExplorerDecoration, 0, list.Len())
		for i := 1; i <= list.Len(); i++ {
			row, ok := list.RawGetInt(i).(*lua.LTable)
			if !ok {
				return bufRet(L, invalidErr("decoration %d: a table expected", i))
			}
			o := bufTableOpts{t: row}
			d := pluginapi.FileExplorerDecoration{
				Path:     optString(&o, "path"),
				Symbol:   clipText(optString(&o, "symbol"), pluginapi.MaxExplorerSymbol),
				Priority: o.integer("priority", 0),
			}
			if o.err != "" {
				return bufRet(L, invalidErr("decoration %d: %s", i, o.err))
			}
			if d.Path == "" || d.Symbol == "" {
				return bufRet(L, invalidErr("decoration %d: path and symbol are required", i))
			}
			if c, err := colorFromLua(row.RawGetString("color")); err == nil {
				d.Color = c
			}
			items = append(items, d)
		}
		return bufTrue(L, host().SetExplorerDecorations(p.name, ns, items))
	})
	e.set(area, "clear_decorations", func(L *lua.LState) int {
		host().ClearExplorerDecorations(p.name, L.CheckString(1))
		L.Push(lua.LTrue)
		return 1
	})

	slotText := func(t *lua.LTable) *pluginapi.FileExplorerSlotText {
		if t == nil {
			return nil
		}
		o := bufTableOpts{t: t}
		out := &pluginapi.FileExplorerSlotText{
			Text:     clipText(optString(&o, "text"), pluginapi.MaxExplorerSlotText),
			MinWidth: o.integer("min_width", 0),
		}
		if c, err := colorFromLua(t.RawGetString("color")); err == nil {
			out.Color = c
		}
		return out
	}
	e.set(area, "set_slots", func(L *lua.LState) int {
		ns := L.CheckString(1)
		list := L.CheckTable(2)
		if list.Len() > pluginapi.MaxExplorerEntries {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "a namespace takes at most %d slot entries", pluginapi.MaxExplorerEntries))
		}
		slots := make([]pluginapi.FileExplorerSlot, 0, list.Len())
		for i := 1; i <= list.Len(); i++ {
			row, ok := list.RawGetInt(i).(*lua.LTable)
			if !ok {
				return bufRet(L, invalidErr("slot %d: a table expected", i))
			}
			o := bufTableOpts{t: row}
			s := pluginapi.FileExplorerSlot{
				Path:              optString(&o, "path"),
				SuppressLeading:   o.boolean("suppress_leading", false),
				SuppressTrailing:  o.boolean("suppress_trailing", false),
				SuppressNameColor: o.boolean("suppress_name_color", false),
				Priority:          o.integer("priority", 0),
			}
			if o.err != "" {
				return bufRet(L, invalidErr("slot %d: %s", i, o.err))
			}
			if s.Path == "" {
				return bufRet(L, invalidErr("slot %d: a path is required", i))
			}
			if lt, ok := row.RawGetString("leading").(*lua.LTable); ok {
				s.Leading = slotText(lt)
			}
			if tt, ok := row.RawGetString("trailing").(*lua.LTable); ok {
				s.Trailing = slotText(tt)
			}
			if c, err := colorFromLua(row.RawGetString("name_color")); err == nil {
				s.NameColor = c
			}
			slots = append(slots, s)
		}
		return bufTrue(L, host().SetExplorerSlots(p.name, ns, slots))
	})
	e.set(area, "clear_slots", func(L *lua.LState) int {
		host().ClearExplorerSlots(p.name, L.CheckString(1))
		L.Push(lua.LTrue)
		return 1
	})
}
