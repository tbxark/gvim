package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// Line targets (tier-1c remainder, Fresh's setLineTargets):
//
//	gvim.buf.set_line_targets(buf, {{line=, path=, target=?, into=?}, ...}) -> true | nil, err
//	gvim.buf.set_line_targets(buf, {{line=, action=}, ...}) -> true | nil, err
//	gvim.buf.clear_line_targets(buf) -> true
//
// A target makes a line of the buffer clickable: Enter on it or a single left click opens
// path at target line, into the split named by into when that label is live, else beside
// the pane holding the targets. The call replaces the buffer's previous targets; an empty
// list clears them. line and target are 0-based, target defaults to the top line.

func installLineTargets(e *apiEnv) {
	p := e.p
	buf := e.area("buf")

	e.set(buf, "set_line_targets", func(L *lua.LState) int {
		id := pluginapi.BufferID(checkID(L, 1, kindBuffer))
		list := L.CheckTable(2)
		if list.Len() > pluginapi.MaxLineTargets {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "a buffer takes at most %d line targets", pluginapi.MaxLineTargets))
		}
		targets := make([]pluginapi.LineTarget, 0, list.Len())
		for i := 1; i <= list.Len(); i++ {
			row, ok := list.RawGetInt(i).(*lua.LTable)
			if !ok {
				return bufRet(L, invalidErr("target %d: a table expected", i))
			}
			o := bufTableOpts{t: row}
			t := pluginapi.LineTarget{
				Line:   o.integer("line", 0),
				Path:   clipText(optString(&o, "path"), pluginapi.MaxLineTargetLen),
				Target: o.integer("target", 0),
				Into:   clipText(optString(&o, "into"), pluginapi.MaxLineTargetInto),
				Action: clipText(optString(&o, "action"), pluginapi.MaxLineTargetAction),
			}
			if o.err != "" {
				return bufRet(L, invalidErr("target %d: %s", i, o.err))
			}
			if t.Path == "" && t.Action == "" {
				return bufRet(L, invalidErr("target %d: a path or action is required", i))
			}
			if t.Path != "" && t.Action != "" {
				return bufRet(L, invalidErr("target %d: a path and an action are mutually exclusive", i))
			}
			targets = append(targets, t)
		}
		return bufTrue(L, p.r.guard.SetLineTargets(p.name, id, targets))
	})
	e.set(buf, "clear_line_targets", func(L *lua.LState) int {
		id := pluginapi.BufferID(checkID(L, 1, kindBuffer))
		p.r.guard.ClearLineTargets(p.name, id)
		L.Push(lua.LTrue)
		return 1
	})
}
