package plugin

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"
)

// json: encode, decode, decode_jsonc, null, array.
//
//   - Arrays are tables with keys 1..n, objects tables with string keys; a mixed or sparse
//     table raises. An empty table is the object {}; json.array(t) marks a table as an
//     array (it then encodes as [], holes become null).
//   - json null decodes to json.null (a sentinel), so arrays stay contiguous.
//   - Caps: input and output at most MaxString bytes, MaxJSONNodes nodes,
//     MaxJSONDepth levels, checked while parsing, before the tables are built.

func installJSON(e *apiEnv) *lua.LTable {
	p, L := e.p, e.L
	d := &p.data
	d.null = L.NewUserData()
	nullMT := L.CreateTable(0, 2)
	nullMT.RawSetString("__tostring", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString("json.null"))
		return 1
	}))
	nullMT.RawSetString("__metatable", lua.LString("locked"))
	L.SetMetatable(d.null, nullMT)
	d.arrayMT = L.CreateTable(0, 1)
	d.arrayMT.RawSetString("__metatable", lua.LString("json.array"))

	t := L.CreateTable(0, 5)
	t.RawSetString("null", d.null)
	e.set(t, "array", func(L *lua.LState) int {
		tb := L.OptTable(1, nil)
		if tb == nil {
			tb = L.NewTable()
		}
		if mt := tb.Metatable; mt != lua.LNil && mt != lua.LValue(d.arrayMT) {
			L.ArgError(1, "table already has a metatable")
		}
		L.SetMetatable(tb, d.arrayMT)
		L.Push(tb)
		return 1
	})
	e.set(t, "encode", func(L *lua.LState) int {
		enc := &jsonEnc{L: L, d: d, seen: map[*lua.LTable]bool{}}
		if o := L.OptTable(2, nil); o != nil {
			switch n := o.RawGetString("indent").(type) {
			case lua.LNumber:
				enc.indent = strings.Repeat(" ", max(0, min(16, clampInt(float64(n)))))
			case *lua.LNilType:
			default:
				L.ArgError(2, "indent must be a number")
			}
			enc.sortKeys = lua.LVAsBool(o.RawGetString("sort_keys"))
		}
		enc.value(L.CheckAny(1), 0)
		L.Push(lua.LString(enc.sb.String()))
		return 1
	})
	decode := func(forceJSONC bool) lua.LGFunction {
		return func(L *lua.LState) int {
			s := L.CheckString(1)
			jsonc := forceJSONC
			if !jsonc {
				if o := L.OptTable(2, nil); o != nil {
					jsonc = lua.LVAsBool(o.RawGetString("jsonc"))
				}
			}
			v, err := decodeJSON(L, d, s, jsonc)
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(err.Error()))
				return 2
			}
			L.Push(v)
			return 1
		}
	}
	e.set(t, "decode", decode(false))
	e.set(t, "decode_jsonc", decode(true))
	return t
}

// ---------- encode ----------

type jsonEnc struct {
	L        *lua.LState
	d        *dataState
	sb       strings.Builder
	indent   string
	sortKeys bool
	nodes    int
	seen     map[*lua.LTable]bool
}

func (e *jsonEnc) fail(format string, args ...any) {
	e.L.RaiseError("%s", fmt.Sprintf(format, args...))
}

func (e *jsonEnc) check() {
	if e.sb.Len() > MaxString {
		tooLarge(e.L)
	}
}

func (e *jsonEnc) newline(depth int) {
	if e.indent == "" {
		return
	}
	e.sb.WriteByte('\n')
	for i := 0; i < depth; i++ {
		e.sb.WriteString(e.indent)
	}
	e.check()
}

func (e *jsonEnc) value(v lua.LValue, depth int) {
	if e.nodes++; e.nodes > MaxJSONNodes {
		e.fail("json: too many nodes")
	}
	pollCtx(e.L, e.nodes)
	switch x := v.(type) {
	case *lua.LNilType:
		e.sb.WriteString("null")
	case lua.LBool:
		e.sb.WriteString(strconv.FormatBool(bool(x)))
	case lua.LNumber:
		f := float64(x)
		switch {
		case math.IsNaN(f) || math.IsInf(f, 0):
			e.fail("json: cannot encode NaN or Infinity")
		case f == math.Trunc(f) && math.Abs(f) <= maxExactInt:
			e.sb.WriteString(strconv.FormatInt(int64(f), 10))
		default:
			e.sb.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
		}
	case lua.LString:
		e.str(string(x))
	case *lua.LUserData:
		if x != e.d.null {
			e.fail("json: cannot encode userdata")
		}
		e.sb.WriteString("null")
	case *lua.LTable:
		e.table(x, depth)
	default:
		e.fail("json: cannot encode %s", v.Type().String())
	}
	e.check()
}

const hexDigits = "0123456789abcdef"

