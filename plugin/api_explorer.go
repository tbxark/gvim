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
			c, err := colorFromLua(row.RawGetString("color"))
			if err != nil {
				return bufRet(L, invalidErr("decoration %d: color: %v", i, err))
			}
			d.Color = c
			items = append(items, d)
		}
		return bufTrue(L, host().SetExplorerDecorations(p.name, ns, items))
	})
	e.set(area, "clear_decorations", func(L *lua.LState) int {
		host().ClearExplorerDecorations(p.name, L.CheckString(1))
		L.Push(lua.LTrue)
		return 1
	})

	slotText := func(t *lua.LTable) (*pluginapi.FileExplorerSlotText, error) {
		if t == nil {
			return nil, nil
		}
		o := bufTableOpts{t: t}
		out := &pluginapi.FileExplorerSlotText{
			Text:     clipText(optString(&o, "text"), pluginapi.MaxExplorerSlotText),
			MinWidth: o.integer("min_width", 0),
		}
		if o.err != "" {
			return nil, invalidErr("%s", o.err)
		}
		c, err := colorFromLua(t.RawGetString("color"))
		if err != nil {
			return nil, invalidErr("color: %v", err)
		}
		out.Color = c
		return out, nil
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
			if v := row.RawGetString("leading"); v != lua.LNil {
				lt, ok := v.(*lua.LTable)
				if !ok {
					return bufRet(L, invalidErr("slot %d: leading must be a table", i))
				}
				st, err := slotText(lt)
				if err != nil {
					return bufRet(L, invalidErr("slot %d: leading: %v", i, err))
				}
				s.Leading = st
			}
			if v := row.RawGetString("trailing"); v != lua.LNil {
				tt, ok := v.(*lua.LTable)
				if !ok {
					return bufRet(L, invalidErr("slot %d: trailing must be a table", i))
				}
				st, err := slotText(tt)
				if err != nil {
					return bufRet(L, invalidErr("slot %d: trailing: %v", i, err))
				}
				s.Trailing = st
			}
			c, err := colorFromLua(row.RawGetString("name_color"))
			if err != nil {
				return bufRet(L, invalidErr("slot %d: name_color: %v", i, err))
			}
			s.NameColor = c
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
