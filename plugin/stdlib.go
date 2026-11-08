package plugin

import (
	"context"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// installStdlib applies the repaired standard library to a fresh state (FINAL 8.1 step 4,
// AMENDMENTS C5/C6): tonumber, math.huge, table.concat/sort/unpack/pack/move,
// string.rep/format/byte, unpack, a reduced os, and string.find/match/gmatch/gsub from
// plugin/pattern (step budget, cancel on the slice context, output cap).
//
// Every function that allocates in proportion to its arguments computes the final size
// before allocating and raises beyond the caps of limits.go (rule 8.1.R). Documented
// differences from stock Lua 5.1:
//   - tonumber accepts "1e2" and hex, and rejects "inf", "nan" and Go-only syntax;
//   - string.format supports %d %i %u %c %x %X %o %e %E %f %g %G %q %s %%, width and
//     precision up to 99, and raises on anything else;
//   - table.sort is a Go sort: unstable, never "invalid order function", capped at MaxSortN;
//   - os has time, clock, date and difftime only; time comes from the runtime clock.
//
// tostring is left as gopher-lua has it: it already honours __tostring.
func installStdlib(p *plugin) {
	installBaseLib(p)
	installTableLib(p)
	installStringLib(p)
	installOSLib(p)
}

// goFn makes a Lua function of f whose Go panics are recorded as plugin panics.
func (p *plugin) goFn(f lua.LGFunction) *lua.LFunction { return p.L.NewFunction(p.guarded(f)) }

// ---------- helpers ----------

// ctxErr returns the cancellation cause of the context of the running thread, or nil.
func ctxErr(L *lua.LState) error {
	ctx := L.Context()
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

// cancelOf is the luapat Options.Cancel of the thread L.
func cancelOf(L *lua.LState) func() error { return func() error { return ctxErr(L) } }

// raiseCtx raises the cancellation of the slice as a Lua error. The runtime classifies the
// failure from the context cause, so the text is only for tools.
func raiseCtx(L *lua.LState, err error) {
	L.RaiseError("%s", err.Error())
}

// clampI64 converts a Lua number to an integer within +-2^53 (NaN becomes 0).
func clampI64(f float64) int64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= maxExactInt:
		return maxExactInt
	case f <= -maxExactInt:
		return -maxExactInt
	}
	return int64(f)
}

func checkI64(L *lua.LState, n int) int64 { return clampI64(float64(L.CheckNumber(n))) }

func optI64(L *lua.LState, n int, def int64) int64 {
	if L.Get(n) == lua.LNil {
		return def
	}
	return checkI64(L, n)
}

// posRelat is lstrlib's posrelat: a negative position counts from the end.
func posRelat(pos int64, l int) int64 {
	switch {
	case pos > 0:
		return pos
	case pos == 0:
		return 1
	case pos < -int64(l):
		return 1
	}
	return int64(l) + pos + 1
}

// pollCtx raises when the slice is over, every 4096th call with the running counter i. Go
// loops that can run long call it so a deadline or a panic chord still cuts them.
func pollCtx(L *lua.LState, i int) {
	if i&0xfff == 0 {
		if err := ctxErr(L); err != nil {
			raiseCtx(L, err)
		}
	}
}

func tooLarge(L *lua.LState) { L.RaiseError("resulting string too large") }

// ---------- base ----------

func installBaseLib(p *plugin) {
	L := p.L
	L.SetGlobal("tonumber", p.goFn(luaToNumber))
	L.SetGlobal("unpack", p.goFn(luaUnpack))
	math := L.GetGlobal("math").(*lua.LTable)
	math.RawSetString("huge", lua.LNumber(posInf))
}

var posInf = math.Inf(1)

func isLuaSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