func (e *jsonEnc) str(s string) {
	e.sb.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			e.sb.WriteString(s[start:i])
			switch c {
			case '"', '\\':
				e.sb.WriteByte('\\')
				e.sb.WriteByte(c)
			case '\n':
				e.sb.WriteString(`\n`)
			case '\r':
				e.sb.WriteString(`\r`)
			case '\t':
				e.sb.WriteString(`\t`)
			default:
				e.sb.WriteString(`\u00`)
				e.sb.WriteByte(hexDigits[c>>4])
				e.sb.WriteByte(hexDigits[c&0xf])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			e.sb.WriteString(s[start:i])
			e.sb.WriteString("\uFFFD") // the replacement character, as UTF-8
			i += size
			start = i
			continue
		}
		i += size
	}
	e.sb.WriteString(s[start:])
	e.sb.WriteByte('"')
}

type jsonPair struct {
	k string
	v lua.LValue
}

func (e *jsonEnc) table(t *lua.LTable, depth int) {
	if depth >= MaxJSONDepth {
		e.fail("json: too deep")
	}
	if e.seen[t] {
		e.fail("json: circular reference")
	}
	e.seen[t] = true
	defer delete(e.seen, t)

	marked := t.Metatable == lua.LValue(e.d.arrayMT)
	n := t.Len()
	var pairs []jsonPair
	ints, strs := 0, 0
	for k, v := t.Next(lua.LNil); k != lua.LNil; k, v = t.Next(k) {
		switch kk := k.(type) {
		case lua.LString:
			strs++
			pairs = append(pairs, jsonPair{string(kk), v})
		case lua.LNumber:
			f := float64(kk)
			if f != math.Trunc(f) || f < 1 {
				e.fail("json: unsupported table key %s", kk.String())
			}
			ints++
		default:
			e.fail("json: unsupported table key type %s", k.Type().String())
		}
	}
	switch {
	case marked:
		if strs > 0 {
			e.fail("json: array table has string keys")
		}
		e.array(t, n, depth)
	case ints > 0 && strs > 0:
		e.fail("json: mixed array and object table")
	case ints > 0:
		if ints != n {
			e.fail("json: sparse array (use json.array and json.null for holes)")
		}
		e.array(t, n, depth)
	default:
		e.object(pairs, depth)
	}
}

func (e *jsonEnc) array(t *lua.LTable, n, depth int) {
	e.sb.WriteByte('[')
	for i := 1; i <= n; i++ {
		if i > 1 {
			e.sb.WriteByte(',')
		}
		e.newline(depth + 1)
		e.value(t.RawGetInt(i), depth+1)
	}
	if n > 0 {
		e.newline(depth)
	}
	e.sb.WriteByte(']')
}

func (e *jsonEnc) object(pairs []jsonPair, depth int) {
	if e.sortKeys {
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	}
	e.sb.WriteByte('{')
	for i, pr := range pairs {
		if i > 0 {
			e.sb.WriteByte(',')
		}
		e.newline(depth + 1)
		e.str(pr.k)
		e.sb.WriteByte(':')
		if e.indent != "" {
			e.sb.WriteByte(' ')
		}
		e.value(pr.v, depth+1)
	}
	if len(pairs) > 0 {
		e.newline(depth)
	}
	e.sb.WriteByte('}')
}

// ---------- decode ----------

type jsonParser struct {
	L     *lua.LState
	d     *dataState
	s     string
	i     int
	jsonc bool
	nodes int
}

// jsonErr is a syntax error at a byte offset, or (pos < 0) a cap error with a fixed text.
type jsonErr struct {
	pos int
	msg string
	src string
}

func (e *jsonErr) Error() string {
	if e.pos < 0 {
		return e.msg
	}
	line, col := 1, 1
	for i := 0; i < e.pos && i < len(e.src); i++ {
		if e.src[i] == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return fmt.Sprintf("%d:%d: %s", line, col, e.msg)
}

func decodeJSON(L *lua.LState, d *dataState, s string, jsonc bool) (v lua.LValue, err error) {
	if len(s) > MaxString {
		return nil, limitErr("json: input larger than %d bytes", MaxString)
	}
	p := &jsonParser{L: L, d: d, s: s, jsonc: jsonc}
	defer func() {
		if rec := recover(); rec != nil {
			je, ok := rec.(*jsonErr)
			if !ok {
				panic(rec)
			}
			v, err = nil, je
		}
	}()
	if strings.HasPrefix(s, "\xef\xbb\xbf") {
		p.i = 3
	}
	p.ws()
	v = p.value(0)
	p.ws()
	if p.i < len(p.s) {
		p.fail("unexpected character after the value")
	}
	return v, nil
}

func (p *jsonParser) fail(format string, args ...any) {
	panic(&jsonErr{pos: p.i, msg: fmt.Sprintf(format, args...), src: p.s})
}

func (p *jsonParser) ws() {
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.i++
		case p.jsonc && c == '/' && p.i+1 < len(p.s) && p.s[p.i+1] == '/':
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		case p.jsonc && c == '/' && p.i+1 < len(p.s) && p.s[p.i+1] == '*':
			end := strings.Index(p.s[p.i+2:], "*/")
			if end < 0 {
				p.i = len(p.s)
				p.fail("unterminated comment")
			}
			p.i += end + 4
		default:
			return
		}
	}
}

