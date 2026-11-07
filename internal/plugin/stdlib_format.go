package plugin

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// stringFormat is string.format over a validated subset of C's printf: %d %i %u %c %x %X
// %o %e %E %f %g %G %q %s %%, flags "-+ #0", width and precision of at most two digits.
// Anything else raises, like the reference implementation. %s honours __tostring. The
// result is capped at MaxFormat, checked after every directive.
func stringFormat(L *lua.LState) int {
	f := L.CheckString(1)
	var sb strings.Builder
	arg := 1
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c != '%' {
			sb.WriteByte(c)
			if sb.Len() > MaxFormat {
				tooLarge(L)
			}
			continue
		}
		i++
		if i >= len(f) {
			L.RaiseError("invalid option '%%' to 'format'")
		}
		if f[i] == '%' {
			sb.WriteByte('%')
			continue
		}
		// flags, width, precision
		start := i
		for i < len(f) && strings.IndexByte("-+ #0", f[i]) >= 0 {
			i++
		}
		flags := f[start:i]
		wstart := i
		for i < len(f) && f[i] >= '0' && f[i] <= '9' {
			i++
		}
		width := f[wstart:i]
		prec, hasPrec := "", false
		if i < len(f) && f[i] == '.' {
			i++
			hasPrec = true
			pstart := i
			for i < len(f) && f[i] >= '0' && f[i] <= '9' {
				i++
			}
			prec = f[pstart:i]
		}
		if len(width) > 2 || len(prec) > 2 {
			L.RaiseError("invalid format (width or precision too long)")
		}
		if i >= len(f) {
			L.RaiseError("invalid option '%%%s' to 'format'", f[start:])
		}
		conv := f[i]
		arg++
		w, _ := strconv.Atoi(width)
		pr, _ := strconv.Atoi(prec)
		left := strings.Contains(flags, "-")
		switch conv {
		case 'd', 'i':
			n := fmtInt(L, arg)
			fmt.Fprintf(&sb, intSpec(flags, width, prec, hasPrec)+"d", n)
		case 'u', 'x', 'X', 'o':
			n := uint64(fmtInt(L, arg))
			verb := string(conv)
			if conv == 'u' {
				verb = "d"
			}
			fmt.Fprintf(&sb, intSpec(flags, width, prec, hasPrec)+verb, n)
		case 'c':
			n := fmtInt(L, arg)
			sb.WriteString(padBytes(string([]byte{byte(n)}), w, left))
		case 'e', 'E', 'f', 'g', 'G':
			x := float64(L.CheckNumber(arg))
			if math.IsInf(x, 0) || math.IsNaN(x) {
				sb.WriteString(padBytes(nonFinite(x, flags, conv), w, left))
				break
			}
			spec := "%" + flags + width
			switch {
			case hasPrec:
				spec += "." + strconv.Itoa(pr)
			case conv == 'g' || conv == 'G':
				spec += ".6" // C's default; Go's would be the shortest representation
			}
			fmt.Fprintf(&sb, spec+string(conv), x)
		case 'q':
			q := L.CheckString(arg)
			if len(q) > MaxFormat { // the quoted form is never shorter
				tooLarge(L)
			}
			sb.WriteString(quoteLua(q))
		case 's':
			if arg > L.GetTop() {
				L.ArgError(arg, "no value")
			}
			s := L.ToStringMeta(L.Get(arg)).String()
			if len(s) > MaxFormat {
				tooLarge(L)
			}
			if hasPrec && pr < len(s) {
				s = s[:pr]
			}
			sb.WriteString(padBytes(s, w, left))
		default:
			L.RaiseError("invalid option '%%%c' to 'format'", conv)
		}
		if sb.Len() > MaxFormat {
			tooLarge(L)
		}
	}
	L.Push(lua.LString(sb.String()))
	return 1
}

func intSpec(flags, width, prec string, hasPrec bool) string {
	spec := "%" + flags + width
	if hasPrec {
		if prec == "" {
			prec = "0"
		}
		spec += "." + prec
	}
	return spec
}

// fmtInt reads argument n as an integer, truncating like a C cast (out-of-range values
// are clamped).
func fmtInt(L *lua.LState, n int) int64 {
	x := float64(L.CheckNumber(n))
	switch {
	case math.IsNaN(x):
		return 0
	case x >= math.MaxInt64:
		return math.MaxInt64
	case x <= math.MinInt64:
		return math.MinInt64
	}
	return int64(x)
}

func nonFinite(x float64, flags string, conv byte) string {
	s := "nan"
	switch {
	case math.IsInf(x, 1):
		s = "inf"
		if strings.Contains(flags, "+") {
			s = "+inf"
		} else if strings.Contains(flags, " ") {
			s = " inf"
		}
	case math.IsInf(x, -1):
		s = "-inf"
	}
	if conv == 'E' || conv == 'G' {
		s = strings.ToUpper(s)
	}
	return s
}

// padBytes pads s with spaces to w bytes (C counts bytes, not runes).
func padBytes(s string, w int, left bool) string {
	if len(s) >= w {
		return s
	}
	pad := strings.Repeat(" ", w-len(s))
	if left {
		return s + pad
	}
	return pad + s
}

// quoteLua is Lua 5.1's %q: a double-quoted string in which ", \ and newline are escaped
// with a backslash, \r as \r and NUL as \000.
func quoteLua(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\', '\n':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case '\r':
			sb.WriteString(`\r`)
		case 0:
			sb.WriteString(`\000`)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
