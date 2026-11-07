package plugin

import (
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/luapat"
)

func installStringLib(p *plugin) {
	L := p.L
	str := L.GetGlobal("string").(*lua.LTable)
	str.RawSetString("rep", p.goFn(stringRep))
	str.RawSetString("byte", p.goFn(stringByte))
	format := p.goFn(stringFormat)
	str.RawSetString("format", format)
	p.format = format
	str.RawSetString("find", p.goFn(stringFind))
	str.RawSetString("match", p.goFn(stringMatch))
	str.RawSetString("gmatch", p.goFn(stringGMatch))
	str.RawSetString("gsub", p.goFn(stringGsub))
}

// ---------- rep, byte ----------

// stringRep is string.rep(s, n [, sep]). The size of the result is computed with division
// before anything is allocated.
func stringRep(L *lua.LState) int {
	s := L.CheckString(1)
	nf := float64(L.CheckNumber(2))
	sep := L.OptString(3, "")
	if !(nf >= 1) { // n <= 0 and NaN
		L.Push(lua.LString(""))
		return 1
	}
	unit := int64(len(s) + len(sep))
	if unit == 0 {
		L.Push(lua.LString(""))
		return 1
	}
	if nf >= maxExactInt {
		tooLarge(L)
	}
	n := int64(nf)
	// total = n*len(s) + (n-1)*len(sep) = n*unit - len(sep) <= MaxString
	if n > (MaxString+int64(len(sep)))/unit {
		tooLarge(L)
	}
	switch {
	case n == 1:
		L.Push(lua.LString(s))
	case sep == "":
		L.Push(lua.LString(strings.Repeat(s, int(n))))
	default:
		// (s..sep) n times, less the trailing separator
		r := strings.Repeat(s+sep, int(n))
		L.Push(lua.LString(r[:len(r)-len(sep)]))
	}
	return 1
}

// maxByteResults is the most values string.byte returns.
const maxByteResults = 64 << 10

func stringByte(L *lua.LState) int {
	s := L.CheckString(1)
	l := len(s)
	posi := posRelat(optI64(L, 2, 1), l)
	pose := posRelat(optI64(L, 3, posi), l)
	if posi <= 0 {
		posi = 1
	}
	if pose > int64(l) {
		pose = int64(l)
	}
	if posi > pose {
		return 0
	}
	n := pose - posi + 1
	if n > maxByteResults {
		L.RaiseError("string slice too long")
	}
	for i := posi - 1; i < pose; i++ {
		L.Push(lua.LNumber(s[i]))
	}
	return int(n)
}

// ---------- patterns (internal/luapat) ----------

// patOptions are the luapat options of a call made on thread L: the default step budget,
// the output cap, and cancellation by the slice context.
func patOptions(L *lua.LState) luapat.Options {
	return luapat.Options{MaxOut: MaxString, Cancel: cancelOf(L)}
}

// raisePat raises a luapat error as a Lua error. The messages are those of the reference
// implementation (PatternError) or of luapat itself ("pattern too complex", "resulting
// string too large", ...). A cancelled slice raises its cause.
func raisePat(L *lua.LState, err error) {
	L.RaiseError("%s", err.Error())
}

func pushCaps(L *lua.LState, caps []luapat.Capture) int {
	for _, c := range caps {
		if c.IsPos {
			L.Push(lua.LNumber(c.Pos))
		} else {
			L.Push(lua.LString(c.Str))
		}
	}
	return len(caps)
}

func stringFind(L *lua.LState) int {
	s, pat := L.CheckString(1), L.CheckString(2)
	init := optI64(L, 3, 1)
	plain := L.Get(4) != lua.LNil && L.Get(4) != lua.LFalse
	res, ok, err := luapat.Find(s, pat, int(init), plain, patOptions(L))
	if err != nil {
		raisePat(L, err)
	}
	if !ok {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LNumber(res.Start + 1))
	L.Push(lua.LNumber(res.End))
	return 2 + pushCaps(L, res.Caps)
}

func stringMatch(L *lua.LState) int {
	s, pat := L.CheckString(1), L.CheckString(2)
	init := optI64(L, 3, 1)
	res, ok, err := luapat.Match(s, pat, int(init), patOptions(L))
	if err != nil {
		raisePat(L, err)
	}
	if !ok {
		L.Push(lua.LNil)
		return 1
	}
	return pushCaps(L, res.Caps)
}

func stringGMatch(L *lua.LState) int {
	s, pat := L.CheckString(1), L.CheckString(2)
	// The iterator may be called from another coroutine or slice: cancellation follows
	// the thread of the current call.
	cur := L
	o := luapat.Options{MaxOut: MaxString, Cancel: func() error { return ctxErr(cur) }}
	it, err := luapat.GMatch(s, pat, o)
	if err != nil {
		raisePat(L, err)
	}
	L.Push(L.NewFunction(func(L *lua.LState) int {
		cur = L
		res, ok, err := it.Next()
		if err != nil {
			raisePat(L, err)
		}
		if !ok {
			L.Push(lua.LNil)
			return 1
		}
		return pushCaps(L, res.Caps)
	}))
	return 1
}

// replacementValue turns the value of a gsub table or function replacement into text.
func replacementValue(L *lua.LState, v lua.LValue) (string, bool) {
	switch x := v.(type) {
	case lua.LString:
		return string(x), true
	case lua.LNumber:
		return x.String(), true
	case lua.LBool:
		if !bool(x) {
			return "", false
		}
	case *lua.LNilType:
		return "", false
	}
	L.RaiseError("invalid replacement value (a %s)", v.Type().String())
	return "", false
}

func stringGsub(L *lua.LState) int {
	s, pat := L.CheckString(1), L.CheckString(2)
	maxN := luapat.Unlimited
	if L.Get(4) != lua.LNil {
		maxN = int(checkI64(L, 4))
	}
	var repl luapat.Replacement
	switch r := L.Get(3).(type) {
	case lua.LString:
		repl.Template = string(r)
	case lua.LNumber:
		repl.Template = r.String()
	case *lua.LTable:
		repl.Func = func(m luapat.Result) (string, bool, error) {
			key := lua.LValue(lua.LNil)
			if len(m.Caps) > 0 {
				key = capValue(m.Caps[0])
			}
			v := L.GetTable(r, key)
			text, ok := replacementValue(L, v)
			return text, ok, nil
		}
	case *lua.LFunction:
		repl.Func = func(m luapat.Result) (string, bool, error) {
			L.Push(r)
			for _, c := range m.Caps {
				L.Push(capValue(c))
			}
			L.Call(len(m.Caps), 1)
			v := L.Get(-1)
			L.Pop(1)
			text, ok := replacementValue(L, v)
			return text, ok, nil
		}
	default:
		L.ArgError(3, "string/function/table expected")
	}
	out, n, err := luapat.Gsub(s, pat, repl, maxN, patOptions(L))
	if err != nil {
		raisePat(L, err)
	}
	L.Push(lua.LString(out))
	L.Push(lua.LNumber(n))
	return 2
}

func capValue(c luapat.Capture) lua.LValue {
	if c.IsPos {
		return lua.LNumber(c.Pos)
	}
	return lua.LString(c.Str)
}