func (p *jsonParser) count() {
	if p.nodes++; p.nodes > MaxJSONNodes {
		panic(&jsonErr{pos: -1, msg: "json: too many nodes"})
	}
	pollCtx(p.L, p.nodes)
}

func (p *jsonParser) value(depth int) lua.LValue {
	p.count()
	if p.i >= len(p.s) {
		p.fail("unexpected end of input")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return lua.LString(p.str())
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case strings.HasPrefix(p.s[p.i:], "true"):
		p.i += 4
		return lua.LTrue
	case strings.HasPrefix(p.s[p.i:], "false"):
		p.i += 5
		return lua.LFalse
	case strings.HasPrefix(p.s[p.i:], "null"):
		p.i += 4
		return p.d.null
	default:
		p.fail("unexpected character %q", string(rune(c)))
	}
	return lua.LNil
}

func (p *jsonParser) array(depth int) lua.LValue {
	if depth >= MaxJSONDepth {
		panic(&jsonErr{pos: -1, msg: "json: too deep"})
	}
	p.i++ // [
	t := p.L.CreateTable(0, 0)
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		p.L.SetMetatable(t, p.d.arrayMT)
		return t
	}
	for {
		t.Append(p.value(depth + 1))
		p.ws()
		if p.i >= len(p.s) {
			p.fail("unexpected end of input in array")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
			p.ws()
			if p.jsonc && p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				return t
			}
		case ']':
			p.i++
			return t
		default:
			p.fail("expected ',' or ']'")
		}
	}
}

func (p *jsonParser) object(depth int) lua.LValue {
	if depth >= MaxJSONDepth {
		panic(&jsonErr{pos: -1, msg: "json: too deep"})
	}
	p.i++ // {
	t := p.L.CreateTable(0, 4)
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return t
	}
	for {
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			p.fail("expected a string key")
		}
		p.count()
		key := p.str()
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			p.fail("expected ':'")
		}
		p.i++
		p.ws()
		t.RawSetString(key, p.value(depth+1))
		p.ws()
		if p.i >= len(p.s) {
			p.fail("unexpected end of input in object")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
			p.ws()
			if p.jsonc && p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				return t
			}
		case '}':
			p.i++
			return t
		default:
			p.fail("expected ',' or '}'")
		}
	}
}

func (p *jsonParser) number() lua.LValue {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	switch {
	case p.i < len(p.s) && p.s[p.i] == '0':
		p.i++
	case digits() == 0:
		p.fail("invalid number")
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			p.fail("invalid number: digits expected after '.'")
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			p.fail("invalid number: digits expected in exponent")
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil && math.IsInf(f, 0) {
		p.i = start
		p.fail("number out of range")
	}
	return lua.LNumber(f)
}

func (p *jsonParser) str() string {
	p.i++ // opening quote
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			s := p.s[start:p.i]
			p.i++
			return strings.Clone(s) // a substring would pin the whole decoded blob
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			p.fail("control character in string")
		}
		p.i++
	}
	var sb strings.Builder
	sb.WriteString(p.s[start:p.i])
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return sb.String()
		case c < 0x20:
			p.fail("control character in string")
		case c != '\\':
			sb.WriteByte(c)
			p.i++
		default:
			p.i++
			if p.i >= len(p.s) {
				p.fail("unterminated string")
			}
			esc := p.s[p.i]
			p.i++
			switch esc {
			case '"', '\\', '/':
				sb.WriteByte(esc)
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				r := p.hex4()
				if r >= 0xd800 && r < 0xdc00 && strings.HasPrefix(p.s[p.i:], `\u`) {
					save := p.i
					p.i += 2
					if lo := p.hex4(); lo >= 0xdc00 && lo < 0xe000 {
						r = 0x10000 + (r-0xd800)<<10 + (lo - 0xdc00)
					} else {
						p.i = save
					}
				}
				sb.WriteRune(r) // a lone surrogate becomes U+FFFD
			default:
				p.i--
				p.fail("invalid escape '\\%c'", esc)
			}
		}
	}
	p.fail("unterminated string")
	return ""
}

func (p *jsonParser) hex4() rune {
	if p.i+4 > len(p.s) {
		p.fail("invalid \\u escape")
	}
	var r rune
	for k := 0; k < 4; k++ {
		d := digitVal(p.s[p.i+k])
		if d < 0 || d >= 16 {
			p.fail("invalid \\u escape")
		}
		r = r<<4 | rune(d)
	}
	p.i += 4
	return r
}
