package plugin

import (
	"strconv"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// A test seam for bindings.go that reaches the set without gvim.keymap: a `B`
// table in every plugin state.
//
//	B.set(chord, action) -> true | nil, err      bind a chord to an action id
//	B.fn(chord, fn)      -> true | nil, err      bind a chord to a Lua function (hidden command)
//	B.del(chord)         -> boolean
//	B.check(chord)       -> normalised chord | nil, err
//	B.fill(n)            -> true | nil, err      n bindings "k1".."kn" to "t.x", published once

func init() {
	extraInstallers = append(extraInstallers, installBindingsSeam)
}

func installBindingsSeam(e *apiEnv) {
	p, L := e.p, e.L
	b := L.NewTable()
	L.SetGlobal("B", b)
	result := func(L *lua.LState, err error) int {
		if err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		L.Push(lua.LTrue)
		return 1
	}
	e.set(b, "set", func(L *lua.LState) int {
		chord, err := checkChord(L.CheckString(1))
		if err == nil {
			err = p.bindings().set(pluginapi.Binding{Chord: chord, Action: L.CheckString(2)})
		}
		return result(L, err)
	})
	e.set(b, "fn", func(L *lua.LState) int {
		chord, err := checkChord(L.CheckString(1))
		if err != nil {
			return result(L, err)
		}
		id, err := p.hiddenCommandTitled(L.CheckFunction(2), "")
		if err == nil {
			err = p.bindings().set(pluginapi.Binding{Chord: chord, Action: id})
		}
		return result(L, err)
	})
	e.set(b, "del", func(L *lua.LState) int {
		L.Push(lua.LBool(p.bindings().del(L.CheckString(1))))
		return 1
	})
	e.set(b, "fill", func(L *lua.LState) int {
		bs := p.bindings()
		var err error
		for i := 1; i <= L.CheckInt(1) && err == nil; i++ {
			err = bs.put(pluginapi.Binding{Chord: "k" + strconv.Itoa(i), Action: "t.x"})
		}
		bs.publish()
		return result(L, err)
	})
	e.set(b, "check", func(L *lua.LState) int {
		chord, err := checkChord(L.CheckString(1))
		if err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		L.Push(lua.LString(chord))
		return 1
	})
}
