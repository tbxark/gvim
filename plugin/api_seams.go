package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// The tier-1c-C seams.
//
//	gvim.win.set_split_label(win, label) -> true | nil, err
//	gvim.win.get_split_by_label(label) -> Window | nil
//	gvim.win.clear_split_label(win) -> true | nil, err
//	gvim.buf.get_window_state(buf, key[, default]) -> value | default
//	gvim.buf.set_window_state(buf, key, value) -> true | nil, err   (nil clears)
//	gvim.buf.open_file_in_split(path, opts?) ⏳ -> true, Buffer | nil, err
//	gvim.buf.focus_split(win) -> true | nil, err
//	gvim.buf.close_split(win) -> true | nil, err
//
// Split labels are per window and survive focus moves; they go away when the window closes.
// Window state is per plugin and buffer, string/number/boolean only, and is cleared when the
// buffer closes; it survives a plugin reload in-session.

func init() {
	registerAwait("buf_open_in_split", awaitBufOpenInSplit)
}

// installSeams installs the split-label, window-state and split wrapper functions.
func installSeams(e *apiEnv) {
	p := e.p
	win := e.area("win")
	buf := e.area("buf")
	host := func() pluginapi.Host { return p.r.guard }

	e.set(win, "set_split_label", func(L *lua.LState) int {
		id := pluginapi.WinID(checkID(L, 1, kindWindow))
		label := L.CheckString(2)
		return bufTrue(L, host().SetSplitLabel(id, label))
	})
	e.set(win, "get_split_by_label", func(L *lua.LState) int {
		label := L.CheckString(1)
		if id, ok := host().SplitByLabel(label); ok && id != 0 {
			L.Push(p.toLua(id))
			return 1
		}
		L.Push(lua.LNil)
		return 1
	})
	e.set(win, "clear_split_label", func(L *lua.LState) int {
		id := pluginapi.WinID(checkID(L, 1, kindWindow))
		return bufTrue(L, host().ClearSplitLabel(id))
	})

	e.set(buf, "get_window_state", func(L *lua.LState) int {
		id := pluginapi.BufferID(checkID(L, 1, kindBuffer))
		key := L.CheckString(2)
		if v, ok := host().GetWindowState(p.name, id, key); ok {
			L.Push(p.toLua(v))
			return 1
		}
		L.Push(L.Get(3))
		return 1
	})
	e.set(buf, "set_window_state", func(L *lua.LState) int {
		id := pluginapi.BufferID(checkID(L, 1, kindBuffer))
		key := L.CheckString(2)
		var v any
		switch val := L.Get(3).(type) {
		case *lua.LNilType:
			v = nil
		case lua.LBool:
			v = bool(val)
		case lua.LString:
			v = string(val)
		case lua.LNumber:
			v = float64(val)
		default:
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "a window state value is a string, number or boolean"))
		}
		return bufTrue(L, host().SetWindowState(p.name, id, key, v))
	})

	buf.RawSetString("open_file_in_split", e.async("buf_open_in_split"))
	e.set(buf, "focus_split", func(L *lua.LState) int {
		id := pluginapi.WinID(checkID(L, 1, kindWindow))
		return bufTrue(L, host().FocusWindow(id))
	})
	e.set(buf, "close_split", func(L *lua.LState) int {
		id := pluginapi.WinID(checkID(L, 1, kindWindow))
		return bufTrue(L, host().CloseWindow(id))
	})
}

// awaitBufOpenInSplit is gvim.buf.open_file_in_split: split the focused window (right by
// default, below with direction="below"), then open the file in it.
func awaitBufOpenInSplit(c *awaitCall) {
	dir := "right"
	if t, ok := bufOptional(c, 1, "open_file_in_split"); ok {
		o := bufTableOpts{t: t}
		if s, ok := o.str("direction"); ok {
			dir = s
		}
		if dir != "right" && dir != "below" {
			c.Bad(2, "open_file_in_split", "option 'direction' must be \"right\" or \"below\"")
			return
		}
	}
	action := "split_horizontal"
	if dir == "below" {
		action = "split_vertical"
	}
	if err := c.r.guard.Do(action, nil, 1); err != nil {
		c.Fail(err)
		return
	}
	awaitBufOpen(c)
}
