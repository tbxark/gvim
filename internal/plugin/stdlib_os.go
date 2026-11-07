package plugin

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// clockBase is the origin of os.clock.
var clockBase = time.Now()

// installOSLib installs the reduced os table: time, clock, date, difftime. There is no
// execute, remove, rename, tmpname, exit, getenv or setenv. Time comes from the runtime
// clock (Options.Now) so tests can drive it.
func installOSLib(p *plugin) {
	L := p.L
	t := L.NewTable()
	t.RawSetString("time", p.goFn(func(L *lua.LState) int { return p.osTime(L) }))
	t.RawSetString("clock", p.goFn(func(L *lua.LState) int {
		L.Push(lua.LNumber(time.Since(clockBase).Seconds()))
		return 1
	}))
	t.RawSetString("date", p.goFn(func(L *lua.LState) int { return p.osDate(L) }))
	t.RawSetString("difftime", p.goFn(func(L *lua.LState) int {
		L.Push(lua.LNumber(float64(L.CheckNumber(1)) - float64(L.OptNumber(2, 0))))
		return 1
	}))
	L.SetGlobal("os", t)
}

// dateField reads an integer field of an os.time table.
func dateField(L *lua.LState, t *lua.LTable, key string, def int, required bool) int {
	v := t.RawGetString(key)
	if v == lua.LNil {
		if required {
			L.RaiseError("field '%s' missing in date table", key)
		}
		return def
	}
	n, ok := v.(lua.LNumber)
	if !ok {
		if s, isStr := v.(lua.LString); isStr {
			if f, ok := parseLuaNumber(string(s)); ok {
				return clampInt(f)
			}
		}
		L.RaiseError("field '%s' is not an integer", key)
	}
	return clampInt(float64(n))
}

func (p *plugin) osTime(L *lua.LState) int {
	if L.Get(1) == lua.LNil {
		L.Push(lua.LNumber(p.r.now().Unix()))
		return 1
	}
	t := L.CheckTable(1)
	tm := time.Date(
		dateField(L, t, "year", 0, true), time.Month(dateField(L, t, "month", 0, true)), dateField(L, t, "day", 0, true),
		dateField(L, t, "hour", 12, false), dateField(L, t, "min", 0, false), dateField(L, t, "sec", 0, false),
		0, time.Local)
	setDateFields(t, tm)
	L.Push(lua.LNumber(tm.Unix()))
	return 1
}

func setDateFields(t *lua.LTable, tm time.Time) {
	t.RawSetString("year", lua.LNumber(tm.Year()))
	t.RawSetString("month", lua.LNumber(int(tm.Month())))
	t.RawSetString("day", lua.LNumber(tm.Day()))
	t.RawSetString("hour", lua.LNumber(tm.Hour()))
	t.RawSetString("min", lua.LNumber(tm.Minute()))
	t.RawSetString("sec", lua.LNumber(tm.Second()))
	t.RawSetString("wday", lua.LNumber(int(tm.Weekday())+1))
	t.RawSetString("yday", lua.LNumber(tm.YearDay()))
	t.RawSetString("isdst", lua.LBool(tm.IsDST()))
}

// osDate is os.date([format [, time]]): "*t" and "!*t" give a table, a leading "!" means
// UTC. The format is capped at MaxDateFormat bytes.
func (p *plugin) osDate(L *lua.LState) int {
	format := L.OptString(1, "%c")
	tm := p.r.now()
	if L.Get(2) != lua.LNil {
		tm = time.Unix(int64(clampF(float64(L.CheckNumber(2)))), 0)
	}
	if len(format) > MaxDateFormat {
		L.RaiseError("date format too long (max %d bytes)", MaxDateFormat)
	}
	if strings.HasPrefix(format, "!") {
		tm = tm.UTC()
		format = format[1:]
	} else {
		tm = tm.Local()
	}
	if strings.HasPrefix(format, "*t") {
		t := L.CreateTable(0, 9)
		setDateFields(t, tm)
		L.Push(t)
		return 1
	}
	out, err := strftime(tm, format)
	if err != nil {
		L.RaiseError("%s", err.Error())
	}
	L.Push(lua.LString(out))
	return 1
}

