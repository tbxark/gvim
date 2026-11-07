package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.win and the Window handle (FINAL 5.3, X9, AMENDMENTS C23).
//
//	gvim.win.current() -> Window | nil     gvim.win.list() -> Window[]
//	gvim.win.get(id) -> Window | nil       gvim.win.equalize() -> true
//	w.id  w:valid()  w:buffer() -> Buffer | nil
//	w:set_buffer(buf) / w:focus() / w:close() -> true | nil, err
//	w:split{dir=, buf=, ratio=, before=, focus=} -> Window | nil, err
//	w:size() -> {x, y, width, height}      the text area in screen cells
//	w:view() -> {top_line, left_col, height, width}
//	w:visible() -> { {line, col_from, col_to, wrapped}, ... }
//	w:scroll_to(line, {center=false}) -> true | nil, err
//
// A window is a split pane. The cursor and the selection belong to the buffer, not to
// the window. Window handles are cached per id, so `a == b` and table keys work, and a
// handle to a closed window stays valid as a value: its methods return `nil, "closed: ..."`
// and w:valid() is false. Window ids are never reused.
//
// Heights are rows, not buffer lines: with soft wrap a window shows fewer lines than it has
// rows, so use w:visible() to learn which lines and which byte columns of them are on the
// screen. The rows are those the painter draws, wrapped continuation rows included, lines
// hidden by a fold left out, at most 500, columns are bytes in [col_from, col_to).
//
// A plugin that splits a window owns the new window: when the plugin is unloaded or
// reloaded the windows it opened and has not closed are closed (the buffers stay).

// winDefaultDir is the dir of w:split when the option is absent.
const winDefaultDir = "right"

// winField is one integer field of a result table.
type winField struct {
	name string
	v    int
}

func winTable(L *lua.LState, fields ...winField) *lua.LTable {
	t := L.CreateTable(0, len(fields)+1)
	for _, f := range fields {
		t.RawSetString(f.name, lua.LNumber(f.v))
	}
	return t
}

