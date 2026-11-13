package plugin

import (
	"math"
	"strings"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"
)

// utf8 (Lua 5.3 semantics, byte positions are 1-based) plus utf8.valid, utf8.sub,
// utf8.byte_to_char and utf8.char_to_byte; and the 32-bit bit library.

// maxUTF8Results is the most values utf8.codepoint and utf8.char handle.
const maxUTF8Results = 64 << 10

// uPosRelat is Lua 5.3's u_posrelat: negative positions count from the end, and one that
// falls before the start becomes 0 (not 1 as in string.sub).
func uPosRelat(pos int64, l int) int64 {
	switch {
	case pos >= 0:
		return pos
	case -pos > int64(l):
		return 0
	}
	return int64(l) + pos + 1
}

func isCont(c byte) bool { return c&0xC0 == 0x80 }

// decodeAt decodes the character at byte offset i of s; ok is false for an invalid
// sequence (including surrogates and values above U+10FFFF).
func decodeAt(s string, i int) (r rune, size int, ok bool) {
	r, size = utf8.DecodeRuneInString(s[i:])
	return r, size, r != utf8.RuneError || size > 1
}

func installUTF8(e *apiEnv) *lua.LTable {
	L := e.L
	t := L.CreateTable(0, 12)
	t.RawSetString("charpattern", lua.LString("[\x00-\x7F\xC2-\xFD][\x80-\xBF]*"))

	e.set(t, "len", func(L *lua.LState) int {
		s := L.CheckString(1)
		posi := uPosRelat(optI64(L, 2, 1), len(s))
		posj := uPosRelat(optI64(L, 3, -1), len(s))
		if posi < 1 || posi > int64(len(s))+1 {
			L.ArgError(2, "initial position out of bounds")
		}
		if posj > int64(len(s)) {
			L.ArgError(3, "final position out of bounds")
		}
		n := 0
		for i := int(posi) - 1; i < int(posj); {
			_, size, ok := decodeAt(s, i)
			if !ok {
				L.Push(lua.LNil)
				L.Push(lua.LNumber(i + 1))
				return 2
			}
			i += size
			n++
		}
		L.Push(lua.LNumber(n))
		return 1
	})
	e.set(t, "char", func(L *lua.LState) int {
		n := L.GetTop()
		if n > maxUTF8Results {
			L.RaiseError("too many arguments")
		}
		var sb strings.Builder
		for i := 1; i <= n; i++ {
			c := L.CheckNumber(i)
			if c < 0 || c > utf8.MaxRune || math.IsNaN(float64(c)) {
				L.ArgError(i, "value out of range")
			}
			sb.WriteRune(rune(c))
		}
		L.Push(lua.LString(sb.String()))
		return 1
	})
	e.set(t, "codepoint", func(L *lua.LState) int {
		s := L.CheckString(1)
		posi := uPosRelat(optI64(L, 2, 1), len(s))
		pose := uPosRelat(optI64(L, 3, posi), len(s))
		if posi < 1 {
			L.ArgError(2, "out of range")
		}
		if pose > int64(len(s)) {
			L.ArgError(3, "out of range")
		}
		if posi > pose {
			return 0
		}
		count := 0
		for i := int(posi) - 1; i < int(pose); {
			r, size, ok := decodeAt(s, i)
			if !ok {
				L.RaiseError("invalid UTF-8 code")
			}
			if count++; count > maxUTF8Results {
				L.RaiseError("string slice too long")
			}
			L.Push(lua.LNumber(r))
			i += size
		}
		return count
	})
	e.set(t, "offset", func(L *lua.LState) int {
		s := L.CheckString(1)
		n := checkI64(L, 2)
		def := int64(1)
		if n <= 0 { // 5.3: i defaults to 1 for a positive n, else to #s + 1
			def = int64(len(s)) + 1
		}
		posi := uPosRelat(optI64(L, 3, def), len(s)) - 1 // 0-based
		if posi < 0 || posi > int64(len(s)) {
			L.ArgError(3, "position out of bounds")
		}
		i := int(posi)
		cont := func(k int) bool { return k < len(s) && isCont(s[k]) }
		if n == 0 {
			for i > 0 && cont(i) {
				i--
			}
			L.Push(lua.LNumber(i + 1))
			return 1
		}
		if cont(i) {
			L.RaiseError("initial position is a continuation byte")
		}
		if n < 0 {
			for n < 0 && i > 0 {
				i--
				for i > 0 && cont(i) {
					i--
				}
				n++
			}
		} else {
			n--
			for n > 0 && i < len(s) {
				i++
				for cont(i) {
					i++
				}
				n--
			}
		}
		if n == 0 {
			L.Push(lua.LNumber(i + 1))
		} else {
			L.Push(lua.LNil)
		}
		return 1
	})
	e.set(t, "codes", func(L *lua.LState) int {
		s := L.CheckString(1)
		L.Push(L.NewFunction(func(L *lua.LState) int {
			i := int(optI64(L, 2, 0)) // byte position of the previous character, 0 at the start
			if i < 0 || i > len(s) {
				return 0
			}
			if i > 0 {
				i-- // 0-based start of the previous character
				_, size, _ := decodeAt(s, i)
				i += max(size, 1)
			}
			if i >= len(s) {
				L.Push(lua.LNil)
				return 1
			}
			r, _, ok := decodeAt(s, i)
			if !ok {
				L.RaiseError("invalid UTF-8 code")
			}
			L.Push(lua.LNumber(i + 1))
			L.Push(lua.LNumber(r))
			return 2
		}))
		L.Push(lua.LString(s))
		L.Push(lua.LNumber(0))
		return 3
	})
	e.set(t, "valid", func(L *lua.LState) int {
		L.Push(lua.LBool(utf8.ValidString(L.CheckString(1))))
		return 1
	})
	e.set(t, "sub", func(L *lua.LState) int {
		s := L.CheckString(1)
		n := utf8.RuneCountInString(s)
		i := posRelat(checkI64(L, 2), n)
		j := optI64(L, 3, -1)
		switch {
		case j < 0:
			j = max(int64(n)+j+1, 0)
		case j > int64(n):
			j = int64(n)
		}
		if i > j {
			L.Push(lua.LString(""))
			return 1
		}
		L.Push(lua.LString(runeSlice(s, int(i)-1, int(j))))
		return 1
	})
	e.set(t, "byte_to_char", func(L *lua.LState) int {
		s := L.CheckString(1)
		b := checkI64(L, 2)
		if b < 1 || b > int64(len(s)) {
			L.Push(lua.LNil)
			return 1
		}
		n := 0
		for i := 0; i < len(s); {
			_, size := utf8.DecodeRuneInString(s[i:])
			n++
			i += size
			if int64(i) >= b {
				break
			}
		}
		L.Push(lua.LNumber(n))
		return 1
	})
	e.set(t, "char_to_byte", func(L *lua.LState) int {
		s := L.CheckString(1)
		c := checkI64(L, 2)
		if c < 1 {
			L.Push(lua.LNil)
			return 1
		}
		i := 0
		for k := int64(1); k < c && i < len(s); k++ {
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		}
		if i >= len(s) {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(i + 1))
		return 1
	})
	return t
}