func trimLuaSpace(s string) string {
	for len(s) > 0 && isLuaSpace(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && isLuaSpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

// parseLuaNumber converts a string like Lua's lua_str2number: decimal with optional
// fraction and exponent, or 0x hexadecimal integer, surrounded by white space. Go-only
// syntax (underscores, "inf", "nan", hex floats) is rejected.
func parseLuaNumber(s string) (float64, bool) {
	s = trimLuaSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	body := s
	if body[0] == '+' || body[0] == '-' {
		neg = body[0] == '-'
		body = body[1:]
	}
	if len(body) > 2 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X') {
		v := 0.0
		for i := 2; i < len(body); i++ {
			d := digitVal(body[i])
			if d < 0 || d >= 16 {
				return 0, false
			}
			v = v*16 + float64(d)
		}
		if neg {
			v = -v
		}
		return v, true
	}
	// [0-9]* [. [0-9]*] [eE [+-] [0-9]+], at least one digit before the exponent.
	i, digits := 0, 0
	for i < len(body) && body[i] >= '0' && body[i] <= '9' {
		i++
		digits++
	}
	if i < len(body) && body[i] == '.' {
		i++
		for i < len(body) && body[i] >= '0' && body[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(body) && (body[i] == 'e' || body[i] == 'E') {
		i++
		if i < len(body) && (body[i] == '+' || body[i] == '-') {
			i++
		}
		exp := 0
		for i < len(body) && body[i] >= '0' && body[i] <= '9' {
			i++
			exp++
		}
		if exp == 0 {
			return 0, false
		}
	}
	if i != len(body) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}

func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

func luaToNumber(L *lua.LState) int {
	if L.GetTop() == 0 {
		L.ArgError(1, "value expected")
	}
	if L.Get(2) == lua.LNil {
		switch v := L.Get(1).(type) {
		case lua.LNumber:
			L.Push(v)
			return 1
		case lua.LString:
			if f, ok := parseLuaNumber(string(v)); ok {
				L.Push(lua.LNumber(f))
				return 1
			}
		}
		L.Push(lua.LNil)
		return 1
	}
	base := L.CheckInt(2)
	s := L.CheckString(1)
	if base < 2 || base > 36 {
		L.ArgError(2, "base out of range")
	}
	s = trimLuaSpace(s)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	}
	if s == "" {
		L.Push(lua.LNil)
		return 1
	}
	v := 0.0
	for i := 0; i < len(s); i++ {
		d := digitVal(s[i])
		if d < 0 || d >= base {
			L.Push(lua.LNil)
			return 1
		}
		v = v*float64(base) + float64(d)
	}
	if neg {
		v = -v
	}
	L.Push(lua.LNumber(v))
	return 1
}

// maxUnpack is the most values unpack returns.
const maxUnpack = 100_000

func luaUnpack(L *lua.LState) int {
	t := L.CheckTable(1)
	i := optI64(L, 2, 1)
	j := optI64(L, 3, int64(t.Len()))
	if i > j {
		return 0
	}
	if j-i >= maxUnpack {
		L.RaiseError("too many results to unpack")
	}
	n := int(j - i + 1)
	for k := 0; k < n; k++ {
		L.Push(t.RawGetInt(int(i) + k))
	}
	return n
}

// ---------- table ----------

func installTableLib(p *plugin) {
	L := p.L
	tab := L.GetGlobal("table").(*lua.LTable)
	tab.RawSetString("concat", p.goFn(tableConcat))
	tab.RawSetString("sort", p.goFn(tableSort))
	tab.RawSetString("pack", p.goFn(tablePack))
	tab.RawSetString("move", p.goFn(tableMove))
	tab.RawSetString("unpack", L.GetGlobal("unpack"))
}

// tableConcat is table.concat with a size check before the single allocation.
func tableConcat(L *lua.LState) int {
	t := L.CheckTable(1)
	sep := L.OptString(2, "")
	i := optI64(L, 3, 1)
	j := optI64(L, 4, int64(t.Len()))
	if i > j {
		L.Push(lua.LString(""))
		return 1
	}
	if j-i+1 > MaxString {
		tooLarge(L)
	}
	piece := func(k int64) string {
		switch v := t.RawGetInt(int(k)).(type) {
		case lua.LString:
			return string(v)
		case lua.LNumber:
			return v.String()
		}
		L.RaiseError("invalid value (at index %d) in table for 'concat'", k)
		return ""
	}
	// First pass: the exact size, before anything is allocated.
	total := (j - i) * int64(len(sep))
	if total > MaxString {
		tooLarge(L)
	}
	for k := i; k <= j; k++ {
		pollCtx(L, int(k))
		if total += int64(len(piece(k))); total > MaxString {
			tooLarge(L)
		}
	}
	var sb strings.Builder
	sb.Grow(int(total))
	for k := i; k <= j; k++ {
		pollCtx(L, int(k))
		if k > i {
			sb.WriteString(sep)
		}
		sb.WriteString(piece(k))
	}
	L.Push(lua.LString(sb.String()))
	return 1
}

// sortPollEvery is the number of default-comparator comparisons between context polls.
const sortPollEvery = 4096

// tableSort sorts t[1..#t] in Go. The elements are copied out, sorted and written back,
// so an error in the comparator leaves the table as it was.
func tableSort(L *lua.LState) int {
	t := L.CheckTable(1)
	var cmp *lua.LFunction
	if L.GetTop() >= 2 && L.Get(2) != lua.LNil {
		cmp = L.CheckFunction(2)
	}
	n := t.Len()
	if n > MaxSortN {
		L.RaiseError("table too large to sort")
	}
	vals := make([]lua.LValue, n)
	for i := range vals {
		vals[i] = t.RawGetInt(i + 1)
	}
	var less func(a, b lua.LValue) bool
	if cmp != nil {
		less = func(a, b lua.LValue) bool {
			L.Push(cmp)
			L.Push(a)
			L.Push(b)
			L.Call(2, 1)
			ok := lua.LVAsBool(L.Get(-1))
			L.Pop(1)
			return ok
		}
	} else {
		polls := 0
		less = func(a, b lua.LValue) bool {
			if polls++; polls%sortPollEvery == 0 {
				if err := ctxErr(L); err != nil {
					raiseCtx(L, err)
				}
			}
			if x, ok := a.(lua.LNumber); ok {
				if y, ok := b.(lua.LNumber); ok {
					return x < y
				}
			}
			if x, ok := a.(lua.LString); ok {
				if y, ok := b.(lua.LString); ok {
					return x < y
				}
			}
			return L.LessThan(a, b)
		}
	}
	sort.Slice(vals, func(i, j int) bool { return less(vals[i], vals[j]) })
	for i, v := range vals {
		t.RawSetInt(i+1, v)
	}
	return 0
}

func tablePack(L *lua.LState) int {
	n := L.GetTop()
	t := L.CreateTable(n, 1)
	for i := 1; i <= n; i++ {
		t.RawSetInt(i, L.Get(i))
	}
	t.RawSetString("n", lua.LNumber(n))
	L.Push(t)
	return 1
}

// maxMove is the most elements one table.move copies.
const maxMove = 2_000_000

// tableMove is table.move(a1, f, e, t [, a2]) of Lua 5.3, with raw access.
func tableMove(L *lua.LState) int {
	a1 := L.CheckTable(1)
	f, e, tpos := checkI64(L, 2), checkI64(L, 3), checkI64(L, 4)
	a2 := a1
	if L.Get(5) != lua.LNil {
		a2 = L.CheckTable(5)
	}
	if e >= f {
		if e-f+1 > maxMove {
			L.RaiseError("too many elements to move")
		}
		n := e - f + 1
		move := func(i int64) {
			pollCtx(L, int(i))
			a2.RawSetInt(int(tpos+i), a1.RawGetInt(int(f+i)))
		}
		if tpos > e || tpos <= f || a1 != a2 {
			for i := int64(0); i < n; i++ {
				move(i)
			}
		} else {
			for i := n - 1; i >= 0; i-- {
				move(i)
			}
		}
	}
	L.Push(a2)
	return 1
}