// clampF limits a time value to what time.Unix handles well (+-2^52 seconds).
func clampF(f float64) float64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f > 1<<52:
		return 1 << 52
	case f < -(1 << 52):
		return -(1 << 52)
	}
	return f
}

var (
	weekdayNames = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	monthNames   = [...]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
)

// strftime formats tm like C's strftime in the "C" locale. Every directive appends at
// most 32 bytes. An unknown directive is an error.
func strftime(tm time.Time, format string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			sb.WriteByte(c)
			continue
		}
		i++
		if i >= len(format) {
			return "", fmt.Errorf("invalid conversion specifier '%%'")
		}
		d := format[i]
		switch d {
		case '%':
			sb.WriteByte('%')
		case 'a':
			sb.WriteString(weekdayNames[tm.Weekday()][:3])
		case 'A':
			sb.WriteString(weekdayNames[tm.Weekday()])
		case 'b', 'h':
			sb.WriteString(monthNames[tm.Month()-1][:3])
		case 'B':
			sb.WriteString(monthNames[tm.Month()-1])
		case 'c':
			s, _ := strftime(tm, "%a %b %e %H:%M:%S %Y")
			sb.WriteString(s)
		case 'C':
			sb.WriteString(pad2(tm.Year() / 100))
		case 'd':
			sb.WriteString(pad2(tm.Day()))
		case 'D', 'x':
			s, _ := strftime(tm, "%m/%d/%y")
			sb.WriteString(s)
		case 'e':
			fmt.Fprintf(&sb, "%2d", tm.Day())
		case 'F':
			s, _ := strftime(tm, "%Y-%m-%d")
			sb.WriteString(s)
		case 'H':
			sb.WriteString(pad2(tm.Hour()))
		case 'I':
			sb.WriteString(pad2((tm.Hour()+11)%12 + 1))
		case 'j':
			fmt.Fprintf(&sb, "%03d", tm.YearDay())
		case 'm':
			sb.WriteString(pad2(int(tm.Month())))
		case 'M':
			sb.WriteString(pad2(tm.Minute()))
		case 'n':
			sb.WriteByte('\n')
		case 'p':
			if tm.Hour() < 12 {
				sb.WriteString("AM")
			} else {
				sb.WriteString("PM")
			}
		case 'r':
			s, _ := strftime(tm, "%I:%M:%S %p")
			sb.WriteString(s)
		case 'R':
			s, _ := strftime(tm, "%H:%M")
			sb.WriteString(s)
		case 'S':
			sb.WriteString(pad2(tm.Second()))
		case 't':
			sb.WriteByte('\t')
		case 'T', 'X':
			s, _ := strftime(tm, "%H:%M:%S")
			sb.WriteString(s)
		case 'u':
			wd := int(tm.Weekday())
			if wd == 0 {
				wd = 7
			}
			sb.WriteString(strconv.Itoa(wd))
		case 'U':
			sb.WriteString(pad2((tm.YearDay() - 1 + 7 - int(tm.Weekday())) / 7))
		case 'w':
			sb.WriteString(strconv.Itoa(int(tm.Weekday())))
		case 'W':
			sb.WriteString(pad2((tm.YearDay() - 1 + 7 - (int(tm.Weekday())+6)%7) / 7))
		case 'y':
			sb.WriteString(pad2(((tm.Year() % 100) + 100) % 100))
		case 'Y':
			sb.WriteString(strconv.Itoa(tm.Year()))
		case 'z':
			sb.WriteString(tm.Format("-0700"))
		case 'Z':
			name, _ := tm.Zone()
			sb.WriteString(name)
		default:
			return "", fmt.Errorf("invalid conversion specifier '%%%c'", d)
		}
	}
	return sb.String(), nil
}

func pad2(n int) string {
	if n >= 0 && n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