// runeSlice returns the characters [from, to) of s (0-based, clamped).
func runeSlice(s string, from, to int) string {
	start, n := len(s), 0
	for i := range s {
		if n == from {
			start = i
		}
		if n == to {
			return s[start:i]
		}
		n++
	}
	if from >= n {
		return ""
	}
	return s[start:]
}

// ---------- bit ----------

// toBit converts a Lua number to a 32-bit integer modulo 2^32, like LuaJIT's bit library.
func toBit(f float64) int32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int32(uint32(int64(math.Mod(math.Trunc(f), 1<<32))))
}

func installBit(e *apiEnv) *lua.LTable {
	t := e.L.CreateTable(0, 6)
	fold := func(name string, op func(a, b int32) int32) {
		e.set(t, name, func(L *lua.LState) int {
			acc := toBit(float64(L.CheckNumber(1)))
			for i := 2; i <= L.GetTop(); i++ {
				acc = op(acc, toBit(float64(L.CheckNumber(i))))
			}
			L.Push(lua.LNumber(acc))
			return 1
		})
	}
	fold("band", func(a, b int32) int32 { return a & b })
	fold("bor", func(a, b int32) int32 { return a | b })
	fold("bxor", func(a, b int32) int32 { return a ^ b })
	e.set(t, "bnot", func(L *lua.LState) int {
		L.Push(lua.LNumber(^toBit(float64(L.CheckNumber(1)))))
		return 1
	})
	shift := func(name string, op func(x int32, n uint) int32) {
		e.set(t, name, func(L *lua.LState) int {
			x := toBit(float64(L.CheckNumber(1)))
			n := uint(toBit(float64(L.CheckNumber(2))) & 31)
			L.Push(lua.LNumber(op(x, n)))
			return 1
		})
	}
	shift("lshift", func(x int32, n uint) int32 { return int32(uint32(x) << n) })
	shift("rshift", func(x int32, n uint) int32 { return int32(uint32(x) >> n) })
	return t
}