// installWin installs windows: gvim.win and the Window handle methods (stage S17).
func installWin(e *apiEnv) {
	p := e.p
	e.provides("windows")
	area := e.area("win")
	methods := e.kind(kindWindow).methods
	host := func() pluginapi.Host { return p.r.guard }

	// opened are the windows this plugin split, oldest first; unload closes the ones
	// still open, newest first.
	var opened []pluginapi.WinID
	p.addCleanup(func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = p.r.host.CloseWindow(opened[i])
		}
	})
	forget := func(id pluginapi.WinID) {
		for i, w := range opened {
			if w == id {
				opened = append(opened[:i:i], opened[i+1:]...)
				return
			}
		}
	}

	info := func(id pluginapi.WinID) (pluginapi.WindowInfo, bool) {
		for _, w := range host().Windows() {
			if w.ID == id {
				return w, true
			}
		}
		return pluginapi.WindowInfo{}, false
	}

	// ---------- gvim.win ----------

	e.set(area, "current", func(L *lua.LState) int {
		L.Push(p.toLua(host().CurrentWindow()))
		return 1
	})
	e.set(area, "list", func(L *lua.LState) int {
		ws := host().Windows()
		t := L.CreateTable(len(ws), 0)
		for _, w := range ws {
			t.Append(p.toLua(w.ID))
		}
		L.Push(t)
		return 1
	})
	e.set(area, "get", func(L *lua.LState) int {
		id := pluginapi.WinID(max(L.CheckInt64(1), 0))
		if _, ok := info(id); !ok {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(p.toLua(id))
		return 1
	})
	e.set(area, "equalize", func(L *lua.LState) int { return bufTrue(L, host().Equalize()) })

	// ---------- the Window handle ----------

	// meth defines a method; f receives the id of the handle argument.
	meth := func(name string, f func(L *lua.LState, id pluginapi.WinID) int) {
		e.set(methods, name, func(L *lua.LState) int {
			return f(L, pluginapi.WinID(checkID(L, 1, kindWindow)))
		})
	}
	// query defines a method that reads the window's current description.
	query := func(name string, f func(L *lua.LState, w pluginapi.WindowInfo) lua.LValue) {
		meth(name, func(L *lua.LState, id pluginapi.WinID) int {
			w, ok := info(id)
			if !ok {
				return bufRet(L, closedWin(id))
			}
			L.Push(f(L, w))
			return 1
		})
	}
	query("buffer", func(_ *lua.LState, w pluginapi.WindowInfo) lua.LValue { return p.toLua(w.Buffer) })
	query("size", func(L *lua.LState, w pluginapi.WindowInfo) lua.LValue {
		return winTable(L, winField{"x", w.X}, winField{"y", w.Y}, winField{"width", w.Width}, winField{"height", w.Height})
	})
	query("view", func(L *lua.LState, w pluginapi.WindowInfo) lua.LValue {
		return winTable(L, winField{"top_line", w.TopLine}, winField{"left_col", w.LeftCol},
			winField{"height", w.Height}, winField{"width", w.Width})
	})
	meth("visible", func(L *lua.LState, id pluginapi.WinID) int {
		rows, err := host().VisibleRows(id)
		if err != nil {
			return bufRet(L, err)
		}
		t := L.CreateTable(len(rows), 0)
		for _, r := range rows {
			row := winTable(L, winField{"line", r.Line}, winField{"col_from", r.ColFrom}, winField{"col_to", r.ColTo})
			row.RawSetString("wrapped", lua.LBool(r.Wrapped))
			t.Append(row)
		}
		L.Push(t)
		return 1
	})
	meth("set_buffer", func(L *lua.LState, id pluginapi.WinID) int {
		buf := pluginapi.BufferID(checkID(L, 2, kindBuffer))
		return bufTrue(L, host().SetWindowBuffer(id, buf))
	})
	meth("focus", func(L *lua.LState, id pluginapi.WinID) int { return bufTrue(L, host().FocusWindow(id)) })
	meth("close", func(L *lua.LState, id pluginapi.WinID) int {
		err := host().CloseWindow(id)
		if err == nil {
			forget(id)
		}
		return bufTrue(L, err)
	})
	meth("scroll_to", func(L *lua.LState, id pluginapi.WinID) int {
		line := clampInt(float64(L.CheckNumber(2)))
		o := bufTableOpts{t: bufOptTable(L, 3)}
		center := o.boolean("center", false)
		if o.err != "" {
			L.ArgError(3, o.err)
		}
		return bufTrue(L, host().ScrollWindow(id, line, center))
	})
	meth("split", func(L *lua.LState, id pluginapi.WinID) int {
		o := bufTableOpts{t: bufOptTable(L, 2)}
		var so pluginapi.SplitOptions
		so.Dir = winDefaultDir
		if s, ok := o.str("dir"); ok {
			so.Dir = s
		}
		so.Before = o.boolean("before", false)
		so.Focus = o.boolean("focus", false)
		switch v := o.get("ratio").(type) {
		case *lua.LNilType:
		case lua.LNumber:
			so.Ratio = float64(v)
			if !(so.Ratio > 0 && so.Ratio < 1) {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "split ratio must be between 0 and 1"))
			}
		default:
			o.fail("option 'ratio' must be a number")
		}
		if v := o.get("buf"); v != lua.LNil {
			ud, _ := v.(*lua.LUserData)
			h, ok := idHandle{}, false
			if ud != nil {
				h, ok = ud.Value.(idHandle)
			}
			if !ok || h.kind != kindBuffer {
				o.fail("option 'buf' must be a Buffer")
			}
			so.Buffer = pluginapi.BufferID(h.id)
		}
		if o.err != "" {
			L.ArgError(2, o.err)
		}
		nw, err := host().SplitWindow(id, so)
		if err != nil {
			return bufRet(L, err)
		}
		opened = append(opened, nw)
		L.Push(p.toLua(nw))
		return 1
	})
}

func closedWin(id pluginapi.WinID) error {
	return pluginapi.Errf(pluginapi.CodeClosed, "window %d is closed", id)
}
